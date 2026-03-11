package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"gamebot/database"
	"gamebot/models"
	"gamebot/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
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
	if _, exists := h.UserStates[userID]; exists {
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
	case "👥 Управление игроками":
		h.showPlayersMenu(chatID)
	case "💰 Управление оплатами":
		h.showPaymentManagement(chatID)
	case "📋 Список игроков":
		h.showPlayersList(chatID, 1, "")
	case "➕ Добавить игрока":
		h.startAddPlayer(message)
	case "🔍 Поиск игрока":
		h.searchPlayer(message)
	case "🚫 Заблокированные":
		h.showPlayersList(chatID, 1, "banned")
	case "🔙 Назад в админку":
		h.showAdminMenu(chatID)
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
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("👥 Управление игроками"),
			tgbotapi.NewKeyboardButton("💰 Управление оплатами"),
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
	case "add_category":
		h.handleAddCategoryInput(message, state)
	case "edit_event":
		h.handleEditEventInput(message, state)
	case "add_player":
		h.handleAddPlayerInput(message, state)
	case "edit_player":
		h.handleEditPlayerInput(message, state)
	case "search_player":
		h.handleSearchPlayerInput(message, state)
	case "entering_names":
		callbackHandler := NewCallbackHandler(h.Bot, h.AdminIDs, h.UserStates)
		callbackHandler.handleParticipantNamesWithSearch(message)
	case "entering_custom_count":
		callbackHandler := NewCallbackHandler(h.Bot, h.AdminIDs, h.UserStates)
		callbackHandler.handleCustomCount(message)
	case "adding_more":
		if state.Step == "entering_names" {
			callbackHandler := NewCallbackHandler(h.Bot, h.AdminIDs, h.UserStates)
			callbackHandler.handleAdditionalParticipantInput(message)
		} else if state.Step == "awaiting_custom_count" {
			callbackHandler := NewCallbackHandler(h.Bot, h.AdminIDs, h.UserStates)
			callbackHandler.handleCustomAddMoreCount(message)
		}
	default:
		log.Printf("⚠️ Неизвестное действие: %s", state.Action)
		delete(h.UserStates, userID)
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
		// Парсим время в локальном часовом поясе
		datetime, err := time.ParseInLocation("2006-01-02 15:04", text, time.Local)
		if err != nil {
			log.Printf("❌ Неверный формат даты: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID,
				"❌ Неверный формат. Используйте: 2026-03-15 10:00\nПопробуйте снова:"))
			return
		}

		// Сохраняем в UTC для базы данных
		state.TempData["datetime"] = datetime.UTC()
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
		// Для отображения используем локальное время
		localTime := datetime.Local()
		preview := fmt.Sprintf(
			"📅 Предварительный просмотр события:\n\n"+
				"Категория ID: %d\n"+
				"📆 Дата: %s\n"+
				"👥 Лимит: %d\n\n"+
				"Подтвердить создание?",
			state.CategoryID,
			localTime.Format("02.01.2006 15:04"),
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
	// Сохраняем в UTC
	err := database.DB.QueryRow(`
		INSERT INTO event (category_id, evn_datetime, member_limit)
		VALUES ($1, $2, $3)
		RETURNING id
	`, state.CategoryID, datetime.UTC(), limit).Scan(&eventID)

	if err != nil {
		log.Printf("❌ Ошибка создания события: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при создании события"))
		return
	}

	// Получаем название категории
	var categoryName string
	database.DB.QueryRow(`SELECT name FROM category WHERE id = $1`, state.CategoryID).Scan(&categoryName)

	// Для отображения используем локальное время
	localTime := datetime.Local()
	h.Bot.Send(tgbotapi.NewMessage(chatID,
		fmt.Sprintf("✅ Событие '%s' на %s успешно создано! ID: #%d",
			categoryName, localTime.Format("02.01.2006 15:04"), eventID)))

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
		var dbDateTime time.Time
		err := rows.Scan(&e.ID, &e.CategoryName, &dbDateTime, &e.MemberLimit, &e.Registered)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		// Конвертируем в локальное время для отображения
		e.DateTime = dbDateTime.Local()

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

// showEventRegistrations показывает записи конкретного события с идентификацией
func (h *MessageHandler) showEventRegistrations(chatID int64, eventID int) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("👥 Запрос записей для события %d от администратора %d", eventID, chatID)

	// Получаем информацию о событии
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

	// Получаем все записи для этого события
	rows, err := database.DB.Query(`
		SELECT 
			p.id as person_id,
			p.nikname,
			p.firstname,
			p.lastname,
			pe.participants_count,
			pe.identification_data,
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

	// Проверяем, есть ли данные
	var hasData bool
	var allEntries []string
	totalParticipants := 0
	totalRegistrations := 0
	totalIdentified := 0

	for rows.Next() {
		hasData = true
		totalRegistrations++

		var personID int64
		var nikname, firstname, lastname string
		var participants int
		var identificationData []byte
		var regDate time.Time

		err := rows.Scan(&personID, &nikname, &firstname, &lastname,
			&participants, &identificationData, &regDate)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		// Формируем информацию о записавшем
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

		// Заголовок записи
		entry := fmt.Sprintf(
			"👤 *Записал:* %s\n"+
				"📅 *Дата записи:* %s\n"+
				"📊 *Участники (%d чел.):*\n",
			registrantInfo,
			regDate.Format("02.01.2006 15:04"),
			participants)

		// Парсим данные об идентифицированных игроках
		if len(identificationData) > 0 {
			var identified []map[string]interface{}
			if err := json.Unmarshal(identificationData, &identified); err == nil {
				for i, id := range identified {
					totalParticipants++

					// Получаем имя участника
					fullName := ""
					if fn, ok := id["full_name"].(string); ok && fn != "" {
						fullName = fn
					} else if input, ok := id["input"].(string); ok && input != "" {
						fullName = input
					} else {
						fullName = "Неизвестно"
					}

					// Получаем ник
					nickPart := ""
					if nick, ok := id["telegram_nick"].(string); ok && nick != "" {
						nickPart = fmt.Sprintf(" %s", nick)
					}

					// Получаем статус оплаты
					paymentStatus := "pending"
					if ps, ok := id["payment_status"].(string); ok {
						paymentStatus = ps
					}

					paymentEmoji := "⏳"
					if paymentStatus == "paid" {
						paymentEmoji = "💰"
						totalIdentified++
					}

					if pid, ok := id["player_id"].(float64); ok && pid > 0 {
						entry += fmt.Sprintf("   %d. %s ✅ *%s*%s (ID: %.0f)\n",
							i+1, paymentEmoji, fullName, nickPart, pid)
					} else {
						entry += fmt.Sprintf("   %d. %s ⚠️ *%s*%s (не в базе)\n",
							i+1, paymentEmoji, fullName, nickPart)
					}
				}
			}
		}

		entry += "   ──────────────────────────\n"
		allEntries = append(allEntries, entry)
	}

	if !hasData {
		h.Bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf(
			"📊 *Событие: %s*\n📆 *Дата:* %s\n\n❌ Нет записей на это событие",
			eventInfo.CategoryName,
			eventInfo.DateTime.Format("02.01.2006 15:04"))))
		return
	}

	header := fmt.Sprintf("📊 *Событие: %s*\n", eventInfo.CategoryName)
	header += fmt.Sprintf("📆 *Дата:* %s\n", eventInfo.DateTime.Format("02.01.2006 15:04"))
	header += fmt.Sprintf("📝 *Всего записей:* %d\n", totalRegistrations)
	header += fmt.Sprintf("👥 *Всего участников:* %d\n", totalParticipants)
	header += fmt.Sprintf("💰 *Оплатили:* %d\n", totalIdentified)
	header += "════════════════════════════════════════\n\n"

	fullText := header + strings.Join(allEntries, "")

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

// truncateUTF8 безопасно обрезает строку с учетом UTF-8 символов
func truncateUTF8(s string, maxLen int) string {
	// Преобразуем в срез рун для правильной работы с UTF-8
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

// ==================== ФУНКЦИИ ДЛЯ УПРАВЛЕНИЯ PLAYERS ====================

// showPlayersMenu показывает меню управления игроками
func (h *MessageHandler) showPlayersMenu(chatID int64) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("👥 Запрос меню управления игроками от администратора %d", chatID)

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📋 Список игроков"),
			tgbotapi.NewKeyboardButton("➕ Добавить игрока"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🔍 Поиск игрока"),
			tgbotapi.NewKeyboardButton("🚫 Заблокированные"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🔙 Назад в админку"),
		),
	)
	keyboard.ResizeKeyboard = true

	msg := tgbotapi.NewMessage(chatID, "👥 *Управление игроками*\n\nВыберите действие:")
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// showPlayersList показывает список игроков с пагинацией
func (h *MessageHandler) showPlayersList(chatID int64, page int, filter string) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("📋 Запрос списка игроков (страница %d, фильтр: %s) от администратора %d", page, filter, chatID)

	limit := 10
	offset := (page - 1) * limit

	var rows *sql.Rows
	var err error
	var totalCount int

	if filter == "banned" {
		// Показываем только заблокированных
		err = database.DB.QueryRow(`SELECT COUNT(*) FROM players WHERE is_active = false`).Scan(&totalCount)
		if err != nil {
			log.Printf("❌ Ошибка подсчета игроков: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
			return
		}

		rows, err = database.DB.Query(`
			SELECT id, full_name, telegram_nick, telegram_name, notes, is_active, created_at
			FROM players
			WHERE is_active = false
			ORDER BY full_name
			LIMIT $1 OFFSET $2
		`, limit, offset)
	} else if filter != "" && filter != "active" {
		// Поиск по имени или нику
		searchPattern := "%" + filter + "%"
		err = database.DB.QueryRow(`
			SELECT COUNT(*) FROM players 
			WHERE full_name ILIKE $1 OR telegram_nick ILIKE $1 OR telegram_name ILIKE $1
		`, searchPattern).Scan(&totalCount)
		if err != nil {
			log.Printf("❌ Ошибка подсчета игроков: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
			return
		}

		rows, err = database.DB.Query(`
			SELECT id, full_name, telegram_nick, telegram_name, notes, is_active, created_at
			FROM players
			WHERE full_name ILIKE $1 OR telegram_nick ILIKE $1 OR telegram_name ILIKE $1
			ORDER BY full_name
			LIMIT $2 OFFSET $3
		`, searchPattern, limit, offset)
	} else {
		// Все активные игроки
		err = database.DB.QueryRow(`SELECT COUNT(*) FROM players WHERE is_active = true`).Scan(&totalCount)
		if err != nil {
			log.Printf("❌ Ошибка подсчета игроков: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
			return
		}

		rows, err = database.DB.Query(`
			SELECT id, full_name, telegram_nick, telegram_name, notes, is_active, created_at
			FROM players
			WHERE is_active = true
			ORDER BY full_name
			LIMIT $1 OFFSET $2
		`, limit, offset)
	}

	if err != nil {
		log.Printf("❌ Ошибка загрузки игроков: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
		return
	}
	defer rows.Close()

	totalPages := (totalCount + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}

	// Формируем заголовок
	filterText := ""
	if filter == "banned" {
		filterText = " (заблокированные)"
	} else if filter != "" && filter != "active" {
		filterText = fmt.Sprintf(" (поиск: %s)", filter)
	}

	text := fmt.Sprintf("📋 *Список игроков%s* (страница %d/%d, всего: %d)\n\n",
		filterText, page, totalPages, totalCount)

	for rows.Next() {
		var id int
		var fullName, notes string
		var telegramNick, telegramName sql.NullString
		var isActive bool
		var createdAt time.Time

		err := rows.Scan(&id, &fullName, &telegramNick, &telegramName, &notes, &isActive, &createdAt)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		status := "✅"
		if !isActive {
			status = "🚫"
		}

		text += fmt.Sprintf("%s *%d.* %s\n", status, id, fullName)
		if telegramNick.Valid && telegramNick.String != "" {
			text += fmt.Sprintf("   📱 %s\n", telegramNick.String)
		}
		if telegramName.Valid && telegramName.String != "" {
			text += fmt.Sprintf("   👤 %s\n", telegramName.String)
		}
		if notes != "" {
			text += fmt.Sprintf("   📝 %s\n", notes)
		}
		text += "\n"
	}

	// Создаем клавиатуру для навигации
	var buttons [][]tgbotapi.InlineKeyboardButton

	// Кнопки пагинации
	var navRow []tgbotapi.InlineKeyboardButton
	if page > 1 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("◀️ Пред",
			fmt.Sprintf("players_page:%d:%s", page-1, filter)))
	}
	if page < totalPages {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("След ▶️",
			fmt.Sprintf("players_page:%d:%s", page+1, filter)))
	}
	if len(navRow) > 0 {
		buttons = append(buttons, navRow)
	}

	// Кнопки для каждого игрока (показываем только если игроков не слишком много)
	if limit <= 10 {
		// Возвращаемся к началу, чтобы добавить кнопки для каждого игрока
		rows, err = database.DB.Query(`
			SELECT id, full_name, telegram_nick
			FROM players
			WHERE 
				CASE 
					WHEN $1 = 'banned' THEN is_active = false
					WHEN $1 != '' AND $1 != 'active' THEN 
						full_name ILIKE '%' || $1 || '%' 
						OR telegram_nick ILIKE '%' || $1 || '%' 
						OR telegram_name ILIKE '%' || $1 || '%'
					ELSE is_active = true
				END
			ORDER BY full_name
			LIMIT $2 OFFSET $3
		`, filter, limit, offset)

		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id int
				var fullName string
				var telegramNick sql.NullString
				rows.Scan(&id, &fullName, &telegramNick)

				// Обрезаем имя с учетом UTF-8
				displayName := fullName
				if len([]rune(displayName)) > 22 {
					displayName = truncateUTF8(displayName, 20)
				}

				buttonText := fmt.Sprintf("👤 %s", displayName)

				buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(buttonText,
						fmt.Sprintf("admin:view_player:%d", id)),
				))
			}
		}
	}

	// Добавляем кнопки для массовых действий
	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("➕ Добавить игрока", "admin:add_player"),
		tgbotapi.NewInlineKeyboardButtonData("🔍 Поиск", "admin:search_player"),
	))
	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔙 Назад в меню", "admin:back_to_players_menu"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// showPlayerDetails показывает детальную информацию об игроке
func (h *MessageHandler) showPlayerDetails(chatID int64, playerID int) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("👤 Запрос деталей игрока %d от администратора %d", playerID, chatID)

	var player struct {
		FullName     string
		TelegramNick sql.NullString
		TelegramName sql.NullString
		Notes        sql.NullString
		IsActive     bool
		CreatedAt    time.Time
	}

	err := database.DB.QueryRow(`
		SELECT full_name, telegram_nick, telegram_name, notes, is_active, created_at
		FROM players WHERE id = $1
	`, playerID).Scan(&player.FullName, &player.TelegramNick, &player.TelegramName, &player.Notes, &player.IsActive, &player.CreatedAt)

	if err != nil {
		log.Printf("❌ Ошибка загрузки игрока: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Игрок не найден"))
		return
	}

	// Получаем статистику игрока
	var totalRegistrations, totalEvents int
	database.DB.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(participants_count), 0)
		FROM person_event WHERE player_id = $1 AND status = 'registered'
	`, playerID).Scan(&totalRegistrations, &totalEvents)

	status := "✅ Активен"
	if !player.IsActive {
		status = "🚫 Заблокирован"
	}

	text := fmt.Sprintf("👤 *Информация об игроке*\n\n")
	text += fmt.Sprintf("🆔 *ID:* %d\n", playerID)
	text += fmt.Sprintf("📝 *ФИО:* %s\n", player.FullName)
	if player.TelegramNick.Valid && player.TelegramNick.String != "" {
		text += fmt.Sprintf("📱 *Telegram:* %s\n", player.TelegramNick.String)
	}
	if player.TelegramName.Valid && player.TelegramName.String != "" {
		text += fmt.Sprintf("👤 *Имя в TG:* %s\n", player.TelegramName.String)
	}
	if player.Notes.Valid && player.Notes.String != "" {
		text += fmt.Sprintf("📝 *Заметки:* %s\n", player.Notes.String)
	}
	text += fmt.Sprintf("📊 *Статус:* %s\n", status)
	text += fmt.Sprintf("📅 *Зарегистрирован:* %s\n", player.CreatedAt.Format("02.01.2006"))
	text += fmt.Sprintf("📊 *Всего записей:* %d\n", totalRegistrations)
	text += fmt.Sprintf("👥 *Всего участников:* %d\n", totalEvents)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✏️ Редактировать", fmt.Sprintf("admin:edit_player:%d", playerID)),
			tgbotapi.NewInlineKeyboardButtonData("📋 История записей", fmt.Sprintf("admin:player_history:%d", playerID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "admin:back_to_players_list"),
		),
	)

	if player.IsActive {
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🚫 Заблокировать", fmt.Sprintf("admin:ban_player:%d", playerID)),
		))
	} else {
		keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Разблокировать", fmt.Sprintf("admin:unban_player:%d", playerID)),
		))
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// startAddPlayer начинает процесс добавления игрока
func (h *MessageHandler) startAddPlayer(message *tgbotapi.Message) {
	if !h.isAdmin(message.From.ID) {
		h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "⛔ Нет прав"))
		return
	}

	log.Printf("➕ Начало создания игрока администратором %d", message.From.ID)

	h.UserStates[message.From.ID] = &models.UserState{
		Action:   "add_player",
		Step:     "awaiting_fullname",
		TempData: make(map[string]interface{}),
	}

	h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID,
		"Введите ФИО игрока:"))
}

// handleAddPlayerInput обрабатывает ввод при создании игрока
func (h *MessageHandler) handleAddPlayerInput(message *tgbotapi.Message, state *models.UserState) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := strings.TrimSpace(message.Text)

	if text == "" {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Поле не может быть пустым. Попробуйте снова:"))
		return
	}

	switch state.Step {
	case "awaiting_fullname":
		state.TempData["full_name"] = text
		state.Step = "awaiting_telegram_nick"
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			"Введите Telegram никнейм (можно пропустить, отправив \"-\"):"))

	case "awaiting_telegram_nick":
		if text != "-" {
			if !strings.HasPrefix(text, "@") {
				text = "@" + text
			}
			state.TempData["telegram_nick"] = text
		}
		state.Step = "awaiting_telegram_name"
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			"Введите имя в Telegram (можно пропустить, отправив \"-\"):"))

	case "awaiting_telegram_name":
		if text != "-" {
			state.TempData["telegram_name"] = text
		}
		state.Step = "awaiting_notes"
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			"Введите заметки (можно пропустить, отправив \"-\"):"))

	case "awaiting_notes":
		if text != "-" {
			state.TempData["notes"] = text
		}

		// Сохраняем игрока в БД
		fullName := state.TempData["full_name"].(string)
		telegramNick := ""
		if tn, ok := state.TempData["telegram_nick"].(string); ok {
			telegramNick = tn
		}
		telegramName := ""
		if tn, ok := state.TempData["telegram_name"].(string); ok {
			telegramName = tn
		}
		notes := ""
		if n, ok := state.TempData["notes"].(string); ok {
			notes = n
		}

		var playerID int
		err := database.DB.QueryRow(`
			INSERT INTO players (full_name, telegram_nick, telegram_name, notes, is_active)
			VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), true)
			RETURNING id
		`, fullName, telegramNick, telegramName, notes).Scan(&playerID)

		if err != nil {
			log.Printf("❌ Ошибка создания игрока: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при создании игрока"))
			delete(h.UserStates, userID)
			return
		}

		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("✅ Игрок '%s' успешно создан! ID: %d", fullName, playerID)))

		// Показываем детали созданного игрока
		h.showPlayerDetails(chatID, playerID)
		delete(h.UserStates, userID)
	}
}

// startEditPlayer начинает процесс редактирования игрока
func (h *MessageHandler) startEditPlayer(chatID int64, userID int64, playerID int) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("✏️ Начало редактирования игрока %d администратором %d", playerID, userID)

	// Получаем текущие данные игрока
	var player struct {
		FullName     string
		TelegramNick sql.NullString
		TelegramName sql.NullString
		Notes        sql.NullString
	}

	err := database.DB.QueryRow(`
		SELECT full_name, telegram_nick, telegram_name, notes
		FROM players WHERE id = $1
	`, playerID).Scan(&player.FullName, &player.TelegramNick, &player.TelegramName, &player.Notes)

	if err != nil {
		log.Printf("❌ Ошибка загрузки игрока: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Игрок не найден"))
		return
	}

	// Сохраняем состояние
	h.UserStates[userID] = &models.UserState{
		Action: "edit_player",
		Step:   "awaiting_fullname",
		TempData: map[string]interface{}{
			"player_id":     playerID,
			"full_name":     player.FullName,
			"telegram_nick": player.TelegramNick.String,
			"telegram_name": player.TelegramName.String,
			"notes":         player.Notes.String,
		},
	}

	msg := fmt.Sprintf(
		"✏️ *Редактирование игрока ID:%d*\n\n"+
			"Текущее ФИО: %s\n\n"+
			"Введите новое ФИО (или отправьте \".\" чтобы оставить текущее):",
		playerID, player.FullName)

	h.Bot.Send(tgbotapi.NewMessage(chatID, msg))
}

// handleEditPlayerInput обрабатывает ввод при редактировании игрока
func (h *MessageHandler) handleEditPlayerInput(message *tgbotapi.Message, state *models.UserState) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := strings.TrimSpace(message.Text)

	playerID := state.TempData["player_id"].(int)

	switch state.Step {
	case "awaiting_fullname":
		if text != "." {
			state.TempData["full_name"] = text
		}
		state.Step = "awaiting_telegram_nick"
		current := state.TempData["telegram_nick"].(string)
		if current == "" {
			current = "не указан"
		}
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("Текущий Telegram ник: %s\nВведите новый Telegram ник (или \".\" чтобы оставить):", current)))

	case "awaiting_telegram_nick":
		if text != "." {
			if text == "-" {
				state.TempData["telegram_nick"] = ""
			} else {
				if !strings.HasPrefix(text, "@") {
					text = "@" + text
				}
				state.TempData["telegram_nick"] = text
			}
		}
		state.Step = "awaiting_telegram_name"
		current := state.TempData["telegram_name"].(string)
		if current == "" {
			current = "не указан"
		}
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("Текущее имя в Telegram: %s\nВведите новое имя (или \".\" чтобы оставить):", current)))

	case "awaiting_telegram_name":
		if text != "." {
			if text == "-" {
				state.TempData["telegram_name"] = ""
			} else {
				state.TempData["telegram_name"] = text
			}
		}
		state.Step = "awaiting_notes"
		current := state.TempData["notes"].(string)
		if current == "" {
			current = "не указан"
		}
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("Текущие заметки: %s\nВведите новые заметки (или \".\" чтобы оставить):", current)))

	case "awaiting_notes":
		if text != "." {
			if text == "-" {
				state.TempData["notes"] = ""
			} else {
				state.TempData["notes"] = text
			}
		}

		// Обновляем данные в БД
		_, err := database.DB.Exec(`
			UPDATE players 
			SET full_name = $1, 
			    telegram_nick = NULLIF($2, ''), 
			    telegram_name = NULLIF($3, ''), 
			    notes = NULLIF($4, ''),
			    updated_at = NOW()
			WHERE id = $5
		`,
			state.TempData["full_name"],
			state.TempData["telegram_nick"],
			state.TempData["telegram_name"],
			state.TempData["notes"],
			playerID)

		if err != nil {
			log.Printf("❌ Ошибка обновления игрока: %v", err)
			h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при обновлении"))
			delete(h.UserStates, userID)
			return
		}

		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("✅ Данные игрока ID:%d обновлены", playerID)))

		// Показываем обновленные детали
		h.showPlayerDetails(chatID, playerID)
		delete(h.UserStates, userID)
	}
}

// banPlayer блокирует игрока
func (h *MessageHandler) banPlayer(chatID int64, playerID int) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("🚫 Блокировка игрока %d администратором %d", playerID, chatID)

	result, err := database.DB.Exec(`UPDATE players SET is_active = false WHERE id = $1`, playerID)
	if err != nil {
		log.Printf("❌ Ошибка блокировки игрока: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при блокировке"))
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Игрок не найден"))
		return
	}

	h.Bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Игрок ID:%d заблокирован", playerID)))
	h.showPlayerDetails(chatID, playerID)
}

// unbanPlayer разблокирует игрока
func (h *MessageHandler) unbanPlayer(chatID int64, playerID int) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("✅ Разблокировка игрока %d администратором %d", playerID, chatID)

	result, err := database.DB.Exec(`UPDATE players SET is_active = true WHERE id = $1`, playerID)
	if err != nil {
		log.Printf("❌ Ошибка разблокировки игрока: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при разблокировке"))
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Игрок не найден"))
		return
	}

	h.Bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Игрок ID:%d разблокирован", playerID)))
	h.showPlayerDetails(chatID, playerID)
}

// searchPlayer начинает поиск игрока
func (h *MessageHandler) searchPlayer(message *tgbotapi.Message) {
	if !h.isAdmin(message.From.ID) {
		h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID, "⛔ Нет прав"))
		return
	}

	log.Printf("🔍 Начало поиска игрока администратором %d", message.From.ID)

	h.UserStates[message.From.ID] = &models.UserState{
		Action:   "search_player",
		Step:     "awaiting_query",
		TempData: make(map[string]interface{}),
	}

	h.Bot.Send(tgbotapi.NewMessage(message.Chat.ID,
		"Введите имя, ник или любую информацию для поиска:"))
}

// handleSearchPlayerInput обрабатывает поисковый запрос
func (h *MessageHandler) handleSearchPlayerInput(message *tgbotapi.Message, state *models.UserState) {
	userID := message.From.ID
	chatID := message.Chat.ID
	query := strings.TrimSpace(message.Text)

	if query == "" {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Введите текст для поиска"))
		return
	}

	log.Printf("🔍 Поиск игроков по запросу: %s", query)

	searchPattern := "%" + query + "%"
	rows, err := database.DB.Query(`
		SELECT id, full_name, telegram_nick, telegram_name, is_active
		FROM players
		WHERE full_name ILIKE $1 
		   OR telegram_nick ILIKE $1 
		   OR telegram_name ILIKE $1
		   OR notes ILIKE $1
		ORDER BY full_name
		LIMIT 20
	`, searchPattern)

	if err != nil {
		log.Printf("❌ Ошибка поиска: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при поиске"))
		delete(h.UserStates, userID)
		return
	}
	defer rows.Close()

	var results []struct {
		ID       int
		FullName string
		Nick     sql.NullString
		TGName   sql.NullString
		IsActive bool
	}

	for rows.Next() {
		var r struct {
			ID       int
			FullName string
			Nick     sql.NullString
			TGName   sql.NullString
			IsActive bool
		}
		err := rows.Scan(&r.ID, &r.FullName, &r.Nick, &r.TGName, &r.IsActive)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}
		results = append(results, r)
	}

	if len(results) == 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ничего не найдено"))
		delete(h.UserStates, userID)
		return
	}

	text := fmt.Sprintf("🔍 *Результаты поиска* (найдено: %d)\n\n", len(results))

	var buttons [][]tgbotapi.InlineKeyboardButton
	for _, r := range results {
		status := "✅"
		if !r.IsActive {
			status = "🚫"
		}
		text += fmt.Sprintf("%s *%d.* %s\n", status, r.ID, r.FullName)
		if r.Nick.Valid && r.Nick.String != "" {
			text += fmt.Sprintf("   📱 %s\n", r.Nick.String)
		}
		text += "\n"

		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("👤 %s", r.FullName),
				fmt.Sprintf("admin:view_player:%d", r.ID),
			),
		))
	}

	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "admin:back_to_players_menu"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)

	delete(h.UserStates, userID)
}

// showPlayerHistory показывает историю записей игрока
func (h *MessageHandler) showPlayerHistory(chatID int64, playerID int) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("📋 Запрос истории игрока %d от администратора %d", playerID, chatID)

	rows, err := database.DB.Query(`
		SELECT e.id, c.name, e.evn_datetime, pe.participants_count, pe.registered_at, pe.status
		FROM person_event pe
		JOIN event e ON pe.event_id = e.id
		JOIN category c ON e.category_id = c.id
		WHERE pe.player_id = $1
		ORDER BY pe.registered_at DESC
		LIMIT 20
	`, playerID)

	if err != nil {
		log.Printf("❌ Ошибка загрузки истории: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
		return
	}
	defer rows.Close()

	var playerName string
	database.DB.QueryRow(`SELECT full_name FROM players WHERE id = $1`, playerID).Scan(&playerName)

	text := fmt.Sprintf("📋 *История записей игрока %s*\n\n", playerName)

	count := 0
	for rows.Next() {
		count++
		var eventID int
		var categoryName string
		var eventDate, regDate time.Time
		var participants int
		var status string

		err := rows.Scan(&eventID, &categoryName, &eventDate, &participants, &regDate, &status)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		statusEmoji := "✅"
		if status != "registered" {
			statusEmoji = "❌"
		}

		text += fmt.Sprintf("%s *Событие #%d:* %s\n", statusEmoji, eventID, categoryName)
		text += fmt.Sprintf("   📆 %s\n", eventDate.Format("02.01.2006 15:04"))
		text += fmt.Sprintf("   👥 Участников: %d\n", participants)
		text += fmt.Sprintf("   📅 Запись: %s\n\n", regDate.Format("02.01.2006 15:04"))
	}

	if count == 0 {
		text += "Нет записей"
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", fmt.Sprintf("admin:view_player:%d", playerID)),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// ==================== ФУНКЦИИ ДЛЯ УПРАВЛЕНИЯ ОПЛАТОЙ ====================

// showPaymentManagement показывает меню управления оплатами
func (h *MessageHandler) showPaymentManagement(chatID int64) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("💰 Запрос меню управления оплатами от администратора %d", chatID)

	// Получаем список событий с количеством неплативших
	rows, err := database.DB.Query(`
		SELECT 
			e.id,
			c.name as category_name,
			e.evn_datetime,
			COUNT(pe.id) as total_registrations,
			COUNT(CASE WHEN pe.payment_status = 'pending' THEN 1 END) as unpaid_count
		FROM event e
		JOIN category c ON e.category_id = c.id
		LEFT JOIN person_event pe ON e.id = pe.event_id AND pe.status = 'registered'
		GROUP BY e.id, c.name, e.evn_datetime
		HAVING COUNT(pe.id) > 0
		ORDER BY e.evn_datetime DESC
	`)

	if err != nil {
		log.Printf("❌ Ошибка загрузки событий: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
		return
	}
	defer rows.Close()

	var buttons [][]tgbotapi.InlineKeyboardButton
	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("💰 Все неплательщики", "admin:all_unpaid"),
	))

	for rows.Next() {
		var id int
		var categoryName string
		var eventDate time.Time
		var total, unpaid int

		err := rows.Scan(&id, &categoryName, &eventDate, &total, &unpaid)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		if unpaid > 0 {
			buttonText := fmt.Sprintf("%s %s (%d/%d не платили)",
				getPaymentEmoji(unpaid > 0),
				categoryName,
				unpaid, total)

			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(buttonText,
					fmt.Sprintf("admin:payment_event:%d", id)),
			))
		}
	}

	if len(buttons) == 1 { // Только кнопка "Все неплательщики"
		msg := tgbotapi.NewMessage(chatID, "💰 Все записи оплачены! 🎉")
		h.Bot.Send(msg)
		return
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)

	msg := tgbotapi.NewMessage(chatID, "💰 *Управление оплатами*\n\nВыберите событие для просмотра:")
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// getPaymentEmoji возвращает эмодзи в зависимости от статуса оплаты
func getPaymentEmoji(hasUnpaid bool) string {
	if hasUnpaid {
		return "💰"
	}
	return "✅"
}

// showEventPayments показывает список участников события с отметками об оплате
func (h *MessageHandler) showEventPayments(chatID int64, eventID int) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("💰 Запрос оплат для события %d от администратора %d", eventID, chatID)

	// Получаем информацию о событии
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
		log.Printf("❌ Ошибка загрузки события: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки события"))
		return
	}

	// Получаем всех участников
	rows, err := database.DB.Query(`
		SELECT 
			pe.id as person_event_id,
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
	paidCount := 0
	var buttons [][]tgbotapi.InlineKeyboardButton

	for rows.Next() {
		hasData = true
		var peID int
		var nikname, firstname, lastname string
		var identificationData []byte
		var registeredAt time.Time

		err := rows.Scan(&peID, &nikname, &firstname, &lastname,
			&identificationData, &registeredAt)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		// Если это первый элемент, формируем заголовок
		if text == "" {
			text = fmt.Sprintf("💰 *Оплаты: %s* (%s)\n\n",
				eventInfo.CategoryName,
				eventInfo.DateTime.Format("02.01.2006 15:04"))
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

		// Парсим участников
		if len(identificationData) > 0 {
			var identified []map[string]interface{}
			if err := json.Unmarshal(identificationData, &identified); err == nil {
				for _, p := range identified {
					totalParticipants++

					// Получаем имя участника
					participantName := ""
					if fn, ok := p["full_name"].(string); ok && fn != "" {
						participantName = fn
					} else if input, ok := p["input"].(string); ok && input != "" {
						participantName = input
					} else {
						participantName = "Неизвестно"
					}

					// Добавляем ник если есть
					displayName := participantName
					if nick, ok := p["telegram_nick"].(string); ok && nick != "" {
						displayName = fmt.Sprintf("%s %s", nick, participantName)
					}

					// Получаем статус оплаты
					paymentStatus := "pending"
					if ps, ok := p["payment_status"].(string); ok {
						paymentStatus = ps
					}

					paymentEmoji := "⏳"
					if paymentStatus == "paid" {
						paymentEmoji = "💰"
						paidCount++
					}

					text += fmt.Sprintf("%s %s (записал: %s)\n",
						paymentEmoji, displayName, registrantName)
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
	text += fmt.Sprintf("\n💰 *Оплатили: %d*", paidCount)
	text += fmt.Sprintf("\n⏳ *Ожидают оплаты: %d*", totalParticipants-paidCount)

	// Добавляем кнопку "Назад"
	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔙 Назад к списку событий", "admin:back_to_payments"),
	))

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	if len(buttons) > 0 {
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons...)
	}
	h.Bot.Send(msg)
}

// getParticipantPaymentEmoji возвращает эмодзи для участника
func getParticipantPaymentEmoji(isPaid bool) string {
	if isPaid {
		return "💰"
	}
	return "⏳"
}

// showAllUnpaid показывает всех неплательщиков по всем событиям
func (h *MessageHandler) showAllUnpaid(chatID int64) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("💰 Запрос всех неплательщиков от администратора %d", chatID)

	rows, err := database.DB.Query(`
		SELECT 
			e.id as event_id,
			c.name as category_name,
			e.evn_datetime,
			p.nikname,
			p.firstname,
			p.lastname,
			pe.id as person_event_id,
			pe.identification_data
		FROM person_event pe
		JOIN event e ON pe.event_id = e.id
		JOIN category c ON e.category_id = c.id
		JOIN person p ON pe.person_id = p.id
		WHERE pe.status = 'registered' AND pe.payment_status = 'pending'
		ORDER BY e.evn_datetime DESC
	`)

	if err != nil {
		log.Printf("❌ Ошибка загрузки: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
		return
	}
	defer rows.Close()

	text := "💰 *Неплательщики по всем событиям*\n\n"
	currentEventID := 0
	var buttons [][]tgbotapi.InlineKeyboardButton

	for rows.Next() {
		var eventID int
		var categoryName string
		var eventDate time.Time
		var nikname, firstname, lastname string
		var peID int
		var identificationData []byte

		err := rows.Scan(&eventID, &categoryName, &eventDate, &nikname, &firstname, &lastname,
			&peID, &identificationData)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		if eventID != currentEventID {
			if currentEventID != 0 {
				text += "\n"
			}
			currentEventID = eventID
			text += fmt.Sprintf("📅 *%s* (%s)\n",
				categoryName,
				eventDate.Format("02.01.2006 15:04"))
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

		// Парсим участников
		if len(identificationData) > 0 {
			var identified []map[string]interface{}
			if err := json.Unmarshal(identificationData, &identified); err == nil {
				for _, p := range identified {
					participantName := ""
					if fn, ok := p["full_name"].(string); ok && fn != "" {
						participantName = fn
					} else if input, ok := p["input"].(string); ok && input != "" {
						participantName = input
					}

					if nick, ok := p["telegram_nick"].(string); ok && nick != "" {
						participantName = fmt.Sprintf("%s %s", nick, participantName)
					}

					text += fmt.Sprintf("  ⏳ %s (записал: %s)\n", participantName, registrantName)
				}
			}
		}

		// Добавляем кнопку для отметки
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("💰 Отметить: %s", registrantName),
				fmt.Sprintf("admin:mark_paid:%d", peID)),
		))
	}

	if currentEventID == 0 {
		text += "✅ Все записи оплачены! 🎉"
	}

	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "admin:back_to_payments"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// markAsPaid отмечает запись как оплаченную
func (h *MessageHandler) markAsPaid(chatID int64, personEventID int) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("💰 Отметка об оплате записи %d администратором %d", personEventID, chatID)

	result, err := database.DB.Exec(`
		UPDATE person_event 
		SET payment_status = 'paid', 
		    payment_date = NOW() 
		WHERE id = $1 AND payment_status = 'pending'
	`, personEventID)

	if err != nil {
		log.Printf("❌ Ошибка отметки об оплате: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при отметке"))
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Запись не найдена или уже оплачена"))
		return
	}

	h.Bot.Send(tgbotapi.NewMessage(chatID, "✅ Отмечено как оплачено"))
}

// markParticipantAsPaid отмечает конкретного участника как оплатившего
func (h *MessageHandler) markParticipantAsPaid(chatID int64, participantKey string) {
	if !h.isAdmin(chatID) {
		return
	}

	// Парсим ключ: person_event_id_index
	parts := strings.Split(participantKey, "_")
	if len(parts) != 2 {
		log.Printf("❌ Неверный формат ключа: %s", participantKey)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка формата данных"))
		return
	}

	peID, _ := strconv.Atoi(parts[0])
	idx, _ := strconv.Atoi(parts[1])

	log.Printf("💰 Отметка участника (запись %d, индекс %d) администратором %d", peID, idx, chatID)

	// Получаем текущие данные
	var identificationData []byte
	err := database.DB.QueryRow(`
		SELECT identification_data FROM person_event WHERE id = $1
	`, peID).Scan(&identificationData)

	if err != nil {
		log.Printf("❌ Ошибка загрузки данных: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Запись не найдена"))
		return
	}

	// Парсим JSON
	var identified []map[string]interface{}
	if err := json.Unmarshal(identificationData, &identified); err != nil {
		log.Printf("❌ Ошибка парсинга JSON: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка данных"))
		return
	}

	if idx < 0 || idx >= len(identified) {
		log.Printf("❌ Неверный индекс: %d", idx)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Участник не найден"))
		return
	}

	// Отмечаем как оплаченного
	identified[idx]["payment_status"] = "paid"
	identified[idx]["payment_date"] = time.Now().Format(time.RFC3339)

	// Сохраняем обратно
	updatedJSON, _ := json.Marshal(identified)
	_, err = database.DB.Exec(`
		UPDATE person_event SET identification_data = $1 WHERE id = $2
	`, updatedJSON, peID)

	if err != nil {
		log.Printf("❌ Ошибка сохранения: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при сохранении"))
		return
	}

	h.Bot.Send(tgbotapi.NewMessage(chatID, "✅ Участник отмечен как оплативший"))
}

// markAllAsPaid отмечает все записи на событие как оплаченные
func (h *MessageHandler) markAllAsPaid(chatID int64, eventID int) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("💰 Отметка всех записей на событие %d как оплаченных администратором %d", eventID, chatID)

	result, err := database.DB.Exec(`
		UPDATE person_event 
		SET payment_status = 'paid', 
		    payment_date = NOW() 
		WHERE event_id = $1 AND status = 'registered' AND payment_status = 'pending'
	`, eventID)

	if err != nil {
		log.Printf("❌ Ошибка массовой отметки: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при отметке"))
		return
	}

	rows, _ := result.RowsAffected()
	h.Bot.Send(tgbotapi.NewMessage(chatID,
		fmt.Sprintf("✅ Отмечено как оплачено: %d записей", rows)))
}
