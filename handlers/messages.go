package handlers

import (
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

// ==================== ФУНКЦИИ ДЛЯ УПРАВЛЕНИЯ ОПЛАТОЙ ====================

// showPaymentManagement показывает меню управления оплатами
func (h *MessageHandler) showPaymentManagement(chatID int64) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("💰 Запрос меню управления оплатами от администратора %d", chatID)

	// Получаем список событий с количеством участников и неплательщиков
	rows, err := database.DB.Query(`
		SELECT 
			e.id,
			c.name as category_name,
			e.evn_datetime,
			COALESCE(SUM(pe.participants_count), 0) as total_participants,
			COALESCE(SUM(
				CASE 
					WHEN pe.identification_data IS NULL THEN pe.participants_count
					ELSE (
						SELECT COUNT(*)
						FROM jsonb_array_elements(pe.identification_data) AS participant
						WHERE (participant->>'payment_status') IS NULL 
						   OR (participant->>'payment_status') = 'pending'
					)
				END
			), 0) as unpaid_count
		FROM event e
		JOIN category c ON e.category_id = c.id
		LEFT JOIN person_event pe ON e.id = pe.event_id AND pe.status = 'registered'
		GROUP BY e.id, c.name, e.evn_datetime
		HAVING COALESCE(SUM(pe.participants_count), 0) > 0
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

	eventCount := 0
	for rows.Next() {
		eventCount++
		var id int
		var categoryName string
		var eventDate time.Time
		var totalParticipants, unpaidParticipants int

		err := rows.Scan(&id, &categoryName, &eventDate, &totalParticipants, &unpaidParticipants)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		log.Printf("📊 Событие %d: всего=%d, не платили=%d", id, totalParticipants, unpaidParticipants)

		if unpaidParticipants > 0 {
			buttonText := fmt.Sprintf("%s %s (%d/%d не платили)",
				getPaymentEmoji(unpaidParticipants > 0),
				categoryName,
				unpaidParticipants,
				totalParticipants)

			buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(buttonText,
					fmt.Sprintf("admin:payment_event:%d", id)),
			))
		}
	}

	// Если есть события, но все оплачены
	if eventCount > 0 && len(buttons) == 1 {
		msg := tgbotapi.NewMessage(chatID, "💰 Все участники оплатили! 🎉")
		h.Bot.Send(msg)
		return
	}

	// Если нет событий с участниками
	if eventCount == 0 {
		msg := tgbotapi.NewMessage(chatID, "📭 Нет событий с участниками")
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

	// Получаем всех участников с информацией об оплате
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

	// Формируем заголовок
	text = fmt.Sprintf("💰 *Оплаты: %s* (%s)\n\n",
		eventInfo.CategoryName,
		eventInfo.DateTime.Format("02.01.2006 15:04"))

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
				for idx, p := range identified {
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

					// Добавляем кнопку для отметки об оплате, если еще не оплачено
					if paymentStatus != "paid" {
						buttonText := fmt.Sprintf("💰 Отметить: %s", displayName)
						if len(buttonText) > 40 {
							// Обрезаем если слишком длинное
							runes := []rune(buttonText)
							if len(runes) > 40 {
								buttonText = string(runes[:37]) + "..."
							}
						}

						// Создаем уникальный идентификатор для участника (person_event_id + индекс)
						participantKey := fmt.Sprintf("%d_%d", peID, idx)
						buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData(buttonText,
								fmt.Sprintf("admin:mark_participant_paid:%s", participantKey)),
						))
					}
				}
			}
		}
	}

	if !hasData {
		h.Bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf(
			"❌ Нет записей на событие %s", eventInfo.CategoryName)))
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

	// Отправляем сообщение
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	if len(buttons) > 0 {
		msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(buttons...)
	}

	if _, err := h.Bot.Send(msg); err != nil {
		log.Printf("❌ Ошибка отправки: %v", err)
		// Пробуем без Markdown
		msg.ParseMode = ""
		if _, err := h.Bot.Send(msg); err != nil {
			log.Printf("❌ Критическая ошибка отправки: %v", err)
		}
	}
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
