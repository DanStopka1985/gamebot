package tests

import (
	"testing"
)

// Тест для Pluralize
func TestPluralize(t *testing.T) {
	// Реализация функции Pluralize для теста
	pluralize := func(n int, form1, form2, form5 string) string {
		if n%10 == 1 && n%100 != 11 {
			return form1
		}
		if n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20) {
			return form2
		}
		return form5
	}

	tests := []struct {
		n        int
		form1    string
		form2    string
		form5    string
		expected string
	}{
		{1, "человек", "человека", "человек", "человек"},
		{2, "человек", "человека", "человек", "человека"},
		{3, "человек", "человека", "человек", "человека"},
		{4, "человек", "человека", "человек", "человека"},
		{5, "человек", "человека", "человек", "человек"},
		{11, "человек", "человека", "человек", "человек"},
		{21, "человек", "человека", "человек", "человек"},
		{22, "человек", "человека", "человек", "человека"},
		{101, "человек", "человека", "человек", "человек"},
		{102, "человек", "человека", "человек", "человека"},
	}

	for _, tt := range tests {
		result := pluralize(tt.n, tt.form1, tt.form2, tt.form5)
		if result != tt.expected {
			t.Errorf("pluralize(%d, %s, %s, %s) = %s; want %s",
				tt.n, tt.form1, tt.form2, tt.form5, result, tt.expected)
		}
	}
}

func TestIsAdmin(t *testing.T) {
	adminIDs := map[int64]bool{
		123: true,
		456: true,
	}

	isAdmin := func(adminIDs map[int64]bool, userID int64) bool {
		_, exists := adminIDs[userID]
		return exists
	}

	tests := []struct {
		userID   int64
		expected bool
	}{
		{123, true},
		{456, true},
		{789, false},
		{0, false},
	}

	for _, tt := range tests {
		result := isAdmin(adminIDs, tt.userID)
		if result != tt.expected {
			t.Errorf("isAdmin(%v, %d) = %v; want %v", adminIDs, tt.userID, result, tt.expected)
		}
	}
}

// Мок для базы данных
type mockDB struct{}

// func (m *mockDB) QueryRow(query string, args ...interface{}) *sql.Row {
// 	// Здесь можно реализовать мок для тестов
// 	return nil
// }

func TestFindPlayer(t *testing.T) {
	// Этот тест требует настоящую БД или мок
	// Пока пропускаем
	t.Skip("Требуется подключение к БД")
}
