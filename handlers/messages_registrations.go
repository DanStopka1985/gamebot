package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/lib/pq"

	"gamebot/database"
)

// ==================== ФУНКЦИИ ДЛЯ ЗАПИСЕЙ ====================

// showAllRegistrations показывает список событий для выбора записей
func (h *MessageHandler) showAllRegistrations(chatID int64) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("👥 Запрос списка событий для просмотра записей от администратора %d", chatID)

	rows, err := database.DB.Query(`
		SELECT 
			e.id,
			c.name as category_name,
			e.evn_datetime,
			COUNT(pe.id) as registrations_count,
			COALESCE(SUM(pe.participants_count), 0) as participants_count
		FROM event e
		JOIN category c ON e.category_id = c.id
		LEFT JOIN person_event pe ON e.id = pe.event_id AND pe.status = 'registered'
		GROUP BY e.id, c.name, e.evn_datetime
		ORDER BY e.evn_datetime DESC
	`)

	if err != nil {
		log.Printf("❌ Ошибка загрузки событий: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки событий"))
		return
	}
	defer rows.Close()

	var events []struct {
		ID                 int
		CategoryName       string
		DateTime           time.Time
		RegistrationsCount int
		ParticipantsCount  int
	}

	for rows.Next() {
		var e struct {
			ID                 int
			CategoryName       string
			DateTime           time.Time
			RegistrationsCount int
			ParticipantsCount  int
		}
		err := rows.Scan(&e.ID, &e.CategoryName, &e.DateTime, &e.RegistrationsCount, &e.ParticipantsCount)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}
		events = append(events, e)
	}

	if len(events) == 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "📭 Нет событий"))
		return
	}

	var buttons [][]tgbotapi.InlineKeyboardButton
	for _, e := range events {
		buttonText := fmt.Sprintf("%s - %s (%d записей, %d уч.)",
			e.CategoryName,
			e.DateTime.Format("02.01.2006 15:04"),
			e.RegistrationsCount,
			e.ParticipantsCount)

		button := tgbotapi.NewInlineKeyboardButtonData(
			buttonText,
			fmt.Sprintf("admin:view_event_registrations:%d", e.ID),
		)
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(button))
	}

	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📋 Все события (все записи)", "admin:view_all_registrations"),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)

	msg := tgbotapi.NewMessage(chatID, "📊 *Выберите событие для просмотра записей:*")
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	h.Bot.Send(msg)
}

// showEventRegistrations показывает записи конкретного события с идентификацией
func (h *MessageHandler) showEventRegistrations(chatID int64, eventID int) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("👥 Запрос записей для события %d от администратора %d", eventID, chatID)

	var eventInfo struct {
		CategoryName string
		DateTime     time.Time
	}
	err := database.DB.QueryRow(`
		SELECT c.name, e.evn_datetime
		FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&eventInfo.CategoryName, &eventInfo.DateTime)

	if err != nil {
		log.Printf("❌ Ошибка загрузки информации о событии: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки события"))
		return
	}

	rows, err := database.DB.Query(`
		SELECT 
			p.id as person_id,
			p.nikname,
			p.firstname,
			p.lastname,
			pe.participants_count,
			pe.participants_info,
			pe.player_ids,
			pe.identification_data,
			pe.registered_at
		FROM person_event pe
		JOIN person p ON pe.person_id = p.id
		WHERE pe.event_id = $1 AND pe.status = 'registered'
		ORDER BY pe.registered_at DESC
	`, eventID)

	if err != nil {
		log.Printf("❌ Ошибка загрузки записей: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки записей"))
		return
	}
	defer rows.Close()

	var allEntries []string
	totalParticipants := 0
	totalRegistrations := 0
	totalIdentified := 0

	for rows.Next() {
		totalRegistrations++

		var personID int64
		var nikname, firstname, lastname string
		var participants int
		var participantsInfo sql.NullString
		var playerIDs []int64
		var identificationData []byte
		var regDate time.Time

		err := rows.Scan(&personID, &nikname, &firstname, &lastname,
			&participants, &participantsInfo, pq.Array(&playerIDs), &identificationData, &regDate)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		// Формируем информацию о записавшем
		registrantInfo := ""
		if nikname != "" {
			registrantInfo += "@" + nikname
		}
		if firstname != "" || lastname != "" {
			if registrantInfo != "" {
				registrantInfo += " "
			}
			registrantInfo += firstname + " " + lastname
		}
		if registrantInfo == "" {
			registrantInfo = fmt.Sprintf("ID: %d", personID)
		}

		// Заголовок записи
		entry := fmt.Sprintf(
			"👤 *Записал:* %s\n"+
				"📅 *Дата записи:* %s\n"+
				"📊 *Участники (%d чел.):*\n",
			registrantInfo,
			regDate.Format("02.01.2006 15:04"),
			participants)

		// Парсим данные об идентифицированных игроках
		if len(identificationData) > 0 {
			var identified []map[string]interface{}
			if err := json.Unmarshal(identificationData, &identified); err == nil {
				for i, id := range identified {
					totalParticipants++
					if pid, ok := id["player_id"].(float64); ok && pid > 0 {
						totalIdentified++
						entry += fmt.Sprintf("   %d. ✅ *%s* (ID: %.0f)\n",
							i+1, id["full_name"], pid)
					} else {
						entry += fmt.Sprintf("   %d. ⚠️ *%s* (не в базе)\n",
							i+1, id["full_name"])
					}
				}
			} else {
				// Если не смогли распарсить JSON, показываем обычную информацию
				if participantsInfo.Valid {
					names := strings.Split(participantsInfo.String, ", ")
					for i, name := range names {
						totalParticipants++
						entry += fmt.Sprintf("   %d. %s\n", i+1, name)
					}
				}
			}
		} else if participantsInfo.Valid {
			names := strings.Split(participantsInfo.String, ", ")
			for i, name := range names {
				totalParticipants++
				entry += fmt.Sprintf("   %d. %s\n", i+1, name)
			}
		}

		entry += "   ──────────────────────────\n"
		allEntries = append(allEntries, entry)
	}

	header := fmt.Sprintf("📊 *Событие: %s*\n", eventInfo.CategoryName)
	header += fmt.Sprintf("📆 *Дата:* %s\n", eventInfo.DateTime.Format("02.01.2006 15:04"))
	header += fmt.Sprintf("📝 *Всего записей:* %d\n", totalRegistrations)
	header += fmt.Sprintf("👥 *Всего участников:* %d\n", totalParticipants)
	header += fmt.Sprintf("✅ *Идентифицировано:* %d\n", totalIdentified)
	header += "════════════════════════════════════════\n\n"

	cleanHeader := strings.ToValidUTF8(header, "?")

	var fullText string
	if len(allEntries) == 0 {
		fullText = cleanHeader + "❌ Нет записей на это событие"
	} else {
		fullText = cleanHeader + strings.Join(allEntries, "")
	}

	backButton := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад к списку событий", "admin:back_to_events"),
		),
	)

	h.sendLongMessage(chatID, fullText, &backButton)
}

// showAllRegistrationsFull показывает все записи по всем событиям
func (h *MessageHandler) showAllRegistrationsFull(chatID int64) {
	if !h.isAdmin(chatID) {
		return
	}

	log.Printf("👥 Запрос всех записей от администратора %d", chatID)

	rows, err := database.DB.Query(`
		SELECT 
			e.id as event_id,
			c.name as category_name,
			TO_CHAR(e.evn_datetime, 'DD.MM.YYYY HH24:MI') as event_date,
			p.id as person_id,
			p.nikname,
			p.firstname,
			p.lastname,
			pe.participants_count,
			pe.participants_info,
			TO_CHAR(pe.registered_at, 'DD.MM.YYYY HH24:MI') as reg_date
		FROM person_event pe
		JOIN event e ON pe.event_id = e.id
		JOIN category c ON e.category_id = c.id
		JOIN person p ON pe.person_id = p.id
		WHERE pe.status = 'registered'
		ORDER BY e.evn_datetime DESC, pe.registered_at DESC
	`)

	if err != nil {
		log.Printf("❌ Ошибка загрузки записей: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
		return
	}
	defer rows.Close()

	var allEntries []string
	totalParticipants := 0
	totalRegistrations := 0
	currentEventID := 0

	for rows.Next() {
		var eventID int
		var categoryName, eventDate, nikname, firstname, lastname, regDate string
		var participants int
		var participantsInfo sql.NullString
		var personID int64

		err := rows.Scan(&eventID, &categoryName, &eventDate, &personID, &nikname, &firstname, &lastname,
			&participants, &participantsInfo, &regDate)
		if err != nil {
			log.Printf("❌ Ошибка сканирования: %v", err)
			continue
		}

		if eventID != currentEventID {
			if currentEventID != 0 {
				allEntries = append(allEntries, "════════════════════════════════════════\n\n")
			}
			currentEventID = eventID
			eventHeader := fmt.Sprintf("📅 *Событие #%d: %s*\n", eventID, categoryName)
			eventHeader += fmt.Sprintf("   📆 %s\n\n", eventDate)
			allEntries = append(allEntries, eventHeader)
		}

		totalRegistrations++

		registrantInfo := ""
		if nikname != "" {
			registrantInfo += "@" + nikname
		}
		if firstname != "" || lastname != "" {
			if registrantInfo != "" {
				registrantInfo += " "
			}
			registrantInfo += firstname + " " + lastname
		}
		if registrantInfo == "" {
			registrantInfo = fmt.Sprintf("ID: %d", personID)
		}

		if participantsInfo.Valid && participantsInfo.String != "" &&
			participantsInfo.String != fmt.Sprintf("%d человек", participants) {
			cleanInfo := strings.ToValidUTF8(participantsInfo.String, "?")
			participantNames := strings.Split(cleanInfo, ", ")
			for _, name := range participantNames {
				totalParticipants++
				cleanName := strings.ToValidUTF8(strings.TrimSpace(name), "?")

				participantDisplay := ""
				if strings.HasPrefix(cleanName, "@") {
					participantDisplay = "📱 " + cleanName
				} else {
					participantDisplay = "👤 " + cleanName
				}

				entry := fmt.Sprintf(
					"   👤 *Записал:* %s\n"+
						"   🧑 *Участник:* %s\n"+
						"   📅 *Запись:* %s\n\n",
					registrantInfo,
					participantDisplay,
					regDate)

				allEntries = append(allEntries, entry)
			}
		} else {
			for i := 1; i <= participants; i++ {
				totalParticipants++
				entry := fmt.Sprintf(
					"   👤 *Записал:* %s\n"+
						"   🧑 *Участник #%d*\n"+
						"   📅 *Запись:* %s\n\n",
					registrantInfo,
					i,
					regDate)

				allEntries = append(allEntries, entry)
			}
		}
	}

	if len(allEntries) == 0 {
		h.Bot.Send(tgbotapi.NewMessage(chatID, "📭 Нет записей"))
		return
	}

	header := fmt.Sprintf("📊 *Все записи по всем событиям*\n\n")
	header += fmt.Sprintf("📝 *Всего записей:* %d\n", totalRegistrations)
	header += fmt.Sprintf("👥 *Всего участников:* %d\n", totalParticipants)
	header += "════════════════════════════════════════\n\n"

	cleanHeader := strings.ToValidUTF8(header, "?")
	fullText := cleanHeader + strings.Join(allEntries, "")

	backButton := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад к списку событий", "admin:back_to_events"),
		),
	)

	h.sendLongMessage(chatID, fullText, &backButton)
}
