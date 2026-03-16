package handlers

import (
	"database/sql"

	"fmt"
	"log"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"gamebot/database"
	"gamebot/models"
	"gamebot/utils"
)

// ==================== ФУНКЦИИ ДЛЯ ИДЕНТИФИКАЦИИ ИГРОКОВ ====================

// askParticipantNames запрашивает имена участников с возможностью идентификации
func (h *CallbackHandler) askParticipantNames(chatID int64, eventID int, userID int64, count int) {
	h.UserStates[userID] = &models.UserState{
		Action: "entering_names",
		Step:   "awaiting_names",
		TempData: map[string]interface{}{
			"event_id":           eventID,
			"count":              count,
			"inputs":             []string{},
			"identified_players": []map[string]interface{}{},
		},
	}

	msgText := fmt.Sprintf(
		"👥 Вы записываете %d человек.\n\n"+
			"Введите имена или никнеймы всех участников, *каждого с новой строки*.\n\n"+
			"📱 *Как лучше вводить:*\n"+
			"• Telegram никнейм (например, @john_doe)\n"+
			"• Имя и фамилию (например, Иван Петров)\n"+
			"• Любую информацию, по которой можно идентифицировать игрока\n\n"+
			"*Пример:*\n"+
			"@john_doe\n"+
			"Иван Петров\n"+
			"Мария\n\n"+
			"Я попробую найти игроков в базе и предложу варианты.",
		count)

	msg := tgbotapi.NewMessage(chatID, msgText)
	msg.ParseMode = "Markdown"
	h.Bot.Send(msg)
}

// handleParticipantNamesWithSearch обрабатывает ввод с поиском игроков
func (h *CallbackHandler) handleParticipantNamesWithSearch(message *tgbotapi.Message) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := message.Text

	state, exists := h.UserStates[userID]
	if !exists || state.Action != "entering_names" {
		return
	}

	// Разбиваем текст на строки
	lines := strings.Split(text, "\n")
	var inputs []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			inputs = append(inputs, line)
		}
	}

	expectedCount := state.TempData["count"].(int)

	if len(inputs) != expectedCount {
		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("❌ Нужно ввести ровно %d имен. Сейчас введено %d.\nПопробуйте снова:",
				expectedCount, len(inputs))))
		return
	}

	// Сохраняем введенные строки
	state.TempData["inputs"] = inputs
	state.TempData["identified_players"] = []map[string]interface{}{}

	// Начинаем процесс идентификации первого игрока
	h.identifyNextPlayer(chatID, userID, 0)
}

// identifyNextPlayer идентифицирует следующего игрока
func (h *CallbackHandler) identifyNextPlayer(chatID int64, userID int64, index int) {
	state, exists := h.UserStates[userID]
	if !exists {
		return
	}

	inputs := state.TempData["inputs"].([]string)
	if index >= len(inputs) {
		// Все игроки обработаны, показываем результат
		h.showIdentificationResult(chatID, userID)
		return
	}

	input := inputs[index]
	state.TempData["current_index"] = index

	// Проверяем специальные ключевые слова (я, себя, меня)
	inputLower := strings.ToLower(strings.TrimSpace(input))
	if inputLower == "я" || inputLower == "себя" || inputLower == "меня" {
		log.Printf("👤 Обнаружено ключевое слово '%s' - добавляем текущего пользователя", input)

		// Получаем данные пользователя из таблицы person
		var dbPersonID int
		var nikname, firstname, lastname string
		err := database.DB.QueryRow(`
			SELECT id, nikname, firstname, lastname FROM person WHERE telegram_id = $1
		`, userID).Scan(&dbPersonID, &nikname, &firstname, &lastname)

		if err != nil {
			log.Printf("❌ Ошибка получения данных пользователя: %v", err)
			// Если не удалось получить, создаем базовую информацию
			nikname = fmt.Sprintf("user_%d", userID)
			firstname = "Пользователь"
		}

		// Пытаемся найти player_id в таблице players
		var playerID int
		var playerFullName string
		var playerTelegramNick string

		// Ищем по никнейму
		if nikname != "" {
			err = database.DB.QueryRow(`
				SELECT id, full_name, telegram_nick FROM players 
				WHERE telegram_nick = $1 OR telegram_nick = '@' || $1
			`, nikname).Scan(&playerID, &playerFullName, &playerTelegramNick)
		}

		// Если не нашли, ищем по имени
		if err != nil && firstname != "" {
			// Убрали неиспользуемую переменную searchName
			err = database.DB.QueryRow(`
				SELECT id, full_name, telegram_nick FROM players 
				WHERE full_name ILIKE $1
			`, "%"+firstname+"%").Scan(&playerID, &playerFullName, &playerTelegramNick)
		}

		fullName := fmt.Sprintf("%s %s", firstname, lastname)
		if strings.TrimSpace(fullName) == "" {
			if nikname != "" {
				fullName = nikname
			} else {
				fullName = "Пользователь"
			}
		}

		telegramUsername := ""
		if nikname != "" {
			telegramUsername = "@" + nikname
		}

		// Сохраняем идентифицированного игрока
		identified := state.TempData["identified_players"].([]map[string]interface{})

		if playerID > 0 {
			// Нашли в базе players
			identified = append(identified, map[string]interface{}{
				"player_id":     playerID,
				"input":         input,
				"full_name":     playerFullName,
				"telegram_nick": playerTelegramNick,
				"method":        "self_identified",
			})
			log.Printf("✅ Пользователь найден в players: ID=%d, имя=%s", playerID, playerFullName)
		} else {
			// Не нашли в базе
			identified = append(identified, map[string]interface{}{
				"player_id":     0,
				"input":         input,
				"full_name":     fullName,
				"telegram_nick": telegramUsername,
				"method":        "self",
			})
			log.Printf("⚠️ Пользователь не найден в players, добавляем как '%s'", fullName)
		}

		state.TempData["identified_players"] = identified

		// Переходим к следующему
		h.identifyNextPlayer(chatID, userID, index+1)
		return
	}

	// Ищем игрока в базе players
	results, err := utils.FindPlayer(database.DB, input)
	if err != nil {
		log.Printf("❌ Ошибка поиска игрока: %v", err)
		h.Bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при поиске. Попробуйте еще раз."))
		return
	}

	if len(results) == 0 {
		// Игрок не найден - предлагаем ввести вручную
		msg := fmt.Sprintf(
			"🔍 Игрок #%d: %s\n\n"+
				"❌ Не удалось найти в базе.\n\n"+
				"Введите информацию об этом игроке вручную (имя, ник, любые данные):",
			index+1, input)

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("⏭ Пропустить (без идентификации)",
					fmt.Sprintf("identify_skip:%d", index)),
			),
		)

		msgObj := tgbotapi.NewMessage(chatID, msg)
		msgObj.ParseMode = ""
		msgObj.ReplyMarkup = keyboard
		h.Bot.Send(msgObj)

		state.Step = "manual_input"
		return
	}

	if len(results) == 1 {
		// Найден один игрок - показываем для подтверждения
		r := results[0]

		fullName := escapeMarkdown(r.FullName)
		inputEscaped := escapeMarkdown(input)

		msg := fmt.Sprintf(
			"🔍 Игрок #%d: *%s*\n\n"+
				"✅ Найден игрок:\n"+
				"📝 ФИО: %s\n",
			index+1, inputEscaped, fullName)

		if r.TelegramNick != "" {
			telegramNick := escapeMarkdown(r.TelegramNick)
			msg += fmt.Sprintf("📱 Telegram: %s\n", telegramNick)
		}
		if r.TelegramName != "" {
			telegramName := escapeMarkdown(r.TelegramName)
			msg += fmt.Sprintf("👤 Имя в TG: %s\n", telegramName)
		}
		msg += fmt.Sprintf("\nЭто тот игрок?")

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Да", fmt.Sprintf("identify_confirm:%d:%d", index, r.ID)),
				tgbotapi.NewInlineKeyboardButtonData("❌ Нет, искать еще", fmt.Sprintf("identify_search:%d", index)),
			),
		)

		msgObj := tgbotapi.NewMessage(chatID, msg)
		msgObj.ParseMode = "Markdown"
		msgObj.ReplyMarkup = keyboard
		h.Bot.Send(msgObj)

		state.Step = "confirming"
		return
	}

	// Найдено несколько вариантов
	msg := fmt.Sprintf("🔍 Игрок #%d: *%s*\n\nНайдено несколько вариантов. Выберите нужного:\n\n",
		index+1, escapeMarkdown(input))

	var buttons [][]tgbotapi.InlineKeyboardButton
	for i, r := range results {
		buttonText := fmt.Sprintf("%d. %s", i+1, r.FullName)
		if r.TelegramNick != "" {
			buttonText += fmt.Sprintf(" (%s)", r.TelegramNick)
		}
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(buttonText,
				fmt.Sprintf("identify_select:%d:%d", index, r.ID)),
		))
	}
	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("➕ Ввести вручную", fmt.Sprintf("identify_manual:%d", index)),
	))
	buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⏭ Пропустить", fmt.Sprintf("identify_skip:%d", index)),
	))

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)
	msgObj := tgbotapi.NewMessage(chatID, msg)
	msgObj.ParseMode = "Markdown"
	msgObj.ReplyMarkup = keyboard
	h.Bot.Send(msgObj)

	state.Step = "selecting"
}

// handleIdentificationCallbacks обрабатывает callback'и идентификации
func (h *CallbackHandler) handleIdentificationCallbacks(callback *tgbotapi.CallbackQuery, data []string) {
	userID := callback.From.ID
	chatID := callback.Message.Chat.ID

	state, exists := h.UserStates[userID]
	if !exists || state.Action != "entering_names" {
		return
	}

	switch data[0] {
	case "identify_confirm":
		if len(data) < 3 {
			return
		}
		index, _ := strconv.Atoi(data[1])
		playerID, _ := strconv.Atoi(data[2])

		// Получаем информацию об игроке
		var player struct {
			FullName     string
			TelegramNick sql.NullString
			TelegramName sql.NullString
		}
		err := database.DB.QueryRow(`
			SELECT full_name, telegram_nick, telegram_name
			FROM players WHERE id = $1
		`, playerID).Scan(&player.FullName, &player.TelegramNick, &player.TelegramName)

		if err != nil {
			log.Printf("❌ Ошибка получения игрока: %v", err)
			return
		}

		// Сохраняем идентифицированного игрока
		identified := state.TempData["identified_players"].([]map[string]interface{})
		identified = append(identified, map[string]interface{}{
			"player_id":     playerID,
			"input":         state.TempData["inputs"].([]string)[index],
			"method":        "identified",
			"full_name":     player.FullName,
			"telegram_nick": player.TelegramNick.String,
			"telegram_name": player.TelegramName.String,
		})
		state.TempData["identified_players"] = identified

		// Переходим к следующему
		h.identifyNextPlayer(chatID, userID, index+1)

	case "identify_select":
		if len(data) < 3 {
			return
		}
		index, _ := strconv.Atoi(data[1])
		playerID, _ := strconv.Atoi(data[2])

		var player struct {
			FullName     string
			TelegramNick sql.NullString
			TelegramName sql.NullString
		}
		database.DB.QueryRow(`
			SELECT full_name, telegram_nick, telegram_name
			FROM players WHERE id = $1
		`, playerID).Scan(&player.FullName, &player.TelegramNick, &player.TelegramName)

		identified := state.TempData["identified_players"].([]map[string]interface{})
		identified = append(identified, map[string]interface{}{
			"player_id":     playerID,
			"input":         state.TempData["inputs"].([]string)[index],
			"method":        "selected",
			"full_name":     player.FullName,
			"telegram_nick": player.TelegramNick.String,
			"telegram_name": player.TelegramName.String,
		})
		state.TempData["identified_players"] = identified

		h.identifyNextPlayer(chatID, userID, index+1)

	case "identify_manual":
		if len(data) < 2 {
			return
		}
		index, _ := strconv.Atoi(data[1])

		state.TempData["manual_index"] = index
		state.Step = "manual_input"

		h.Bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("Введите информацию об игроке #%d вручную:", index+1)))

	case "identify_skip":
		if len(data) < 2 {
			return
		}
		index, _ := strconv.Atoi(data[1])

		inputs := state.TempData["inputs"].([]string)
		identified := state.TempData["identified_players"].([]map[string]interface{})
		identified = append(identified, map[string]interface{}{
			"player_id": 0,
			"input":     inputs[index],
			"method":    "skipped",
			"full_name": inputs[index],
		})
		state.TempData["identified_players"] = identified

		h.identifyNextPlayer(chatID, userID, index+1)

	case "identify_search":
		if len(data) < 2 {
			return
		}
		index, _ := strconv.Atoi(data[1])

		// Возвращаемся к поиску для этого индекса
		h.identifyNextPlayer(chatID, userID, index)
	}
}

// handleManualInput обрабатывает ручной ввод информации об игроке
func (h *CallbackHandler) handleManualInput(message *tgbotapi.Message) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := message.Text

	state, exists := h.UserStates[userID]
	if !exists || state.Action != "entering_names" || state.Step != "manual_input" {
		return
	}

	index := state.TempData["manual_index"].(int)

	identified := state.TempData["identified_players"].([]map[string]interface{})
	identified = append(identified, map[string]interface{}{
		"player_id": 0,
		"input":     text,
		"method":    "manual",
		"full_name": text,
	})
	state.TempData["identified_players"] = identified

	h.identifyNextPlayer(chatID, userID, index+1)
}

// showIdentificationResult показывает результат идентификации
func (h *CallbackHandler) showIdentificationResult(chatID int64, userID int64) {
	state, exists := h.UserStates[userID]
	if !exists {
		return
	}

	identified := state.TempData["identified_players"].([]map[string]interface{})

	// Проверяем, есть ли inputs в TempData
	inputsRaw, ok := state.TempData["inputs"]
	var inputs []string
	if ok {
		inputs = inputsRaw.([]string)
	} else {
		// Если inputs нет, создаем пустой слайс
		inputs = make([]string, len(identified))
		for i, id := range identified {
			if input, ok := id["input"].(string); ok {
				inputs[i] = input
			} else {
				inputs[i] = fmt.Sprintf("Игрок %d", i+1)
			}
		}
	}

	msg := "📋 *Результат идентификации игроков:*\n\n"
	found := 0

	for i, id := range identified {
		// Проверяем, что индекс i не выходит за пределы inputs
		inputText := ""
		if i < len(inputs) {
			inputText = inputs[i]
		} else if input, ok := id["input"].(string); ok {
			inputText = input
		} else {
			inputText = fmt.Sprintf("Игрок %d", i+1)
		}

		msg += fmt.Sprintf("*%d.* Введено: %s\n", i+1, escapeMarkdown(inputText))

		if playerID, ok := id["player_id"].(int); ok && playerID > 0 {
			found++
			fullName := ""
			if fn, ok := id["full_name"].(string); ok {
				fullName = fn
			} else {
				fullName = "Неизвестно"
			}
			msg += fmt.Sprintf("   ✅ *Найден:* %s\n", escapeMarkdown(fullName))

			if nick, ok := id["telegram_nick"].(string); ok && nick != "" {
				msg += fmt.Sprintf("   📱 %s\n", escapeMarkdown(nick))
			}
		} else {
			fullName := ""
			if fn, ok := id["full_name"].(string); ok {
				fullName = fn
			} else {
				fullName = "Неизвестно"
			}
			msg += fmt.Sprintf("   ❌ *Не идентифицирован*\n")
			msg += fmt.Sprintf("   📝 Данные: %s\n", escapeMarkdown(fullName))
		}
		msg += "\n"
	}

	msg += fmt.Sprintf("\n📊 Итого: найдено %d из %d игроков", found, len(identified))

	// Проверяем, есть ли event_id в TempData
	eventID := 0
	if ei, ok := state.TempData["event_id"].(int); ok {
		eventID = ei
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить и записать",
				fmt.Sprintf("confirm_reg_with_identification:%d", eventID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", "cancel_registration"),
		),
	)

	msgObj := tgbotapi.NewMessage(chatID, msg)
	msgObj.ParseMode = "Markdown"
	msgObj.ReplyMarkup = keyboard
	h.Bot.Send(msgObj)

	state.Step = "final_confirm"
}
