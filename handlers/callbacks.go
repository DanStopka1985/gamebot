package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/lib/pq"

	"gamebot/database"
	"gamebot/models"
	"gamebot/utils"
)

// CallbackHandler обработчик callback запросов
type CallbackHandler struct {
	Bot        *tgbotapi.BotAPI
	AdminIDs   *map[int64]bool
	UserStates map[int64]*models.UserState
}

// NewCallbackHandler создает новый обработчик callback'ов
func NewCallbackHandler(bot *tgbotapi.BotAPI, adminIDs *map[int64]bool, userStates map[int64]*models.UserState) *CallbackHandler {
	return &CallbackHandler{
		Bot:        bot,
		AdminIDs:   adminIDs,
		UserStates: userStates,
	}
}

// isAdmin проверяет, является ли пользователь администратором
func (h *CallbackHandler) isAdmin(userID int64) bool {
	if h.AdminIDs == nil {
		return false
	}
	_, exists := (*h.AdminIDs)[userID]
	return exists
}

// HandleCallback обрабатывает callback запросы
func (h *CallbackHandler) HandleCallback(callback *tgbotapi.CallbackQuery) {
	if h.Bot == nil {
		log.Println("❌ Ошибка: бот не инициализирован")
		return
	}

	log.Printf("🔍 Получен callback: %s от пользователя %d", callback.Data, callback.From.ID)

	callbackConfig := tgbotapi.NewCallback(callback.ID, "")
	if _, err := h.Bot.Request(callbackConfig); err != nil {
		log.Printf("❌ Ошибка ответа на callback: %v", err)
	}

	data := strings.Split(callback.Data, ":")
	if len(data) < 1 {
		log.Println("❌ Пустой callback data")
		return
	}

	userID := callback.From.ID
	chatID := callback.Message.Chat.ID

	// Регистрация пользователя
	utils.RegisterPersonIfNotExists(database.DB, callback.From)

	log.Printf("📨 Обработка callback: command=%s, data=%v", data[0], data)

	// Проверяем, не является ли это callback'ом идентификации
	if strings.HasPrefix(data[0], "identify_") {
		h.handleIdentificationCallbacks(callback, data)
		return
	}

	switch data[0] {
	case "register":
		if len(data) < 2 {
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка: не указан ID события"))
			return
		}
		eventID, err := strconv.Atoi(data[1])
		if err != nil {
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка: неверный ID события"))
			return
		}
		log.Printf("📝 Запрос регистрации на событие %d от пользователя %d", eventID, userID)
		h.askParticipantsCount(chatID, eventID, userID)

	case "confirm_reg":
		if len(data) < 2 {
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка формата данных"))
			return
		}
		parts := strings.Split(data[1], "_")
		if len(parts) < 2 {
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка формата данных"))
			return
		}
		eventID, _ := strconv.Atoi(parts[0])
		count, _ := strconv.Atoi(parts[1])
		log.Printf("✅ Подтверждение регистрации: событие %d, количество %d", eventID, count)

		if count == 1 {
			// Для одного человека запрашиваем идентификацию
			h.askParticipantNames(chatID, eventID, userID, count)
		} else {
			// Для нескольких - запрашиваем имена
			h.askParticipantNames(chatID, eventID, userID, count)
		}

	case "confirm_reg_with_identification":
		if len(data) < 2 {
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка формата данных"))
			return
		}
		eventID, _ := strconv.Atoi(data[1])

		state, exists := h.UserStates[userID]
		if !exists || state.TempData["identified_players"] == nil {
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка: данные не найдены. Начните заново."))
			return
		}

		identified := state.TempData["identified_players"].([]map[string]interface{})
		count := len(identified)
		h.registerForEventWithIdentification(chatID, eventID, userID, count, identified)
		delete(h.UserStates, userID)

	case "custom_count":
		if len(data) < 2 {
			return
		}
		eventID, _ := strconv.Atoi(data[1])
		h.askCustomCount(chatID, eventID, userID)

	case "cancel_reg":
		if len(data) < 2 {
			return
		}
		eventID, _ := strconv.Atoi(data[1])
		h.cancelRegistration(chatID, eventID, userID)

	case "cancel_registration":
		delete(h.UserStates, userID)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Регистрация отменена"))

	case "event":
		if len(data) < 2 {
			return
		}
		eventID, _ := strconv.Atoi(data[1])
		h.showEventDetails(chatID, eventID, userID)

	case "admin":
		if len(data) < 2 {
			return
		}
		if h.isAdmin(userID) {
			h.handleAdminCallback(callback, data)
		} else {
			h.Bot.Send(tgbotapi.NewMessage(chatID, "⛔ У вас нет прав администратора"))
		}

	default:
		log.Printf("❌ Неизвестная команда: %s", data[0])
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Неизвестная команда"))
	}
}

// askParticipantsCount запрашивает количество участников
func (h *CallbackHandler) askParticipantsCount(chatID int64, eventID int, userID int64) {
	log.Printf("📊 Запрос количества для события %d", eventID)

	var eventName string
	err := database.DB.QueryRow(`
		SELECT c.name 
		FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&eventName)

	if err != nil {
		eventName = "Событие"
	}

	var dbPersonID int
	err = database.DB.QueryRow(`SELECT id FROM person WHERE telegram_id = $1`, userID).Scan(&dbPersonID)
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Пользователь не найден. Напишите /start"))
		return
	}

	var existing int
	err = database.DB.QueryRow(`
		SELECT COUNT(*) FROM person_event 
		WHERE person_id = $1 AND event_id = $2 AND status = 'registered'
	`, dbPersonID, eventID).Scan(&existing)

	if err == nil && existing > 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Вы уже записаны на '%s'", eventName)))
		return
	}

	var registered, limit int
	err = database.DB.QueryRow(`
		SELECT COALESCE(SUM(participants_count), 0), e.member_limit 
		FROM event e 
		LEFT JOIN person_event pe ON e.id = pe.event_id AND pe.status = 'registered'
		WHERE e.id = $1
		GROUP BY e.member_limit
	`, eventID).Scan(&registered, &limit)

	if err != nil {
		log.Printf("❌ Ошибка загрузки события: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки события"))
		return
	}

	available := limit - registered
	log.Printf("📊 Событие %d: занято %d, лимит %d, свободно %d", eventID, registered, limit, available)

	if available <= 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ На '%s' свободных мест нет", eventName)))
		return
	}

	var rows [][]tgbotapi.InlineKeyboardButton

	for i := 1; i <= 5 && i <= available; i++ {
		buttonText := fmt.Sprintf("%d %s", i, utils.Pluralize(i, "человек", "человека", "человек"))
		callbackData := fmt.Sprintf("confirm_reg:%d_%d", eventID, i)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(buttonText, callbackData),
		))
	}

	if available > 5 {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✏️ Другое количество", fmt.Sprintf("custom_count:%d", eventID)),
		))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	msgText := fmt.Sprintf("📅 *%s*\nСвободно мест: %d\n\nВыберите количество участников:", eventName, available)
	msg := tgbotapi.NewMessage(chatID, msgText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	if _, err := h.Bot.Send(msg); err != nil {
		log.Printf("❌ Ошибка отправки сообщения: %v", err)
	}
}

// askCustomCount запрашивает произвольное количество
func (h *CallbackHandler) askCustomCount(chatID int64, eventID int, userID int64) {
	h.UserStates[userID] = &models.UserState{
		Action: "entering_custom_count",
		Step:   "awaiting_count",
		TempData: map[string]interface{}{
			"event_id": eventID,
		},
	}

	h.Bot.Send(tgbotapi.NewMessage(chatID,
		"Введите нужное количество участников (число):"))
}

// handleCustomCount обрабатывает ввод произвольного количества
func (h *CallbackHandler) handleCustomCount(message *tgbotapi.Message) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := message.Text

	state, exists := h.UserStates[userID]
	if !exists || state.Action != "entering_custom_count" {
		return
	}

	count, err := strconv.Atoi(text)
	if err != nil || count < 1 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Введите положительное число:"))
		return
	}

	eventID := state.TempData["event_id"].(int)

	var registered, limit int
	err = database.DB.QueryRow(`
		SELECT COALESCE(SUM(participants_count), 0), e.member_limit 
		FROM event e 
		LEFT JOIN person_event pe ON e.id = pe.event_id AND pe.status = 'registered'
		WHERE e.id = $1
		GROUP BY e.member_limit
	`, eventID).Scan(&registered, &limit)

	if err != nil {
		log.Printf("❌ Ошибка загрузки события: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки события"))
		delete(h.UserStates, userID)
		return
	}

	available := limit - registered
	if count > available {
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("❌ Свободно только %d мест. Введите другое число:", available)))
		return
	}

	h.askParticipantNames(chatID, eventID, userID, count)
}

// askParticipantNames запрашивает имена участников с возможностью идентификации
func (h *CallbackHandler) askParticipantNames(chatID int64, eventID int, userID int64, count int) {
	h.UserStates[userID] = &models.UserState{
		Action: "entering_names",
		Step:   "awaiting_names",
		TempData: map[string]interface{}{
			"event_id":           eventID,
			"count":              count,
			"inputs":             []string{},
			"identified_players": []map[string]interface{}{},
		},
	}

	msgText := fmt.Sprintf(
		"👥 Вы записываете %d человек.\n\n"+
			"Введите имена или никнеймы всех участников, *каждого с новой строки*.\n\n"+
			"📱 *Как лучше вводить:*\n"+
			"• Telegram никнейм (например, @john_doe)\n"+
			"• Имя и фамилию (например, Иван Петров)\n"+
			"• Любую информацию, по которой можно идентифицировать игрока\n\n"+
			"*Пример:*\n"+
			"@john_doe\n"+
			"Иван Петров\n"+
			"Мария\n\n"+
			"Я попробую найти игроков в базе и предложу варианты.",
		count)

	msg := tgbotapi.NewMessage(chatID, msgText)
	msg.ParseMode = "Markdown"
	h.Bot.Send(msg)
}

// handleParticipantNamesWithSearch обрабатывает ввод с поиском игроков
func (h *CallbackHandler) handleParticipantNamesWithSearch(message *tgbotapi.Message) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := message.Text

	state, exists := h.UserStates[userID]
	if !exists || state.Action != "entering_names" {
		return
	}

	// Разбиваем текст на строки
	lines := strings.Split(text, "\n")
	var inputs []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			inputs = append(inputs, line)
		}
	}

	expectedCount := state.TempData["count"].(int)

	if len(inputs) != expectedCount {
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("❌ Нужно ввести ровно %d имен. Сейчас введено %d.\nПопробуйте снова:",
				expectedCount, len(inputs))))
		return
	}

	// Сохраняем введенные строки
	state.TempData["inputs"] = inputs
	state.TempData["identified_players"] = []map[string]interface{}{}

	// Начинаем процесс идентификации первого игрока
	h.identifyNextPlayer(chatID, userID, 0)
}

// identifyNextPlayer идентифицирует следующего игрока
func (h *CallbackHandler) identifyNextPlayer(chatID int64, userID int64, index int) {
	state, exists := h.UserStates[userID]
	if !exists {
		return
	}

	inputs := state.TempData["inputs"].([]string)
	if index >= len(inputs) {
		// Все игроки обработаны, показываем результат
		h.showIdentificationResult(chatID, userID)
		return
	}

	input := inputs[index]
	state.TempData["current_index"] = index

	// Ищем игрока
	results, err := utils.FindPlayer(database.DB, input)
	if err != nil {
		log.Printf("❌ Ошибка поиска игрока: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при поиске. Попробуйте еще раз."))
		return
	}

	if len(results) == 0 {
		// Игрок не найден - предлагаем ввести вручную
		msg := fmt.Sprintf(
			"🔍 Игрок #%d: %s\n\n"+
				"❌ Не удалось найти в базе.\n\n"+
				"Введите информацию об этом игроке вручную (имя, ник, любые данные):",
			index+1, input)

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("⏭ Пропустить (без идентификации)",
					fmt.Sprintf("identify_skip:%d", index)),
			),
		)

		msgObj := tgbotapi.NewMessage(chatID, msg)
		// Убираем Markdown разметку для этого сообщения
		msgObj.ParseMode = ""
		msgObj.ReplyMarkup = keyboard
		h.Bot.Send(msgObj)

		state.Step = "manual_input"
		return
	}

	if len(results) == 1 {
		// Найден один игрок - показываем для подтверждения
		r := results[0]

		// Экранируем специальные символы для Markdown
		fullName := escapeMarkdown(r.FullName)
		inputEscaped := escapeMarkdown(input)

		msg := fmt.Sprintf(
			"🔍 Игрок #%d: *%s*\n\n"+
				"✅ Найден игрок:\n"+
				"📝 ФИО: %s\n",
			index+1, inputEscaped, fullName)

		if r.TelegramNick != "" {
			telegramNick := escapeMarkdown(r.TelegramNick)
			msg += fmt.Sprintf("📱 Telegram: %s\n", telegramNick)
		}
		if r.TelegramName != "" {
			telegramName := escapeMarkdown(r.TelegramName)
			msg += fmt.Sprintf("👤 Имя в TG: %s\n", telegramName)
		}
		msg += fmt.Sprintf("\nЭто тот игрок?")

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Да", fmt.Sprintf("identify_confirm:%d:%d", index, r.ID)),
				tgbotapi.NewInlineKeyboardButtonData("❌ Нет, искать еще", fmt.Sprintf("identify_search:%d", index)),
			),
		)

		msgObj := tgbotapi.NewMessage(chatID, msg)
		msgObj.ParseMode = "Markdown"
		msgObj.ReplyMarkup = keyboard
		h.Bot.Send(msgObj)

		state.Step = "confirming"
		return
	}

	// Найдено несколько вариантов
	msg := fmt.Sprintf("🔍 Игрок #%d: *%s*\n\nНайдено несколько вариантов. Выберите нужного:\n\n",
		index+1, escapeMarkdown(input))

	var buttons [][]tgbotapi.InlineKeyboardButton
	for i, r := range results {
		buttonText := fmt.Sprintf("%d. %s", i+1, r.FullName)
		if r.TelegramNick != "" {
			buttonText += fmt.Sprintf(" (%s)", r.TelegramNick)
		}
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(buttonText,
				fmt.Sprintf("identify_select:%d:%d", index, r.ID)),
		))
	}
	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("➕ Ввести вручную", fmt.Sprintf("identify_manual:%d", index)),
	))
	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⏭ Пропустить", fmt.Sprintf("identify_skip:%d", index)),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)
	msgObj := tgbotapi.NewMessage(chatID, msg)
	msgObj.ParseMode = "Markdown"
	msgObj.ReplyMarkup = keyboard
	h.Bot.Send(msgObj)

	state.Step = "selecting"
}

// escapeMarkdown экранирует специальные символы для Markdown
func escapeMarkdown(text string) string {
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return replacer.Replace(text)
}

// handleIdentificationCallbacks обрабатывает callback'и идентификации
func (h *CallbackHandler) handleIdentificationCallbacks(callback *tgbotapi.CallbackQuery, data []string) {
	userID := callback.From.ID
	chatID := callback.Message.Chat.ID

	state, exists := h.UserStates[userID]
	if !exists || state.Action != "entering_names" {
		return
	}

	switch data[0] {
	case "identify_confirm":
		if len(data) < 3 {
			return
		}
		index, _ := strconv.Atoi(data[1])
		playerID, _ := strconv.Atoi(data[2])

		// Получаем информацию об игроке
		var player struct {
			FullName     string
			TelegramNick sql.NullString
			TelegramName sql.NullString
		}
		err := database.DB.QueryRow(`
			SELECT full_name, telegram_nick, telegram_name
			FROM players WHERE id = $1
		`, playerID).Scan(&player.FullName, &player.TelegramNick, &player.TelegramName)

		if err != nil {
			log.Printf("❌ Ошибка получения игрока: %v", err)
			return
		}

		// Сохраняем идентифицированного игрока
		identified := state.TempData["identified_players"].([]map[string]interface{})
		identified = append(identified, map[string]interface{}{
			"player_id":     playerID,
			"input":         state.TempData["inputs"].([]string)[index],
			"method":        "identified",
			"full_name":     player.FullName,
			"telegram_nick": player.TelegramNick.String,
			"telegram_name": player.TelegramName.String,
		})
		state.TempData["identified_players"] = identified

		// Переходим к следующему
		h.identifyNextPlayer(chatID, userID, index+1)

	case "identify_select":
		if len(data) < 3 {
			return
		}
		index, _ := strconv.Atoi(data[1])
		playerID, _ := strconv.Atoi(data[2])

		var player struct {
			FullName     string
			TelegramNick sql.NullString
			TelegramName sql.NullString
		}
		database.DB.QueryRow(`
			SELECT full_name, telegram_nick, telegram_name
			FROM players WHERE id = $1
		`, playerID).Scan(&player.FullName, &player.TelegramNick, &player.TelegramName)

		identified := state.TempData["identified_players"].([]map[string]interface{})
		identified = append(identified, map[string]interface{}{
			"player_id":     playerID,
			"input":         state.TempData["inputs"].([]string)[index],
			"method":        "selected",
			"full_name":     player.FullName,
			"telegram_nick": player.TelegramNick.String,
			"telegram_name": player.TelegramName.String,
		})
		state.TempData["identified_players"] = identified

		h.identifyNextPlayer(chatID, userID, index+1)

	case "identify_manual":
		if len(data) < 2 {
			return
		}
		index, _ := strconv.Atoi(data[1])

		state.TempData["manual_index"] = index
		state.Step = "manual_input"

		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("Введите информацию об игроке #%d вручную:", index+1)))

	case "identify_skip":
		if len(data) < 2 {
			return
		}
		index, _ := strconv.Atoi(data[1])

		inputs := state.TempData["inputs"].([]string)
		identified := state.TempData["identified_players"].([]map[string]interface{})
		identified = append(identified, map[string]interface{}{
			"player_id": 0,
			"input":     inputs[index],
			"method":    "skipped",
			"full_name": inputs[index],
		})
		state.TempData["identified_players"] = identified

		h.identifyNextPlayer(chatID, userID, index+1)

	case "identify_search":
		if len(data) < 2 {
			return
		}
		index, _ := strconv.Atoi(data[1])

		// Возвращаемся к поиску для этого индекса
		h.identifyNextPlayer(chatID, userID, index)
	}
}

// handleManualInput обрабатывает ручной ввод информации об игроке
func (h *CallbackHandler) handleManualInput(message *tgbotapi.Message) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := message.Text

	state, exists := h.UserStates[userID]
	if !exists || state.Action != "entering_names" || state.Step != "manual_input" {
		return
	}

	index := state.TempData["manual_index"].(int)

	identified := state.TempData["identified_players"].([]map[string]interface{})
	identified = append(identified, map[string]interface{}{
		"player_id": 0,
		"input":     text,
		"method":    "manual",
		"full_name": text,
	})
	state.TempData["identified_players"] = identified

	h.identifyNextPlayer(chatID, userID, index+1)
}

// showIdentificationResult показывает результат идентификации
func (h *CallbackHandler) showIdentificationResult(chatID int64, userID int64) {
	state, exists := h.UserStates[userID]
	if !exists {
		return
	}

	identified := state.TempData["identified_players"].([]map[string]interface{})

	// Проверяем, есть ли inputs в TempData
	inputsRaw, ok := state.TempData["inputs"]
	var inputs []string
	if ok {
		inputs = inputsRaw.([]string)
	} else {
		// Если inputs нет, создаем пустой слайс
		inputs = make([]string, len(identified))
		for i, id := range identified {
			if input, ok := id["input"].(string); ok {
				inputs[i] = input
			} else {
				inputs[i] = fmt.Sprintf("Игрок %d", i+1)
			}
		}
	}

	msg := "📋 *Результат идентификации игроков:*\n\n"
	found := 0

	for i, id := range identified {
		// Проверяем, что индекс i не выходит за пределы inputs
		inputText := ""
		if i < len(inputs) {
			inputText = inputs[i]
		} else if input, ok := id["input"].(string); ok {
			inputText = input
		} else {
			inputText = fmt.Sprintf("Игрок %d", i+1)
		}

		msg += fmt.Sprintf("*%d.* Введено: %s\n", i+1, escapeMarkdown(inputText))

		if playerID, ok := id["player_id"].(int); ok && playerID > 0 {
			found++
			fullName := ""
			if fn, ok := id["full_name"].(string); ok {
				fullName = fn
			} else {
				fullName = "Неизвестно"
			}
			msg += fmt.Sprintf("   ✅ *Найден:* %s\n", escapeMarkdown(fullName))

			if nick, ok := id["telegram_nick"].(string); ok && nick != "" {
				msg += fmt.Sprintf("   📱 %s\n", escapeMarkdown(nick))
			}
		} else {
			fullName := ""
			if fn, ok := id["full_name"].(string); ok {
				fullName = fn
			} else {
				fullName = "Неизвестно"
			}
			msg += fmt.Sprintf("   ❌ *Не идентифицирован*\n")
			msg += fmt.Sprintf("   📝 Данные: %s\n", escapeMarkdown(fullName))
		}
		msg += "\n"
	}

	msg += fmt.Sprintf("\n📊 Итого: найдено %d из %d игроков", found, len(identified))

	// Проверяем, есть ли event_id в TempData
	eventID := 0
	if ei, ok := state.TempData["event_id"].(int); ok {
		eventID = ei
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить и записать",
				fmt.Sprintf("confirm_reg_with_identification:%d", eventID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "cancel_registration"),
		),
	)

	msgObj := tgbotapi.NewMessage(chatID, msg)
	msgObj.ParseMode = "Markdown"
	msgObj.ReplyMarkup = keyboard
	h.Bot.Send(msgObj)

	state.Step = "final_confirm"
}

// registerForEventWithIdentification регистрирует на событие с идентифицированными игроками
func (h *CallbackHandler) registerForEventWithIdentification(chatID int64, eventID int, userID int64, count int, identifiedPlayers []map[string]interface{}) {
	log.Printf("📝 Регистрация %d человек на событие %d от пользователя %d", count, eventID, userID)

	// Получаем название события и дату
	var eventName string
	var eventDateTime time.Time
	err := database.DB.QueryRow(`
		SELECT c.name, e.evn_datetime
		FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&eventName, &eventDateTime)

	if err != nil {
		eventName = fmt.Sprintf("событие #%d", eventID)
		eventDateTime = time.Now()
	}

	// Начинаем транзакцию
	tx, err := database.DB.Begin()
	if err != nil {
		log.Printf("❌ Ошибка начала транзакции: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка сервера"))
		return
	}
	defer tx.Rollback()

	// Проверяем наличие мест
	var registered int
	err = tx.QueryRow(`
		SELECT COALESCE(SUM(participants_count), 0) FROM person_event 
		WHERE event_id = $1 AND status = 'registered'
	`, eventID).Scan(&registered)

	if err != nil {
		log.Printf("❌ Ошибка проверки мест: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка проверки мест"))
		return
	}

	var memberLimit int
	err = tx.QueryRow(`SELECT member_limit FROM event WHERE id = $1`, eventID).Scan(&memberLimit)
	if err != nil {
		log.Printf("❌ Ошибка загрузки события: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки события"))
		return
	}

	log.Printf("📊 Текущее состояние: занято %d, лимит %d, запрос %d", registered, memberLimit, count)

	if registered+count > memberLimit {
		available := memberLimit - registered
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("❌ На '%s' недостаточно мест. Свободно: %d", eventName, available)))
		return
	}

	// Получаем ID пользователя в БД
	var dbPersonID int
	err = tx.QueryRow(`SELECT id FROM person WHERE telegram_id = $1`, userID).Scan(&dbPersonID)
	if err != nil {
		log.Printf("❌ Пользователь не найден: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Пользователь не найден. Напишите /start"))
		return
	}

	// Проверяем, есть ли уже запись (в любом статусе)
	var existingID int
	var existingStatus string
	err = tx.QueryRow(`
		SELECT id, status FROM person_event 
		WHERE person_id = $1 AND event_id = $2
	`, dbPersonID, eventID).Scan(&existingID, &existingStatus)

	// Формируем информацию об участниках
	var participantsInfo string
	var playerIDs []int64

	var participantNames []string
	for _, p := range identifiedPlayers {
		if pid, ok := p["player_id"].(int); ok && pid > 0 {
			playerIDs = append(playerIDs, int64(pid))
			participantNames = append(participantNames, fmt.Sprintf("%s (ID:%d)", p["full_name"], pid))
		} else {
			participantNames = append(participantNames, p["full_name"].(string))
		}
	}
	participantsInfo = strings.Join(participantNames, ", ")

	// Сохраняем информацию об идентифицированных игроках в JSON
	identifiedJSON, _ := json.Marshal(identifiedPlayers)

	if err == nil {
		// Запись уже существует
		if existingStatus == "registered" {
			// Уже активная запись
			h.Bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Вы уже записаны на '%s'", eventName)))
			return
		} else {
			// Была отмененная запись - обновляем её
			log.Printf("🔄 Обновление существующей записи ID=%d со статусом %s", existingID, existingStatus)

			_, err = tx.Exec(`
				UPDATE person_event 
				SET status = 'registered', 
				    participants_count = $1, 
				    registered_at = NOW(), 
				    participants_info = $2,
				    player_ids = $3,
				    identification_data = $4
				WHERE id = $5
			`, count, participantsInfo, pq.Array(playerIDs), identifiedJSON, existingID)

			if err != nil {
				log.Printf("❌ Ошибка обновления записи: %v", err)
				h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка регистрации"))
				return
			}
		}
	} else {
		// Нет записи - создаем новую
		log.Printf("🆕 Создание новой записи для пользователя %d на событие %d", dbPersonID, eventID)

		_, err = tx.Exec(`
			INSERT INTO person_event (
				person_id, event_id, participants_count, 
				participants_info, player_ids, identification_data,
				status, registered_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, 'registered', NOW())
		`, dbPersonID, eventID, count, participantsInfo, pq.Array(playerIDs), identifiedJSON)

		if err != nil {
			log.Printf("❌ Ошибка регистрации: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка регистрации"))
			return
		}
	}

	// Подтверждаем транзакцию
	if err = tx.Commit(); err != nil {
		log.Printf("❌ Ошибка сохранения: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка сохранения"))
		return
	}

	// Формируем сообщение об успехе с датой и временем в одной строке
	successMsg := fmt.Sprintf("✅ Вы успешно записаны на *%s* (%s %s)!\n\n",
		escapeMarkdown(eventName),
		escapeMarkdown(eventDateTime.Format("02.01.2006")),
		escapeMarkdown(eventDateTime.Format("15:04")))
	successMsg += "📋 *Участники:*\n"

	for i, p := range identifiedPlayers {
		fullName := ""
		if fn, ok := p["full_name"].(string); ok {
			fullName = fn
		} else {
			fullName = "Неизвестно"
		}

		if pid, ok := p["player_id"].(int); ok && pid > 0 {
			successMsg += fmt.Sprintf("%d. ✅ %s (идентифицирован)\n", i+1, escapeMarkdown(fullName))
		} else {
			successMsg += fmt.Sprintf("%d. ⚠️ %s (не в базе)\n", i+1, escapeMarkdown(fullName))
		}
	}

	log.Printf("✅ Успешная регистрация на событие %d", eventID)

	msgObj := tgbotapi.NewMessage(chatID, successMsg)
	msgObj.ParseMode = "Markdown"
	h.Bot.Send(msgObj)

	// Показываем обновленные детали события
	h.showEventDetails(chatID, eventID, userID)
}

// cancelRegistration отменяет регистрацию
func (h *CallbackHandler) cancelRegistration(chatID int64, eventID int, userID int64) {
	var eventName string
	err := database.DB.QueryRow(`
		SELECT c.name 
		FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&eventName)

	if err != nil {
		eventName = fmt.Sprintf("событие #%d", eventID)
	}

	var dbPersonID int
	err = database.DB.QueryRow(`SELECT id FROM person WHERE telegram_id = $1`, userID).Scan(&dbPersonID)
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Пользователь не найден"))
		return
	}

	// Проверяем, есть ли активная запись
	var existingID int
	var existingStatus string
	var participantsCount int
	var participantsInfo sql.NullString
	err = database.DB.QueryRow(`
		SELECT id, status, participants_count, participants_info FROM person_event 
		WHERE person_id = $1 AND event_id = $2
	`, dbPersonID, eventID).Scan(&existingID, &existingStatus, &participantsCount, &participantsInfo)

	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Запись на '%s' не найдена", eventName)))
		return
	}

	if existingStatus != "registered" {
		h.Bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ У вас нет активной записи на '%s'", eventName)))
		return
	}

	// Обновляем статус записи на 'cancelled'
	result, err := database.DB.Exec(`
		UPDATE person_event SET status = 'cancelled' 
		WHERE id = $1 AND status = 'registered'
	`, existingID)

	if err != nil {
		log.Printf("❌ Ошибка отмены: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка отмены"))
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Не удалось отменить запись на '%s'", eventName)))
	} else {
		cancelMsg := fmt.Sprintf("✅ Запись на '%s' отменена!", eventName)
		if participantsInfo.Valid && participantsInfo.String != "" && participantsInfo.String != fmt.Sprintf("%d человек", participantsCount) {
			cancelMsg += fmt.Sprintf("\n\nБыли записаны:\n%s", participantsInfo.String)
		} else {
			cancelMsg += fmt.Sprintf("\n\nБыло записано: %d чел.", participantsCount)
		}
		h.Bot.Send(tgbotapi.NewMessage(chatID, cancelMsg))

		h.showEventDetails(chatID, eventID, userID)
	}
}

// showEventDetails показывает детали события
func (h *CallbackHandler) showEventDetails(chatID int64, eventID int, userID int64) {
	var e models.Event
	err := database.DB.QueryRow(`
		SELECT e.id, c.name, e.evn_datetime, e.member_limit,
		       COALESCE((SELECT SUM(participants_count) FROM person_event WHERE event_id = e.id AND status = 'registered'), 0)
		FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&e.ID, &e.CategoryName, &e.DateTime, &e.MemberLimit, &e.Registered)

	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Событие не найдено"))
		return
	}

	var isRegistered bool
	var dbPersonID int
	database.DB.QueryRow(`SELECT id FROM person WHERE telegram_id = $1`, userID).Scan(&dbPersonID)

	var registrationStatus string
	var participantsCount int
	var participantsInfo sql.NullString
	err = database.DB.QueryRow(`
		SELECT status, participants_count, participants_info FROM person_event 
		WHERE person_id = $1 AND event_id = $2
	`, dbPersonID, eventID).Scan(&registrationStatus, &participantsCount, &participantsInfo)

	if err == nil && registrationStatus == "registered" {
		isRegistered = true
	}

	text := fmt.Sprintf(
		"📅 *%s*\n\n"+
			"📆 Дата: %s\n"+
			"👥 Записано: %d/%d\n"+
			"📊 Свободно: %d\n",
		e.CategoryName,
		e.DateTime.Format("02.01.2006 15:04"),
		e.Registered,
		e.MemberLimit,
		e.MemberLimit-e.Registered,
	)

	if isRegistered {
		text += fmt.Sprintf("\n✅ Вы записаны!")
		if participantsInfo.Valid && participantsInfo.String != "" && participantsInfo.String != fmt.Sprintf("%d человек", participantsCount) {
			text += fmt.Sprintf("\n📋 Участники: %s", participantsInfo.String)
		} else {
			text += fmt.Sprintf("\n👥 Количество: %d", participantsCount)
		}
	}

	var keyboard tgbotapi.InlineKeyboardMarkup
	if isRegistered {
		keyboard = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("❌ Отменить запись", fmt.Sprintf("cancel_reg:%d", eventID)),
			),
		)
	} else if e.Registered < e.MemberLimit {
		keyboard = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Записаться", fmt.Sprintf("register:%d", eventID)),
			),
		)
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// handleAdminCallback обрабатывает admin callback'и
func (h *CallbackHandler) handleAdminCallback(callback *tgbotapi.CallbackQuery, data []string) {
	chatID := callback.Message.Chat.ID
	userID := callback.From.ID

	if !h.isAdmin(userID) {
		return
	}

	if len(data) < 2 {
		return
	}

	log.Printf("👑 Админ callback: %v", data)

	switch data[1] {
	case "add_category":
		if len(data) < 3 {
			return
		}
		categoryID, _ := strconv.Atoi(data[2])

		h.UserStates[userID] = &models.UserState{
			Action:     "add_event",
			CategoryID: categoryID,
			Step:       "awaiting_datetime",
			TempData:   make(map[string]interface{}),
		}

		msg := tgbotapi.NewMessage(chatID,
			"Введите дату и время события в формате:\n`2026-03-15 10:00`\n\n"+
				"Пример: 2026-03-20 15:30")
		msg.ParseMode = "Markdown"
		h.Bot.Send(msg)

	case "confirm_add":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.confirmAddEvent(chatID, userID)

	case "cancel_add":
		delete(h.UserStates, userID)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Создание события отменено"))

	case "edit_event":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		h.showEventEditMenu(chatID, eventID)

	case "edit_date":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		h.UserStates[userID] = &models.UserState{
			Action:   "edit_event",
			Step:     "awaiting_new_date",
			TempData: map[string]interface{}{"event_id": eventID},
		}
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			"Введите новую дату и время в формате:\n2026-03-15 10:00"))

	case "edit_limit":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		h.UserStates[userID] = &models.UserState{
			Action:   "edit_event",
			Step:     "awaiting_new_limit",
			TempData: map[string]interface{}{"event_id": eventID},
		}
		h.Bot.Send(tgbotapi.NewMessage(chatID, "Введите новый лимит участников:"))

	case "delete_event":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		h.confirmDeleteEvent(chatID, eventID)

	case "confirm_delete":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		h.deleteEvent(chatID, eventID)

	case "delete_category":
		if len(data) < 3 {
			return
		}
		categoryID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.deleteCategory(chatID, categoryID)

	case "confirm_delete_category":
		if len(data) < 3 {
			return
		}
		categoryID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.confirmDeleteCategory(chatID, categoryID)

	case "view_registrations":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		h.showEventRegistrations(chatID, eventID)

	case "view_event_registrations":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showEventRegistrations(chatID, eventID)

	case "view_all_registrations":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showAllRegistrationsFull(chatID)

	case "back_to_events":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showAllRegistrations(chatID)

	case "back":
		delete(h.UserStates, userID)
		msg := tgbotapi.NewMessage(chatID, "👑 Панель администратора:")
		msg.ReplyMarkup = h.getAdminKeyboard()
		h.Bot.Send(msg)
	}
}

// getAdminKeyboard возвращает клавиатуру для админа
func (h *CallbackHandler) getAdminKeyboard() tgbotapi.ReplyKeyboardMarkup {
	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("➕ Добавить событие"),
			tgbotapi.NewKeyboardButton("📋 Все события"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 Статистика"),
			tgbotapi.NewKeyboardButton("👥 Все записи"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📁 Управление категориями"),
			tgbotapi.NewKeyboardButton("➕ Добавить категорию"),
		),
	)
	keyboard.ResizeKeyboard = true
	return keyboard
}

// showEventEditMenu показывает меню редактирования события
func (h *CallbackHandler) showEventEditMenu(chatID int64, eventID int) {
	var e models.Event
	err := database.DB.QueryRow(`
		SELECT e.id, c.name, e.evn_datetime, e.member_limit
		FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&e.ID, &e.CategoryName, &e.DateTime, &e.MemberLimit)

	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Событие не найдено"))
		return
	}

	text := fmt.Sprintf(
		"Редактирование события #%d - *%s*\n\n"+
			"Текущие данные:\n"+
			"📆 Дата: %s\n"+
			"👥 Лимит: %d\n\n"+
			"Выберите действие:",
		e.ID,
		e.CategoryName,
		e.DateTime.Format("02.01.2006 15:04"),
		e.MemberLimit,
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📅 Изменить дату", fmt.Sprintf("admin:edit_date:%d", eventID)),
			tgbotapi.NewInlineKeyboardButtonData("👥 Изменить лимит", fmt.Sprintf("admin:edit_limit:%d", eventID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🗑 Удалить", fmt.Sprintf("admin:delete_event:%d", eventID)),
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "admin:back"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// confirmDeleteEvent подтверждает удаление события
func (h *CallbackHandler) confirmDeleteEvent(chatID int64, eventID int) {
	var eventName string
	database.DB.QueryRow(`
		SELECT c.name FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&eventName)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Да, удалить", fmt.Sprintf("admin:confirm_delete:%d", eventID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Нет, отмена", "admin:back"),
		),
	)

	msg := tgbotapi.NewMessage(chatID,
		fmt.Sprintf("⚠️ Вы уверены, что хотите удалить событие '%s'?\n"+
			"Все записи на это событие также будут удалены.", eventName))
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// deleteEvent удаляет событие
func (h *CallbackHandler) deleteEvent(chatID int64, eventID int) {
	var eventName string
	database.DB.QueryRow(`
		SELECT c.name FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&eventName)

	tx, err := database.DB.Begin()
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка сервера"))
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM person_event WHERE event_id = $1`, eventID)
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при удалении записей"))
		return
	}

	result, err := tx.Exec(`DELETE FROM event WHERE id = $1`, eventID)
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при удалении события"))
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Событие не найдено"))
		return
	}

	if err = tx.Commit(); err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при сохранении"))
		return
	}

	h.Bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Событие '%s' удалено", eventName)))
}

// showEventRegistrations показывает записи на событие (для callback)
func (h *CallbackHandler) showEventRegistrations(chatID int64, eventID int) {
	rows, err := database.DB.Query(`
		SELECT p.nikname, p.firstname, p.lastname, pe.participants_count, pe.participants_info, pe.registered_at
		FROM person_event pe
		JOIN person p ON pe.person_id = p.id
		WHERE pe.event_id = $1 AND pe.status = 'registered'
		ORDER BY pe.registered_at
	`, eventID)

	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
		return
	}
	defer rows.Close()

	var eventInfo struct {
		Name     string
		DateTime time.Time
		Limit    int
	}
	database.DB.QueryRow(`
		SELECT c.name, e.evn_datetime, e.member_limit
		FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&eventInfo.Name, &eventInfo.DateTime, &eventInfo.Limit)

	text := fmt.Sprintf("📊 Записи на событие *%s*\n"+
		"📅 %s\n"+
		"👥 Лимит: %d\n\n",
		eventInfo.Name,
		eventInfo.DateTime.Format("02.01.2006 15:04"),
		eventInfo.Limit,
	)

	count := 0
	totalParticipants := 0
	for rows.Next() {
		count++
		var nikname, first, last string
		var participants int
		var participantsInfo sql.NullString
		var regTime time.Time
		err := rows.Scan(&nikname, &first, &last, &participants, &participantsInfo, &regTime)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}
		totalParticipants += participants

		userName := nikname
		if first != "" {
			userName = first + " " + last
		}

		text += fmt.Sprintf("%d. *%s* - %d чел.\n", count, userName, participants)
		if participantsInfo.Valid && participantsInfo.String != "" && participantsInfo.String != fmt.Sprintf("%d человек", participants) {
			text += fmt.Sprintf("   📋 %s\n", participantsInfo.String)
		}
		text += fmt.Sprintf("   📅 %s\n\n", regTime.Format("02.01 15:04"))
	}

	if count == 0 {
		text += "\n❌ Нет записей"
	} else {
		text += fmt.Sprintf("\n📊 Всего записей: %d\n", count)
		text += fmt.Sprintf("👥 Всего участников: %d\n", totalParticipants)
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	h.Bot.Send(msg)
}
