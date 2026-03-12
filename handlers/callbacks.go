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

// formatDateTime возвращает дату и время в нужном формате без экранирования
func formatDateTime(t time.Time) string {
	return t.Format("02.01.2006 15:04")
}

// NewCallbackHandler создает новый обработчик callback'ов
func NewCallbackHandler(bot *tgbotapi.BotAPI, adminIDs *map[int64]bool, userStates map[int64]*models.UserState) *CallbackHandler {
	return &CallbackHandler{
		Bot:        bot,
		AdminIDs:   adminIDs,
		UserStates: userStates,
	}
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

	// Проверяем специальные команды для дозаписи
	switch data[0] {
	case "add_more_count":
		h.handleAddMoreCount(callback, data)
		return
	case "add_more_custom":
		if len(data) < 2 {
			return
		}
		eventID, _ := strconv.Atoi(data[1])
		h.askCustomAddMore(chatID, eventID, userID)
		return
	case "select_additional", "select_additional_keep":
		h.handleAdditionalSelection(callback, data)
		return
	case "remove_participant":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[1])
		participantIndex, _ := strconv.Atoi(data[2])
		h.removeParticipant(chatID, eventID, userID, participantIndex)
		return
	}

	// Проверяем, не является ли это админским callback'ом
	if data[0] == "admin" {
		if h.isAdmin(userID) {
			h.handleAdminCallback(callback, data)
		} else {
			h.Bot.Send(tgbotapi.NewMessage(chatID, "⛔ У вас нет прав администратора"))
		}
		return
	}

	// Проверяем специальные команды админки
	switch data[0] {
	case "players_page":
		if len(data) < 3 {
			return
		}
		page, _ := strconv.Atoi(data[1])
		filter := data[2]
		if h.isAdmin(userID) {
			msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
			msgHandler.showPlayersList(chatID, page, filter)
		} else {
			h.Bot.Send(tgbotapi.NewMessage(chatID, "⛔ У вас нет прав администратора"))
		}
		return
	}

	// Обычные callback'и для пользователей
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

	case "add_more":
		if len(data) < 2 {
			return
		}
		eventID, _ := strconv.Atoi(data[1])
		h.askAdditionalParticipants(chatID, eventID, userID)

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

		h.askParticipantNames(chatID, eventID, userID, count)

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

	case "confirm_add_more":
		if len(data) < 2 {
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка формата данных"))
			return
		}
		eventID, _ := strconv.Atoi(data[1])

		state, exists := h.UserStates[userID]
		if !exists || state.TempData["additional_players"] == nil {
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка: данные не найдены. Начните заново."))
			return
		}

		additional := state.TempData["additional_players"].([]map[string]interface{})
		h.addMoreParticipants(chatID, eventID, userID, additional)
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

	case "view_participants":
		if len(data) < 2 {
			return
		}
		eventID, _ := strconv.Atoi(data[1])
		h.showEventParticipants(chatID, eventID)

	default:
		log.Printf("❌ Неизвестная команда: %s", data[0])
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Неизвестная команда"))
	}
}

// ==================== ФУНКЦИИ ДЛЯ ПРОСМОТРА УЧАСТНИКОВ ====================

// showEventParticipants показывает список всех записавшихся на событие
func (h *CallbackHandler) showEventParticipants(chatID int64, eventID int) {
	log.Printf("👥 Запрос списка участников для события %d от пользователя %d", eventID, chatID)

	// Получаем информацию о событии
	var eventName string
	var eventDateTime time.Time
	err := database.DB.QueryRow(`
		SELECT c.name, e.evn_datetime
		FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&eventName, &eventDateTime)

	if err != nil {
		log.Printf("❌ Ошибка загрузки события: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки события"))
		return
	}

	// Получаем всех участников
	rows, err := database.DB.Query(`
		SELECT 
			p.nikname,
			p.firstname,
			p.lastname,
			pe.identification_data,
			pe.registered_at
		FROM person_event pe
		JOIN person p ON pe.person_id = p.id
		WHERE pe.event_id = $1 AND pe.status = 'registered'
		ORDER BY pe.registered_at
	`, eventID)

	if err != nil {
		log.Printf("❌ Ошибка загрузки участников: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
		return
	}
	defer rows.Close()

	// Проверяем, есть ли данные
	var hasData bool
	var text string
	totalParticipants := 0
	paidParticipants := 0
	participantNumber := 1

	for rows.Next() {
		hasData = true
		var nikname, firstname, lastname string
		var identificationData []byte
		var registeredAt time.Time

		err := rows.Scan(&nikname, &firstname, &lastname,
			&identificationData, &registeredAt)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		// Если это первый элемент, формируем заголовок
		if text == "" {
			text = fmt.Sprintf("📅 *%s* (%s)\n\n",
				escapeMarkdown(eventName),
				formatDateTime(eventDateTime.Local()))
			text += "👥 *Список участников:*\n\n"
		}

		// Формируем имя записавшего
		registrantName := ""
		if nikname != "" {
			registrantName += "@" + nikname
		}
		if firstname != "" || lastname != "" {
			if registrantName != "" {
				registrantName += " "
			}
			registrantName += firstname + " " + lastname
		}
		if registrantName == "" {
			registrantName = "Аноним"
		}
		registrantName = escapeMarkdown(registrantName)

		// Парсим данные об участниках
		if len(identificationData) > 0 {
			var identified []map[string]interface{}
			if err := json.Unmarshal(identificationData, &identified); err == nil {
				for _, p := range identified {
					// Получаем имя для отображения
					displayName := ""
					if fn, ok := p["full_name"].(string); ok && fn != "" {
						displayName = fn
					} else if input, ok := p["input"].(string); ok && input != "" {
						displayName = input
					} else {
						displayName = "Неизвестно"
					}

					// Получаем ник для отображения
					nickDisplay := ""
					if nick, ok := p["telegram_nick"].(string); ok && nick != "" {
						nickDisplay = fmt.Sprintf(" %s", escapeMarkdown(nick))
					}

					// Получаем статус оплаты для этого участника
					paymentStatus := "pending"
					if ps, ok := p["payment_status"].(string); ok {
						paymentStatus = ps
					}

					// Добавляем эмодзи оплаты
					paymentEmoji := "⏳"
					if paymentStatus == "paid" {
						paymentEmoji = "💰"
						paidParticipants++
					}

					text += fmt.Sprintf("%d. %s %s%s (записал: %s)\n",
						participantNumber, paymentEmoji, escapeMarkdown(displayName), nickDisplay, registrantName)

					participantNumber++
					totalParticipants++
				}
			}
		}
	}

	if !hasData {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Нет записей на это событие"))
		return
	}

	// Добавляем статистику
	text += fmt.Sprintf("\n📊 *Всего участников: %d*", totalParticipants)
	text += fmt.Sprintf("\n💰 *Оплатили: %d*", paidParticipants)
	text += fmt.Sprintf("\n⏳ *Ожидают оплаты: %d*", totalParticipants-paidParticipants)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад к событию", fmt.Sprintf("event:%d", eventID)),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// ==================== ФУНКЦИИ ДЛЯ УДАЛЕНИЯ УЧАСТНИКОВ ====================

// removeParticipant удаляет конкретного участника из записи
func (h *CallbackHandler) removeParticipant(chatID int64, eventID int, userID int64, participantIndex int) {
	log.Printf("🗑 Удаление участника #%d из записи на событие %d от пользователя %d", participantIndex, eventID, userID)

	// Получаем информацию о событии
	var eventName string
	err := database.DB.QueryRow(`
		SELECT c.name
		FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&eventName)

	if err != nil {
		log.Printf("❌ Ошибка загрузки события: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки события"))
		return
	}

	// Получаем ID пользователя в БД
	var dbPersonID int
	err = database.DB.QueryRow(`SELECT id FROM person WHERE telegram_id = $1`, userID).Scan(&dbPersonID)
	if err != nil {
		log.Printf("❌ Пользователь не найден: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Пользователь не найден"))
		return
	}

	// Начинаем транзакцию
	tx, err := database.DB.Begin()
	if err != nil {
		log.Printf("❌ Ошибка начала транзакции: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка сервера"))
		return
	}
	defer tx.Rollback()

	// Получаем существующую запись
	var existingID int
	var existingParticipantsInfo sql.NullString
	var existingPlayerIDs []int64
	var existingIdentificationData []byte
	var participantsCount int

	err = tx.QueryRow(`
		SELECT id, participants_count, participants_info, COALESCE(player_ids, ARRAY[]::INTEGER[]), COALESCE(identification_data, '[]'::JSONB)
		FROM person_event 
		WHERE person_id = $1 AND event_id = $2 AND status = 'registered'
	`, dbPersonID, eventID).Scan(&existingID, &participantsCount, &existingParticipantsInfo, pq.Array(&existingPlayerIDs), &existingIdentificationData)

	if err != nil {
		log.Printf("❌ Активная запись не найдена: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Активная запись не найдена"))
		return
	}

	// Парсим существующие данные
	var existingIdentified []map[string]interface{}
	if len(existingIdentificationData) > 0 {
		json.Unmarshal(existingIdentificationData, &existingIdentified)
	}

	// Проверяем, что индекс корректен
	if participantIndex < 0 || participantIndex >= len(existingIdentified) {
		log.Printf("❌ Неверный индекс участника: %d", participantIndex)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Участник не найден"))
		return
	}

	// Удаляем участника
	removedParticipant := existingIdentified[participantIndex]
	existingIdentified = append(existingIdentified[:participantIndex], existingIdentified[participantIndex+1:]...)

	// Обновляем player_ids
	if pid, ok := removedParticipant["player_id"].(int); ok && pid > 0 {
		for i, id := range existingPlayerIDs {
			if id == int64(pid) {
				existingPlayerIDs = append(existingPlayerIDs[:i], existingPlayerIDs[i+1:]...)
				break
			}
		}
	}

	// Формируем новую информацию об участниках
	var participantNames []string
	for _, p := range existingIdentified {
		if pid, ok := p["player_id"].(int); ok && pid > 0 {
			participantNames = append(participantNames, fmt.Sprintf("%s (ID:%d)", p["full_name"], pid))
		} else {
			participantNames = append(participantNames, p["full_name"].(string))
		}
	}
	newParticipantsInfo := strings.Join(participantNames, ", ")
	newCount := len(existingIdentified)

	// Если участников не осталось, отменяем всю запись
	if newCount == 0 {
		_, err = tx.Exec(`
			UPDATE person_event SET status = 'cancelled' WHERE id = $1
		`, existingID)

		if err != nil {
			log.Printf("❌ Ошибка отмены записи: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при удалении"))
			return
		}

		if err = tx.Commit(); err != nil {
			log.Printf("❌ Ошибка сохранения: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка сохранения"))
			return
		}

		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("✅ Все участники удалены. Запись на '%s' отменена.", eventName)))
	} else {
		// Обновляем запись
		updatedJSON, _ := json.Marshal(existingIdentified)

		_, err = tx.Exec(`
			UPDATE person_event 
			SET participants_count = $1,
			    participants_info = $2,
			    player_ids = $3,
			    identification_data = $4,
			    registered_at = NOW()
			WHERE id = $5
		`, newCount, newParticipantsInfo, pq.Array(existingPlayerIDs), updatedJSON, existingID)

		if err != nil {
			log.Printf("❌ Ошибка обновления записи: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при удалении"))
			return
		}

		if err = tx.Commit(); err != nil {
			log.Printf("❌ Ошибка сохранения: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка сохранения"))
			return
		}

		removedName := ""
		if fn, ok := removedParticipant["full_name"].(string); ok {
			removedName = fn
		} else {
			removedName = "Участник"
		}

		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("✅ Участник '%s' удален из записи на '%s'", removedName, eventName)))
	}

	// Показываем обновленные детали
	h.showEventDetails(chatID, eventID, userID)
}

// ==================== ФУНКЦИИ ДЛЯ ЗАПИСИ НА СОБЫТИЯ ====================

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

	// Получаем информацию о событии
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
			"Мария",
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

	// Проверяем, есть ли среди введенных строк "себя", "я", "меня"
	for i, input := range inputs {
		inputLower := strings.ToLower(input)
		if inputLower == "себя" || inputLower == "я" || inputLower == "меня" {
			// Заменяем на данные текущего пользователя
			inputs[i] = fmt.Sprintf("@%s %s %s", message.From.UserName, message.From.FirstName, message.From.LastName)
		}
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
		// Все игроки обработаны, регистрируем сразу
		identified := state.TempData["identified_players"].([]map[string]interface{})
		eventID := state.TempData["event_id"].(int)
		count := len(identified)
		h.registerForEventWithIdentification(chatID, eventID, userID, count, identified)
		delete(h.UserStates, userID)
		return
	}

	input := inputs[index]
	state.TempData["current_index"] = index

	// Проверяем, не является ли это специальным значением "себя"
	inputLower := strings.ToLower(input)
	if inputLower == "себя" || inputLower == "я" || inputLower == "меня" {
		// Это сам пользователь
		// Получаем данные пользователя из таблицы person
		var personID int64
		var nikname, firstname, lastname string
		_ = database.DB.QueryRow(`
			SELECT id, nikname, firstname, lastname FROM person WHERE telegram_id = $1
		`, userID).Scan(&personID, &nikname, &firstname, &lastname)

		// Получаем username
		telegramUsername := ""
		if nikname != "" {
			telegramUsername = "@" + nikname
		}

		// Формируем полное имя
		fullName := fmt.Sprintf("%s %s", firstname, lastname)
		if strings.TrimSpace(fullName) == "" {
			if nikname != "" {
				fullName = nikname
			} else {
				fullName = "Пользователь"
			}
		}

		// Проверяем, нет ли уже такого пользователя в списке
		identified := state.TempData["identified_players"].([]map[string]interface{})
		isDuplicate := false

		// Проверяем по telegram_nick
		for _, existing := range identified {
			if existingNick, ok := existing["telegram_nick"].(string); ok && existingNick == telegramUsername {
				isDuplicate = true
				break
			}
		}

		if isDuplicate {
			// Если дубликат, пропускаем и переходим к следующему
			h.Bot.Send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("⚠️ Вы (%s) уже добавлены в список участников", telegramUsername)))
			h.identifyNextPlayer(chatID, userID, index+1)
			return
		}

		// Добавляем нового участника
		identified = append(identified, map[string]interface{}{
			"player_id":     0,
			"input":         input,
			"full_name":     fullName,
			"telegram_nick": telegramUsername,
		})
		state.TempData["identified_players"] = identified
		h.identifyNextPlayer(chatID, userID, index+1)
		return
	}

	// Ищем игрока в базе players
	results, err := utils.FindPlayer(database.DB, input)
	if err != nil {
		log.Printf("❌ Ошибка поиска игрока: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при поиске. Попробуйте еще раз."))
		return
	}

	if len(results) == 0 {
		// Игрок не найден - сохраняем как есть
		identified := state.TempData["identified_players"].([]map[string]interface{})
		identified = append(identified, map[string]interface{}{
			"player_id": 0,
			"input":     input,
			"full_name": input,
		})
		state.TempData["identified_players"] = identified

		// Переходим к следующему игроку
		h.identifyNextPlayer(chatID, userID, index+1)
		return
	}

	if len(results) == 1 {
		// Найден один игрок - проверяем на дубликат
		r := results[0]

		// Проверяем, нет ли уже такого игрока в списке
		identified := state.TempData["identified_players"].([]map[string]interface{})
		isDuplicate := false

		// Проверяем по ID игрока
		for _, existing := range identified {
			if existingID, ok := existing["player_id"].(int); ok && existingID == r.ID {
				isDuplicate = true
				break
			}
			// Также проверяем по нику
			if existingNick, ok := existing["telegram_nick"].(string); ok && existingNick == r.TelegramNick {
				isDuplicate = true
				break
			}
		}

		if isDuplicate {
			// Если дубликат, пропускаем
			displayName := r.FullName
			if r.TelegramNick != "" {
				displayName = fmt.Sprintf("%s %s", r.TelegramNick, r.FullName)
			}
			h.Bot.Send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("⚠️ Игрок %s уже добавлен в список", displayName)))
			h.identifyNextPlayer(chatID, userID, index+1)
			return
		}

		// Добавляем нового участника
		identified = append(identified, map[string]interface{}{
			"player_id":     r.ID,
			"input":         input,
			"full_name":     r.FullName,
			"telegram_nick": r.TelegramNick,
			"telegram_name": r.TelegramName,
		})
		state.TempData["identified_players"] = identified

		// Переходим к следующему игроку
		h.identifyNextPlayer(chatID, userID, index+1)
		return
	}

	// Найдено несколько вариантов - показываем для выбора
	msg := fmt.Sprintf("🔍 Найдено несколько вариантов для '%s'. Выберите нужного:\n\n", input)

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
		tgbotapi.NewInlineKeyboardButtonData("⏭ Пропустить (оставить как есть)",
			fmt.Sprintf("identify_skip:%d", index)),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)
	msgObj := tgbotapi.NewMessage(chatID, msg)
	msgObj.ReplyMarkup = keyboard
	h.Bot.Send(msgObj)

	state.Step = "selecting"
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
			"full_name":     player.FullName,
			"telegram_nick": player.TelegramNick.String,
			"telegram_name": player.TelegramName.String,
		})
		state.TempData["identified_players"] = identified

		h.identifyNextPlayer(chatID, userID, index+1)

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
			"full_name": inputs[index],
		})
		state.TempData["identified_players"] = identified

		h.identifyNextPlayer(chatID, userID, index+1)
	}
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

	// Добавляем статус оплаты для каждого участника (по умолчанию "pending")
	for i := range identifiedPlayers {
		identifiedPlayers[i]["payment_status"] = "pending"
	}

	// Сохраняем информацию об идентифицированных игроках в JSON
	identifiedJSON, _ := json.Marshal(identifiedPlayers)

	// Проверяем, есть ли уже запись
	var existingID int
	var existingStatus string
	err = tx.QueryRow(`
		SELECT id, status FROM person_event 
		WHERE person_id = $1 AND event_id = $2
	`, dbPersonID, eventID).Scan(&existingID, &existingStatus)

	if err == nil {
		// Запись уже существует - обновляем
		if existingStatus == "registered" {
			// Получаем существующие данные
			var existingIdentificationData []byte
			var existingParticipantsInfo sql.NullString
			var existingCount int

			tx.QueryRow(`
				SELECT participants_count, participants_info, identification_data
				FROM person_event WHERE id = $1
			`, existingID).Scan(&existingCount, &existingParticipantsInfo, &existingIdentificationData)

			// Объединяем существующих и новых участников
			var allIdentified []map[string]interface{}
			if len(existingIdentificationData) > 0 {
				json.Unmarshal(existingIdentificationData, &allIdentified)
			}
			allIdentified = append(allIdentified, identifiedPlayers...)

			// Формируем информацию
			var allNames []string
			for _, p := range allIdentified {
				displayName := p["full_name"].(string)
				if nick, ok := p["telegram_nick"].(string); ok && nick != "" {
					displayName = fmt.Sprintf("%s %s", nick, displayName)
				}
				allNames = append(allNames, displayName)
			}
			newParticipantsInfo := strings.Join(allNames, ", ")
			newCount := len(allIdentified)
			updatedJSON, _ := json.Marshal(allIdentified)

			_, err = tx.Exec(`
				UPDATE person_event 
				SET participants_count = $1,
				    participants_info = $2,
				    identification_data = $3,
				    registered_at = NOW()
				WHERE id = $4
			`, newCount, newParticipantsInfo, updatedJSON, existingID)
		} else {
			// Была отмененная запись - обновляем
			_, err = tx.Exec(`
				UPDATE person_event 
				SET status = 'registered', 
				    participants_count = $1, 
				    registered_at = NOW(), 
				    participants_info = $2,
				    identification_data = $3
				WHERE id = $4
			`, count, "", identifiedJSON, existingID)
		}
	} else {
		// Нет записи - создаем новую
		var participantNames []string
		for _, p := range identifiedPlayers {
			displayName := p["full_name"].(string)
			if nick, ok := p["telegram_nick"].(string); ok && nick != "" {
				displayName = fmt.Sprintf("%s %s", nick, displayName)
			}
			participantNames = append(participantNames, displayName)
		}
		participantsInfo := strings.Join(participantNames, ", ")

		_, err = tx.Exec(`
			INSERT INTO person_event (
				person_id, event_id, participants_count, 
				participants_info, identification_data,
				status, registered_at
			)
			VALUES ($1, $2, $3, $4, $5, 'registered', NOW())
		`, dbPersonID, eventID, count, participantsInfo, identifiedJSON)
	}

	if err != nil {
		log.Printf("❌ Ошибка сохранения: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка регистрации"))
		return
	}

	// Подтверждаем транзакцию
	if err = tx.Commit(); err != nil {
		log.Printf("❌ Ошибка сохранения: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка сохранения"))
		return
	}

	// Формируем сообщение об успехе
	successMsg := fmt.Sprintf("✅ Вы успешно записаны на *%s* (%s)!\n\n",
		escapeMarkdown(eventName),
		formatDateTime(eventDateTime.Local()))
	successMsg += "📋 *Участники:*\n"

	for i, p := range identifiedPlayers {
		displayName := p["full_name"].(string)
		if nick, ok := p["telegram_nick"].(string); ok && nick != "" {
			displayName = fmt.Sprintf("%s %s", nick, displayName)
		}

		successMsg += fmt.Sprintf("%d. ⏳ %s\n", i+1, escapeMarkdown(displayName))
	}

	log.Printf("✅ Успешная регистрация на событие %d", eventID)

	msgObj := tgbotapi.NewMessage(chatID, successMsg)
	msgObj.ParseMode = "Markdown"
	h.Bot.Send(msgObj)

	// Показываем обновленные детали события
	h.showEventDetails(chatID, eventID, userID)
}

// ==================== ФУНКЦИИ ДЛЯ ОТМЕНЫ РЕГИСТРАЦИИ ====================

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

// ==================== ФУНКЦИИ ДЛЯ ПРОСМОТРА ДЕТАЛЕЙ ====================

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

	// Конвертируем в локальное время
	e.DateTime = e.DateTime.Local()

	var isRegistered bool
	var dbPersonID int
	database.DB.QueryRow(`SELECT id FROM person WHERE telegram_id = $1`, userID).Scan(&dbPersonID)

	var registrationStatus string
	var participantsCount int
	var participantsInfo sql.NullString
	var existingPlayerIDs []int64
	var identificationData []byte
	var paymentStatus sql.NullString
	var paymentDate sql.NullTime

	err = database.DB.QueryRow(`
		SELECT status, participants_count, participants_info, 
		       COALESCE(player_ids, ARRAY[]::INTEGER[]), 
		       COALESCE(identification_data, '[]'::JSONB),
		       payment_status, payment_date
		FROM person_event 
		WHERE person_id = $1 AND event_id = $2
	`, dbPersonID, eventID).Scan(&registrationStatus, &participantsCount, &participantsInfo,
		pq.Array(&existingPlayerIDs), &identificationData, &paymentStatus, &paymentDate)

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

	// Добавляем информацию о статусе оплаты для админа
	if h.isAdmin(userID) && isRegistered {
		if paymentStatus.Valid && paymentStatus.String == "paid" {
			text += fmt.Sprintf("\n💰 *Оплачено:* %s", paymentDate.Time.Format("02.01.2006 15:04"))
		} else if isRegistered {
			text += "\n⏳ *Ожидает оплаты*"
		}
	}

	var keyboard [][]tgbotapi.InlineKeyboardButton

	// Кнопка для просмотра всех участников (доступна всем)
	keyboard = append(keyboard, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("👥 Список участников", fmt.Sprintf("view_participants:%d", eventID)),
	))

	if isRegistered {
		text += fmt.Sprintf("\n✅ Вы записаны! (%d чел.)\n", participantsCount)

		// Парсим данные для показа списка с возможностью удаления
		if len(identificationData) > 0 {
			var identified []map[string]interface{}
			if err := json.Unmarshal(identificationData, &identified); err == nil {
				text += "\n📋 *Ваши участники:*\n"
				for i, p := range identified {
					// Формируем полное имя для отображения
					fullName := ""
					if fn, ok := p["full_name"].(string); ok && fn != "" {
						fullName = fn
					} else if input, ok := p["input"].(string); ok && input != "" {
						fullName = input
					} else {
						fullName = "Неизвестно"
					}

					// Добавляем ник если есть
					nickPart := ""
					if nick, ok := p["telegram_nick"].(string); ok && nick != "" {
						nickPart = fmt.Sprintf("%s ", nick)
					}

					// Отображаем в тексте
					text += fmt.Sprintf("%d. %s%s\n", i+1, nickPart, fullName)

					// Формируем текст кнопки удаления
					buttonText := fmt.Sprintf("❌ Удалить %s%s", nickPart, fullName)
					if len(buttonText) > 40 {
						// Обрезаем если слишком длинное
						runes := []rune(buttonText)
						if len(runes) > 40 {
							buttonText = string(runes[:37]) + "..."
						}
					}

					// Добавляем кнопку удаления для каждого участника
					keyboard = append(keyboard, tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData(
							buttonText,
							fmt.Sprintf("remove_participant:%d:%d", eventID, i),
						),
					))
				}
			}
		}

		// Кнопки для дозаписи и отмены
		available := e.MemberLimit - e.Registered
		var actionRow []tgbotapi.InlineKeyboardButton
		if available > 0 {
			actionRow = append(actionRow, tgbotapi.NewInlineKeyboardButtonData("➕ Добавить еще", fmt.Sprintf("add_more:%d", eventID)))
		}
		actionRow = append(actionRow, tgbotapi.NewInlineKeyboardButtonData("❌ Отменить всё", fmt.Sprintf("cancel_reg:%d", eventID)))

		if len(actionRow) > 0 {
			keyboard = append(keyboard, actionRow)
		}
	} else if e.Registered < e.MemberLimit {
		keyboard = append(keyboard, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Записаться", fmt.Sprintf("register:%d", eventID)),
		))
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	if len(keyboard) > 0 {
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(keyboard...)
	}
	h.Bot.Send(msg)
}
