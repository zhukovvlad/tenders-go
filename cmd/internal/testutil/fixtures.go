package testutil

import (
	"net"
	"time"

	"golang.org/x/crypto/bcrypt"

	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
)

const (
	// TestPasswordHash is the bcrypt hash for the password "password"
	// Used in test fixtures for predictable authentication testing
	// Generated with: bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	TestPasswordHash = "$2a$10$j94IoTEXd628/ESukxvscuehNcj11LE80UtkFt3U5FqfNH1dloP.."
	TestPassword     = "password"
)

// Fixtures содержит готовые тестовые данные
type Fixtures struct {
	Users       []db.User
	Tenders     []db.Tender
	Lots        []db.Lot
	Categories  []db.TenderCategory
	Types       []db.TenderType
	Contractors []db.Contractor
	Executors   []db.Executor
	Proposals   []db.Proposal
}

// NewFixtures создает новый набор фикстур
func NewFixtures() *Fixtures {
	return &Fixtures{}
}

// CreateTestUser создает тестового пользователя
func CreateTestUser(email, role string, isActive bool) db.User {
	now := time.Now()
	return db.User{
		ID:           1,
		Email:        email,
		PasswordHash: TestPasswordHash,
		Role:         role,
		IsActive:     isActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// CreateTestTender создает тестовый тендер
func CreateTestTender(id int64, etpID, title string, objectID, executorID int64) db.Tender {
	now := time.Now()
	return db.Tender{
		ID:         id,
		EtpID:      etpID,
		Title:      title,
		ObjectID:   objectID,
		ExecutorID: executorID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// CreateTestLot создает тестовый лот
func CreateTestLot(id int64, lotKey, lotTitle string, tenderID int64) db.Lot {
	now := time.Now()
	return db.Lot{
		ID:        id,
		LotKey:    lotKey,
		LotTitle:  lotTitle,
		TenderID:  tenderID,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// CreateTestCategory создает тестовую категорию
func CreateTestCategory(id int64, title string, chapterID int64) db.TenderCategory {
	now := time.Now()
	return db.TenderCategory{
		ID:              id,
		Title:           title,
		TenderChapterID: chapterID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

// CreateTestType создает тестовый тип тендера
func CreateTestType(id int64, title string) db.TenderType {
	now := time.Now()
	return db.TenderType{
		ID:        id,
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// CreateTestContractor создает тестового подрядчика
func CreateTestContractor(id int64, title, inn string) db.Contractor {
	now := time.Now()
	return db.Contractor{
		ID:            id,
		Title:         title,
		Inn:           inn,
		Address:       "Test Address",
		Accreditation: "active",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// CreateTestExecutor создает тестового исполнителя
func CreateTestExecutor(id int64, name, phone string) db.Executor {
	now := time.Now()
	return db.Executor{
		ID:        id,
		Name:      name,
		Phone:     phone,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// CreateTestProposal создает тестовое предложение
func CreateTestProposal(id int64, lotID, contractorID int64) db.Proposal {
	now := time.Now()
	return db.Proposal{
		ID:           id,
		LotID:        lotID,
		ContractorID: contractorID,
		IsBaseline:   false,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// DefaultFixtures создает набор стандартных фикстур для тестов
func DefaultFixtures() *Fixtures {
	f := NewFixtures()

	// Пользователи
	f.Users = []db.User{
		CreateTestUser("admin@test.com", "admin", true),
		CreateTestUser("user@test.com", "viewer", true),
	}

	// Категории
	f.Categories = []db.TenderCategory{
		CreateTestCategory(1, "Строительство", 1),
		CreateTestCategory(2, "IT услуги", 1),
		CreateTestCategory(3, "Консалтинг", 2),
	}

	// Типы
	f.Types = []db.TenderType{
		CreateTestType(1, "Открытый конкурс"),
		CreateTestType(2, "Запрос котировок"),
	}

	// Подрядчики
	f.Contractors = []db.Contractor{
		CreateTestContractor(1, "ООО Подрядчик 1", "1234567890"),
		CreateTestContractor(2, "ООО Подрядчик 2", "0987654321"),
	}

	// Исполнители
	f.Executors = []db.Executor{
		CreateTestExecutor(1, "Иванов И.И.", "+71234567890"),
		CreateTestExecutor(2, "Петров П.П.", "+70987654321"),
	}

	// Тендеры
	f.Tenders = []db.Tender{
		CreateTestTender(1, "ETP-001", "Test Tender 1", 1, 1),
		CreateTestTender(2, "ETP-002", "Test Tender 2", 1, 2),
	}

	// Лоты
	f.Lots = []db.Lot{
		CreateTestLot(1, "LOT-001", "Лот 1", 1),
		CreateTestLot(2, "LOT-002", "Лот 2", 1),
		CreateTestLot(3, "LOT-003", "Лот 3", 2),
	}

	// Предложения
	f.Proposals = []db.Proposal{
		CreateTestProposal(1, 1, 1),
		CreateTestProposal(2, 2, 2),
	}

	return f
}

// Helper functions для создания nullable типов

// String возвращает указатель на string
func String(s string) *string {
	return &s
}

// Int64 возвращает указатель на int64
func Int64(i int64) *int64 {
	return &i
}

// Float64 возвращает указатель на float64
func Float64(f float64) *float64 {
	return &f
}

// Bool возвращает указатель на bool
func Bool(b bool) *bool {
	return &b
}

// Time возвращает указатель на time.Time
func Time(t time.Time) *time.Time {
	return &t
}

// CompareTestPassword compares a password with a bcrypt hash
// Returns nil if password matches the hash
func CompareTestPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// ParseIP parses an IP address for testing
func ParseIP(ip string) net.IP {
	return net.ParseIP(ip)
}
