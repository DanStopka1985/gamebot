package handlers

import (
	"fmt"
	"log"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"gamebot/database"
)

// ==================== ФУНКЦИИ ДЛЯ НАПОМИНАНИЙ ====================

// askAboutReminder спрашивает, нужно ли напоминание
func (h *CallbackHandler) askAboutReminder(chatID int64, eventID int, userID int64, personEventID int) {
	log.Printf("🔔 Запрос о напоминании для записи %d", personEventID)

	// Получаем информацию о событии из базы данных
	var eventName string
	var eventDateTime time.Time

	err := database.DB.QueryRow(`
		SELECT c.name, e.evn_datetime
		FROM person_event pe
		JOIN event e ON pe.event_id = e.id
		JOIN category c ON e.category_id = c.id
		WHERE pe.id = $1
	`, personEventID).Scan(&eventName, &eventDateTime)

	if err != nil {
		log.Printf("❌ Ошибка загрузки события: %v", err)
		return
	}

	text := fmt.Sprintf(
		"🔔 *Напоминание о событии*\n\n"+
			"Хотите получить напоминание за час до начала *%s* (%s %s)?",
		escapeMarkdown(eventName),
		escapeMarkdown(eventDateTime.Format("02.01.2006")),
		escapeMarkdown(eventDateTime.Format("15:04")))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Да", fmt.Sprintf("reminder_yes:%d", eventID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Нет", fmt.Sprintf("reminder_no:%d", eventID)),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// handleReminderChoice обрабатывает выбор о напоминании
func (h *CallbackHandler) handleReminderChoice(callback *tgbotapi.CallbackQuery, data []string) {
	log.Printf("🔔 handleReminderChoice: data=%v", data)

	if len(data) < 2 {
		log.Printf("❌ Недостаточно данных в callback")
		return
	}

	eventID, _ := strconv.Atoi(data[1])
	userID := callback.From.ID
	chatID := callback.Message.Chat.ID

	// Получаем person_event_id из базы данных по event_id и user_id
	var dbPersonID int
	err := database.DB.QueryRow(`SELECT id FROM person WHERE telegram_id = $1`, userID).Scan(&dbPersonID)
	if err != nil {
		log.Printf("❌ Пользователь не найден: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Пользователь не найден"))
		return
	}

	var personEventID int
	var eventDateTime time.Time
	var eventName string

	err = database.DB.QueryRow(`
		SELECT pe.id, e.evn_datetime, c.name
		FROM person_event pe
		JOIN event e ON pe.event_id = e.id
		JOIN category c ON e.category_id = c.id
		WHERE pe.person_id = $1 AND pe.event_id = $2 AND pe.status = 'registered'
	`, dbPersonID, eventID).Scan(&personEventID, &eventDateTime, &eventName)

	if err != nil {
		log.Printf("❌ Запись не найдена: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Запись на событие не найдена"))
		return
	}

	log.Printf("🔔 Найдена запись: personEventID=%d, eventDateTime=%v", personEventID, eventDateTime)

	if data[0] == "reminder_yes" {
		// Устанавливаем напоминание за час до события
		remindAt := eventDateTime.Add(-1 * time.Hour)
		log.Printf("🔔 remindAt=%v", remindAt)

		// Если событие уже скоро или прошло, не устанавливаем напоминание
		if remindAt.Before(time.Now()) {
			log.Printf("⚠️ Событие слишком скоро, remindAt=%v, now=%v", remindAt, time.Now())
			h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Событие слишком скоро, напоминание не будет установлено"))
		} else {
			// Проверяем, нет ли уже напоминания
			var exists bool
			err = database.DB.QueryRow(`
				SELECT EXISTS(SELECT 1 FROM reminders WHERE person_event_id = $1)
			`, personEventID).Scan(&exists)

			if err != nil {
				log.Printf("❌ Ошибка проверки существующего напоминания: %v", err)
			}

			if exists {
				log.Printf("⚠️ Напоминание уже существует для person_event_id=%d", personEventID)
				h.Bot.Send(tgbotapi.NewMessage(chatID, "⚠️ Напоминание уже было установлено ранее"))
			} else {
				log.Printf("📝 Вставляем напоминание: person_event_id=%d, chat_id=%d, remind_at=%v",
					personEventID, chatID, remindAt)

				_, err := database.DB.Exec(`
					INSERT INTO reminders (person_event_id, chat_id, remind_at)
					VALUES ($1, $2, $3)
				`, personEventID, chatID, remindAt)

				if err != nil {
					log.Printf("❌ Ошибка создания напоминания: %v", err)
					h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при установке напоминания"))
				} else {
					log.Printf("✅ Напоминание успешно создано")
					h.Bot.Send(tgbotapi.NewMessage(chatID, "✅ Напоминание установлено! Я напомню вам за час до события."))
				}
			}
		}
	} else {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "✅ Хорошо, напоминать не буду."))
	}

	// Удаляем состояние, если оно еще есть
	delete(h.UserStates, userID)

	// Показываем обновленные детали события
	h.showEventDetails(chatID, eventID, userID)
}

// StartReminderChecker запускает проверку напоминаний
func (h *CallbackHandler) StartReminderChecker() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		h.checkAndSendReminders()
	}
}

// checkAndSendReminders проверяет и отправляет напоминания
func (h *CallbackHandler) checkAndSendReminders() {
	// Ищем напоминания, которые нужно отправить
	rows, err := database.DB.Query(`
		SELECT r.id, r.chat_id, e.evn_datetime, c.name
		FROM reminders r
		JOIN person_event pe ON r.person_event_id = pe.id
		JOIN event e ON pe.event_id = e.id
		JOIN category c ON e.category_id = c.id
		WHERE r.remind_at <= NOW() AND r.sent = FALSE
	`)

	if err != nil {
		log.Printf("❌ Ошибка поиска напоминаний: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var reminderID int
		var chatID int64
		var eventDateTime time.Time
		var categoryName string

		err := rows.Scan(&reminderID, &chatID, &eventDateTime, &categoryName)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		// Отправляем напоминание
		text := fmt.Sprintf(
			"🔔 *Напоминание!*\n\n"+
				"Через час начинается событие *%s* в %s!\n\n"+
				"Не пропустите!",
			categoryName,
			eventDateTime.Format("15:04"))

		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = "Markdown"

		// Добавляем кнопку для быстрого перехода к событию
		var eventID int
		database.DB.QueryRow(`
			SELECT event_id FROM person_event WHERE id = $1
		`, reminderID).Scan(&eventID)

		if eventID > 0 {
			keyboard := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("📋 Посмотреть событие", fmt.Sprintf("event:%d", eventID)),
				),
			)
			msg.ReplyMarkup = keyboard
		}

		_, err = h.Bot.Send(msg)
		if err != nil {
			log.Printf("❌ Ошибка отправки напоминания: %v", err)
			continue
		}

		// Отмечаем как отправленное
		_, err = database.DB.Exec(`UPDATE reminders SET sent = TRUE WHERE id = $1`, reminderID)
		if err != nil {
			log.Printf("❌ Ошибка обновления статуса напоминания: %v", err)
		}
	}
}
