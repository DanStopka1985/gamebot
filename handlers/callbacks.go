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

// ==================== ФУНКЦИИ ДЛЯ ДОЗАПИСИ ====================

// askAdditionalParticipants запрашивает дополнительных участников
func (h *CallbackHandler) askAdditionalParticipants(chatID int64, eventID int, userID int64) {
	log.Printf("➕ Запрос дополнительных участников для события %d от пользователя %d", eventID, userID)

	// Получаем информацию о событии
	var eventName string
	var eventDateTime time.Time
	var registered, limit int
	err := database.DB.QueryRow(`
		SELECT c.name, e.evn_datetime, e.member_limit,
		       COALESCE(SUM(pe.participants_count), 0)
		FROM event e
		JOIN category c ON e.category_id = c.id
		LEFT JOIN person_event pe ON e.id = pe.event_id AND pe.status = 'registered'
		WHERE e.id = $1
		GROUP BY c.name, e.evn_datetime, e.member_limit
	`, eventID).Scan(&eventName, &eventDateTime, &limit, &registered)

	if err != nil {
		log.Printf("❌ Ошибка загрузки события: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки события"))
		return
	}

	available := limit - registered
	if available <= 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Свободных мест нет"))
		return
	}

	// Сохраняем состояние
	h.UserStates[userID] = &models.UserState{
		Action: "adding_more",
		Step:   "awaiting_count",
		TempData: map[string]interface{}{
			"event_id":   eventID,
			"event_name": eventName,
			"available":  available,
		},
	}

	msgText := fmt.Sprintf(
		"📅 *%s* (%s %s)\n\n"+
			"Свободно мест: %d\n\n"+
			"Сколько еще человек хотите добавить?",
		eventName,
		eventDateTime.Format("02.01.2006"),
		eventDateTime.Format("15:04"),
		available)

	// Создаем клавиатуру для быстрого выбора
	var rows [][]tgbotapi.InlineKeyboardButton
	for i := 1; i <= 5 && i <= available; i++ {
		buttonText := fmt.Sprintf("%d %s", i, utils.Pluralize(i, "человек", "человека", "человек"))
		callbackData := fmt.Sprintf("add_more_count:%d:%d", eventID, i)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(buttonText, callbackData),
		))
	}

	if available > 5 {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✏️ Другое количество", fmt.Sprintf("add_more_custom:%d", eventID)),
		))
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "cancel_registration"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	msg := tgbotapi.NewMessage(chatID, msgText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// handleAddMoreCount обрабатывает выбор количества для дозаписи
func (h *CallbackHandler) handleAddMoreCount(callback *tgbotapi.CallbackQuery, data []string) {
	if len(data) < 3 {
		return
	}

	eventID, _ := strconv.Atoi(data[1])
	count, _ := strconv.Atoi(data[2])
	userID := callback.From.ID
	chatID := callback.Message.Chat.ID

	state, exists := h.UserStates[userID]
	if !exists || state.Action != "adding_more" {
		return
	}

	available := state.TempData["available"].(int)
	if count > available {
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("❌ Свободно только %d мест. Выберите другое количество:", available)))
		return
	}

	h.askAdditionalNames(chatID, eventID, userID, count, 0)
}

// askCustomAddMore запрашивает произвольное количество для дозаписи
func (h *CallbackHandler) askCustomAddMore(chatID int64, eventID int, userID int64) {
	state, exists := h.UserStates[userID]
	if !exists || state.Action != "adding_more" {
		return
	}

	state.Step = "awaiting_custom_count"
	h.Bot.Send(tgbotapi.NewMessage(chatID,
		"Введите количество дополнительных участников:"))
}

// handleCustomAddMoreCount обрабатывает ввод произвольного количества для дозаписи
func (h *CallbackHandler) handleCustomAddMoreCount(message *tgbotapi.Message) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := message.Text

	state, exists := h.UserStates[userID]
	if !exists || state.Action != "adding_more" || state.Step != "awaiting_custom_count" {
		return
	}

	count, err := strconv.Atoi(text)
	if err != nil || count < 1 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Введите положительное число:"))
		return
	}

	available := state.TempData["available"].(int)
	if count > available {
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("❌ Свободно только %d мест. Введите другое число:", available)))
		return
	}

	eventID := state.TempData["event_id"].(int)
	h.askAdditionalNames(chatID, eventID, userID, count, 0)
}

// askAdditionalNames запрашивает имена для дополнительных участников
func (h *CallbackHandler) askAdditionalNames(chatID int64, eventID int, userID int64, totalCount int, currentIndex int) {
	state, exists := h.UserStates[userID]
	if !exists {
		return
	}

	// Если это первый запрос, инициализируем массив
	if currentIndex == 0 {
		state.TempData["additional_players"] = []map[string]interface{}{}
		state.TempData["total_count"] = totalCount
		state.Step = "entering_names"
	}

	msgText := fmt.Sprintf(
		"👤 Введите данные для дополнительного участника #%d из %d:\n\n"+
			"📱 *Как лучше вводить:*\n"+
			"• Telegram никнейм (например, @john_doe)\n"+
			"• Имя и фамилию (например, Иван Петров)\n"+
			"• Любую информацию, по которой можно идентифицировать игрока",
		currentIndex+1, totalCount)

	state.TempData["current_index"] = currentIndex
	state.TempData["awaiting_for_index"] = currentIndex

	h.Bot.Send(tgbotapi.NewMessage(chatID, msgText))
}

// handleAdditionalParticipantInput обрабатывает ввод для дополнительного участника
func (h *CallbackHandler) handleAdditionalParticipantInput(message *tgbotapi.Message) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := strings.TrimSpace(message.Text)

	state, exists := h.UserStates[userID]
	if !exists || state.Action != "adding_more" || state.Step != "entering_names" {
		return
	}

	currentIndex := state.TempData["current_index"].(int)
	totalCount := state.TempData["total_count"].(int)
	eventID := state.TempData["event_id"].(int)

	// Получаем существующих участников из текущей сессии
	additional := state.TempData["additional_players"].([]map[string]interface{})

	// Также получаем уже записанных участников из БД для проверки дубликатов
	var existingIdentified []map[string]interface{}

	// Получаем ID пользователя в БД
	var dbPersonID int
	_ = database.DB.QueryRow(`SELECT id FROM person WHERE telegram_id = $1`, userID).Scan(&dbPersonID)

	// Получаем существующую запись
	var identificationData []byte
	_ = database.DB.QueryRow(`
		SELECT COALESCE(identification_data, '[]'::JSONB)
		FROM person_event 
		WHERE person_id = $1 AND event_id = $2 AND status = 'registered'
	`, dbPersonID, eventID).Scan(&identificationData)

	if len(identificationData) > 0 {
		json.Unmarshal(identificationData, &existingIdentified)
	}

	// Проверяем, не является ли это специальным значением "себя"
	inputLower := strings.ToLower(text)
	if inputLower == "себя" || inputLower == "я" || inputLower == "меня" {
		// Это сам пользователь
		var nikname, firstname, lastname string
		_ = database.DB.QueryRow(`
			SELECT nikname, firstname, lastname FROM person WHERE telegram_id = $1
		`, userID).Scan(&nikname, &firstname, &lastname)

		telegramUsername := ""
		if nikname != "" {
			telegramUsername = "@" + nikname
		}

		fullName := fmt.Sprintf("%s %s", firstname, lastname)
		if strings.TrimSpace(fullName) == "" {
			if nikname != "" {
				fullName = nikname
			} else {
				fullName = "Пользователь"
			}
		}

		// Проверяем на дубликат в текущей сессии
		for _, p := range additional {
			if pNick, ok := p["telegram_nick"].(string); ok && pNick == telegramUsername {
				h.Bot.Send(tgbotapi.NewMessage(chatID,
					"⚠️ Вы уже добавлены в список в этой сессии"))
				h.askAdditionalNames(chatID, eventID, userID, totalCount, currentIndex)
				return
			}
		}

		// Проверяем на дубликат в БД
		for _, p := range existingIdentified {
			if pNick, ok := p["telegram_nick"].(string); ok && pNick == telegramUsername {
				h.Bot.Send(tgbotapi.NewMessage(chatID,
					"⚠️ Вы уже записаны на это событие"))
				h.askAdditionalNames(chatID, eventID, userID, totalCount, currentIndex)
				return
			}
		}

		// Добавляем нового участника
		additional = append(additional, map[string]interface{}{
			"player_id":     0,
			"input":         text,
			"full_name":     fullName,
			"telegram_nick": telegramUsername,
		})
		state.TempData["additional_players"] = additional

		// Переходим к следующему или завершаем
		if currentIndex+1 < totalCount {
			h.askAdditionalNames(chatID, eventID, userID, totalCount, currentIndex+1)
		} else {
			// Все собрали, сразу добавляем
			h.addMoreParticipants(chatID, eventID, userID, additional)
			delete(h.UserStates, userID)
		}
		return
	}

	// Ищем игрока в базе players
	results, err := utils.FindPlayer(database.DB, text)
	if err != nil {
		log.Printf("❌ Ошибка поиска игрока: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при поиске. Попробуйте еще раз."))
		return
	}

	if len(results) == 0 {
		// Игрок не найден - проверяем на дубликат по тексту
		for _, p := range additional {
			if pInput, ok := p["input"].(string); ok && pInput == text {
				h.Bot.Send(tgbotapi.NewMessage(chatID,
					fmt.Sprintf("⚠️ Игрок '%s' уже добавлен в этой сессии", text)))
				h.askAdditionalNames(chatID, eventID, userID, totalCount, currentIndex)
				return
			}
		}

		// Сохраняем как есть
		additional = append(additional, map[string]interface{}{
			"player_id": 0,
			"input":     text,
			"full_name": text,
		})
		state.TempData["additional_players"] = additional

		// Переходим к следующему или завершаем
		if currentIndex+1 < totalCount {
			h.askAdditionalNames(chatID, eventID, userID, totalCount, currentIndex+1)
		} else {
			// Все собрали, сразу добавляем
			h.addMoreParticipants(chatID, eventID, userID, additional)
			delete(h.UserStates, userID)
		}
		return
	}

	if len(results) == 1 {
		// Найден один игрок - проверяем на дубликат
		r := results[0]

		// Проверяем в текущей сессии
		for _, p := range additional {
			if pID, ok := p["player_id"].(int); ok && pID == r.ID {
				h.Bot.Send(tgbotapi.NewMessage(chatID,
					fmt.Sprintf("⚠️ Игрок %s уже добавлен в этой сессии", r.FullName)))
				h.askAdditionalNames(chatID, eventID, userID, totalCount, currentIndex)
				return
			}
			if pNick, ok := p["telegram_nick"].(string); ok && pNick == r.TelegramNick {
				h.Bot.Send(tgbotapi.NewMessage(chatID,
					fmt.Sprintf("⚠️ Игрок %s уже добавлен в этой сессии", r.FullName)))
				h.askAdditionalNames(chatID, eventID, userID, totalCount, currentIndex)
				return
			}
		}

		// Проверяем в БД
		for _, p := range existingIdentified {
			if pID, ok := p["player_id"].(float64); ok && int(pID) == r.ID {
				h.Bot.Send(tgbotapi.NewMessage(chatID,
					fmt.Sprintf("⚠️ Игрок %s уже записан на это событие", r.FullName)))
				h.askAdditionalNames(chatID, eventID, userID, totalCount, currentIndex)
				return
			}
			if pNick, ok := p["telegram_nick"].(string); ok && pNick == r.TelegramNick {
				h.Bot.Send(tgbotapi.NewMessage(chatID,
					fmt.Sprintf("⚠️ Игрок %s уже записан на это событие", r.FullName)))
				h.askAdditionalNames(chatID, eventID, userID, totalCount, currentIndex)
				return
			}
		}

		// Добавляем нового участника
		additional = append(additional, map[string]interface{}{
			"player_id":     r.ID,
			"input":         text,
			"full_name":     r.FullName,
			"telegram_nick": r.TelegramNick,
			"telegram_name": r.TelegramName,
		})
		state.TempData["additional_players"] = additional

		// Переходим к следующему или завершаем
		if currentIndex+1 < totalCount {
			h.askAdditionalNames(chatID, eventID, userID, totalCount, currentIndex+1)
		} else {
			// Все собрали, сразу добавляем
			h.addMoreParticipants(chatID, eventID, userID, additional)
			delete(h.UserStates, userID)
		}
		return
	}

	// Найдено несколько вариантов - показываем для выбора
	msg := fmt.Sprintf("🔍 Найдено несколько вариантов для '%s'. Выберите нужного:\n\n", text)

	var buttons [][]tgbotapi.InlineKeyboardButton
	for i, r := range results {
		buttonText := fmt.Sprintf("%d. %s", i+1, r.FullName)
		if r.TelegramNick != "" {
			buttonText += fmt.Sprintf(" (%s)", r.TelegramNick)
		}
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(buttonText,
				fmt.Sprintf("select_additional:%d:%d:%d", currentIndex, r.ID, totalCount)),
		))
	}
	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("➕ Оставить как есть",
			fmt.Sprintf("select_additional_keep:%d:%s", currentIndex, text)),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)
	msgObj := tgbotapi.NewMessage(chatID, msg)
	msgObj.ReplyMarkup = keyboard
	h.Bot.Send(msgObj)

	state.Step = "selecting"
}

// handleAdditionalSelection обрабатывает выбор из нескольких вариантов
func (h *CallbackHandler) handleAdditionalSelection(callback *tgbotapi.CallbackQuery, data []string) {
	if len(data) < 3 {
		return
	}

	userID := callback.From.ID
	chatID := callback.Message.Chat.ID

	state, exists := h.UserStates[userID]
	if !exists || state.Action != "adding_more" {
		return
	}

	eventID := state.TempData["event_id"].(int)
	totalCount := state.TempData["total_count"].(int)

	// Получаем существующих участников из текущей сессии
	additional := state.TempData["additional_players"].([]map[string]interface{})

	// Получаем уже записанных участников из БД
	var existingIdentified []map[string]interface{}
	var dbPersonID int
	_ = database.DB.QueryRow(`SELECT id FROM person WHERE telegram_id = $1`, userID).Scan(&dbPersonID)

	var identificationData []byte
	_ = database.DB.QueryRow(`
		SELECT COALESCE(identification_data, '[]'::JSONB)
		FROM person_event 
		WHERE person_id = $1 AND event_id = $2 AND status = 'registered'
	`, dbPersonID, eventID).Scan(&identificationData)

	if len(identificationData) > 0 {
		json.Unmarshal(identificationData, &existingIdentified)
	}

	switch data[0] {
	case "select_additional":
		if len(data) < 4 {
			return
		}
		idx, _ := strconv.Atoi(data[1])
		playerID, _ := strconv.Atoi(data[2])

		// Проверяем на дубликат
		for _, p := range additional {
			if pID, ok := p["player_id"].(int); ok && pID == playerID {
				h.Bot.Send(tgbotapi.NewMessage(chatID,
					"⚠️ Этот игрок уже добавлен в этой сессии"))
				h.askAdditionalNames(chatID, eventID, userID, totalCount, idx)
				return
			}
		}

		for _, p := range existingIdentified {
			if pID, ok := p["player_id"].(float64); ok && int(pID) == playerID {
				h.Bot.Send(tgbotapi.NewMessage(chatID,
					"⚠️ Этот игрок уже записан на событие"))
				h.askAdditionalNames(chatID, eventID, userID, totalCount, idx)
				return
			}
		}

		// Получаем информацию об игроке
		var player struct {
			FullName     string
			TelegramNick sql.NullString
			TelegramName sql.NullString
		}
		database.DB.QueryRow(`
			SELECT full_name, telegram_nick, telegram_name
			FROM players WHERE id = $1
		`, playerID).Scan(&player.FullName, &player.TelegramNick, &player.TelegramName)

		additional = append(additional, map[string]interface{}{
			"player_id":     playerID,
			"full_name":     player.FullName,
			"telegram_nick": player.TelegramNick.String,
			"telegram_name": player.TelegramName.String,
		})
		state.TempData["additional_players"] = additional

		// Переходим к следующему или завершаем
		if idx+1 < totalCount {
			h.askAdditionalNames(chatID, eventID, userID, totalCount, idx+1)
		} else {
			// Все собрали, сразу добавляем
			h.addMoreParticipants(chatID, eventID, userID, additional)
			delete(h.UserStates, userID)
		}

	case "select_additional_keep":
		if len(data) < 3 {
			return
		}
		idx, _ := strconv.Atoi(data[1])
		text := data[2]

		// Проверяем на дубликат по тексту
		for _, p := range additional {
			if pInput, ok := p["input"].(string); ok && pInput == text {
				h.Bot.Send(tgbotapi.NewMessage(chatID,
					fmt.Sprintf("⚠️ Игрок '%s' уже добавлен в этой сессии", text)))
				h.askAdditionalNames(chatID, eventID, userID, totalCount, idx)
				return
			}
		}

		additional = append(additional, map[string]interface{}{
			"player_id": 0,
			"full_name": text,
			"input":     text,
		})
		state.TempData["additional_players"] = additional

		// Переходим к следующему или завершаем
		if idx+1 < totalCount {
			h.askAdditionalNames(chatID, eventID, userID, totalCount, idx+1)
		} else {
			// Все собрали, сразу добавляем
			h.addMoreParticipants(chatID, eventID, userID, additional)
			delete(h.UserStates, userID)
		}
	}
}

// addMoreParticipants добавляет дополнительных участников
func (h *CallbackHandler) addMoreParticipants(chatID int64, eventID int, userID int64, additional []map[string]interface{}) {
	log.Printf("➕ Дозапись %d человек на событие %d от пользователя %d", len(additional), eventID, userID)

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

	newCount := len(additional)
	if registered+newCount > memberLimit {
		available := memberLimit - registered
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("❌ Недостаточно мест. Свободно: %d", available)))
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

	// Получаем существующую запись
	var existingID int
	var existingParticipantsInfo sql.NullString
	var existingPlayerIDs []int64
	var existingIdentificationData []byte
	var existingCount int

	err = tx.QueryRow(`
		SELECT id, participants_count, participants_info, COALESCE(player_ids, ARRAY[]::INTEGER[]), COALESCE(identification_data, '[]'::JSONB)
		FROM person_event 
		WHERE person_id = $1 AND event_id = $2 AND status = 'registered'
	`, dbPersonID, eventID).Scan(&existingID, &existingCount, &existingParticipantsInfo, pq.Array(&existingPlayerIDs), &existingIdentificationData)

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

	// Добавляем новых участников
	var newPlayerIDs []int64
	for _, p := range additional {
		existingIdentified = append(existingIdentified, p)
		if pid, ok := p["player_id"].(int); ok && pid > 0 {
			newPlayerIDs = append(newPlayerIDs, int64(pid))
		}
	}

	// Объединяем player_ids
	allPlayerIDs := append(existingPlayerIDs, newPlayerIDs...)

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
	newTotalCount := len(existingIdentified)

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
	`, newTotalCount, newParticipantsInfo, pq.Array(allPlayerIDs), updatedJSON, existingID)

	if err != nil {
		log.Printf("❌ Ошибка обновления записи: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при дозаписи"))
		return
	}

	// Подтверждаем транзакцию
	if err = tx.Commit(); err != nil {
		log.Printf("❌ Ошибка сохранения: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка сохранения"))
		return
	}

	// Формируем сообщение об успехе
	successMsg := fmt.Sprintf("✅ Вы успешно добавили участников на *%s* (%s %s)!\n\n",
		escapeMarkdown(eventName),
		escapeMarkdown(eventDateTime.Format("02.01.2006")),
		escapeMarkdown(eventDateTime.Format("15:04")))
	successMsg += "📋 *Добавлены:*\n"

	for i, p := range additional {
		if pid, ok := p["player_id"].(int); ok && pid > 0 {
			successMsg += fmt.Sprintf("%d. ✅ %s\n", i+1, p["full_name"])
		} else {
			successMsg += fmt.Sprintf("%d. ⚠️ %s\n", i+1, p["full_name"])
		}
	}

	log.Printf("✅ Успешная дозапись на событие %d", eventID)

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

// ==================== ФУНКЦИИ ДЛЯ АДМИНИСТРИРОВАНИЯ ====================

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

	// НОВЫЕ CASE ДЛЯ УПРАВЛЕНИЯ ИГРОКАМИ
	case "players_menu":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showPlayersMenu(chatID)

	case "view_player":
		if len(data) < 3 {
			return
		}
		playerID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showPlayerDetails(chatID, playerID)

	case "edit_player":
		if len(data) < 3 {
			return
		}
		playerID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.startEditPlayer(chatID, userID, playerID)

	case "ban_player":
		if len(data) < 3 {
			return
		}
		playerID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.banPlayer(chatID, playerID)

	case "unban_player":
		if len(data) < 3 {
			return
		}
		playerID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.unbanPlayer(chatID, playerID)

	case "player_history":
		if len(data) < 3 {
			return
		}
		playerID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showPlayerHistory(chatID, playerID)

	case "add_player":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.startAddPlayer(&tgbotapi.Message{
			Chat: &tgbotapi.Chat{ID: chatID},
			From: &tgbotapi.User{ID: userID},
		})

	case "search_player":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.searchPlayer(&tgbotapi.Message{
			Chat: &tgbotapi.Chat{ID: chatID},
			From: &tgbotapi.User{ID: userID},
		})

	case "back_to_players_list":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showPlayersList(chatID, 1, "")

	case "back_to_players_menu":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showPlayersMenu(chatID)

	// НОВЫЕ CASE ДЛЯ УПРАВЛЕНИЯ ОПЛАТАМИ
	case "payment_menu":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showPaymentManagement(chatID)

	case "payment_event":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showEventPayments(chatID, eventID)

	case "all_unpaid":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showAllUnpaid(chatID)

	case "mark_paid":
		if len(data) < 3 {
			return
		}
		personEventID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.markAsPaid(chatID, personEventID)
		msgHandler.showPaymentManagement(chatID)

	case "mark_participant_paid":
		if len(data) < 3 {
			return
		}
		participantKey := data[2]
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)

		// Получаем eventID из participantKey (формат: "peID_idx")
		parts := strings.Split(participantKey, "_")
		if len(parts) == 2 {
			peID, _ := strconv.Atoi(parts[0])
			// Получаем eventID по person_event_id
			var evID int
			_ = database.DB.QueryRow(`SELECT event_id FROM person_event WHERE id = $1`, peID).Scan(&evID)

			msgHandler.markParticipantAsPaid(chatID, participantKey)
			// Обновляем отображение с правильным eventID
			if evID > 0 {
				msgHandler.showEventPayments(chatID, evID)
			} else {
				msgHandler.showPaymentManagement(chatID)
			}
		}
		return

	case "mark_all_paid":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.markAllAsPaid(chatID, eventID)
		msgHandler.showEventPayments(chatID, eventID)

	case "back_to_payments":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showPaymentManagement(chatID)

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
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("👥 Управление игроками"),
			tgbotapi.NewKeyboardButton("💰 Управление оплатами"),
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
//func (h *CallbackHandler) showEventRegistrations(chatID int64, eventID int) {
//	rows, err := database.DB.Query(`
//		SELECT p.nikname, p.firstname, p.lastname, pe.participants_count, pe.participants_info, pe.registered_at
//		FROM person_event pe
//		JOIN person p ON pe.person_id = p.id
//		WHERE pe.event_id = $1 AND pe.status = 'registered'
//		ORDER BY pe.registered_at
//	`, eventID)
//
//	if err != nil {
//		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
//		return
//	}
//	defer rows.Close()
//
//	var eventInfo struct {
//		Name     string
//		DateTime time.Time
//		Limit    int
//	}
//	database.DB.QueryRow(`
//		SELECT c.name, e.evn_datetime, e.member_limit
//		FROM event e
//		JOIN category c ON e.category_id = c.id
//		WHERE e.id = $1
//	`, eventID).Scan(&eventInfo.Name, &eventInfo.DateTime, &eventInfo.Limit)
//
//	text := fmt.Sprintf("📊 Записи на событие *%s*\n"+
//		"📅 %s\n"+
//		"👥 Лимит: %d\n\n",
//		eventInfo.Name,
//		eventInfo.DateTime.Format("02.01.2006 15:04"),
//		eventInfo.Limit,
//	)
//
//	count := 0
//	totalParticipants := 0
//	for rows.Next() {
//		count++
//		var nikname, first, last string
//		var participants int
//		var participantsInfo sql.NullString
//		var regTime time.Time
//		err := rows.Scan(&nikname, &first, &last, &participants, &participantsInfo, &regTime)
//		if err != nil {
//			log.Printf("❌ Ошибка сканирования: %v", err)
//			continue
//		}
//		totalParticipants += participants
//
//		userName := nikname
//		if first != "" {
//			userName = first + " " + last
//		}
//
//		text += fmt.Sprintf("%d. *%s* - %d чел.\n", count, userName, participants)
//		if participantsInfo.Valid && participantsInfo.String != "" && participantsInfo.String != fmt.Sprintf("%d человек", participants) {
//			text += fmt.Sprintf("   📋 %s\n", participantsInfo.String)
//		}
//		text += fmt.Sprintf("   📅 %s\n\n", regTime.Format("02.01 15:04"))
//	}
//
//	if count == 0 {
//		text += "\n❌ Нет записей"
//	} else {
//		text += fmt.Sprintf("\n📊 Всего записей: %d\n", count)
//		text += fmt.Sprintf("👥 Всего участников: %d\n", totalParticipants)
//	}
//
//	msg := tgbotapi.NewMessage(chatID, text)
//	msg.ParseMode = "Markdown"
//	h.Bot.Send(msg)
//}
