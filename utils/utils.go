package utils

import (
	"database/sql"
	"fmt"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Pluralize возвращает правильную форму слова в зависимости от числа
func Pluralize(n int, form1, form2, form5 string) string {
	if n%10 == 1 && n%100 != 11 {
		return form1
	}
	if n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20) {
		return form2
	}
	return form5
}

// RegisterPersonIfNotExists регистрирует человека, если его нет в БД
func RegisterPersonIfNotExists(db *sql.DB, tgUser *tgbotapi.User) {
	var exists bool
	err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM person WHERE telegram_id = $1)`,
		tgUser.ID).Scan(&exists)

	if err != nil || exists {
		return
	}

	nikname := tgUser.UserName
	if nikname == "" {
		nikname = fmt.Sprintf("user_%d", tgUser.ID)
	}

	_, err = db.Exec(`
		INSERT INTO person (telegram_id, nikname, firstname, lastname)
		VALUES ($1, $2, $3, $4)
	`, tgUser.ID, nikname, tgUser.FirstName, tgUser.LastName)

	if err != nil {
		log.Printf("Ошибка регистрации пользователя: %v", err)
	}
}

// IsAdmin проверяет, является ли пользователь администратором
func IsAdmin(adminIDs map[int64]bool, userID int64) bool {
	_, exists := adminIDs[userID]
	return exists
}
