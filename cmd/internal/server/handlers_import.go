package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
)

// ImportTenderHandler — импорт полного тендера через POST /api/v1/import-tender.
//
// Что делает хэндлер:
//  1. Считывает исходный JSON из тела запроса в raw []byte (это «слепок» для БД).
//  2. Восстанавливает c.Request.Body из raw, чтобы можно было распарсить JSON в структуру.
//  3. Биндит в api_models.FullTenderData и валидирует payload.
//  4. Передаёт payload + raw в сервисный слой. Сервис в одной транзакции:
//     - создаёт/обновляет тендер и связанные сущности,
//     - делает UPSERT в tender_raw_data(raw_data) тем самым исходным raw.
//  5. Возвращает 201 с db_id и map ID лотов.
//
// Возможные ответы:
//   - 201 Created — успешный импорт
//   - 400 Bad Request — невалидный JSON или провал валидации
//   - 500 Internal Server Error — ошибка бизнес-логики/БД
func (s *Server) ImportTenderHandler(c *gin.Context) {
	logger := s.logger.WithField("handler", "ImportTenderHandler")
	logger.Info("Начало обработки запроса на импорт тендера")

	// --- 1) Считываем исходный JSON один раз в raw ---
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.Errorf("Ошибка чтения тела запроса: %v", err)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("не удалось прочитать тело запроса: %w", err)))
		return
	}
	// Важно: вернуть тело, чтобы биндер смог его прочитать повторно
	c.Request.Body = io.NopCloser(bytes.NewBuffer(raw))

	// --- 2) Биндинг в модель ---
	var payload api_models.FullTenderData
	if err := c.ShouldBindJSON(&payload); err != nil {
		logger.Errorf("Ошибка парсинга JSON: %v", err)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("некорректный JSON: %w", err)))
		return
	}

	// --- 3) Валидация бизнес-правил ---
	if err := payload.Validate(); err != nil {
		logger.Warnf("Невалидные данные для импорта тендера: %v", err)
		c.JSON(http.StatusBadRequest, errorResponse(err))
		return
	}

	// --- 4) Сервисный слой: передаём payload + raw ---
	dbID, lotsMap, err := s.tenderService.ImportFullTender(c.Request.Context(), &payload, raw)
	if err != nil {
		// Ошибка уже должна быть залогирована в сервисе
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	logger.Infof("Импорт завершён. TenderID=%s, DB_ID=%d, lots=%v", payload.TenderID, dbID, lotsMap)

	// --- 5) Ответ ---
	c.JSON(http.StatusCreated, gin.H{
		"message":   "Тендер успешно импортирован",
		"tender_id": payload.TenderID,
		"db_id":     dbID,
		"lots_id":   lotsMap,
	})
}
