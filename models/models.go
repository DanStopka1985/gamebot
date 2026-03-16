package models

import "time"

// Event структура для события
// Event структура для события
type Event struct {
	ID           int
	CategoryID   int
	CategoryName string
	DateTime     time.Time
	MemberLimit  int
	Registered   int
	EventType    string // regular или flexible
}

// Person структура для пользователя
type Person struct {
	ID         int
	TelegramID int64
	Nikname    string
	FirstName  string
	LastName   string
}

// PersonEvent структура для записи
type PersonEvent struct {
	ID                int
	PersonID          int
	EventID           int
	ParticipantsCount int
	Status            string
	ParticipantsInfo  string
	RegisteredAt      time.Time
}

// UserState структура для хранения состояния пользователя
type UserState struct {
	Action     string
	CategoryID int
	EventType  string // regular или flexible
	Step       string
	TempData   map[string]interface{}
}
