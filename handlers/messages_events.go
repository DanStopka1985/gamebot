package handlers

import (
	"fmt"
	"log"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"gamebot/database"
	"gamebot/models"
)

// ==================== ФУНКЦИИ ДЛЯ СОБЫТИЙ ====================

// startAddEvent начинает процесс добавления события
func (h *MessageHandler) startAddEvent(message *tgbotapi.Message) {
	if !h.isAdmin(message.From.ID) {
		h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "⛔ Нет прав"))
		return
	}

	log.Printf("➕ Начало создания события администратором %d", message.From.ID)

	// Сначала спрашиваем тип события
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎮 Обычное", "admin:set_event_type:regular"),
			tgbotapi.NewInlineKeyboardButtonData("🔄 Опциональное", "admin:set_event_type:flexible"),
		),
	)

	msg := tgbotapi.NewMessage(message.Chat.ID, "Выберите тип события:")
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// handleAddEventInput обрабатывает ввод при создании события
func (h *MessageHandler) handleAddEventInput(message *tgbotapi.Message, state *models.UserState) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := message.Text

	log.Printf("📝 Создание события: шаг=%s, текст=%s", state.Step, text)

	switch state.Step {
	case "awaiting_datetime":
		// Пробуем разные форматы
		var datetime time.Time
		var err error

		// Устанавливаем локальный часовой пояс (Москва, UTC+3)
		loc, _ := time.LoadLocation("Europe/Moscow")

		// Сначала пробуем с секундами
		datetime, err = time.ParseInLocation("2006-01-02 15:04:05", text, loc)
		if err != nil {
			// Если не получилось, пробуем без секунд
			datetime, err = time.ParseInLocation("2006-01-02 15:04", text, loc)
		}

		if err != nil {
			log.Printf("❌ Неверный формат даты: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID,
				"❌ Неверный формат. Используйте: 2026-03-15 10:00 или 2026-03-15 10:00:00\nПопробуйте снова:"))
			return
		}

		// Конвертируем в UTC для хранения в БД
		datetimeUTC := datetime.UTC()

		state.TempData["datetime"] = datetimeUTC
		state.Step = "awaiting_limit"

		h.Bot.Send(tgbotapi.NewMessage(chatID,
			"Введите максимальное количество участников (число):"))

	case "awaiting_limit":
		limit, err := strconv.Atoi(text)
		if err != nil || limit < 1 {
			h.Bot.Send(tgbotapi.NewMessage(chatID,
				"❌ Введите положительное число:"))
			return
		}

		state.TempData["limit"] = limit

		datetime := state.TempData["datetime"].(time.Time)
		// Для отображения конвертируем обратно в локальный часовой пояс
		localTime := datetime.In(time.FixedZone("UTC+3", 3*60*60))

		preview := fmt.Sprintf(
			"📅 Предварительный просмотр события:\n\n"+
				"Категория ID: %d\n"+
				"📆 Дата: %s (по Москве)\n"+
				"👥 Лимит: %d\n\n"+
				"Подтвердить создание?",
			state.CategoryID,
			localTime.Format("02.01.2006 15:04:05"),
			limit,
		)

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Да", "admin:confirm_add"),
				tgbotapi.NewInlineKeyboardButtonData("❌ Нет", "admin:cancel_add"),
			),
		)

		msg := tgbotapi.NewMessage(chatID, preview)
		msg.ReplyMarkup = keyboard
		h.Bot.Send(msg)

		log.Printf("✅ Ожидание подтверждения создания события")

	default:
		log.Printf("⚠️ Неизвестный шаг: %s", state.Step)
		delete(h.UserStates, userID)
	}
}

// confirmAddEvent подтверждает создание события (вызывается из callback)
func (h *MessageHandler) confirmAddEvent(chatID int64, userID int64) {
	state, exists := h.UserStates[userID]
	if !exists {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Сессия истекла. Начните заново."))
		return
	}

	datetime, ok := state.TempData["datetime"].(time.Time)
	if !ok {
		delete(h.UserStates, userID)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка данных. Начните заново."))
		return
	}

	limit, ok := state.TempData["limit"].(int)
	if !ok {
		delete(h.UserStates, userID)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка данных. Начните заново."))
		return
	}

	eventType := "regular"
	if state.EventType != "" {
		eventType = state.EventType
	}

	var eventID int
	err := database.DB.QueryRow(`
		INSERT INTO event (category_id, evn_datetime, member_limit, event_type)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, state.CategoryID, datetime, limit, eventType).Scan(&eventID)

	if err != nil {
		log.Printf("❌ Ошибка создания события: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при создании события"))
		return
	}

	// Получаем название категории
	var categoryName string
	database.DB.QueryRow(`SELECT name FROM category WHERE id = $1`, state.CategoryID).Scan(&categoryName)

	// Для отображения конвертируем обратно в локальный часовой пояс
	loc, _ := time.LoadLocation("Europe/Moscow")
	localTime := datetime.In(loc)

	typeText := "🎮 Обычное"
	if eventType == "flexible" {
		typeText = "🔄 Опциональное (новички/общая)"
	}

	h.Bot.Send(tgbotapi.NewMessage(chatID,
		fmt.Sprintf("✅ Событие '%s' на %s успешно создано! ID: #%d\n%s",
			categoryName, localTime.Format("02.01.2006 15:04"), eventID, typeText)))

	delete(h.UserStates, userID)
}

// showAllEvents показывает все события для администратора
func (h *MessageHandler) showAllEvents(chatID int64) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("👑 Запрос всех событий от администратора %d", chatID)

	rows, err := database.DB.Query(`
		SELECT e.id, c.name, e.evn_datetime, e.member_limit, COALESCE(e.event_type, 'regular'),
		       COALESCE((SELECT SUM(participants_count) FROM person_event WHERE event_id = e.id AND status = 'registered'), 0)
		FROM event e
		JOIN category c ON e.category_id = c.id
		ORDER BY e.evn_datetime DESC
		LIMIT 20
	`)

	if err != nil {
		log.Printf("❌ Ошибка загрузки событий: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
		return
	}
	defer rows.Close()

	// Конвертируем время в локальный часовой пояс (Москва, UTC+3)
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.FixedZone("UTC+3", 3*60*60)
	}

	count := 0
	for rows.Next() {
		count++
		var e models.Event
		err := rows.Scan(&e.ID, &e.CategoryName, &e.DateTime, &e.MemberLimit, &e.EventType, &e.Registered)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		localDateTime := e.DateTime.In(loc)

		// Определяем иконку типа
		typeIcon := "🎮"
		if e.EventType == "flexible" {
			typeIcon = "🔄"
		}

		text := fmt.Sprintf(
			"%s *%d* | %s\n📆 %s\n👥 %d/%d\n",
			typeIcon, e.ID, e.CategoryName,
			localDateTime.Format("02.01.2006 15:04"),
			e.Registered, e.MemberLimit,
		)

		// Добавляем пояснение для опциональных событий
		if e.EventType == "flexible" {
			text += "🔄 *Опциональное* (новички/общая)\n"
		}

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✏️ Редактировать", fmt.Sprintf("admin:edit_event:%d", e.ID)),
				tgbotapi.NewInlineKeyboardButtonData("👥 Записи", fmt.Sprintf("admin:view_event_registrations:%d", e.ID)),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🗑 Удалить", fmt.Sprintf("admin:delete_event:%d", e.ID)),
			),
		)

		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = keyboard
		h.Bot.Send(msg)
	}

	if count == 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "📭 Нет событий"))
	} else {
		log.Printf("✅ Показано %d событий", count)
	}
}
