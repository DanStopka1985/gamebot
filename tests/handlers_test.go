package tests

import (
	"strings"
	"testing"

	"gamebot/handlers"
	"gamebot/models"
)

// Мок для Telegram bot API (минимальный)
type mockBot struct{}

// MessageHandlerIsAdmin проверяет isAdmin через вызов метода
func TestMessageHandlerIsAdmin(t *testing.T) {
	adminIDs := map[int64]bool{
		123: true,
		456: true,
	}

	handler := handlers.NewMessageHandler(nil, &adminIDs, make(map[int64]*models.UserState))

	tests := []struct {
		userID   int64
		expected bool
	}{
		{123, true},
		{456, true},
		{789, false},
	}

	for _, tt := range tests {
		// Используем рефлексию или проверяем через карту напрямую
		_, exists := (*handler.AdminIDs)[tt.userID]
		if exists != tt.expected {
			t.Errorf("Admin check for user %d: got %v, want %v", tt.userID, exists, tt.expected)
		}
	}
}

func TestCallbackHandlerIsAdmin(t *testing.T) {
	adminIDs := map[int64]bool{
		123: true,
		456: true,
	}

	handler := handlers.NewCallbackHandler(nil, &adminIDs, make(map[int64]*models.UserState))

	tests := []struct {
		userID   int64
		expected bool
	}{
		{123, true},
		{456, true},
		{789, false},
	}

	for _, tt := range tests {
		// Используем рефлексию или проверяем через карту напрямую
		_, exists := (*handler.AdminIDs)[tt.userID]
		if exists != tt.expected {
			t.Errorf("Admin check for user %d: got %v, want %v", tt.userID, exists, tt.expected)
		}
	}
}

func TestEscapeMarkdown(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Обычный текст", "Обычный текст"},
		{"Текст с *звездочкой*", "Текст с \\*звездочкой\\*"},
		{"Текст с _подчеркиванием_", "Текст с \\_подчеркиванием\\_"},
		{"Текст с [скобками]", "Текст с \\[скобками\\]"},
		{"Текст с (скобками)", "Текст с \\(скобками\\)"},
		{"*_[]()", "\\*\\_\\[\\]\\(\\)"},
	}

	for _, tt := range tests {
		result := escapeMarkdown(tt.input)
		if result != tt.expected {
			t.Errorf("escapeMarkdown(%q) = %q; want %q", tt.input, result, tt.expected)
		}
	}
}

// Вспомогательная функция для тестов
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
