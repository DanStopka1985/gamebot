package database

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

var DB *sql.DB

// InitDB инициализирует подключение к базе данных
func InitDB(dbPassword string) error {
	connStr := fmt.Sprintf("host=localhost port=5433 user=postgres password=%s dbname=game sslmode=disable", dbPassword)
	var err error
	DB, err = sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("ошибка подключения к БД: %v", err)
	}

	if err = DB.Ping(); err != nil {
		return fmt.Errorf("БД не отвечает: %v", err)
	}

	log.Println("✅ Подключение к БД успешно")
	return nil
}

// CloseDB закрывает подключение к базе данных
func CloseDB() {
	if DB != nil {
		DB.Close()
	}
}
