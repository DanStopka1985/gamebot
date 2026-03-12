package handlers

import (
	"fmt"
	"gamebot/database"
	"gamebot/models"
	"log"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// ==================== ФУНКЦИИ ДЛЯ АДМИНИСТРИРОВАНИЯ ====================

// handleAdminCallback обрабатывает admin callback'и
func (h *CallbackHandler) handleAdminCallback(callback *tgbotapi.CallbackQuery, data []string) {
	chatID := callback.Message.Chat.ID
	userID := callback.From.ID

	if !h.isAdmin(userID) {
		return
	}

	if len(data) < 2 {
		return
	}

	log.Printf("👑 Админ callback: %v", data)

	switch data[1] {
	case "add_category":
		if len(data) < 3 {
			return
		}
		categoryID, _ := strconv.Atoi(data[2])

		h.UserStates[userID] = &models.UserState{
			Action:     "add_event",
			CategoryID: categoryID,
			Step:       "awaiting_datetime",
			TempData:   make(map[string]interface{}),
		}

		msg := tgbotapi.NewMessage(chatID,
			"Введите дату и время события в формате:\n`2026-03-15 10:00`\n\n"+
				"Пример: 2026-03-20 15:30")
		msg.ParseMode = "Markdown"
		h.Bot.Send(msg)

	case "confirm_add":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.confirmAddEvent(chatID, userID)

	case "cancel_add":
		delete(h.UserStates, userID)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Создание события отменено"))

	case "edit_event":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		h.showEventEditMenu(chatID, eventID)

	case "edit_date":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		h.UserStates[userID] = &models.UserState{
			Action:   "edit_event",
			Step:     "awaiting_new_date",
			TempData: map[string]interface{}{"event_id": eventID},
		}
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			"Введите новую дату и время в формате:\n2026-03-15 10:00"))

	case "edit_limit":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		h.UserStates[userID] = &models.UserState{
			Action:   "edit_event",
			Step:     "awaiting_new_limit",
			TempData: map[string]interface{}{"event_id": eventID},
		}
		h.Bot.Send(tgbotapi.NewMessage(chatID, "Введите новый лимит участников:"))

	case "delete_event":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		h.confirmDeleteEvent(chatID, eventID)

	case "confirm_delete":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		h.deleteEvent(chatID, eventID)

	case "delete_category":
		if len(data) < 3 {
			return
		}
		categoryID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.deleteCategory(chatID, categoryID)

	case "confirm_delete_category":
		if len(data) < 3 {
			return
		}
		categoryID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.confirmDeleteCategory(chatID, categoryID)

	case "view_event_registrations":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showEventRegistrations(chatID, eventID)

	case "view_all_registrations":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showAllRegistrationsFull(chatID)

	case "back_to_events":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showAllRegistrations(chatID)

	// НОВЫЕ CASE ДЛЯ УПРАВЛЕНИЯ ИГРОКАМИ
	case "players_menu":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showPlayersMenu(chatID)

	case "view_player":
		if len(data) < 3 {
			return
		}
		playerID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showPlayerDetails(chatID, playerID)

	case "edit_player":
		if len(data) < 3 {
			return
		}
		playerID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.startEditPlayer(chatID, userID, playerID)

	case "ban_player":
		if len(data) < 3 {
			return
		}
		playerID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.banPlayer(chatID, playerID)

	case "unban_player":
		if len(data) < 3 {
			return
		}
		playerID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.unbanPlayer(chatID, playerID)

	case "player_history":
		if len(data) < 3 {
			return
		}
		playerID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showPlayerHistory(chatID, playerID)

	case "add_player":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.startAddPlayer(&tgbotapi.Message{
			Chat: &tgbotapi.Chat{ID: chatID},
			From: &tgbotapi.User{ID: userID},
		})

	case "search_player":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.searchPlayer(&tgbotapi.Message{
			Chat: &tgbotapi.Chat{ID: chatID},
			From: &tgbotapi.User{ID: userID},
		})

	case "back_to_players_list":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showPlayersList(chatID, 1, "")

	case "back_to_players_menu":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showPlayersMenu(chatID)

	// НОВЫЕ CASE ДЛЯ УПРАВЛЕНИЯ ОПЛАТАМИ
	case "payment_menu":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showPaymentManagement(chatID)

	case "payment_event":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showEventPayments(chatID, eventID)

	case "all_unpaid":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showAllUnpaid(chatID)

	case "mark_paid":
		if len(data) < 3 {
			return
		}
		personEventID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.markAsPaid(chatID, personEventID)
		msgHandler.showPaymentManagement(chatID)

	case "mark_participant_paid":
		if len(data) < 3 {
			return
		}
		participantKey := data[2]
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)

		// Получаем eventID из participantKey (формат: "peID_idx")
		parts := strings.Split(participantKey, "_")
		if len(parts) == 2 {
			peID, _ := strconv.Atoi(parts[0])
			// Получаем eventID по person_event_id
			var evID int
			_ = database.DB.QueryRow(`SELECT event_id FROM person_event WHERE id = $1`, peID).Scan(&evID)

			msgHandler.markParticipantAsPaid(chatID, participantKey)
			// Обновляем отображение с правильным eventID
			if evID > 0 {
				msgHandler.showEventPayments(chatID, evID)
			} else {
				msgHandler.showPaymentManagement(chatID)
			}
		}
		return

	case "mark_all_paid":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.markAllAsPaid(chatID, eventID)
		msgHandler.showEventPayments(chatID, eventID)

	case "back_to_payments":
		msgHandler := NewMessageHandler(h.Bot, h.AdminIDs, h.UserStates)
		msgHandler.showPaymentManagement(chatID)

	case "back":
		delete(h.UserStates, userID)
		msg := tgbotapi.NewMessage(chatID, "👑 Панель администратора:")
		msg.ReplyMarkup = h.getAdminKeyboard()
		h.Bot.Send(msg)
	}
}

// getAdminKeyboard возвращает клавиатуру для админа
func (h *CallbackHandler) getAdminKeyboard() tgbotapi.ReplyKeyboardMarkup {
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
	return keyboard
}

// showEventEditMenu показывает меню редактирования события
func (h *CallbackHandler) showEventEditMenu(chatID int64, eventID int) {
	var e models.Event
	err := database.DB.QueryRow(`
		SELECT e.id, c.name, e.evn_datetime, e.member_limit
		FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&e.ID, &e.CategoryName, &e.DateTime, &e.MemberLimit)

	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Событие не найдено"))
		return
	}

	text := fmt.Sprintf(
		"Редактирование события #%d - *%s*\n\n"+
			"Текущие данные:\n"+
			"📆 Дата: %s\n"+
			"👥 Лимит: %d\n\n"+
			"Выберите действие:",
		e.ID,
		e.CategoryName,
		e.DateTime.Format("02.01.2006 15:04"),
		e.MemberLimit,
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📅 Изменить дату", fmt.Sprintf("admin:edit_date:%d", eventID)),
			tgbotapi.NewInlineKeyboardButtonData("👥 Изменить лимит", fmt.Sprintf("admin:edit_limit:%d", eventID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🗑 Удалить", fmt.Sprintf("admin:delete_event:%d", eventID)),
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "admin:back"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// confirmDeleteEvent подтверждает удаление события
func (h *CallbackHandler) confirmDeleteEvent(chatID int64, eventID int) {
	var eventName string
	database.DB.QueryRow(`
		SELECT c.name FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&eventName)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Да, удалить", fmt.Sprintf("admin:confirm_delete:%d", eventID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Нет, отмена", "admin:back"),
		),
	)

	msg := tgbotapi.NewMessage(chatID,
		fmt.Sprintf("⚠️ Вы уверены, что хотите удалить событие '%s'?\n"+
			"Все записи на это событие также будут удалены.", eventName))
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// deleteEvent удаляет событие
func (h *CallbackHandler) deleteEvent(chatID int64, eventID int) {
	var eventName string
	database.DB.QueryRow(`
		SELECT c.name FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&eventName)

	tx, err := database.DB.Begin()
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка сервера"))
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM person_event WHERE event_id = $1`, eventID)
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при удалении записей"))
		return
	}

	result, err := tx.Exec(`DELETE FROM event WHERE id = $1`, eventID)
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при удалении события"))
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Событие не найдено"))
		return
	}

	if err = tx.Commit(); err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при сохранении"))
		return
	}

	h.Bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Событие '%s' удалено", eventName)))
}
