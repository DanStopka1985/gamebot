package handlers

import (
	"fmt"
	"log"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"gamebot/database"
)

// ==================== СТАТИСТИКА ====================

// showStats показывает общую статистику
func (h *MessageHandler) showStats(chatID int64) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("📊 Запрос статистики от администратора %d", chatID)

	var totalEvents, totalPersons, totalRegistrations, totalCategories int
	var upcomingEvents int
	var totalParticipants int

	database.DB.QueryRow(`SELECT COUNT(*) FROM event`).Scan(&totalEvents)
	database.DB.QueryRow(`SELECT COUNT(*) FROM person`).Scan(&totalPersons)
	database.DB.QueryRow(`SELECT COUNT(*) FROM category`).Scan(&totalCategories)
	database.DB.QueryRow(`SELECT COUNT(*) FROM person_event WHERE status = 'registered'`).Scan(&totalRegistrations)
	database.DB.QueryRow(`SELECT COALESCE(SUM(participants_count), 0) FROM person_event WHERE status = 'registered'`).Scan(&totalParticipants)
	database.DB.QueryRow(`SELECT COUNT(*) FROM event WHERE evn_datetime > NOW()`).Scan(&upcomingEvents)

	text := fmt.Sprintf(
		"📊 *Общая статистика*\n\n"+
			"📁 Категорий: %d\n"+
			"📅 Всего событий: %d\n"+
			"⏳ Предстоящих: %d\n"+
			"👥 Пользователей: %d\n"+
			"📝 Всего записей: %d\n"+
			"👥 Всего участников: %d",
		totalCategories, totalEvents, upcomingEvents, totalPersons, totalRegistrations, totalParticipants)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	h.Bot.Send(msg)
}

// showEventStats показывает статистику по конкретному событию
func (h *MessageHandler) showEventStats(chatID int64, eventIDStr string) {
	if !h.isAdmin(chatID) {
		return
	}

	eventID, err := strconv.Atoi(eventIDStr)
	if err != nil {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "Использование: /eventstats ID"))
		return
	}

	log.Printf("📊 Запрос статистики по событию %d от администратора %d", eventID, chatID)

	var eventName string
	var eventDate time.Time
	var memberLimit, registered, totalParticipants int

	err = database.DB.QueryRow(`
		SELECT c.name, e.evn_datetime, e.member_limit,
		       COUNT(DISTINCT pe.id),
		       COALESCE(SUM(pe.participants_count), 0)
		FROM event e
		JOIN category c ON e.category_id = c.id
		LEFT JOIN person_event pe ON e.id = pe.event_id AND pe.status = 'registered'
		WHERE e.id = $1
		GROUP BY c.name, e.evn_datetime, e.member_limit
	`, eventID).Scan(&eventName, &eventDate, &memberLimit, &registered, &totalParticipants)

	if err != nil {
		log.Printf("❌ Ошибка загрузки статистики: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
		return
	}

	text := fmt.Sprintf(
		"📊 *Статистика события #%d*\n\n"+
			"📌 %s\n"+
			"📆 %s\n"+
			"👥 Лимит: %d\n"+
			"📝 Записей: %d\n"+
			"👥 Участников: %d\n"+
			"📈 Свободно: %d",
		eventID, eventName, eventDate.Format("02.01.2006 15:04"),
		memberLimit, registered, totalParticipants, memberLimit-totalParticipants)

	h.Bot.Send(tgbotapi.NewMessage(chatID, text))
}
