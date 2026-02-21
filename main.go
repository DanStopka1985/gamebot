package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

// Структуры данных
type Event struct {
	ID           int
	CategoryID   int
	CategoryName string
	DateTime     time.Time
	MemberLimit  int
	Registered   int
}

type User struct {
	ID         int
	TelegramID int64
	Nikname    string
	FirstName  string
	LastName   string
}

type Registration struct {
	UserID            int
	EventID           int
	ParticipantsCount int
	Status            string
}

// Структура для хранения состояния пользователя
type UserState struct {
	Action     string
	CategoryID int
	Step       string
	TempData   map[string]interface{}
}

// Глобальные переменные
var (
	bot           *tgbotapi.BotAPI
	db            *sql.DB
	adminIDs      = make(map[int64]bool)
	userStates    = make(map[int64]*UserState)
	channelChatID int64
)

func main() {
	// Загружаем переменные из .env файла
	err := godotenv.Load()
	if err != nil {
		log.Fatal("❌ Error loading .env file")
	}
	log.Println("✅ .env file loaded successfully")

	// Получаем токен из переменной окружения (из .env файла)
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Fatal("❌ BOT_TOKEN не установлен в .env файле")
	}

	// Получаем ID канала из переменной окружения (из .env файла)
	channelIDstr := os.Getenv("CHANNEL_ID")
	if channelIDstr == "" {
		log.Fatal("❌ CHANNEL_ID не установлен в .env файле")
	}

	channelChatID, err = strconv.ParseInt(channelIDstr, 10, 64)
	if err != nil {
		log.Fatalf("❌ CHANNEL_ID должен быть числовым ID (получите его через @getidsbot): %v", err)
	}

	// Создаем бота с токеном из .env
	bot, err = tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true
	log.Printf("✅ Бот авторизован как %s", bot.Self.UserName)
	log.Printf("📢 ID канала: %d", channelChatID)

	// Проверяем, что бот создан
	if bot == nil {
		log.Fatal("❌ Бот не инициализирован!")
	}

	// Получаем пароль PostgreSQL из .env (опционально, если не указан - используем "postgres")
	dbPassword := os.Getenv("DB_PASSWORD")
	if dbPassword == "" {
		dbPassword = "postgres" // пароль по умолчанию
		log.Println("⚠️ DB_PASSWORD не указан в .env, используется 'postgres'")
	}

	// Подключение к базе данных с паролем из .env
	connStr := fmt.Sprintf("host=localhost port=5433 user=postgres password=%s dbname=game sslmode=disable", dbPassword)
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("❌ Ошибка подключения к БД: %v", err)
	}
	defer db.Close()

	// Проверка подключения к БД
	if err = db.Ping(); err != nil {
		log.Fatalf("❌ БД не отвечает: %v", err)
	}
	log.Println("✅ Подключение к БД успешно")

	// Загрузка администраторов
	loadAdmins()

	// Настройка обновлений
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	log.Println("🚀 Бот запущен и слушает обновления")

	// Обрабатываем обновления
	for update := range updates {
		// Новые посты в канале - добавляем кнопку
		if update.ChannelPost != nil {
			log.Printf("📨 Получен новый пост в канале: ID=%d", update.ChannelPost.MessageID)
			handleChannelPost(update.ChannelPost)
			continue
		}

		// Обработка нажатий на кнопки
		if update.CallbackQuery != nil {
			handleCallback(update.CallbackQuery)
			continue
		}

		// Обработка личных сообщений
		if update.Message != nil {
			handleMessage(update.Message)
		}
	}
}

// Загрузка администраторов
func loadAdmins() {
	// Здесь можно загружать админов из БД
	// Пока используем тестовый ID - замените на ваш Telegram ID
	adminIDs[123456789] = true
	log.Printf("👑 Загружено %d администраторов", len(adminIDs))
	log.Printf("⚠️ ВНИМАНИЕ: Замените ID администратора в функции loadAdmins() на ваш Telegram ID")
}

// Обработка нового поста в канале
func handleChannelPost(post *tgbotapi.Message) {
	if bot == nil {
		log.Println("❌ Ошибка: бот не инициализирован")
		return
	}

	// Создаем URL-кнопку с ссылкой на бота
	button := tgbotapi.NewInlineKeyboardButtonURL(
		"📝 Записаться на квиз",
		fmt.Sprintf("https://t.me/%s?start=event", bot.Self.UserName),
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(button),
	)

	// Добавляем кнопку к посту
	editMsg := tgbotapi.NewEditMessageReplyMarkup(
		post.Chat.ID,
		post.MessageID,
		keyboard,
	)

	if _, err := bot.Send(editMsg); err != nil {
		log.Printf("⚠️ Ошибка добавления кнопки к посту %d: %v", post.MessageID, err)

		// Если не получилось отредактировать, пробуем отправить новое сообщение
		msg := tgbotapi.NewMessage(post.Chat.ID, post.Text)
		msg.ReplyMarkup = keyboard
		if _, err := bot.Send(msg); err != nil {
			log.Printf("❌ Ошибка отправки нового сообщения: %v", err)
		}
	} else {
		log.Printf("✅ Кнопка добавлена к посту %d", post.MessageID)
	}
}

// Обработка текстовых сообщений
func handleMessage(message *tgbotapi.Message) {
	if bot == nil {
		log.Println("❌ Ошибка: бот не инициализирован в handleMessage")
		return
	}

	userID := message.From.ID

	// Регистрация пользователя если новый
	registerUserIfNotExists(message.From)

	// Проверяем, есть ли у пользователя активное состояние
	if _, exists := userStates[userID]; exists {
		handleUserInput(message)
		return
	}

	if message.IsCommand() {
		switch message.Command() {
		case "start":
			handleStart(message)
		case "events":
			handleListEvents(message)
		case "myevents":
			handleMyEvents(message)
		default:
			if isAdmin(userID) {
				handleAdminCommand(message)
			}
		}
		return
	}
}

// Обработка команды start
func handleStart(message *tgbotapi.Message) {
	if bot == nil {
		log.Println("❌ Ошибка: бот не инициализирован в handleStart")
		return
	}

	// Проверяем, есть ли параметр в команде (например, ?start=event)
	if len(message.CommandArguments()) > 0 {
		// Если пришли по ссылке из канала, показываем список событий
		handleListEvents(message)
		return
	}

	text := `👋 Добро пожаловать в бот для записи на квизы!

Я помогаю записываться на события, которые публикуются в канале.

📌 **Как это работает:**
1. В канале под каждым постом есть кнопка "Записаться на квиз"
2. Нажмите на неё, чтобы перейти ко мне
3. Выберите событие и количество участников

📋 **Доступные команды:**
/events - список ближайших событий
/myevents - мои записи

👑 **Для администраторов:**
/admin - панель администратора`

	msg := tgbotapi.NewMessage(message.Chat.ID, text)
	msg.ParseMode = "Markdown"

	if _, err := bot.Send(msg); err != nil {
		log.Printf("❌ Ошибка отправки start сообщения: %v", err)
	}
}

// Обработка команд администратора
func handleAdminCommand(message *tgbotapi.Message) {
	switch message.Command() {
	case "admin":
		showAdminMenu(message.Chat.ID)
	case "addevent":
		startAddEvent(message)
	case "allevents":
		showAllEvents(message.Chat.ID)
	case "eventstats":
		showEventStats(message.Chat.ID, message.CommandArguments())
	}
}

// Показать меню администратора
func showAdminMenu(chatID int64) {
	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("➕ Добавить событие"),
			tgbotapi.NewKeyboardButton("📋 Все события"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📊 Статистика"),
			tgbotapi.NewKeyboardButton("👥 Все записи"),
		),
	)
	keyboard.ResizeKeyboard = true

	msg := tgbotapi.NewMessage(chatID, "👑 Панель администратора:")
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

// Обработка callback запросов
func handleCallback(callback *tgbotapi.CallbackQuery) {
	data := strings.Split(callback.Data, ":")
	userID := callback.From.ID
	chatID := callback.Message.Chat.ID

	// Регистрация пользователя
	registerUserIfNotExists(callback.From)

	switch data[0] {
	case "event":
		eventID, _ := strconv.Atoi(data[1])
		showEventDetails(chatID, eventID, userID)

	case "register":
		eventID, _ := strconv.Atoi(data[1])
		askParticipantsCount(chatID, eventID, userID)

	case "confirm_reg":
		parts := strings.Split(data[1], "_")
		eventID, _ := strconv.Atoi(parts[0])
		count, _ := strconv.Atoi(parts[1])
		registerForEvent(chatID, eventID, userID, count)

	case "cancel_reg":
		eventID, _ := strconv.Atoi(data[1])
		cancelRegistration(chatID, eventID, userID)

	case "admin":
		if isAdmin(userID) {
			handleAdminCallback(callback, data)
		}
	}

	// Закрываем callback
	bot.Request(tgbotapi.NewCallback(callback.ID, ""))
}

// Обработка admin callback'ов
func handleAdminCallback(callback *tgbotapi.CallbackQuery, data []string) {
	chatID := callback.Message.Chat.ID
	userID := callback.From.ID

	if len(data) < 2 {
		return
	}

	switch data[1] {
	case "add_category":
		if len(data) < 3 {
			return
		}
		categoryID, _ := strconv.Atoi(data[2])

		userStates[userID] = &UserState{
			Action:     "add_event",
			CategoryID: categoryID,
			Step:       "awaiting_datetime",
			TempData:   make(map[string]interface{}),
		}

		msg := tgbotapi.NewMessage(chatID,
			"Введите дату и время события в формате:\n`2026-03-15 10:00`\n\n"+
				"Пример: 2026-03-20 15:30")
		msg.ParseMode = "Markdown"
		bot.Send(msg)

	case "confirm_add":
		confirmAddEvent(chatID, userID)

	case "cancel_add":
		delete(userStates, userID)
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Создание события отменено"))

	case "edit_event":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		showEventEditMenu(chatID, eventID)

	case "edit_date":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		userStates[userID] = &UserState{
			Action:   "edit_event",
			Step:     "awaiting_new_date",
			TempData: map[string]interface{}{"event_id": eventID},
		}
		bot.Send(tgbotapi.NewMessage(chatID,
			"Введите новую дату и время в формате:\n2026-03-15 10:00"))

	case "edit_limit":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		userStates[userID] = &UserState{
			Action:   "edit_event",
			Step:     "awaiting_new_limit",
			TempData: map[string]interface{}{"event_id": eventID},
		}
		bot.Send(tgbotapi.NewMessage(chatID, "Введите новый лимит участников:"))

	case "delete_event":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		confirmDeleteEvent(chatID, eventID)

	case "confirm_delete":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		deleteEvent(chatID, eventID)

	case "view_registrations":
		if len(data) < 3 {
			return
		}
		eventID, _ := strconv.Atoi(data[2])
		showEventRegistrations(chatID, eventID)

	case "back":
		delete(userStates, userID)
		showAdminMenu(chatID)
	}
}

// Обработка ввода пользователя (для многошаговых действий)
func handleUserInput(message *tgbotapi.Message) {
	userID := message.From.ID

	state, exists := userStates[userID]
	if !exists {
		return
	}

	switch state.Action {
	case "add_event":
		handleAddEventInput(message, state)
	case "edit_event":
		handleEditEventInput(message, state)
	default:
		delete(userStates, userID)
	}
}

// Обработка ввода при создании события
func handleAddEventInput(message *tgbotapi.Message, state *UserState) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := message.Text

	switch state.Step {
	case "awaiting_datetime":
		datetime, err := time.Parse("2006-01-02 15:04", text)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID,
				"❌ Неверный формат. Используйте: 2026-03-15 10:00\nПопробуйте снова:"))
			return
		}

		state.TempData["datetime"] = datetime
		state.Step = "awaiting_limit"

		bot.Send(tgbotapi.NewMessage(chatID,
			"Введите максимальное количество участников (число):"))

	case "awaiting_limit":
		limit, err := strconv.Atoi(text)
		if err != nil || limit < 1 {
			bot.Send(tgbotapi.NewMessage(chatID,
				"❌ Введите положительное число:"))
			return
		}

		state.TempData["limit"] = limit

		datetime := state.TempData["datetime"].(time.Time)
		preview := fmt.Sprintf(
			"📅 Предварительный просмотр события:\n\n"+
				"Категория ID: %d\n"+
				"📆 Дата: %s\n"+
				"👥 Лимит: %d\n\n"+
				"Подтвердить создание?",
			state.CategoryID,
			datetime.Format("02.01.2006 15:04"),
			limit,
		)

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Да", "admin:confirm_add"),
				tgbotapi.NewInlineKeyboardButtonData("❌ Нет", "admin:cancel_add"),
			),
		)

		msg := tgbotapi.NewMessage(chatID, preview)
		msg.ReplyMarkup = keyboard
		bot.Send(msg)

		delete(userStates, userID)

	default:
		delete(userStates, userID)
	}
}

// Обработка ввода при редактировании события
func handleEditEventInput(message *tgbotapi.Message, state *UserState) {
	userID := message.From.ID
	chatID := message.Chat.ID
	text := message.Text

	eventID, ok := state.TempData["event_id"].(int)
	if !ok {
		delete(userStates, userID)
		return
	}

	switch state.Step {
	case "awaiting_new_date":
		datetime, err := time.Parse("2006-01-02 15:04", text)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID,
				"❌ Неверный формат. Используйте: 2026-03-15 10:00\nПопробуйте снова:"))
			return
		}

		_, err = db.Exec(`UPDATE event SET evn_datetime = $1 WHERE id = $2`, datetime, eventID)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при обновлении даты"))
		} else {
			bot.Send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("✅ Дата события #%d обновлена", eventID)))
		}

		delete(userStates, userID)

	case "awaiting_new_limit":
		limit, err := strconv.Atoi(text)
		if err != nil || limit < 1 {
			bot.Send(tgbotapi.NewMessage(chatID,
				"❌ Введите положительное число:"))
			return
		}

		_, err = db.Exec(`UPDATE event SET member_limit = $1 WHERE id = $2`, limit, eventID)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при обновлении лимита"))
		} else {
			bot.Send(tgbotapi.NewMessage(chatID,
				fmt.Sprintf("✅ Лимит события #%d обновлен", eventID)))
		}

		delete(userStates, userID)

	default:
		delete(userStates, userID)
	}
}

// Подтверждение создания события
func confirmAddEvent(chatID int64, userID int64) {
	state, exists := userStates[userID]
	if !exists {
		return
	}

	datetime, ok := state.TempData["datetime"].(time.Time)
	if !ok {
		delete(userStates, userID)
		return
	}

	limit, ok := state.TempData["limit"].(int)
	if !ok {
		delete(userStates, userID)
		return
	}

	var eventID int
	err := db.QueryRow(`
		INSERT INTO event (category_id, evn_datetime, member_limit)
		VALUES ($1, $2, $3)
		RETURNING id
	`, state.CategoryID, datetime, limit).Scan(&eventID)

	if err != nil {
		log.Printf("Ошибка создания события: %v", err)
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при создании события"))
		return
	}

	bot.Send(tgbotapi.NewMessage(chatID,
		fmt.Sprintf("✅ Событие #%d успешно создано!", eventID)))

	delete(userStates, userID)
}

// Показать меню редактирования события
func showEventEditMenu(chatID int64, eventID int) {
	var event Event
	err := db.QueryRow(`
		SELECT e.id, c.name, e.evn_datetime, e.member_limit
		FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&event.ID, &event.CategoryName, &event.DateTime, &event.MemberLimit)

	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Событие не найдено"))
		return
	}

	text := fmt.Sprintf(
		"Редактирование события #%d\n\n"+
			"Текущие данные:\n"+
			"Категория: %s\n"+
			"Дата: %s\n"+
			"Лимит: %d\n\n"+
			"Выберите действие:",
		event.ID,
		event.CategoryName,
		event.DateTime.Format("02.01.2006 15:04"),
		event.MemberLimit,
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📅 Изменить дату", fmt.Sprintf("admin:edit_date:%d", eventID)),
			tgbotapi.NewInlineKeyboardButtonData("👥 Изменить лимит", fmt.Sprintf("admin:edit_limit:%d", eventID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🗑 Удалить", fmt.Sprintf("admin:delete_event:%d", eventID)),
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "admin:back"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

// Подтверждение удаления события
func confirmDeleteEvent(chatID int64, eventID int) {
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Да, удалить", fmt.Sprintf("admin:confirm_delete:%d", eventID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Нет, отмена", "admin:back"),
		),
	)

	msg := tgbotapi.NewMessage(chatID,
		fmt.Sprintf("⚠️ Вы уверены, что хотите удалить событие #%d?\n"+
			"Все записи на это событие также будут удалены.", eventID))
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

// Удаление события
func deleteEvent(chatID int64, eventID int) {
	tx, err := db.Begin()
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка сервера"))
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM user_event WHERE event_id = $1`, eventID)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при удалении записей"))
		return
	}

	result, err := tx.Exec(`DELETE FROM event WHERE id = $1`, eventID)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при удалении события"))
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Событие не найдено"))
		return
	}

	if err = tx.Commit(); err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка при сохранении"))
		return
	}

	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("✅ Событие #%d удалено", eventID)))
}

// Показать записи на событие
func showEventRegistrations(chatID int64, eventID int) {
	rows, err := db.Query(`
		SELECT u.nikname, u.firstname, u.lastname, ue.participants_count, ue.registered_at
		FROM user_event ue
		JOIN "user" u ON ue.user_id = u.id
		WHERE ue.event_id = $1 AND ue.status = 'registered'
		ORDER BY ue.registered_at
	`, eventID)

	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
		return
	}
	defer rows.Close()

	var eventInfo struct {
		Name     string
		DateTime time.Time
		Limit    int
	}
	db.QueryRow(`
		SELECT c.name, e.evn_datetime, e.member_limit
		FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&eventInfo.Name, &eventInfo.DateTime, &eventInfo.Limit)

	text := fmt.Sprintf("📊 Записи на событие #%d\n"+
		"📅 %s\n"+
		"📌 %s\n"+
		"👥 Лимит: %d\n\n",
		eventID,
		eventInfo.DateTime.Format("02.01.2006 15:04"),
		eventInfo.Name,
		eventInfo.Limit,
	)

	count := 0
	totalParticipants := 0
	for rows.Next() {
		count++
		var nikname, first, last string
		var participants int
		var regTime time.Time
		rows.Scan(&nikname, &first, &last, &participants, &regTime)
		totalParticipants += participants

		text += fmt.Sprintf("%d. @%s (%s %s) - %d чел.\n   📅 %s\n",
			count, nikname, first, last, participants,
			regTime.Format("02.01 15:04"))
	}

	if count == 0 {
		text += "\n❌ Нет записей"
	} else {
		text += fmt.Sprintf("\n📊 Всего записей: %d\n", count)
		text += fmt.Sprintf("👥 Всего участников: %d\n", totalParticipants)
	}

	bot.Send(tgbotapi.NewMessage(chatID, text))
}

// Запрос количества участников
func askParticipantsCount(chatID int64, eventID int, userID int64) {
	var event Event
	err := db.QueryRow(`
		SELECT e.id, e.member_limit, 
		       (SELECT COUNT(*) FROM user_event WHERE event_id = e.id AND status = 'registered')
		FROM event e WHERE e.id = $1
	`, eventID).Scan(&event.ID, &event.MemberLimit, &event.Registered)

	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки события"))
		return
	}

	available := event.MemberLimit - event.Registered
	if available <= 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Свободных мест нет"))
		return
	}

	var rows [][]tgbotapi.InlineKeyboardButton
	for i := 1; i <= available && i <= 5; i++ {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(
				fmt.Sprintf("%d %s", i, pluralize(i, "человек", "человека", "человек")),
				fmt.Sprintf("confirm_reg:%d_%d", eventID, i),
			),
		))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)

	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"Свободно мест: %d\n\nСколько человек записать?", available))
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

// Регистрация на событие
func registerForEvent(chatID int64, eventID int, userID int64, count int) {
	tx, err := db.Begin()
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка сервера"))
		return
	}
	defer tx.Rollback()

	var registered int
	err = tx.QueryRow(`
		SELECT COUNT(*) FROM user_event 
		WHERE event_id = $1 AND status = 'registered'
	`, eventID).Scan(&registered)

	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка проверки мест"))
		return
	}

	var memberLimit int
	err = tx.QueryRow(`SELECT member_limit FROM event WHERE id = $1`, eventID).Scan(&memberLimit)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки события"))
		return
	}

	if registered+count > memberLimit {
		bot.Send(tgbotapi.NewMessage(chatID,
			fmt.Sprintf("❌ Недостаточно мест. Свободно: %d", memberLimit-registered)))
		return
	}

	var dbUserID int
	err = tx.QueryRow(`SELECT id FROM "user" WHERE telegram_id = $1`, userID).Scan(&dbUserID)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Пользователь не найден"))
		return
	}

	var existing int
	err = tx.QueryRow(`
		SELECT COUNT(*) FROM user_event 
		WHERE user_id = $1 AND event_id = $2 AND status = 'registered'
	`, dbUserID, eventID).Scan(&existing)

	if err == nil && existing > 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Вы уже записаны на это событие"))
		return
	}

	_, err = tx.Exec(`
		INSERT INTO user_event (user_id, event_id, participants_count, status)
		VALUES ($1, $2, $3, 'registered')
	`, dbUserID, eventID, count)

	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка регистрации"))
		return
	}

	if err = tx.Commit(); err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка сохранения"))
		return
	}

	bot.Send(tgbotapi.NewMessage(chatID,
		fmt.Sprintf("✅ Вы успешно записаны!\nКоличество: %d", count)))
}

// Отмена регистрации
func cancelRegistration(chatID int64, eventID int, userID int64) {
	var dbUserID int
	err := db.QueryRow(`SELECT id FROM "user" WHERE telegram_id = $1`, userID).Scan(&dbUserID)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Пользователь не найден"))
		return
	}

	result, err := db.Exec(`
		UPDATE user_event SET status = 'cancelled' 
		WHERE user_id = $1 AND event_id = $2 AND status = 'registered'
	`, dbUserID, eventID)

	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка отмены"))
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Запись не найдена"))
	} else {
		bot.Send(tgbotapi.NewMessage(chatID, "✅ Запись отменена"))
	}
}

// Вспомогательные функции
func pluralize(n int, form1, form2, form5 string) string {
	if n%10 == 1 && n%100 != 11 {
		return form1
	}
	if n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20) {
		return form2
	}
	return form5
}

func registerUserIfNotExists(tgUser *tgbotapi.User) {
	var exists bool
	err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM "user" WHERE telegram_id = $1)`,
		tgUser.ID).Scan(&exists)

	if err != nil || exists {
		return
	}

	nikname := tgUser.UserName
	if nikname == "" {
		nikname = fmt.Sprintf("user_%d", tgUser.ID)
	}

	_, err = db.Exec(`
		INSERT INTO "user" (telegram_id, nikname, firstname, lastname)
		VALUES ($1, $2, $3, $4)
	`, tgUser.ID, nikname, tgUser.FirstName, tgUser.LastName)

	if err != nil {
		log.Printf("Ошибка регистрации пользователя: %v", err)
	}
}

func isAdmin(userID int64) bool {
	return adminIDs[userID]
}

func handleListEvents(message *tgbotapi.Message) {
	rows, err := db.Query(`
		SELECT e.id, c.name, e.evn_datetime, e.member_limit,
		       (SELECT COUNT(*) FROM user_event WHERE event_id = e.id AND status = 'registered')
		FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.evn_datetime > NOW()
		ORDER BY e.evn_datetime
		LIMIT 10
	`)

	if err != nil {
		bot.Send(tgbotapi.NewMessage(message.Chat.ID, "❌ Ошибка загрузки событий"))
		return
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		rows.Scan(&e.ID, &e.CategoryName, &e.DateTime, &e.MemberLimit, &e.Registered)
		events = append(events, e)
	}

	if len(events) == 0 {
		bot.Send(tgbotapi.NewMessage(message.Chat.ID, "📭 Ближайших событий нет"))
		return
	}

	for _, e := range events {
		showEventPreview(message.Chat.ID, e)
	}
}

func showEventPreview(chatID int64, e Event) {
	text := fmt.Sprintf(
		"📅 *%s*\n"+
			"📆 %s\n"+
			"👥 Мест: %d/%d\n",
		e.CategoryName,
		e.DateTime.Format("02.01.2006 15:04"),
		e.Registered,
		e.MemberLimit,
	)

	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📝 Записаться", fmt.Sprintf("event:%d", e.ID)),
			tgbotapi.NewInlineKeyboardButtonData("ℹ️ Подробнее", fmt.Sprintf("event:%d", e.ID)),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func showEventDetails(chatID int64, eventID int, userID int64) {
	var e Event
	err := db.QueryRow(`
		SELECT e.id, c.name, e.evn_datetime, e.member_limit,
		       (SELECT COUNT(*) FROM user_event WHERE event_id = e.id AND status = 'registered')
		FROM event e
		JOIN category c ON e.category_id = c.id
		WHERE e.id = $1
	`, eventID).Scan(&e.ID, &e.CategoryName, &e.DateTime, &e.MemberLimit, &e.Registered)

	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Событие не найдено"))
		return
	}

	var isRegistered bool
	var dbUserID int
	db.QueryRow(`SELECT id FROM "user" WHERE telegram_id = $1`, userID).Scan(&dbUserID)
	db.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM user_event 
		WHERE user_id = $1 AND event_id = $2 AND status = 'registered')
	`, dbUserID, eventID).Scan(&isRegistered)

	text := fmt.Sprintf(
		"📅 *%s*\n\n"+
			"📆 Дата: %s\n"+
			"👥 Записано: %d/%d\n"+
			"📊 Свободно: %d\n",
		e.CategoryName,
		e.DateTime.Format("02.01.2006 15:04"),
		e.Registered,
		e.MemberLimit,
		e.MemberLimit-e.Registered,
	)

	var keyboard tgbotapi.InlineKeyboardMarkup
	if isRegistered {
		keyboard = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("❌ Отменить запись", fmt.Sprintf("cancel_reg:%d", eventID)),
			),
		)
	} else if e.Registered < e.MemberLimit {
		keyboard = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Записаться", fmt.Sprintf("register:%d", eventID)),
			),
		)
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func handleMyEvents(message *tgbotapi.Message) {
	var dbUserID int
	err := db.QueryRow(`SELECT id FROM "user" WHERE telegram_id = $1`, message.From.ID).Scan(&dbUserID)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(message.Chat.ID, "❌ Пользователь не найден"))
		return
	}

	rows, err := db.Query(`
		SELECT e.id, c.name, e.evn_datetime, ue.participants_count
		FROM user_event ue
		JOIN event e ON ue.event_id = e.id
		JOIN category c ON e.category_id = c.id
		WHERE ue.user_id = $1 AND ue.status = 'registered' AND e.evn_datetime > NOW()
		ORDER BY e.evn_datetime
	`, dbUserID)

	if err != nil {
		bot.Send(tgbotapi.NewMessage(message.Chat.ID, "❌ Ошибка загрузки"))
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
		var id int
		var name string
		var dt time.Time
		var participants int
		rows.Scan(&id, &name, &dt, &participants)

		text := fmt.Sprintf(
			"📅 *%s*\n📆 %s\n👥 Записано: %d\n",
			name, dt.Format("02.01.2006 15:04"), participants,
		)

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("❌ Отменить", fmt.Sprintf("cancel_reg:%d", id)),
			),
		)

		msg := tgbotapi.NewMessage(message.Chat.ID, text)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = keyboard
		bot.Send(msg)
	}

	if count == 0 {
		bot.Send(tgbotapi.NewMessage(message.Chat.ID, "📭 У вас нет активных записей"))
	}
}

// Функции администратора
func startAddEvent(message *tgbotapi.Message) {
	if !isAdmin(message.From.ID) {
		bot.Send(tgbotapi.NewMessage(message.Chat.ID, "⛔ Нет прав"))
		return
	}

	rows, err := db.Query(`SELECT id, name FROM category ORDER BY name`)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(message.Chat.ID, "❌ Ошибка загрузки категорий"))
		return
	}
	defer rows.Close()

	var buttons [][]tgbotapi.InlineKeyboardButton
	for rows.Next() {
		var id int
		var name string
		rows.Scan(&id, &name)
		buttons = append(buttons, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(name, fmt.Sprintf("admin:add_category:%d", id)),
		))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)
	msg := tgbotapi.NewMessage(message.Chat.ID, "Выберите категорию:")
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func showAllEvents(chatID int64) {
	if !isAdmin(chatID) {
		return
	}

	rows, err := db.Query(`
		SELECT e.id, c.name, e.evn_datetime, e.member_limit,
		       (SELECT COUNT(*) FROM user_event WHERE event_id = e.id AND status = 'registered')
		FROM event e
		JOIN category c ON e.category_id = c.id
		ORDER BY e.evn_datetime DESC
		LIMIT 20
	`)

	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
		var e Event
		rows.Scan(&e.ID, &e.CategoryName, &e.DateTime, &e.MemberLimit, &e.Registered)

		text := fmt.Sprintf(
			"🆔 *%d* | %s\n📆 %s\n👥 %d/%d\n",
			e.ID, e.CategoryName,
			e.DateTime.Format("02.01.2006 15:04"),
			e.Registered, e.MemberLimit,
		)

		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✏️ Редактировать", fmt.Sprintf("admin:edit_event:%d", e.ID)),
				tgbotapi.NewInlineKeyboardButtonData("👥 Записи", fmt.Sprintf("admin:view_registrations:%d", e.ID)),
			),
		)

		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = "Markdown"
		msg.ReplyMarkup = keyboard
		bot.Send(msg)
	}

	if count == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "📭 Нет событий"))
	}
}

func showEventStats(chatID int64, eventIDStr string) {
	if !isAdmin(chatID) {
		return
	}

	eventID, err := strconv.Atoi(eventIDStr)
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Использование: /eventstats ID"))
		return
	}

	rows, err := db.Query(`
		SELECT u.nikname, u.firstname, u.lastname, ue.participants_count, ue.registered_at
		FROM user_event ue
		JOIN "user" u ON ue.user_id = u.id
		WHERE ue.event_id = $1 AND ue.status = 'registered'
		ORDER BY ue.registered_at
	`, eventID)

	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "❌ Ошибка загрузки"))
		return
	}
	defer rows.Close()

	text := fmt.Sprintf("📊 Записи на событие #%d:\n\n", eventID)
	count := 0
	totalParticipants := 0
	for rows.Next() {
		count++
		var nikname, first, last string
		var participants int
		var regTime time.Time
		rows.Scan(&nikname, &first, &last, &participants, &regTime)
		totalParticipants += participants

		text += fmt.Sprintf("%d. @%s (%s %s) - %d чел.\n   📅 %s\n",
			count, nikname, first, last, participants,
			regTime.Format("02.01 15:04"))
	}

	if count == 0 {
		text += "Нет записей"
	} else {
		text += fmt.Sprintf("\n📊 Всего записей: %d\n", count)
		text += fmt.Sprintf("👥 Всего участников: %d\n", totalParticipants)
	}

	bot.Send(tgbotapi.NewMessage(chatID, text))
}
