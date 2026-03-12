package handlers

import (
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

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
	case "📋 Список игроков":
		h.showPlayersList(chatID, 1, "")
	case "➕ Добавить игрока":
		h.startAddPlayer(message)
	case "🔍 Поиск игрока":
		h.searchPlayer(message)
	case "🚫 Заблокированные":
		h.showPlayersList(chatID, 1, "banned")
	case "💰 Управление оплатами":
		h.showPaymentManagement(chatID)
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
