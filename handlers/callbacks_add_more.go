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
