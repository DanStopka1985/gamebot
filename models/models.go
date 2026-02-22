package models

import "time"

// Event структура для события
type Event struct {
	ID           int
	CategoryID   int
	CategoryName string
	DateTime     time.Time
	MemberLimit  int
	Registered   int
}

// User структура для пользователя
type User struct {
	ID         int
	TelegramID int64
	Nikname    string
	FirstName  string
	LastName   string
}

// Registration структура для записи
type Registration struct {
	UserID            int
	EventID           int
	ParticipantsCount int
	Status            string
}

// UserState структура для хранения состояния пользователя
type UserState struct {
	Action     string
	CategoryID int
	Step       string
	TempData   map[string]interface{}
}
