package tests

import (
	"log"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Настройка перед тестами
	log.Println("Начинаем тестирование...")

	// Запуск тестов
	code := m.Run()

	// Очистка после тестов
	log.Println("Тестирование завершено")

	os.Exit(code)
}
