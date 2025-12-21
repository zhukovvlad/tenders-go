package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strings"
	"syscall"

	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"

	"github.com/zhukovvlad/tenders-go/cmd/internal/config"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"

	_ "github.com/lib/pq"
)

func main() {
	logger := logging.GetLogger()
	logger.Info("Create Admin User Tool")

	// Загружаем .env файл
	err := godotenv.Load()
	if err != nil {
		logger.Warnf("Warning: error loading .env file: %v", err)
	}

	cfg := config.GetConfig()

	// Подключение к базе данных
	conn, err := sql.Open(cfg.Database.Driver, cfg.Database.Source)
	if err != nil {
		logger.Fatalf("error connecting to database: %v", err)
	}
	defer conn.Close()

	if err = conn.Ping(); err != nil {
		logger.Fatalf("error pinging database: %v", err)
	}

	logger.Info("Database connection established")

	store := db.NewStore(conn)
	ctx := context.Background()

	// Запрашиваем email
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter admin email: ")
	email, err := reader.ReadString('\n')
	if err != nil {
		logger.Fatalf("failed to read email: %v", err)
	}
	email = strings.ToLower(strings.TrimSpace(email))

	// Проверяем валидность email
	// Basic email pattern: localpart@domain.tld
	emailPattern := `^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`
	matched, err := regexp.MatchString(emailPattern, email)
	if err != nil || !matched {
		logger.Fatal("invalid email address format")
	}

	// Проверяем, не существует ли уже такой пользователь
	_, err = store.GetUserAuthByEmail(ctx, email)
	if err == nil {
		// User found
		logger.Fatalf("user with email %s already exists", email)
	} else if err != sql.ErrNoRows {
		// Database error (not "no rows")
		logger.Fatalf("failed to check existing user: %v", err)
	}
	// err == sql.ErrNoRows: no user exists, continue

	// Запрашиваем пароль (без отображения на экране)
	fmt.Print("Enter admin password: ")
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		logger.Fatalf("failed to read password: %v", err)
	}
	fmt.Println() // Переход на новую строку после ввода пароля

	password := string(passwordBytes)
	if len(password) < 8 {
		logger.Fatal("password must be at least 8 characters long")
	}

	// Запрашиваем подтверждение пароля
	fmt.Print("Confirm admin password: ")
	confirmBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		logger.Fatalf("failed to read password confirmation: %v", err)
	}
	fmt.Println()

	if password != string(confirmBytes) {
		logger.Fatal("passwords do not match")
	}

	// Хешируем пароль
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		logger.Fatalf("failed to hash password: %v", err)
	}

	// Создаем пользователя с ролью admin
	user, err := store.CreateUser(ctx, db.CreateUserParams{
		Email:        email,
		PasswordHash: string(passwordHash),
		Role:         "admin",
		IsActive:     true,
	})
	if err != nil {
		logger.Fatalf("failed to create admin user: %v", err)
	}

	logger.Infof("✓ Admin user created successfully!")
	logger.Infof("  ID: %d", user.ID)
	logger.Infof("  Email: %s", user.Email)
	logger.Infof("  Role: %s", user.Role)
	logger.Infof("  Active: %v", user.IsActive)
	logger.Infof("  Created: %s", user.CreatedAt.Format("2006-01-02 15:04:05"))
}
