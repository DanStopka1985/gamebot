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
