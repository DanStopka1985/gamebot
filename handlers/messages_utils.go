package handlers

import (
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// ==================== ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ ====================

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

// splitMessage вспомогательная функция для разбивки длинных сообщений
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
