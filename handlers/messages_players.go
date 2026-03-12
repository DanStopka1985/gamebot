package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"gamebot/database"
	"gamebot/models"
)

// ==================== ФУНКЦИИ ДЛЯ УПРАВЛЕНИЯ PLAYERS ====================

// truncateUTF8 безопасно обрезает строку с учетом UTF-8 символов
func truncateUTF8(s string, maxLen int) string {
	// Преобразуем в срез рун для правильной работы с UTF-8
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

// showPlayersMenu показывает меню управления игроками
func (h *MessageHandler) showPlayersMenu(chatID int64) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("👥 Запрос меню управления игроками от администратора %d", chatID)

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📋 Список игроков"),
			tgbotapi.NewKeyboardButton("➕ Добавить игрока"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🔍 Поиск игрока"),
			tgbotapi.NewKeyboardButton("🚫 Заблокированные"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🔙 Назад в админку"),
		),
	)
	keyboard.ResizeKeyboard = true

	msg := tgbotapi.NewMessage(chatID, "👥 *Управление игроками*\n\nВыберите действие:")
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// showPlayersList показывает список игроков с пагинацией
func (h *MessageHandler) showPlayersList(chatID int64, page int, filter string) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("📋 Запрос списка игроков (страница %d, фильтр: %s) от администратора %d", page, filter, chatID)

	limit := 10
	offset := (page - 1) * limit

	var rows *sql.Rows
	var err error
	var totalCount int

	if filter == "banned" {
		// Показываем только заблокированных
		err = database.DB.QueryRow(`SELECT COUNT(*) FROM players WHERE is_active = false`).Scan(&totalCount)
		if err != nil {
			log.Printf("❌ Ошибка подсчета игроков: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
			return
		}

		rows, err = database.DB.Query(`
			SELECT id, full_name, telegram_nick, telegram_name, notes, is_active, created_at
			FROM players
			WHERE is_active = false
			ORDER BY full_name
			LIMIT $1 OFFSET $2
		`, limit, offset)
	} else if filter != "" && filter != "active" {
		// Поиск по имени или нику
		searchPattern := "%" + filter + "%"
		err = database.DB.QueryRow(`
			SELECT COUNT(*) FROM players 
			WHERE full_name ILIKE $1 OR telegram_nick ILIKE $1 OR telegram_name ILIKE $1
		`, searchPattern).Scan(&totalCount)
		if err != nil {
			log.Printf("❌ Ошибка подсчета игроков: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
			return
		}

		rows, err = database.DB.Query(`
			SELECT id, full_name, telegram_nick, telegram_name, notes, is_active, created_at
			FROM players
			WHERE full_name ILIKE $1 OR telegram_nick ILIKE $1 OR telegram_name ILIKE $1
			ORDER BY full_name
			LIMIT $2 OFFSET $3
		`, searchPattern, limit, offset)
	} else {
		// Все активные игроки
		err = database.DB.QueryRow(`SELECT COUNT(*) FROM players WHERE is_active = true`).Scan(&totalCount)
		if err != nil {
			log.Printf("❌ Ошибка подсчета игроков: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
			return
		}

		rows, err = database.DB.Query(`
			SELECT id, full_name, telegram_nick, telegram_name, notes, is_active, created_at
			FROM players
			WHERE is_active = true
			ORDER BY full_name
			LIMIT $1 OFFSET $2
		`, limit, offset)
	}

	if err != nil {
		log.Printf("❌ Ошибка загрузки игроков: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
		return
	}
	defer rows.Close()

	totalPages := (totalCount + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}

	// Формируем заголовок
	filterText := ""
	if filter == "banned" {
		filterText = " (заблокированные)"
	} else if filter != "" && filter != "active" {
		filterText = fmt.Sprintf(" (поиск: %s)", filter)
	}

	text := fmt.Sprintf("📋 *Список игроков%s* (страница %d/%d, всего: %d)\n\n",
		filterText, page, totalPages, totalCount)

	for rows.Next() {
		var id int
		var fullName, notes string
		var telegramNick, telegramName sql.NullString
		var isActive bool
		var createdAt time.Time

		err := rows.Scan(&id, &fullName, &telegramNick, &telegramName, &notes, &isActive, &createdAt)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		status := "✅"
		if !isActive {
			status = "🚫"
		}

		text += fmt.Sprintf("%s *%d.* %s\n", status, id, fullName)
		if telegramNick.Valid && telegramNick.String != "" {
			text += fmt.Sprintf("   📱 %s\n", telegramNick.String)
		}
		if telegramName.Valid && telegramName.String != "" {
			text += fmt.Sprintf("   👤 %s\n", telegramName.String)
		}
		if notes != "" {
			text += fmt.Sprintf("   📝 %s\n", notes)
		}
		text += "\n"
	}

	// Создаем клавиатуру для навигации
	var buttons [][]tgbotapi.InlineKeyboardButton

	// Кнопки пагинации
	var navRow []tgbotapi.InlineKeyboardButton
	if page > 1 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("◀️ Пред",
			fmt.Sprintf("players_page:%d:%s", page-1, filter)))
	}
	if page < totalPages {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("След ▶️",
			fmt.Sprintf("players_page:%d:%s", page+1, filter)))
	}
	if len(navRow) > 0 {
		buttons = append(buttons, navRow)
	}

	// Кнопки для каждого игрока
	if limit <= 10 {
		// Возвращаемся к началу, чтобы добавить кнопки для каждого игрока
		rows, err = database.DB.Query(`
			SELECT id, full_name, telegram_nick
			FROM players
			WHERE 
				CASE 
					WHEN $1 = 'banned' THEN is_active = false
					WHEN $1 != '' AND $1 != 'active' THEN 
						full_name ILIKE '%' || $1 || '%' 
						OR telegram_nick ILIKE '%' || $1 || '%' 
						OR telegram_name ILIKE '%' || $1 || '%'
					ELSE is_active = true
				END
			ORDER BY full_name
			LIMIT $2 OFFSET $3
		`, filter, limit, offset)

		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id int
				var fullName string
				var telegramNick sql.NullString
				rows.Scan(&id, &fullName, &telegramNick)

				// Обрезаем имя с учетом UTF-8
				displayName := fullName
				// Максимальная длина для кнопки (примерно 22 символа)
				if len([]rune(displayName)) > 22 {
					displayName = truncateUTF8(displayName, 20)
				}

				buttonText := fmt.Sprintf("👤 %s", displayName)

				buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(buttonText,
						fmt.Sprintf("admin:view_player:%d", id)),
				))
			}
		}
	}

	// Добавляем кнопки для массовых действий
	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("➕ Добавить игрока", "admin:add_player"),
		tgbotapi.NewInlineKeyboardButtonData("🔍 Поиск", "admin:search_player"),
	))
	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔙 Назад в меню", "admin:back_to_players_menu"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// startAddPlayer начинает процесс добавления игрока
func (h *MessageHandler) startAddPlayer(message *tgbotapi.Message) {
	if !h.isAdmin(message.From.ID) {
		h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "⛔ Нет прав"))
		return
	}

	log.Printf("➕ Начало создания игрока администратором %d", message.From.ID)

	h.UserStates[message.From.ID] = &models.UserState{
		Action:   "add_player",
		Step:     "awaiting_fullname",
		TempData: make(map[string]interface{}),
	}

	h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID,
		"Введите ФИО игрока:"))
}

// handleAddPlayerInput обрабатывает ввод при создании игрока
func (h *MessageHandler) handleAddPlayerInput(message *tgbotapi.Message, state *models.UserState) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := strings.TrimSpace(message.Text)

	if text == "" {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Поле не может быть пустым. Попробуйте снова:"))
		return
	}

	switch state.Step {
	case "awaiting_fullname":
		state.TempData["full_name"] = text
		state.Step = "awaiting_telegram_nick"
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			"Введите Telegram никнейм (можно пропустить, отправив \"-\"):"))

	case "awaiting_telegram_nick":
		if text != "-" {
			if !strings.HasPrefix(text, "@") {
				text = "@" + text
			}
			state.TempData["telegram_nick"] = text
		}
		state.Step = "awaiting_telegram_name"
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			"Введите имя в Telegram (можно пропустить, отправив \"-\"):"))

	case "awaiting_telegram_name":
		if text != "-" {
			state.TempData["telegram_name"] = text
		}
		state.Step = "awaiting_notes"
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			"Введите заметки (можно пропустить, отправив \"-\"):"))

	case "awaiting_notes":
		if text != "-" {
			state.TempData["notes"] = text
		}

		// Сохраняем игрока в БД
		fullName := state.TempData["full_name"].(string)
		telegramNick := ""
		if tn, ok := state.TempData["telegram_nick"].(string); ok {
			telegramNick = tn
		}
		telegramName := ""
		if tn, ok := state.TempData["telegram_name"].(string); ok {
			telegramName = tn
		}
		notes := ""
		if n, ok := state.TempData["notes"].(string); ok {
			notes = n
		}

		var playerID int
		err := database.DB.QueryRow(`
			INSERT INTO players (full_name, telegram_nick, telegram_name, notes, is_active)
			VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), true)
			RETURNING id
		`, fullName, telegramNick, telegramName, notes).Scan(&playerID)

		if err != nil {
			log.Printf("❌ Ошибка создания игрока: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при создании игрока"))
			delete(h.UserStates, userID)
			return
		}

		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("✅ Игрок '%s' успешно создан! ID: %d", fullName, playerID)))

		// Показываем детали созданного игрока
		h.showPlayerDetails(chatID, playerID)
		delete(h.UserStates, userID)
	}
}

// showPlayerDetails показывает детальную информацию об игроке
func (h *MessageHandler) showPlayerDetails(chatID int64, playerID int) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("👤 Запрос деталей игрока %d от администратора %d", playerID, chatID)

	var player struct {
		FullName     string
		TelegramNick sql.NullString
		TelegramName sql.NullString
		Notes        sql.NullString
		IsActive     bool
		CreatedAt    time.Time
	}

	err := database.DB.QueryRow(`
		SELECT full_name, telegram_nick, telegram_name, notes, is_active, created_at
		FROM players WHERE id = $1
	`, playerID).Scan(&player.FullName, &player.TelegramNick, &player.TelegramName, &player.Notes, &player.IsActive, &player.CreatedAt)

	if err != nil {
		log.Printf("❌ Ошибка загрузки игрока: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Игрок не найден"))
		return
	}

	// Получаем статистику игрока
	var totalRegistrations, totalEvents int
	database.DB.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(participants_count), 0)
		FROM person_event WHERE player_id = $1 AND status = 'registered'
	`, playerID).Scan(&totalRegistrations, &totalEvents)

	status := "✅ Активен"
	if !player.IsActive {
		status = "🚫 Заблокирован"
	}

	text := fmt.Sprintf("👤 *Информация об игроке*\n\n")
	text += fmt.Sprintf("🆔 *ID:* %d\n", playerID)
	text += fmt.Sprintf("📝 *ФИО:* %s\n", player.FullName)
	if player.TelegramNick.Valid && player.TelegramNick.String != "" {
		text += fmt.Sprintf("📱 *Telegram:* %s\n", player.TelegramNick.String)
	}
	if player.TelegramName.Valid && player.TelegramName.String != "" {
		text += fmt.Sprintf("👤 *Имя в TG:* %s\n", player.TelegramName.String)
	}
	if player.Notes.Valid && player.Notes.String != "" {
		text += fmt.Sprintf("📝 *Заметки:* %s\n", player.Notes.String)
	}
	text += fmt.Sprintf("📊 *Статус:* %s\n", status)
	text += fmt.Sprintf("📅 *Зарегистрирован:* %s\n", player.CreatedAt.Format("02.01.2006"))
	text += fmt.Sprintf("📊 *Всего записей:* %d\n", totalRegistrations)
	text += fmt.Sprintf("👥 *Всего участников:* %d\n", totalEvents)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✏️ Редактировать", fmt.Sprintf("admin:edit_player:%d", playerID)),
			tgbotapi.NewInlineKeyboardButtonData("📋 История записей", fmt.Sprintf("admin:player_history:%d", playerID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "admin:back_to_players_list"),
		),
	)

	if player.IsActive {
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🚫 Заблокировать", fmt.Sprintf("admin:ban_player:%d", playerID)),
		))
	} else {
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Разблокировать", fmt.Sprintf("admin:unban_player:%d", playerID)),
		))
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// startEditPlayer начинает процесс редактирования игрока
func (h *MessageHandler) startEditPlayer(chatID int64, userID int64, playerID int) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("✏️ Начало редактирования игрока %d администратором %d", playerID, userID)

	// Получаем текущие данные игрока
	var player struct {
		FullName     string
		TelegramNick sql.NullString
		TelegramName sql.NullString
		Notes        sql.NullString
	}

	err := database.DB.QueryRow(`
		SELECT full_name, telegram_nick, telegram_name, notes
		FROM players WHERE id = $1
	`, playerID).Scan(&player.FullName, &player.TelegramNick, &player.TelegramName, &player.Notes)

	if err != nil {
		log.Printf("❌ Ошибка загрузки игрока: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Игрок не найден"))
		return
	}

	// Сохраняем состояние
	h.UserStates[userID] = &models.UserState{
		Action: "edit_player",
		Step:   "awaiting_fullname",
		TempData: map[string]interface{}{
			"player_id":     playerID,
			"full_name":     player.FullName,
			"telegram_nick": player.TelegramNick.String,
			"telegram_name": player.TelegramName.String,
			"notes":         player.Notes.String,
		},
	}

	msg := fmt.Sprintf(
		"✏️ *Редактирование игрока ID:%d*\n\n"+
			"Текущее ФИО: %s\n\n"+
			"Введите новое ФИО (или отправьте \".\" чтобы оставить текущее):",
		playerID, player.FullName)

	h.Bot.Send(tgbotapi.NewMessage(chatID, msg))
}

// handleEditPlayerInput обрабатывает ввод при редактировании игрока
func (h *MessageHandler) handleEditPlayerInput(message *tgbotapi.Message, state *models.UserState) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := strings.TrimSpace(message.Text)

	playerID := state.TempData["player_id"].(int)

	switch state.Step {
	case "awaiting_fullname":
		if text != "." {
			state.TempData["full_name"] = text
		}
		state.Step = "awaiting_telegram_nick"
		current := state.TempData["telegram_nick"].(string)
		if current == "" {
			current = "не указан"
		}
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("Текущий Telegram ник: %s\nВведите новый Telegram ник (или \".\" чтобы оставить):", current)))

	case "awaiting_telegram_nick":
		if text != "." {
			if text == "-" {
				state.TempData["telegram_nick"] = ""
			} else {
				if !strings.HasPrefix(text, "@") {
					text = "@" + text
				}
				state.TempData["telegram_nick"] = text
			}
		}
		state.Step = "awaiting_telegram_name"
		current := state.TempData["telegram_name"].(string)
		if current == "" {
			current = "не указан"
		}
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("Текущее имя в Telegram: %s\nВведите новое имя (или \".\" чтобы оставить):", current)))

	case "awaiting_telegram_name":
		if text != "." {
			if text == "-" {
				state.TempData["telegram_name"] = ""
			} else {
				state.TempData["telegram_name"] = text
			}
		}
		state.Step = "awaiting_notes"
		current := state.TempData["notes"].(string)
		if current == "" {
			current = "не указан"
		}
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("Текущие заметки: %s\nВведите новые заметки (или \".\" чтобы оставить):", current)))

	case "awaiting_notes":
		if text != "." {
			if text == "-" {
				state.TempData["notes"] = ""
			} else {
				state.TempData["notes"] = text
			}
		}

		// Обновляем данные в БД
		_, err := database.DB.Exec(`
			UPDATE players 
			SET full_name = $1, 
			    telegram_nick = NULLIF($2, ''), 
			    telegram_name = NULLIF($3, ''), 
			    notes = NULLIF($4, ''),
			    updated_at = NOW()
			WHERE id = $5
		`,
			state.TempData["full_name"],
			state.TempData["telegram_nick"],
			state.TempData["telegram_name"],
			state.TempData["notes"],
			playerID)

		if err != nil {
			log.Printf("❌ Ошибка обновления игрока: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при обновлении"))
			delete(h.UserStates, userID)
			return
		}

		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("✅ Данные игрока ID:%d обновлены", playerID)))

		// Показываем обновленные детали
		h.showPlayerDetails(chatID, playerID)
		delete(h.UserStates, userID)
	}
}

// banPlayer блокирует игрока
func (h *MessageHandler) banPlayer(chatID int64, playerID int) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("🚫 Блокировка игрока %d администратором %d", playerID, chatID)

	result, err := database.DB.Exec(`UPDATE players SET is_active = false WHERE id = $1`, playerID)
	if err != nil {
		log.Printf("❌ Ошибка блокировки игрока: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при блокировке"))
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Игрок не найден"))
		return
	}

	h.Bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Игрок ID:%d заблокирован", playerID)))
	h.showPlayerDetails(chatID, playerID)
}

// unbanPlayer разблокирует игрока
func (h *MessageHandler) unbanPlayer(chatID int64, playerID int) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("✅ Разблокировка игрока %d администратором %d", playerID, chatID)

	result, err := database.DB.Exec(`UPDATE players SET is_active = true WHERE id = $1`, playerID)
	if err != nil {
		log.Printf("❌ Ошибка разблокировки игрока: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при разблокировке"))
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Игрок не найден"))
		return
	}

	h.Bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Игрок ID:%d разблокирован", playerID)))
	h.showPlayerDetails(chatID, playerID)
}

// searchPlayer начинает поиск игрока
func (h *MessageHandler) searchPlayer(message *tgbotapi.Message) {
	if !h.isAdmin(message.From.ID) {
		h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "⛔ Нет прав"))
		return
	}

	log.Printf("🔍 Начало поиска игрока администратором %d", message.From.ID)

	h.UserStates[message.From.ID] = &models.UserState{
		Action:   "search_player",
		Step:     "awaiting_query",
		TempData: make(map[string]interface{}),
	}

	h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID,
		"Введите имя, ник или любую информацию для поиска:"))
}

// handleSearchPlayerInput обрабатывает поисковый запрос
func (h *MessageHandler) handleSearchPlayerInput(message *tgbotapi.Message, state *models.UserState) {
	userID := message.From.ID
	chatID := message.Chat.ID
	query := strings.TrimSpace(message.Text)

	if query == "" {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Введите текст для поиска"))
		return
	}

	log.Printf("🔍 Поиск игроков по запросу: %s", query)

	searchPattern := "%" + query + "%"
	rows, err := database.DB.Query(`
		SELECT id, full_name, telegram_nick, telegram_name, is_active
		FROM players
		WHERE full_name ILIKE $1 
		   OR telegram_nick ILIKE $1 
		   OR telegram_name ILIKE $1
		   OR notes ILIKE $1
		ORDER BY full_name
		LIMIT 20
	`, searchPattern)

	if err != nil {
		log.Printf("❌ Ошибка поиска: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при поиске"))
		delete(h.UserStates, userID)
		return
	}
	defer rows.Close()

	var results []struct {
		ID       int
		FullName string
		Nick     sql.NullString
		TGName   sql.NullString
		IsActive bool
	}

	for rows.Next() {
		var r struct {
			ID       int
			FullName string
			Nick     sql.NullString
			TGName   sql.NullString
			IsActive bool
		}
		err := rows.Scan(&r.ID, &r.FullName, &r.Nick, &r.TGName, &r.IsActive)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}
		results = append(results, r)
	}

	if len(results) == 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ничего не найдено"))
		delete(h.UserStates, userID)
		return
	}

	text := fmt.Sprintf("🔍 *Результаты поиска* (найдено: %d)\n\n", len(results))

	var buttons [][]tgbotapi.InlineKeyboardButton
	for _, r := range results {
		status := "✅"
		if !r.IsActive {
			status = "🚫"
		}
		text += fmt.Sprintf("%s *%d.* %s\n", status, r.ID, r.FullName)
		if r.Nick.Valid && r.Nick.String != "" {
			text += fmt.Sprintf("   📱 %s\n", r.Nick.String)
		}
		text += "\n"

		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("👤 %s", r.FullName),
				fmt.Sprintf("admin:view_player:%d", r.ID),
			),
		))
	}

	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "admin:back_to_players_menu"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)

	delete(h.UserStates, userID)
}

// showPlayerHistory показывает историю записей игрока
func (h *MessageHandler) showPlayerHistory(chatID int64, playerID int) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("📋 Запрос истории игрока %d от администратора %d", playerID, chatID)

	rows, err := database.DB.Query(`
		SELECT e.id, c.name, e.evn_datetime, pe.participants_count, pe.registered_at, pe.status
		FROM person_event pe
		JOIN event e ON pe.event_id = e.id
		JOIN category c ON e.category_id = c.id
		WHERE pe.player_id = $1
		ORDER BY pe.registered_at DESC
		LIMIT 20
	`, playerID)

	if err != nil {
		log.Printf("❌ Ошибка загрузки истории: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
		return
	}
	defer rows.Close()

	var playerName string
	database.DB.QueryRow(`SELECT full_name FROM players WHERE id = $1`, playerID).Scan(&playerName)

	text := fmt.Sprintf("📋 *История записей игрока %s*\n\n", playerName)

	count := 0
	for rows.Next() {
		count++
		var eventID int
		var categoryName string
		var eventDate, regDate time.Time
		var participants int
		var status string

		err := rows.Scan(&eventID, &categoryName, &eventDate, &participants, &regDate, &status)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		statusEmoji := "✅"
		if status != "registered" {
			statusEmoji = "❌"
		}

		text += fmt.Sprintf("%s *Событие #%d:* %s\n", statusEmoji, eventID, categoryName)
		text += fmt.Sprintf("   📆 %s\n", eventDate.Format("02.01.2006 15:04"))
		text += fmt.Sprintf("   👥 Участников: %d\n", participants)
		text += fmt.Sprintf("   📅 Запись: %s\n\n", regDate.Format("02.01.2006 15:04"))
	}

	if count == 0 {
		text += "Нет записей"
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", fmt.Sprintf("admin:view_player:%d", playerID)),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}
