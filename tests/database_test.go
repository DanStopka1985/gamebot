package tests

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

const testConnStr = "host=localhost port=5433 user=postgres password=postgres dbname=game_test sslmode=disable"

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("postgres", testConnStr)
	if err != nil {
		t.Fatalf("Не удалось подключиться к тестовой БД: %v", err)
	}

	// Очищаем тестовые таблицы
	_, err = db.Exec(`
		DROP TABLE IF EXISTS person_event CASCADE;
		DROP TABLE IF EXISTS person CASCADE;
		DROP TABLE IF EXISTS event CASCADE;
		DROP TABLE IF EXISTS category CASCADE;
		DROP TABLE IF EXISTS players CASCADE;
		
		CREATE TABLE category (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL
		);
		
		CREATE TABLE event (
			id SERIAL PRIMARY KEY,
			category_id INTEGER REFERENCES category(id),
			evn_datetime TIMESTAMPTZ NOT NULL,
			member_limit INTEGER
		);
		
		CREATE TABLE person (
			id SERIAL PRIMARY KEY,
			telegram_id BIGINT UNIQUE,
			nikname TEXT,
			firstname TEXT,
			lastname TEXT
		);
		
		CREATE TABLE person_event (
			id SERIAL PRIMARY KEY,
			person_id INTEGER REFERENCES person(id),
			event_id INTEGER REFERENCES event(id),
			participants_count INTEGER DEFAULT 1,
			participants_info TEXT,
			player_ids INTEGER[],
			identification_data JSONB,
			status TEXT DEFAULT 'registered',
			registered_at TIMESTAMPTZ DEFAULT NOW()
		);
		
		CREATE TABLE players (
			id SERIAL PRIMARY KEY,
			full_name TEXT NOT NULL,
			telegram_nick TEXT,
			telegram_name TEXT,
			notes TEXT,
			is_active BOOLEAN DEFAULT true
		);
	`)
	if err != nil {
		t.Fatalf("Не удалось создать тестовые таблицы: %v", err)
	}

	return db
}

func TestCategoryCRUD(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Create
	var categoryID int
	err := db.QueryRow(`INSERT INTO category (name) VALUES ($1) RETURNING id`, "Тестовая категория").Scan(&categoryID)
	if err != nil {
		t.Fatalf("Failed to create category: %v", err)
	}

	if categoryID == 0 {
		t.Error("categoryID is 0")
	}

	// Read
	var name string
	err = db.QueryRow(`SELECT name FROM category WHERE id = $1`, categoryID).Scan(&name)
	if err != nil {
		t.Fatalf("Failed to read category: %v", err)
	}

	if name != "Тестовая категория" {
		t.Errorf("name = %s; want Тестовая категория", name)
	}

	// Update
	_, err = db.Exec(`UPDATE category SET name = $1 WHERE id = $2`, "Обновленная категория", categoryID)
	if err != nil {
		t.Fatalf("Failed to update category: %v", err)
	}

	err = db.QueryRow(`SELECT name FROM category WHERE id = $1`, categoryID).Scan(&name)
	if err != nil {
		t.Fatalf("Failed to read updated category: %v", err)
	}

	if name != "Обновленная категория" {
		t.Errorf("name after update = %s; want Обновленная категория", name)
	}

	// Delete
	result, err := db.Exec(`DELETE FROM category WHERE id = $1`, categoryID)
	if err != nil {
		t.Fatalf("Failed to delete category: %v", err)
	}

	rows, _ := result.RowsAffected()
	if rows != 1 {
		t.Errorf("Rows affected = %d; want 1", rows)
	}
}

func TestEventCRUD(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Create category first
	var categoryID int
	err := db.QueryRow(`INSERT INTO category (name) VALUES ($1) RETURNING id`, "Тестовая категория").Scan(&categoryID)
	if err != nil {
		t.Fatalf("Failed to create category: %v", err)
	}

	// Create event
	var eventID int
	err = db.QueryRow(`
		INSERT INTO event (category_id, evn_datetime, member_limit) 
		VALUES ($1, $2, $3) RETURNING id
	`, categoryID, time.Now().Add(24*time.Hour), 20).Scan(&eventID)
	if err != nil {
		t.Fatalf("Failed to create event: %v", err)
	}

	if eventID == 0 {
		t.Error("eventID is 0")
	}

	// Verify
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM event WHERE id = $1`, eventID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to verify event: %v", err)
	}

	if count != 1 {
		t.Errorf("count = %d; want 1", count)
	}
}

func TestPersonCRUD(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Create
	var personID int
	err := db.QueryRow(`
		INSERT INTO person (telegram_id, nikname, firstname, lastname) 
		VALUES ($1, $2, $3, $4) RETURNING id
	`, int64(123456789), "test_user", "Test", "User").Scan(&personID)
	if err != nil {
		t.Fatalf("Failed to create person: %v", err)
	}

	if personID == 0 {
		t.Error("personID is 0")
	}

	// Read
	var nikname string
	err = db.QueryRow(`SELECT nikname FROM person WHERE id = $1`, personID).Scan(&nikname)
	if err != nil {
		t.Fatalf("Failed to read person: %v", err)
	}

	if nikname != "test_user" {
		t.Errorf("nikname = %s; want test_user", nikname)
	}
}

func TestPlayersCRUD(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Create
	var playerID int
	err := db.QueryRow(`
		INSERT INTO players (full_name, telegram_nick, telegram_name, notes, is_active) 
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, "Иван Петров", "@ivan_petrov", "Иван", "Тестовая запись", true).Scan(&playerID)
	if err != nil {
		t.Fatalf("Failed to create player: %v", err)
	}

	if playerID == 0 {
		t.Error("playerID is 0")
	}

	// Read
	var fullName string
	err = db.QueryRow(`SELECT full_name FROM players WHERE id = $1`, playerID).Scan(&fullName)
	if err != nil {
		t.Fatalf("Failed to read player: %v", err)
	}

	if fullName != "Иван Петров" {
		t.Errorf("fullName = %s; want Иван Петров", fullName)
	}
}
