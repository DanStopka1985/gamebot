package handlers

import (
	"fmt"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// ChannelHandler обработчик сообщений из канала
type ChannelHandler struct {
	Bot *tgbotapi.BotAPI
}

// NewChannelHandler создает новый обработчик канала
func NewChannelHandler(bot *tgbotapi.BotAPI) *ChannelHandler {
	return &ChannelHandler{
		Bot: bot,
	}
}

// HandleChannelPost обрабатывает новый пост в канале
func (h *ChannelHandler) HandleChannelPost(post *tgbotapi.Message) {
	if h.Bot == nil {
		log.Println("❌ Ошибка: бот не инициализирован")
		return
	}

	// Создаем URL-кнопку с ссылкой на бота
	button := tgbotapi.NewInlineKeyboardButtonURL(
		"📝 Записаться на квиз",
		fmt.Sprintf("https://t.me/%s?start=event", h.Bot.Self.UserName),
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(button),
	)

	// Добавляем кнопку к посту
	editMsg := tgbotapi.NewEditMessageReplyMarkup(
		post.Chat.ID,
		post.MessageID,
		keyboard,
	)

	if _, err := h.Bot.Send(editMsg); err != nil {
		log.Printf("⚠️ Ошибка добавления кнопки к посту %d: %v", post.MessageID, err)
	} else {
		log.Printf("✅ Кнопка добавлена к посту %d", post.MessageID)
	}
}
