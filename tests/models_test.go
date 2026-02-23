package tests

import (
	"gamebot/models"
	"testing"
	"time"
)

func TestEventModel(t *testing.T) {
	now := time.Now()
	event := models.Event{
		ID:           1,
		CategoryID:   1,
		CategoryName: "Кино",
		DateTime:     now,
		MemberLimit:  20,
		Registered:   5,
	}

	if event.ID != 1 {
		t.Errorf("Event.ID = %d; want 1", event.ID)
	}
	if event.CategoryName != "Кино" {
		t.Errorf("Event.CategoryName = %s; want Кино", event.CategoryName)
	}
	if event.MemberLimit != 20 {
		t.Errorf("Event.MemberLimit = %d; want 20", event.MemberLimit)
	}
	if event.Registered != 5 {
		t.Errorf("Event.Registered = %d; want 5", event.Registered)
	}
}

func TestPersonModel(t *testing.T) {
	person := models.Person{
		ID:         1,
		TelegramID: 123456789,
		Nikname:    "test_user",
		FirstName:  "Test",
		LastName:   "User",
	}

	if person.ID != 1 {
		t.Errorf("Person.ID = %d; want 1", person.ID)
	}
	if person.TelegramID != 123456789 {
		t.Errorf("Person.TelegramID = %d; want 123456789", person.TelegramID)
	}
	if person.Nikname != "test_user" {
		t.Errorf("Person.Nikname = %s; want test_user", person.Nikname)
	}
}

func TestPersonEventModel(t *testing.T) {
	now := time.Now()
	personEvent := models.PersonEvent{
		ID:                1,
		PersonID:          1,
		EventID:           1,
		ParticipantsCount: 3,
		Status:            "registered",
		ParticipantsInfo:  "Иван, Петр, Мария",
		RegisteredAt:      now,
	}

	if personEvent.ID != 1 {
		t.Errorf("PersonEvent.ID = %d; want 1", personEvent.ID)
	}
	if personEvent.PersonID != 1 {
		t.Errorf("PersonEvent.PersonID = %d; want 1", personEvent.PersonID)
	}
	if personEvent.ParticipantsCount != 3 {
		t.Errorf("PersonEvent.ParticipantsCount = %d; want 3", personEvent.ParticipantsCount)
	}
	if personEvent.Status != "registered" {
		t.Errorf("PersonEvent.Status = %s; want registered", personEvent.Status)
	}
}

func TestUserStateModel(t *testing.T) {
	userState := models.UserState{
		Action:     "add_event",
		CategoryID: 1,
		Step:       "awaiting_datetime",
		TempData:   make(map[string]interface{}),
	}

	if userState.Action != "add_event" {
		t.Errorf("UserState.Action = %s; want add_event", userState.Action)
	}
	if userState.CategoryID != 1 {
		t.Errorf("UserState.CategoryID = %d; want 1", userState.CategoryID)
	}
	if userState.Step != "awaiting_datetime" {
		t.Errorf("UserState.Step = %s; want awaiting_datetime", userState.Step)
	}
	if userState.TempData == nil {
		t.Error("UserState.TempData is nil")
	}
}
