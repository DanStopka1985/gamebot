package main

import (
	"log"
	"os"
	"strconv"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"

	"gamebot/database"
	"gamebot/handlers"
	"gamebot/models"
)

var (
	bot             *tgbotapi.BotAPI
	channelChatID   int64
	adminIDs        = make(map[int64]bool)
	userStates      = make(map[int64]*models.UserState)
	lastAdminUpdate time.Time
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

	// Получаем пароль PostgreSQL из .env
	dbPassword := os.Getenv("DB_PASSWORD")
	if dbPassword == "" {
		dbPassword = "postgres"
		log.Println("⚠️ DB_PASSWORD не указан в .env, используется 'postgres'")
	}

	// Подключаемся к базе данных
	if err := database.InitDB(dbPassword); err != nil {
		log.Fatal(err)
	}
	defer database.CloseDB()

	// Загружаем администраторов канала
	updateAdminList()

	// Создаем обработчики
	channelHandler := handlers.NewChannelHandler(bot)
	msgHandler := handlers.NewMessageHandler(bot, &adminIDs, userStates)
	callbackHandler := handlers.NewCallbackHandler(bot, &adminIDs, userStates)

	// Проверка создания обработчиков
	log.Println("✅ Обработчики созданы:")
	log.Printf("   - ChannelHandler: %v", channelHandler != nil)
	log.Printf("   - MessageHandler: %v", msgHandler != nil)
	log.Printf("   - CallbackHandler: %v", callbackHandler != nil)

	// Проверка и удаление webhook если есть
	log.Println("🔄 Проверка наличия webhook...")
	webhookInfo, err := bot.GetWebhookInfo()
	if err != nil {
		log.Printf("⚠️ Ошибка получения информации о webhook: %v", err)
	} else if webhookInfo.IsSet() {
		log.Printf("⚠️ Обнаружен webhook: %s, удаляем...", webhookInfo.URL)
		_, err = bot.Request(tgbotapi.DeleteWebhookConfig{})
		if err != nil {
			log.Printf("⚠️ Ошибка удаления webhook: %v", err)
		} else {
			log.Println("✅ Webhook удален")
		}
		time.Sleep(2 * time.Second)
	} else {
		log.Println("✅ Webhook не обнаружен, используем long polling")
	}

	// Настройка обновлений
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	u.AllowedUpdates = []string{"message", "callback_query", "channel_post", "my_chat_member", "chat_member"}

	log.Println("🔄 Получаем обновления...")
	updates := bot.GetUpdatesChan(u)

	// Пропускаем старые обновления
	time.Sleep(2 * time.Second)
	log.Println("🚀 Бот запущен и слушает обновления")
	log.Println("📝 Для остановки нажмите Ctrl+C")

	// Запускаем горутину для периодического обновления списка админов (каждый час)
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		for range ticker.C {
			updateAdminList()
		}
	}()

	// Обрабатываем обновления
	for update := range updates {
		// Обновляем список админов при изменении в чате
		if update.MyChatMember != nil || update.ChatMember != nil {
			log.Println("🔄 Обновление списка администраторов (изменение в чате)")
			updateAdminList()
		}

		// Новые посты в канале - добавляем кнопку
		if update.ChannelPost != nil {
			log.Printf("📨 Получен новый пост в канале: ID=%d, текст=%s",
				update.ChannelPost.MessageID,
				update.ChannelPost.Text)
			channelHandler.HandleChannelPost(update.ChannelPost)
			continue
		}

		// Обработка нажатий на кнопки
		if update.CallbackQuery != nil {
			log.Printf("🔘🔘🔘 ПОЛУЧЕН CALLBACK: %s от пользователя @%s (ID: %d)",
				update.CallbackQuery.Data,
				update.CallbackQuery.From.UserName,
				update.CallbackQuery.From.ID)

			// Отправляем немедленный ответ на callback
			callbackConfig := tgbotapi.NewCallback(update.CallbackQuery.ID, "Обрабатываю...")
			if _, err := bot.Request(callbackConfig); err != nil {
				log.Printf("❌ Ошибка ответа на callback: %v", err)
			}

			callbackHandler.HandleCallback(update.CallbackQuery)
			continue
		}

		// Обработка личных сообщений
		if update.Message != nil {
			log.Printf("💬 Получено сообщение от @%s (ID: %d): %s",
				update.Message.From.UserName,
				update.Message.From.ID,
				update.Message.Text)
			msgHandler.HandleMessage(update.Message)
		}
	}
}

// updateAdminList обновляет список администраторов канала
func updateAdminList() {
	log.Println("🔄 Обновление списка администраторов канала...")

	admins, err := bot.GetChatAdministrators(tgbotapi.ChatAdministratorsConfig{
		ChatConfig: tgbotapi.ChatConfig{
			ChatID: channelChatID,
		},
	})

	if err != nil {
		log.Printf("❌ Ошибка получения списка администраторов: %v", err)
		return
	}

	// Очищаем старый список
	adminIDs = make(map[int64]bool)

	// Добавляем всех администраторов
	for _, admin := range admins {
		adminIDs[admin.User.ID] = true
		log.Printf("   👑 Админ: @%s (ID: %d)", admin.User.UserName, admin.User.ID)
	}

	log.Printf("✅ Загружено %d администраторов канала", len(adminIDs))
	lastAdminUpdate = time.Now()
}
