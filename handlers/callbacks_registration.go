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

// ==================== ФУНКЦИИ ДЛЯ РЕГИСТРАЦИИ НА СОБЫТИЯ ====================

// askParticipantsCount запрашивает количество участников
func (h *CallbackHandler) askParticipantsCount(chatID int64, eventID int, userID int64) {
	log.Printf("📊 Запрос количества для события %d", eventID)

	var eventName string
	var eventDateTime time.Time
	err := database.DB.QueryRow(`
		SELECT c.name, e.evn_datetime
		FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&eventName, &eventDateTime)

	if err != nil {
		eventName = "Событие"
		eventDateTime = time.Now()
	}

	var dbPersonID int
	err = database.DB.QueryRow(`SELECT id FROM person WHERE telegram_id = $1`, userID).Scan(&dbPersonID)
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Пользователь не найден. Напишите /start"))
		return
	}

	// Проверяем, есть ли уже активная запись
	var existingID int
	var existingStatus string
	var participantsCount int
	var identificationData []byte
	err = database.DB.QueryRow(`
		SELECT id, status, participants_count, identification_data
		FROM person_event 
		WHERE person_id = $1 AND event_id = $2
	`, dbPersonID, eventID).Scan(&existingID, &existingStatus, &participantsCount, &identificationData)

	hasExisting := (err == nil)

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

	// Если есть активная запись
	if hasExisting && existingStatus == "registered" {
		// Показываем информацию о текущей записи
		text := fmt.Sprintf("📅 *%s* (%s %s)\n\n",
			escapeMarkdown(eventName),
			escapeMarkdown(eventDateTime.Format("02.01.2006")),
			escapeMarkdown(eventDateTime.Format("15:04")))

		text += "✅ *Вы уже записаны на это событие!*\n\n"

		// Показываем текущих участников
		if len(identificationData) > 0 {
			var identified []map[string]interface{}
			if err := json.Unmarshal(identificationData, &identified); err == nil {
				text += fmt.Sprintf("📋 *Текущие участники (%d чел.):*\n", len(identified))
				for i, id := range identified {
					if pid, ok := id["player_id"].(float64); ok && pid > 0 {
						text += fmt.Sprintf("  %d. ✅ %s\n", i+1, id["full_name"])
					} else {
						text += fmt.Sprintf("  %d. ⚠️ %s\n", i+1, id["full_name"])
					}
				}
			}
		} else {
			text += fmt.Sprintf("👥 *Количество:* %d чел.\n", participantsCount)
		}

		text += fmt.Sprintf("\n📊 *Свободно мест:* %d\n", available)

		// Предлагаем варианты
		var rows [][]tgbotapi.InlineKeyboardButton

		if available > 0 {
			rows = append(rows, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("➕ Добавить еще участников", fmt.Sprintf("add_more:%d", eventID)),
			))
		}

		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Отменить запись", fmt.Sprintf("cancel_reg:%d", eventID)),
		))

		keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = keyboard
		h.Bot.Send(msg)
		return
	}

	// Если нет записи - обычный процесс регистрации
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

	msgText := fmt.Sprintf("📅 *%s* (%s %s)\nСвободно мест: %d\n\nВыберите количество участников:",
		escapeMarkdown(eventName),
		escapeMarkdown(eventDateTime.Format("02.01.2006")),
		escapeMarkdown(eventDateTime.Format("15:04")),
		available)
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

	// Переходим к запросу имен (идентификации)
	// Создаем временные inputs для идентификации
	inputs := make([]string, count)
	for i := 0; i < count; i++ {
		inputs[i] = fmt.Sprintf("Участник %d", i+1)
	}

	// Сохраняем состояние для идентификации
	h.UserStates[userID] = &models.UserState{
		Action: "entering_names",
		Step:   "awaiting_names",
		TempData: map[string]interface{}{
			"event_id":           eventID,
			"count":              count,
			"inputs":             inputs,
			"identified_players": []map[string]interface{}{},
		},
	}

	// Начинаем процесс идентификации
	h.identifyNextPlayer(chatID, userID, 0)
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

		// После отмены показываем обновленную информацию о событии
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

	// Проверяем, есть ли у пользователя запись на это событие
	var dbPersonID int
	database.DB.QueryRow(`SELECT id FROM person WHERE telegram_id = $1`, userID).Scan(&dbPersonID)

	var existingID int
	var registrationStatus string
	var participantsCount int
	var paymentStatus string

	err = database.DB.QueryRow(`
		SELECT id, status, participants_count, COALESCE(payment_status, 'pending')
		FROM person_event 
		WHERE person_id = $1 AND event_id = $2
	`, dbPersonID, eventID).Scan(&existingID, &registrationStatus, &participantsCount, &paymentStatus)

	isRegistered := (err == nil && registrationStatus == "registered")

	// Получаем ВСЕХ участников события из всех записей
	rows, err := database.DB.Query(`
		SELECT 
			pe.id as person_event_id,
			p.nikname,
			p.firstname,
			p.lastname,
			pe.identification_data,
			pe.payment_status
		FROM person_event pe
		JOIN person p ON pe.person_id = p.id
		WHERE pe.event_id = $1 AND pe.status = 'registered'
		ORDER BY pe.registered_at
	`, eventID)

	if err != nil {
		log.Printf("❌ Ошибка загрузки участников: %v", err)
	} else {
		defer rows.Close()
	}

	// Формируем заголовок
	text := fmt.Sprintf(
		"📅 *%s*\n\n"+
			"📆 Дата: %s\n"+
			"👥 Всего записано: %d/%d\n"+
			"📊 Свободно: %d\n\n",
		e.CategoryName,
		e.DateTime.Format("02.01.2006 15:04"),
		e.Registered,
		e.MemberLimit,
		e.MemberLimit-e.Registered,
	)

	// Если у пользователя есть запись, показываем его статус
	if isRegistered {
		paymentEmoji := "⏳"
		if paymentStatus == "paid" {
			paymentEmoji = "💰"
		}
		text += fmt.Sprintf("%s *Ваша запись:* %d чел.\n", paymentEmoji, participantsCount)
	}

	// Показываем ВСЕХ участников по группам (кто записал)
	text += "\n📋 *Все участники:*\n\n"

	participantIndex := 1
	for rows.Next() {
		var peID int
		var nikname, firstname, lastname string
		var identificationData []byte
		var pePaymentStatus string

		err := rows.Scan(&peID, &nikname, &firstname, &lastname, &identificationData, &pePaymentStatus)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		// Кто записал
		registrantName := ""
		if nikname != "" {
			registrantName = "@" + nikname
		}
		if firstname != "" || lastname != "" {
			if registrantName != "" {
				registrantName += " "
			}
			registrantName += firstname + " " + lastname
		}
		if registrantName == "" {
			registrantName = fmt.Sprintf("ID: %d", peID)
		}

		text += fmt.Sprintf("👤 *Записал:* %s\n", registrantName)

		// Получаем участников из identification_data
		if len(identificationData) > 0 {
			var identified []map[string]interface{}
			if err := json.Unmarshal(identificationData, &identified); err == nil {
				for _, participant := range identified {
					// Иконка идентификации
					idIcon := "⚠️"
					if pid, ok := participant["player_id"].(float64); ok && pid > 0 {
						idIcon = "✅"
					}

					// Иконка оплаты
					payIcon := "⏳"
					if pePaymentStatus == "paid" {
						payIcon = "💰"
					}

					fullName := "Неизвестно"
					if fn, ok := participant["full_name"].(string); ok {
						fullName = fn
					}

					text += fmt.Sprintf("  %d. %s %s %s\n", participantIndex, idIcon, payIcon, fullName)
					participantIndex++
				}
			}
		}
		text += "\n"
	}

	var keyboard tgbotapi.InlineKeyboardMarkup
	if isRegistered {
		// Если пользователь уже записан - показываем кнопки отмены и дозаписи
		keyboard = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("❌ Отменить запись", fmt.Sprintf("cancel_reg:%d", eventID)),
				tgbotapi.NewInlineKeyboardButtonData("➕ Добавить еще", fmt.Sprintf("add_more:%d", eventID)),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("💰 Отметить оплату", fmt.Sprintf("payment_mark:%d", eventID)),
			),
		)
	} else if e.Registered < e.MemberLimit {
		// Если есть свободные места и пользователь не записан - показываем кнопку записи
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
