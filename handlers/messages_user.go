package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"gamebot/database"
	"gamebot/models"
)

// ==================== ПОЛЬЗОВАТЕЛЬСКИЕ ФУНКЦИИ ====================

// handleStart обрабатывает команду /start
func (h *MessageHandler) handleStart(message *tgbotapi.Message) {
	log.Printf("📝 Команда /start от пользователя %d с аргументом: %s",
		message.From.ID, message.CommandArguments())

	if len(message.CommandArguments()) > 0 {
		h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "📋 Загружаю список доступных событий..."))
		h.handleListEvents(message)
		return
	}

	text := `👋 Добро пожаловать в бот для записи на квизы!

Я помогаю записываться на события, которые публикуются в канале.

📌 **Как это работает:**
1. В канале под каждым постом есть кнопка "Записаться на квиз"
2. Нажмите на неё, чтобы перейти ко мне
3. Выберите событие и количество участников

📋 **Доступные команды:**
/events - список ближайших событий
/myevents - мои записи

👑 **Для администраторов:**
/admin - панель администратора`

	msg := tgbotapi.NewMessage(message.Chat.ID, text)
	msg.ParseMode = "Markdown"

	if _, err := h.Bot.Send(msg); err != nil {
		log.Printf("❌ Ошибка отправки start сообщения: %v", err)
	}
}

// handleListEvents показывает список событий
func (h *MessageHandler) handleListEvents(message *tgbotapi.Message) {
	log.Printf("📋 Запрос списка событий от пользователя %d", message.From.ID)

	rows, err := database.DB.Query(`
		SELECT e.id, c.name, e.evn_datetime, e.member_limit,
		       COALESCE((SELECT SUM(participants_count) FROM person_event WHERE event_id = e.id AND status = 'registered'), 0)
		FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.evn_datetime > NOW()
		ORDER BY e.evn_datetime
		LIMIT 10
	`)

	if err != nil {
		log.Printf("❌ Ошибка загрузки событий: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "❌ Ошибка загрузки событий"))
		return
	}
	defer rows.Close()

	var events []models.Event
	for rows.Next() {
		var e models.Event
		err := rows.Scan(&e.ID, &e.CategoryName, &e.DateTime, &e.MemberLimit, &e.Registered)
		if err != nil {
			log.Printf("❌ Ошибка сканирования события: %v", err)
			continue
		}
		events = append(events, e)
	}

	if len(events) == 0 {
		h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "📭 Ближайших событий нет"))
		return
	}

	for _, e := range events {
		h.showEventPreview(message.Chat.ID, e)
	}
}

// showEventPreview показывает предпросмотр события
func (h *MessageHandler) showEventPreview(chatID int64, e models.Event) {
	text := fmt.Sprintf(
		"📅 *%s*\n"+
			"📆 %s\n"+
			"👥 Записано: %d/%d\n",
		e.CategoryName,
		e.DateTime.Format("02.01.2006 15:04"),
		e.Registered,
		e.MemberLimit,
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📝 Записаться", fmt.Sprintf("register:%d", e.ID)),
			tgbotapi.NewInlineKeyboardButtonData("ℹ️ Подробнее", fmt.Sprintf("event:%d", e.ID)),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard

	if _, err := h.Bot.Send(msg); err != nil {
		log.Printf("❌ Ошибка отправки предпросмотра события %d: %v", e.ID, err)
	}
}

// handleMyEvents показывает события пользователя
func (h *MessageHandler) handleMyEvents(message *tgbotapi.Message) {
	log.Printf("📋 Запрос моих событий от пользователя %d", message.From.ID)

	var dbPersonID int
	err := database.DB.QueryRow(`SELECT id FROM person WHERE telegram_id = $1`, message.From.ID).Scan(&dbPersonID)
	if err != nil {
		log.Printf("❌ Пользователь не найден: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "❌ Пользователь не найден. Напишите /start"))
		return
	}

	rows, err := database.DB.Query(`
		SELECT e.id, c.name, e.evn_datetime, ue.participants_count, ue.participants_info
		FROM person_event ue
		JOIN event e ON ue.event_id = e.id
		JOIN category c ON e.category_id = c.id
		WHERE ue.person_id = $1 AND ue.status = 'registered' AND e.evn_datetime > NOW()
		ORDER BY e.evn_datetime
	`, dbPersonID)

	if err != nil {
		log.Printf("❌ Ошибка загрузки записей: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "❌ Ошибка загрузки"))
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
		var id int
		var name string
		var dt time.Time
		var participants int
		var participantsInfo sql.NullString
		err := rows.Scan(&id, &name, &dt, &participants, &participantsInfo)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		text := fmt.Sprintf(
			"📅 *%s*\n📆 %s\n👥 Записано: %d\n",
			name, dt.Format("02.01.2006 15:04"), participants,
		)

		if participantsInfo.Valid && participantsInfo.String != "" && participantsInfo.String != fmt.Sprintf("%d человек", participants) {
			text += fmt.Sprintf("📋 Участники: %s\n", participantsInfo.String)
		}

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", fmt.Sprintf("cancel_reg:%d", id)),
			),
		)

		msg := tgbotapi.NewMessage(message.Chat.ID, text)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = keyboard
		h.Bot.Send(msg)
	}

	if count == 0 {
		h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "📭 У вас нет активных записей"))
	} else {
		log.Printf("✅ Найдено %d активных записей", count)
	}
}
