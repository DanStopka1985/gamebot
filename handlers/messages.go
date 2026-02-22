package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"gamebot/database"
	"gamebot/models"
	"gamebot/utils"
)

// MessageHandler обработчик текстовых сообщений
type MessageHandler struct {
	Bot        *tgbotapi.BotAPI
	AdminIDs   *map[int64]bool
	UserStates map[int64]*models.UserState
}

// NewMessageHandler создает новый обработчик сообщений
func NewMessageHandler(bot *tgbotapi.BotAPI, adminIDs *map[int64]bool, userStates map[int64]*models.UserState) *MessageHandler {
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
	utils.RegisterPersonIfNotExists(database.DB, message.From)

	// Проверяем, есть ли у пользователя активное состояние
	if state, exists := h.UserStates[userID]; exists {
		// Для состояний, связанных с вводом имен и количества
		if state.Action == "entering_names" {
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
		if h.isAdmin(userID) {
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
			if h.isAdmin(userID) {
				h.handleAdminCommand(message)
			}
		}
		return
	}
}

// isAdmin проверяет, является ли пользователь администратором
func (h *MessageHandler) isAdmin(userID int64) bool {
	if h.AdminIDs == nil {
		return false
	}
	_, exists := (*h.AdminIDs)[userID]
	return exists
}

// handleAdminText обрабатывает текстовые кнопки админ-меню
func (h *MessageHandler) handleAdminText(message *tgbotapi.Message) {
	text := message.Text
	chatID := message.Chat.ID
	userID := message.From.ID

	if !h.isAdmin(userID) {
		return
	}

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
	case "📁 Управление категориями":
		h.showCategories(chatID)
	case "➕ Добавить категорию":
		h.startAddCategory(message)
	default:
		log.Printf("❓ Неизвестная админская команда: %s", text)
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
	case "categories":
		h.showCategories(message.Chat.ID)
	case "addcategory":
		h.startAddCategory(message)
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
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📁 Управление категориями"),
			tgbotapi.NewKeyboardButton("➕ Добавить категорию"),
		),
	)
	keyboard.ResizeKeyboard = true

	msg := tgbotapi.NewMessage(chatID, "👑 Панель администратора:")
	msg.ReplyMarkup = keyboard

	if _, err := h.Bot.Send(msg); err != nil {
		log.Printf("❌ Ошибка отправки меню администратора: %v", err)
	}
}

// ==================== ФУНКЦИИ ДЛЯ КАТЕГОРИЙ ====================

// startAddCategory начинает процесс добавления категории
func (h *MessageHandler) startAddCategory(message *tgbotapi.Message) {
	if !h.isAdmin(message.From.ID) {
		h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "⛔ Нет прав"))
		return
	}

	log.Printf("➕ Начало создания категории администратором %d", message.From.ID)

	// Сохраняем состояние
	h.UserStates[message.From.ID] = &models.UserState{
		Action:   "add_category",
		Step:     "awaiting_name",
		TempData: make(map[string]interface{}),
	}

	h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID,
		"Введите название новой категории:"))
}

// handleAddCategoryInput обрабатывает ввод названия категории
func (h *MessageHandler) handleAddCategoryInput(message *tgbotapi.Message, state *models.UserState) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := strings.TrimSpace(message.Text)

	if text == "" {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Название не может быть пустым. Введите название:"))
		return
	}

	// Проверяем, существует ли уже такая категория
	var exists bool
	err := database.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM category WHERE name = $1)`, text).Scan(&exists)
	if err != nil {
		log.Printf("❌ Ошибка проверки категории: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при проверке категории"))
		delete(h.UserStates, userID)
		return
	}

	if exists {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Категория с таким названием уже существует. Введите другое название:"))
		return
	}

	// Создаем категорию
	var categoryID int
	err = database.DB.QueryRow(`INSERT INTO category (name) VALUES ($1) RETURNING id`, text).Scan(&categoryID)
	if err != nil {
		log.Printf("❌ Ошибка создания категории: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при создании категории"))
		delete(h.UserStates, userID)
		return
	}

	h.Bot.Send(tgbotapi.NewMessage(chatID,
		fmt.Sprintf("✅ Категория '%s' успешно создана! ID: %d", text, categoryID)))

	delete(h.UserStates, userID)
}

// showCategories показывает список всех категорий
func (h *MessageHandler) showCategories(chatID int64) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("📋 Запрос списка категорий от администратора %d", chatID)

	rows, err := database.DB.Query(`
		SELECT id, name, 
		       (SELECT COUNT(*) FROM event WHERE category_id = c.id) as events_count
		FROM category c
		ORDER BY name
	`)

	if err != nil {
		log.Printf("❌ Ошибка загрузки категорий: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки категорий"))
		return
	}
	defer rows.Close()

	var categories []struct {
		ID          int
		Name        string
		EventsCount int
	}

	for rows.Next() {
		var c struct {
			ID          int
			Name        string
			EventsCount int
		}
		err := rows.Scan(&c.ID, &c.Name, &c.EventsCount)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}
		categories = append(categories, c)
	}

	if len(categories) == 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "📭 Нет категорий"))
		return
	}

	// Формируем текст со списком категорий
	text := "📁 *Список категорий:*\n\n"
	for _, c := range categories {
		text += fmt.Sprintf("🆔 *%d* | %s\n", c.ID, c.Name)
		text += fmt.Sprintf("   📅 Событий: %d\n\n", c.EventsCount)
	}

	// Создаем клавиатуру для удаления категорий
	var buttons [][]tgbotapi.InlineKeyboardButton
	for _, c := range categories {
		if c.EventsCount == 0 {
			// Можно удалять только пустые категории
			button := tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("🗑 Удалить %s", c.Name),
				fmt.Sprintf("admin:delete_category:%d", c.ID),
			)
			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(button))
		}
	}

	if len(buttons) > 0 {
		text += "\n_Категории без событий можно удалить:_\n"
		keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = keyboard
		h.Bot.Send(msg)
	} else {
		// Если нет категорий для удаления
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = "Markdown"
		h.Bot.Send(msg)
	}
}

// deleteCategory удаляет категорию
func (h *MessageHandler) deleteCategory(chatID int64, categoryID int) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("🗑 Удаление категории %d администратором %d", categoryID, chatID)

	// Проверяем, есть ли события в этой категории
	var eventsCount int
	err := database.DB.QueryRow(`SELECT COUNT(*) FROM event WHERE category_id = $1`, categoryID).Scan(&eventsCount)
	if err != nil {
		log.Printf("❌ Ошибка проверки категории: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при проверке категории"))
		return
	}

	if eventsCount > 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			"❌ Нельзя удалить категорию, в которой есть события"))
		return
	}

	// Получаем название категории для подтверждения
	var categoryName string
	err = database.DB.QueryRow(`SELECT name FROM category WHERE id = $1`, categoryID).Scan(&categoryName)
	if err != nil {
		log.Printf("❌ Категория не найдена: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Категория не найдена"))
		return
	}

	// Подтверждение удаления
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Да, удалить", fmt.Sprintf("admin:confirm_delete_category:%d", categoryID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Нет, отмена", "admin:back"),
		),
	)

	msg := tgbotapi.NewMessage(chatID,
		fmt.Sprintf("⚠️ Вы уверены, что хотите удалить категорию '%s'?", categoryName))
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// confirmDeleteCategory подтверждает удаление категории
func (h *MessageHandler) confirmDeleteCategory(chatID int64, categoryID int) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("✅ Подтверждение удаления категории %d", categoryID)

	// Проверяем еще раз наличие событий
	var eventsCount int
	err := database.DB.QueryRow(`SELECT COUNT(*) FROM event WHERE category_id = $1`, categoryID).Scan(&eventsCount)
	if err != nil {
		log.Printf("❌ Ошибка проверки категории: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при удалении"))
		return
	}

	if eventsCount > 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			"❌ Нельзя удалить категорию, в которой есть события"))
		return
	}

	// Удаляем категорию
	result, err := database.DB.Exec(`DELETE FROM category WHERE id = $1`, categoryID)
	if err != nil {
		log.Printf("❌ Ошибка удаления категории: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при удалении категории"))
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Категория не найдена"))
		return
	}

	h.Bot.Send(tgbotapi.NewMessage(chatID, "✅ Категория успешно удалена"))
}

// ==================== ФУНКЦИИ ДЛЯ СОБЫТИЙ ====================

// startAddEvent начинает процесс добавления события
func (h *MessageHandler) startAddEvent(message *tgbotapi.Message) {
	if !h.isAdmin(message.From.ID) {
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
		h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "❌ Нет доступных категорий. Сначала создайте категорию."))
		return
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)
	msg := tgbotapi.NewMessage(message.Chat.ID, "Выберите категорию:")
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

	var eventID int
	err := database.DB.QueryRow(`
		INSERT INTO event (category_id, evn_datetime, member_limit)
		VALUES ($1, $2, $3)
		RETURNING id
	`, state.CategoryID, datetime, limit).Scan(&eventID)

	if err != nil {
		log.Printf("❌ Ошибка создания события: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при создании события"))
		return
	}

	// Получаем название категории
	var categoryName string
	database.DB.QueryRow(`SELECT name FROM category WHERE id = $1`, state.CategoryID).Scan(&categoryName)

	h.Bot.Send(tgbotapi.NewMessage(chatID,
		fmt.Sprintf("✅ Событие '%s' на %s успешно создано! ID: #%d",
			categoryName, datetime.Format("02.01.2006 15:04"), eventID)))

	delete(h.UserStates, userID)
}

// showAllEvents показывает все события для администратора
func (h *MessageHandler) showAllEvents(chatID int64) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("👑 Запрос всех событий от администратора %d", chatID)

	rows, err := database.DB.Query(`
		SELECT e.id, c.name, e.evn_datetime, e.member_limit,
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

// ==================== ФУНКЦИИ ДЛЯ ЗАПИСЕЙ ====================

// showAllRegistrations показывает список событий для выбора записей
func (h *MessageHandler) showAllRegistrations(chatID int64) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("👥 Запрос списка событий для просмотра записей от администратора %d", chatID)

	rows, err := database.DB.Query(`
		SELECT 
			e.id,
			c.name as category_name,
			e.evn_datetime,
			COUNT(pe.id) as registrations_count,
			COALESCE(SUM(pe.participants_count), 0) as participants_count
		FROM event e
		JOIN category c ON e.category_id = c.id
		LEFT JOIN person_event pe ON e.id = pe.event_id AND pe.status = 'registered'
		GROUP BY e.id, c.name, e.evn_datetime
		ORDER BY e.evn_datetime DESC
	`)

	if err != nil {
		log.Printf("❌ Ошибка загрузки событий: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки событий"))
		return
	}
	defer rows.Close()

	var events []struct {
		ID                 int
		CategoryName       string
		DateTime           time.Time
		RegistrationsCount int
		ParticipantsCount  int
	}

	for rows.Next() {
		var e struct {
			ID                 int
			CategoryName       string
			DateTime           time.Time
			RegistrationsCount int
			ParticipantsCount  int
		}
		err := rows.Scan(&e.ID, &e.CategoryName, &e.DateTime, &e.RegistrationsCount, &e.ParticipantsCount)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}
		events = append(events, e)
	}

	if len(events) == 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "📭 Нет событий"))
		return
	}

	var buttons [][]tgbotapi.InlineKeyboardButton
	for _, e := range events {
		buttonText := fmt.Sprintf("%s - %s (%d записей, %d уч.)",
			e.CategoryName,
			e.DateTime.Format("02.01.2006 15:04"),
			e.RegistrationsCount,
			e.ParticipantsCount)

		button := tgbotapi.NewInlineKeyboardButtonData(
			buttonText,
			fmt.Sprintf("admin:view_event_registrations:%d", e.ID),
		)
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(button))
	}

	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📋 Все события (все записи)", "admin:view_all_registrations"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)

	msg := tgbotapi.NewMessage(chatID, "📊 *Выберите событие для просмотра записей:*")
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// showEventRegistrations показывает записи конкретного события
func (h *MessageHandler) showEventRegistrations(chatID int64, eventID int) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("👥 Запрос записей для события %d от администратора %d", eventID, chatID)

	var eventInfo struct {
		CategoryName string
		DateTime     time.Time
	}
	err := database.DB.QueryRow(`
		SELECT c.name, e.evn_datetime
		FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&eventInfo.CategoryName, &eventInfo.DateTime)

	if err != nil {
		log.Printf("❌ Ошибка загрузки информации о событии: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки события"))
		return
	}

	rows, err := database.DB.Query(`
		SELECT 
			p.id as person_id,
			p.nikname,
			p.firstname,
			p.lastname,
			pe.participants_count,
			pe.participants_info,
			pe.registered_at
		FROM person_event pe
		JOIN person p ON pe.person_id = p.id
		WHERE pe.event_id = $1 AND pe.status = 'registered'
		ORDER BY pe.registered_at DESC
	`, eventID)

	if err != nil {
		log.Printf("❌ Ошибка загрузки записей: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки записей"))
		return
	}
	defer rows.Close()

	var allEntries []string
	totalParticipants := 0
	totalRegistrations := 0

	for rows.Next() {
		totalRegistrations++

		var personID int64
		var nikname, firstname, lastname string
		var participants int
		var participantsInfo sql.NullString
		var regDate time.Time

		err := rows.Scan(&personID, &nikname, &firstname, &lastname,
			&participants, &participantsInfo, &regDate)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		registrantInfo := ""
		if nikname != "" {
			registrantInfo += "@" + nikname
		}
		if firstname != "" || lastname != "" {
			if registrantInfo != "" {
				registrantInfo += " "
			}
			registrantInfo += firstname + " " + lastname
		}
		if registrantInfo == "" {
			registrantInfo = fmt.Sprintf("ID: %d", personID)
		}

		if participantsInfo.Valid && participantsInfo.String != "" &&
			participantsInfo.String != fmt.Sprintf("%d человек", participants) {
			cleanInfo := strings.ToValidUTF8(participantsInfo.String, "?")
			participantNames := strings.Split(cleanInfo, ", ")
			for _, name := range participantNames {
				totalParticipants++

				cleanName := strings.ToValidUTF8(strings.TrimSpace(name), "?")

				participantDisplay := ""
				if strings.HasPrefix(cleanName, "@") {
					participantDisplay = "📱 " + cleanName
				} else {
					participantDisplay = "👤 " + cleanName
				}

				entry := fmt.Sprintf(
					"👤 *Записал:* %s\n"+
						"   🧑 *Участник:* %s\n"+
						"   📅 *Запись создана:* %s\n"+
						"   ──────────────────────────\n",
					registrantInfo,
					participantDisplay,
					regDate.Format("02.01.2006 15:04"))

				cleanEntry := strings.ToValidUTF8(entry, "?")
				allEntries = append(allEntries, cleanEntry)
			}
		} else {
			for i := 1; i <= participants; i++ {
				totalParticipants++
				entry := fmt.Sprintf(
					"👤 *Записал:* %s\n"+
						"   🧑 *Участник #%d* (имя не указано)\n"+
						"   📅 *Запись создана:* %s\n"+
						"   ──────────────────────────\n",
					registrantInfo,
					i,
					regDate.Format("02.01.2006 15:04"))

				cleanEntry := strings.ToValidUTF8(entry, "?")
				allEntries = append(allEntries, cleanEntry)
			}
		}
	}

	header := fmt.Sprintf("📊 *Событие: %s*\n", eventInfo.CategoryName)
	header += fmt.Sprintf("📆 *Дата:* %s\n", eventInfo.DateTime.Format("02.01.2006 15:04"))
	header += fmt.Sprintf("📝 *Всего записей:* %d\n", totalRegistrations)
	header += fmt.Sprintf("👥 *Всего участников:* %d\n", totalParticipants)
	header += "════════════════════════════════════════\n\n"

	cleanHeader := strings.ToValidUTF8(header, "?")

	var fullText string
	if len(allEntries) == 0 {
		fullText = cleanHeader + "❌ Нет записей на это событие"
	} else {
		fullText = cleanHeader + strings.Join(allEntries, "")
	}

	backButton := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад к списку событий", "admin:back_to_events"),
		),
	)

	h.sendLongMessage(chatID, fullText, &backButton)
}

// showAllRegistrationsFull показывает все записи по всем событиям
func (h *MessageHandler) showAllRegistrationsFull(chatID int64) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("👥 Запрос всех записей от администратора %d", chatID)

	rows, err := database.DB.Query(`
		SELECT 
			e.id as event_id,
			c.name as category_name,
			TO_CHAR(e.evn_datetime, 'DD.MM.YYYY HH24:MI') as event_date,
			p.id as person_id,
			p.nikname,
			p.firstname,
			p.lastname,
			pe.participants_count,
			pe.participants_info,
			TO_CHAR(pe.registered_at, 'DD.MM.YYYY HH24:MI') as reg_date
		FROM person_event pe
		JOIN event e ON pe.event_id = e.id
		JOIN category c ON e.category_id = c.id
		JOIN person p ON pe.person_id = p.id
		WHERE pe.status = 'registered'
		ORDER BY e.evn_datetime DESC, pe.registered_at DESC
	`)

	if err != nil {
		log.Printf("❌ Ошибка загрузки записей: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
		return
	}
	defer rows.Close()

	var allEntries []string
	totalParticipants := 0
	totalRegistrations := 0
	currentEventID := 0

	for rows.Next() {
		var eventID int
		var categoryName, eventDate, nikname, firstname, lastname, regDate string
		var participants int
		var participantsInfo sql.NullString
		var personID int64

		err := rows.Scan(&eventID, &categoryName, &eventDate, &personID, &nikname, &firstname, &lastname,
			&participants, &participantsInfo, &regDate)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		if eventID != currentEventID {
			if currentEventID != 0 {
				allEntries = append(allEntries, "════════════════════════════════════════\n\n")
			}
			currentEventID = eventID
			eventHeader := fmt.Sprintf("📅 *Событие #%d: %s*\n", eventID, categoryName)
			eventHeader += fmt.Sprintf("   📆 %s\n\n", eventDate)
			allEntries = append(allEntries, eventHeader)
		}

		totalRegistrations++

		registrantInfo := ""
		if nikname != "" {
			registrantInfo += "@" + nikname
		}
		if firstname != "" || lastname != "" {
			if registrantInfo != "" {
				registrantInfo += " "
			}
			registrantInfo += firstname + " " + lastname
		}
		if registrantInfo == "" {
			registrantInfo = fmt.Sprintf("ID: %d", personID)
		}

		if participantsInfo.Valid && participantsInfo.String != "" &&
			participantsInfo.String != fmt.Sprintf("%d человек", participants) {
			cleanInfo := strings.ToValidUTF8(participantsInfo.String, "?")
			participantNames := strings.Split(cleanInfo, ", ")
			for _, name := range participantNames {
				totalParticipants++
				cleanName := strings.ToValidUTF8(strings.TrimSpace(name), "?")

				participantDisplay := ""
				if strings.HasPrefix(cleanName, "@") {
					participantDisplay = "📱 " + cleanName
				} else {
					participantDisplay = "👤 " + cleanName
				}

				entry := fmt.Sprintf(
					"   👤 *Записал:* %s\n"+
						"   🧑 *Участник:* %s\n"+
						"   📅 *Запись:* %s\n\n",
					registrantInfo,
					participantDisplay,
					regDate)

				allEntries = append(allEntries, entry)
			}
		} else {
			for i := 1; i <= participants; i++ {
				totalParticipants++
				entry := fmt.Sprintf(
					"   👤 *Записал:* %s\n"+
						"   🧑 *Участник #%d*\n"+
						"   📅 *Запись:* %s\n\n",
					registrantInfo,
					i,
					regDate)

				allEntries = append(allEntries, entry)
			}
		}
	}

	if len(allEntries) == 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "📭 Нет записей"))
		return
	}

	header := fmt.Sprintf("📊 *Все записи по всем событиям*\n\n")
	header += fmt.Sprintf("📝 *Всего записей:* %d\n", totalRegistrations)
	header += fmt.Sprintf("👥 *Всего участников:* %d\n", totalParticipants)
	header += "════════════════════════════════════════\n\n"

	cleanHeader := strings.ToValidUTF8(header, "?")
	fullText := cleanHeader + strings.Join(allEntries, "")

	backButton := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад к списку событий", "admin:back_to_events"),
		),
	)

	h.sendLongMessage(chatID, fullText, &backButton)
}

// ==================== СТАТИСТИКА ====================

// showStats показывает общую статистику
func (h *MessageHandler) showStats(chatID int64) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("📊 Запрос статистики от администратора %d", chatID)

	var totalEvents, totalPersons, totalRegistrations, totalCategories int
	var upcomingEvents int
	var totalParticipants int

	database.DB.QueryRow(`SELECT COUNT(*) FROM event`).Scan(&totalEvents)
	database.DB.QueryRow(`SELECT COUNT(*) FROM person`).Scan(&totalPersons)
	database.DB.QueryRow(`SELECT COUNT(*) FROM category`).Scan(&totalCategories)
	database.DB.QueryRow(`SELECT COUNT(*) FROM person_event WHERE status = 'registered'`).Scan(&totalRegistrations)
	database.DB.QueryRow(`SELECT COALESCE(SUM(participants_count), 0) FROM person_event WHERE status = 'registered'`).Scan(&totalParticipants)
	database.DB.QueryRow(`SELECT COUNT(*) FROM event WHERE evn_datetime > NOW()`).Scan(&upcomingEvents)

	text := fmt.Sprintf(
		"📊 *Общая статистика*\n\n"+
			"📁 Категорий: %d\n"+
			"📅 Всего событий: %d\n"+
			"⏳ Предстоящих: %d\n"+
			"👥 Пользователей: %d\n"+
			"📝 Всего записей: %d\n"+
			"👥 Всего участников: %d",
		totalCategories, totalEvents, upcomingEvents, totalPersons, totalRegistrations, totalParticipants)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	h.Bot.Send(msg)
}

// showEventStats показывает статистику по конкретному событию
func (h *MessageHandler) showEventStats(chatID int64, eventIDStr string) {
	if !h.isAdmin(chatID) {
		return
	}

	eventID, err := strconv.Atoi(eventIDStr)
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "Использование: /eventstats ID"))
		return
	}

	log.Printf("📊 Запрос статистики по событию %d от администратора %d", eventID, chatID)

	var eventName string
	var eventDate time.Time
	var memberLimit, registered, totalParticipants int

	err = database.DB.QueryRow(`
		SELECT c.name, e.evn_datetime, e.member_limit,
		       COUNT(DISTINCT pe.id),
		       COALESCE(SUM(pe.participants_count), 0)
		FROM event e
		JOIN category c ON e.category_id = c.id
		LEFT JOIN person_event pe ON e.id = pe.event_id AND pe.status = 'registered'
		WHERE e.id = $1
		GROUP BY c.name, e.evn_datetime, e.member_limit
	`, eventID).Scan(&eventName, &eventDate, &memberLimit, &registered, &totalParticipants)

	if err != nil {
		log.Printf("❌ Ошибка загрузки статистики: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
		return
	}

	text := fmt.Sprintf(
		"📊 *Статистика события #%d*\n\n"+
			"📌 %s\n"+
			"📆 %s\n"+
			"👥 Лимит: %d\n"+
			"📝 Записей: %d\n"+
			"👥 Участников: %d\n"+
			"📈 Свободно: %d",
		eventID, eventName, eventDate.Format("02.01.2006 15:04"),
		memberLimit, registered, totalParticipants, memberLimit-totalParticipants)

	h.Bot.Send(tgbotapi.NewMessage(chatID, text))
}

// ==================== ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ ====================

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
		SELECT e.id, c.name, e.evn_datetime, pe.participants_count, pe.participants_info
		FROM person_event pe
		JOIN event e ON pe.event_id = e.id
		JOIN category c ON e.category_id = c.id
		WHERE pe.person_id = $1 AND pe.status = 'registered' AND e.evn_datetime > NOW()
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
	case "add_category":
		h.handleAddCategoryInput(message, state)
	case "edit_event":
		h.handleEditEventInput(message, state)
	default:
		log.Printf("⚠️ Неизвестное действие: %s", state.Action)
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

// sendLongMessage отправляет длинное сообщение с разбивкой на части
func (h *MessageHandler) sendLongMessage(chatID int64, text string, keyboard *tgbotapi.InlineKeyboardMarkup) {
	text = strings.ToValidUTF8(text, "?")

	if len(text) <= 4000 {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = "Markdown"
		if keyboard != nil {
			msg.ReplyMarkup = keyboard
		}
		if _, err := h.Bot.Send(msg); err != nil {
			log.Printf("❌ Ошибка отправки с Markdown: %v", err)
			msg.ParseMode = ""
			if _, err := h.Bot.Send(msg); err != nil {
				log.Printf("❌ Критическая ошибка отправки: %v", err)
			}
		}
		return
	}

	parts := splitMessage(text, 4000)
	for i, part := range parts {
		cleanPart := strings.ToValidUTF8(part, "?")
		msg := tgbotapi.NewMessage(chatID, cleanPart)
		msg.ParseMode = "Markdown"

		if i == 0 {
			msg.Text = "📌 *Часть 1*\n\n" + cleanPart
		} else {
			msg.Text = fmt.Sprintf("📌 *Часть %d*\n\n%s", i+1, cleanPart)
		}

		if i == len(parts)-1 && keyboard != nil {
			msg.ReplyMarkup = keyboard
		}

		if _, err := h.Bot.Send(msg); err != nil {
			log.Printf("❌ Ошибка отправки части %d: %v", i+1, err)
			msg.ParseMode = ""
			if _, err := h.Bot.Send(msg); err != nil {
				log.Printf("❌ Критическая ошибка отправки части %d: %v", i+1, err)
			}
		}
	}
}

// Вспомогательная функция для разбивки длинных сообщений
func splitMessage(text string, limit int) []string {
	var parts []string
	for len(text) > limit {
		cutIndex := strings.LastIndex(text[:limit], "\n════════════════════════════════════════\n")
		if cutIndex == -1 {
			cutIndex = strings.LastIndex(text[:limit], "\n\n")
		}
		if cutIndex == -1 {
			cutIndex = strings.LastIndex(text[:limit], "\n")
		}
		if cutIndex == -1 {
			cutIndex = limit
		} else {
			cutIndex += 1
		}
		parts = append(parts, text[:cutIndex])
		text = text[cutIndex:]
	}
	if len(text) > 0 {
		parts = append(parts, text)
	}
	return parts
}
