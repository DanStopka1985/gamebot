package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"gamebot/database"
	"gamebot/models"
	"gamebot/utils"
)

// MessageHandler обработчик текстовых сообщений
type MessageHandler struct {
	Bot        *tgbotapi.BotAPI
	AdminIDs   map[int64]bool
	UserStates map[int64]*models.UserState
}

// NewMessageHandler создает новый обработчик сообщений
func NewMessageHandler(bot *tgbotapi.BotAPI, adminIDs map[int64]bool, userStates map[int64]*models.UserState) *MessageHandler {
	return &MessageHandler{
		Bot:        bot,
		AdminIDs:   adminIDs,
		UserStates: userStates,
	}
}

// HandleMessage обрабатывает текстовые сообщения
func (h *MessageHandler) HandleMessage(message *tgbotapi.Message) {
	if h.Bot == nil {
		log.Println("❌ Ошибка: бот не инициализирован")
		return
	}

	userID := message.From.ID

	// Регистрация пользователя если новый
	utils.RegisterUserIfNotExists(database.DB, message.From)

	// Проверяем, есть ли у пользователя активное состояние
	if state, exists := h.UserStates[userID]; exists {
		// Для состояний, связанных с вводом имен и количества
		if state.Action == "entering_names" {
			// Создаем временный callbackHandler для обработки имен
			callbackHandler := NewCallbackHandler(h.Bot, h.AdminIDs, h.UserStates)
			callbackHandler.handleParticipantNames(message)
			return
		}
		if state.Action == "entering_custom_count" {
			callbackHandler := NewCallbackHandler(h.Bot, h.AdminIDs, h.UserStates)
			callbackHandler.handleCustomCount(message)
			return
		}
		h.handleUserInput(message)
		return
	}

	// Обработка обычных текстовых сообщений (не команд)
	if !message.IsCommand() {
		// Проверяем, является ли пользователь админом
		if utils.IsAdmin(h.AdminIDs, userID) {
			h.handleAdminText(message)
		}
		return
	}

	// Обработка команд
	if message.IsCommand() {
		switch message.Command() {
		case "start":
			h.handleStart(message)
		case "events":
			h.handleListEvents(message)
		case "myevents":
			h.handleMyEvents(message)
		default:
			if utils.IsAdmin(h.AdminIDs, userID) {
				h.handleAdminCommand(message)
			}
		}
		return
	}
}

// handleAdminText обрабатывает текстовые кнопки админ-меню
func (h *MessageHandler) handleAdminText(message *tgbotapi.Message) {
	text := message.Text
	chatID := message.Chat.ID
	userID := message.From.ID

	log.Printf("👑 Админское текстовое сообщение: %s от %d", text, userID)

	switch text {
	case "➕ Добавить событие":
		h.startAddEvent(message)
	case "📋 Все события":
		h.showAllEvents(chatID)
	case "📊 Статистика":
		h.showStats(chatID)
	case "👥 Все записи":
		h.showAllRegistrations(chatID)
	default:
		// Если текст не совпадает с кнопками, ничего не делаем
		log.Printf("❓ Неизвестная админская команда: %s", text)
	}
}

// showStats показывает общую статистику
func (h *MessageHandler) showStats(chatID int64) {
	log.Printf("📊 Запрос статистики от администратора %d", chatID)

	var totalEvents, totalUsers, totalRegistrations int
	var upcomingEvents int

	// Всего событий
	database.DB.QueryRow(`SELECT COUNT(*) FROM event`).Scan(&totalEvents)

	// Всего пользователей
	database.DB.QueryRow(`SELECT COUNT(*) FROM "user"`).Scan(&totalUsers)

	// Всего записей (активных)
	database.DB.QueryRow(`SELECT COUNT(*) FROM user_event WHERE status = 'registered'`).Scan(&totalRegistrations)

	// Предстоящих событий
	database.DB.QueryRow(`SELECT COUNT(*) FROM event WHERE evn_datetime > NOW()`).Scan(&upcomingEvents)

	text := fmt.Sprintf(
		"📊 *Общая статистика*\n\n"+
			"📅 Всего событий: %d\n"+
			"⏳ Предстоящих: %d\n"+
			"👥 Пользователей: %d\n"+
			"📝 Всего записей: %d",
		totalEvents, upcomingEvents, totalUsers, totalRegistrations)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	h.Bot.Send(msg)
}

// showAllRegistrations показывает все записи
func (h *MessageHandler) showAllRegistrations(chatID int64) {
	log.Printf("👥 Запрос всех записей от администратора %d", chatID)

	rows, err := database.DB.Query(`
		SELECT e.id, c.name, e.evn_datetime, u.nikname, u.firstname, u.lastname, ue.participants_count, ue.participants_info, ue.registered_at
		FROM user_event ue
		JOIN event e ON ue.event_id = e.id
		JOIN category c ON e.category_id = c.id
		JOIN "user" u ON ue.user_id = u.id
		WHERE ue.status = 'registered'
		ORDER BY ue.registered_at DESC
		LIMIT 20
	`)

	if err != nil {
		log.Printf("❌ Ошибка загрузки записей: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
		return
	}
	defer rows.Close()

	text := "📝 *Последние записи*\n\n"
	count := 0

	for rows.Next() {
		count++
		var eventID int
		var categoryName, nikname, first, last string
		var eventDate, regDate time.Time
		var participants int
		var participantsInfo sql.NullString

		err := rows.Scan(&eventID, &categoryName, &eventDate, &nikname, &first, &last, &participants, &participantsInfo, &regDate)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		userName := nikname
		if first != "" {
			userName = first + " " + last
		}

		text += fmt.Sprintf("*Событие #%d:* %s\n", eventID, categoryName)
		text += fmt.Sprintf("📆 %s\n", eventDate.Format("02.01.2006 15:04"))
		text += fmt.Sprintf("👤 %s - %d чел.\n", userName, participants)
		if participantsInfo.Valid && participantsInfo.String != "" && participantsInfo.String != fmt.Sprintf("%d человек", participants) {
			text += fmt.Sprintf("   📋 %s\n", participantsInfo.String)
		}
		text += fmt.Sprintf("📅 Запись: %s\n\n", regDate.Format("02.01.2006 15:04"))
	}

	if count == 0 {
		text += "Нет записей"
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	h.Bot.Send(msg)
}

// handleStart обрабатывает команду /start
func (h *MessageHandler) handleStart(message *tgbotapi.Message) {
	log.Printf("📝 Команда /start от пользователя %d с аргументом: %s",
		message.From.ID, message.CommandArguments())

	// Проверяем, есть ли параметр в команде (например, ?start=event)
	if len(message.CommandArguments()) > 0 {
		// Если пришли по ссылке из канала, показываем список событий
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
		       COALESCE((SELECT SUM(participants_count) FROM user_event WHERE event_id = e.id AND status = 'registered'), 0)
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
		log.Printf("📅 Загружено событие: ID=%d, Название=%s, Дата=%s, Мест=%d/%d",
			e.ID, e.CategoryName, e.DateTime.Format("02.01.2006 15:04"), e.Registered, e.MemberLimit)
	}

	if len(events) == 0 {
		h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "📭 Ближайших событий нет"))
		return
	}

	log.Printf("✅ Загружено %d событий", len(events))

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

	// Важно: используем "register" а не "event"
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
	} else {
		log.Printf("✅ Отправлен предпросмотр события %d", e.ID)
	}
}

// handleMyEvents показывает события пользователя
func (h *MessageHandler) handleMyEvents(message *tgbotapi.Message) {
	log.Printf("📋 Запрос моих событий от пользователя %d", message.From.ID)

	var dbUserID int
	err := database.DB.QueryRow(`SELECT id FROM "user" WHERE telegram_id = $1`, message.From.ID).Scan(&dbUserID)
	if err != nil {
		log.Printf("❌ Пользователь не найден: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "❌ Пользователь не найден. Напишите /start"))
		return
	}

	rows, err := database.DB.Query(`
		SELECT e.id, c.name, e.evn_datetime, ue.participants_count, ue.participants_info
		FROM user_event ue
		JOIN event e ON ue.event_id = e.id
		JOIN category c ON e.category_id = c.id
		WHERE ue.user_id = $1 AND ue.status = 'registered' AND e.evn_datetime > NOW()
		ORDER BY e.evn_datetime
	`, dbUserID)

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

// handleAdminCommand обрабатывает команды администратора
func (h *MessageHandler) handleAdminCommand(message *tgbotapi.Message) {
	log.Printf("👑 Админская команда от пользователя %d: %s", message.From.ID, message.Command())

	switch message.Command() {
	case "admin":
		h.showAdminMenu(message.Chat.ID)
	case "addevent":
		h.startAddEvent(message)
	case "allevents":
		h.showAllEvents(message.Chat.ID)
	case "eventstats":
		h.showEventStats(message.Chat.ID, message.CommandArguments())
	}
}

// showAdminMenu показывает меню администратора
func (h *MessageHandler) showAdminMenu(chatID int64) {
	log.Printf("👑 Показ меню администратора для чата %d", chatID)

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("➕ Добавить событие"),
			tgbotapi.NewKeyboardButton("📋 Все события"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 Статистика"),
			tgbotapi.NewKeyboardButton("👥 Все записи"),
		),
	)
	keyboard.ResizeKeyboard = true

	msg := tgbotapi.NewMessage(chatID, "👑 Панель администратора:")
	msg.ReplyMarkup = keyboard

	if _, err := h.Bot.Send(msg); err != nil {
		log.Printf("❌ Ошибка отправки меню администратора: %v", err)
	}
}

// handleUserInput обрабатывает ввод пользователя для многошаговых действий
func (h *MessageHandler) handleUserInput(message *tgbotapi.Message) {
	userID := message.From.ID

	state, exists := h.UserStates[userID]
	if !exists {
		log.Printf("⚠️ Нет активного состояния для пользователя %d", userID)
		return
	}

	log.Printf("📝 Обработка ввода пользователя %d: действие=%s, шаг=%s",
		userID, state.Action, state.Step)

	switch state.Action {
	case "add_event":
		h.handleAddEventInput(message, state)
	case "edit_event":
		h.handleEditEventInput(message, state)
	default:
		log.Printf("⚠️ Неизвестное действие: %s", state.Action)
		delete(h.UserStates, userID)
	}
}

// handleAddEventInput обрабатывает ввод при создании события
func (h *MessageHandler) handleAddEventInput(message *tgbotapi.Message, state *models.UserState) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := message.Text

	log.Printf("📝 Создание события: шаг=%s, текст=%s", state.Step, text)

	switch state.Step {
	case "awaiting_datetime":
		datetime, err := time.Parse("2006-01-02 15:04", text)
		if err != nil {
			log.Printf("❌ Неверный формат даты: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID,
				"❌ Неверный формат. Используйте: 2026-03-15 10:00\nПопробуйте снова:"))
			return
		}

		state.TempData["datetime"] = datetime
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
		preview := fmt.Sprintf(
			"📅 Предварительный просмотр события:\n\n"+
				"Категория ID: %d\n"+
				"📆 Дата: %s\n"+
				"👥 Лимит: %d\n\n"+
				"Подтвердить создание?",
			state.CategoryID,
			datetime.Format("02.01.2006 15:04"),
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

		// Не удаляем состояние, оно понадобится для подтверждения
		log.Printf("✅ Ожидание подтверждения создания события")

	default:
		log.Printf("⚠️ Неизвестный шаг: %s", state.Step)
		delete(h.UserStates, userID)
	}
}

// handleEditEventInput обрабатывает ввод при редактировании события
func (h *MessageHandler) handleEditEventInput(message *tgbotapi.Message, state *models.UserState) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := message.Text

	eventID, ok := state.TempData["event_id"].(int)
	if !ok {
		log.Printf("❌ Нет ID события в состоянии")
		delete(h.UserStates, userID)
		return
	}

	log.Printf("📝 Редактирование события %d: шаг=%s", eventID, state.Step)

	switch state.Step {
	case "awaiting_new_date":
		datetime, err := time.Parse("2006-01-02 15:04", text)
		if err != nil {
			h.Bot.Send(tgbotapi.NewMessage(chatID,
				"❌ Неверный формат. Используйте: 2026-03-15 10:00\nПопробуйте снова:"))
			return
		}

		_, err = database.DB.Exec(`UPDATE event SET evn_datetime = $1 WHERE id = $2`, datetime, eventID)
		if err != nil {
			log.Printf("❌ Ошибка обновления даты: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при обновлении даты"))
		} else {
			h.Bot.Send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("✅ Дата события #%d обновлена", eventID)))
		}

		delete(h.UserStates, userID)

	case "awaiting_new_limit":
		limit, err := strconv.Atoi(text)
		if err != nil || limit < 1 {
			h.Bot.Send(tgbotapi.NewMessage(chatID,
				"❌ Введите положительное число:"))
			return
		}

		_, err = database.DB.Exec(`UPDATE event SET member_limit = $1 WHERE id = $2`, limit, eventID)
		if err != nil {
			log.Printf("❌ Ошибка обновления лимита: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при обновлении лимита"))
		} else {
			h.Bot.Send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("✅ Лимит события #%d обновлен", eventID)))
		}

		delete(h.UserStates, userID)

	default:
		log.Printf("⚠️ Неизвестный шаг: %s", state.Step)
		delete(h.UserStates, userID)
	}
}

// startAddEvent начинает процесс добавления события
func (h *MessageHandler) startAddEvent(message *tgbotapi.Message) {
	if !utils.IsAdmin(h.AdminIDs, message.From.ID) {
		h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "⛔ Нет прав"))
		return
	}

	log.Printf("➕ Начало создания события администратором %d", message.From.ID)

	rows, err := database.DB.Query(`SELECT id, name FROM category ORDER BY name`)
	if err != nil {
		log.Printf("❌ Ошибка загрузки категорий: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "❌ Ошибка загрузки категорий"))
		return
	}
	defer rows.Close()

	var buttons [][]tgbotapi.InlineKeyboardButton
	for rows.Next() {
		var id int
		var name string
		rows.Scan(&id, &name)
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(name, fmt.Sprintf("admin:add_category:%d", id)),
		))
	}

	if len(buttons) == 0 {
		h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "❌ Нет доступных категорий"))
		return
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)
	msg := tgbotapi.NewMessage(message.Chat.ID, "Выберите категорию:")
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// showAllEvents показывает все события для администратора
func (h *MessageHandler) showAllEvents(chatID int64) {
	if !utils.IsAdmin(h.AdminIDs, chatID) {
		return
	}

	log.Printf("👑 Запрос всех событий от администратора %d", chatID)

	rows, err := database.DB.Query(`
		SELECT e.id, c.name, e.evn_datetime, e.member_limit,
		       COALESCE((SELECT SUM(participants_count) FROM user_event WHERE event_id = e.id AND status = 'registered'), 0)
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

	count := 0
	for rows.Next() {
		count++
		var e models.Event
		err := rows.Scan(&e.ID, &e.CategoryName, &e.DateTime, &e.MemberLimit, &e.Registered)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		text := fmt.Sprintf(
			"🆔 *%d* | %s\n📆 %s\n👥 %d/%d\n",
			e.ID, e.CategoryName,
			e.DateTime.Format("02.01.2006 15:04"),
			e.Registered, e.MemberLimit,
		)

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✏️ Редактировать", fmt.Sprintf("admin:edit_event:%d", e.ID)),
				tgbotapi.NewInlineKeyboardButtonData("👥 Записи", fmt.Sprintf("admin:view_registrations:%d", e.ID)),
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

// showEventStats показывает статистику по событию
func (h *MessageHandler) showEventStats(chatID int64, eventIDStr string) {
	if !utils.IsAdmin(h.AdminIDs, chatID) {
		return
	}

	eventID, err := strconv.Atoi(eventIDStr)
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "Использование: /eventstats ID"))
		return
	}

	log.Printf("📊 Запрос статистики по событию %d от администратора %d", eventID, chatID)

	rows, err := database.DB.Query(`
		SELECT u.nikname, u.firstname, u.lastname, ue.participants_count, ue.participants_info, ue.registered_at
		FROM user_event ue
		JOIN "user" u ON ue.user_id = u.id
		WHERE ue.event_id = $1 AND ue.status = 'registered'
		ORDER BY ue.registered_at
	`, eventID)

	if err != nil {
		log.Printf("❌ Ошибка загрузки статистики: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
		return
	}
	defer rows.Close()

	text := fmt.Sprintf("📊 Записи на событие #%d:\n\n", eventID)
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

		text += fmt.Sprintf("%d. @%s - %d чел.\n", count, userName, participants)
		if participantsInfo.Valid && participantsInfo.String != "" && participantsInfo.String != fmt.Sprintf("%d человек", participants) {
			text += fmt.Sprintf("   📋 %s\n", participantsInfo.String)
		}
		text += fmt.Sprintf("   📅 %s\n\n", regTime.Format("02.01 15:04"))
	}

	if count == 0 {
		text += "Нет записей"
	} else {
		text += fmt.Sprintf("\n📊 Всего записей: %d\n", count)
		text += fmt.Sprintf("👥 Всего участников: %d\n", totalParticipants)
	}

	h.Bot.Send(tgbotapi.NewMessage(chatID, text))
	log.Printf("✅ Статистика по событию %d отправлена", eventID)
}
