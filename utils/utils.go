package utils

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// PlayerSearchResult результат поиска игрока
type PlayerSearchResult struct {
	ID           int
	FullName     string
	TelegramNick string
	TelegramName string
	MatchType    string // "telegram_nick", "telegram_name", "full_name", "partial"
	Confidence   int    // 0-100
}

// FindPlayer ищет игрока в базе по введенному тексту
func FindPlayer(db *sql.DB, input string) ([]PlayerSearchResult, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}

	// Подготавливаем варианты поиска
	cleanInput := strings.ToLower(input)
	cleanInput = strings.TrimPrefix(cleanInput, "@") // убираем @ если есть

	var results []PlayerSearchResult

	// 1. Точное совпадение по telegram_nick (без @)
	rows, err := db.Query(`
		SELECT id, full_name, telegram_nick, telegram_name
		FROM players
		WHERE LOWER(telegram_nick) = LOWER($1) OR LOWER(telegram_nick) = LOWER('@' || $1)
	`, cleanInput)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var r PlayerSearchResult
		var tn, tm sql.NullString
		err := rows.Scan(&r.ID, &r.FullName, &tn, &tm)
		if err != nil {
			continue
		}
		r.TelegramNick = tn.String
		r.TelegramName = tm.String
		r.MatchType = "telegram_nick"
		r.Confidence = 100
		results = append(results, r)
	}

	// 2. Поиск по full_name (точное совпадение)
	if len(results) == 0 {
		rows, err = db.Query(`
			SELECT id, full_name, telegram_nick, telegram_name
			FROM players
			WHERE LOWER(full_name) = LOWER($1)
		`, input)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var r PlayerSearchResult
				var tn, tm sql.NullString
				err := rows.Scan(&r.ID, &r.FullName, &tn, &tm)
				if err != nil {
					continue
				}
				r.TelegramNick = tn.String
				r.TelegramName = tm.String
				r.MatchType = "full_name"
				r.Confidence = 95
				results = append(results, r)
			}
		}
	}

	// 3. Поиск по частям (если ничего не нашли)
	if len(results) == 0 {
		searchPattern := "%" + cleanInput + "%"
		rows, err = db.Query(`
			SELECT id, full_name, telegram_nick, telegram_name
			FROM players
			WHERE LOWER(full_name) LIKE $1 
			   OR LOWER(telegram_nick) LIKE $1
			   OR LOWER(telegram_name) LIKE $1
			LIMIT 10
		`, searchPattern)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var r PlayerSearchResult
				var tn, tm sql.NullString
				err := rows.Scan(&r.ID, &r.FullName, &tn, &tm)
				if err != nil {
					continue
				}
				r.TelegramNick = tn.String
				r.TelegramName = tm.String
				r.MatchType = "partial"
				r.Confidence = 70
				results = append(results, r)
			}
		}
	}

	return results, nil
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

// IsAdmin проверяет, является ли пользователь администратором
func IsAdmin(adminIDs map[int64]bool, userID int64) bool {
	_, exists := adminIDs[userID]
	return exists
}
