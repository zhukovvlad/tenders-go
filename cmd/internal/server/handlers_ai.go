package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
)

// SimpleLotAIResultsHandler — упрощенный обработчик AI результатов только по lot_id.
// Endpoint: POST /api/v1/lots/{lot_id}/ai-results
//
// Что делает хэндлер:
//  1. Извлекает lot_id из URL параметра.
//  2. Принимает JSON с результатами AI обработки в теле запроса.
//  3. Валидирует входящие данные.
//  4. Обновляет lot_key_parameters напрямую по lot_id без проверки tender_id.
//
// Возможные ответы:
//   - 200 OK — успешное обновление
//   - 400 Bad Request — невалидный JSON или провал валидации
//   - 404 Not Found — лот не найден
//   - 500 Internal Server Error — ошибка бизнес-логики/БД
func (s *Server) SimpleLotAIResultsHandler(c *gin.Context) {
	logger := s.logger.WithField("handler", "SimpleLotAIResultsHandler")
	logger.Info("Начало обработки упрощенного запроса с результатами AI обработки")

	// --- 1) Извлекаем параметр из URL ---
	lotID := c.Param("lot_id")

	if strings.TrimSpace(lotID) == "" {
		logger.Warn("Отсутствует параметр lot_id в URL")
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("параметр lot_id обязателен")))
		return
	}

	// --- 2) Биндинг JSON в модель ---
	var payload api_models.SimpleLotAIResult
	if err := c.ShouldBindJSON(&payload); err != nil {
		logger.Errorf("Ошибка парсинга JSON: %v", err)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("некорректный JSON: %w", err)))
		return
	}

	// --- 3) Валидация бизнес-правил ---
	if err := payload.Validate(); err != nil {
		logger.Warnf("Невалидные данные для обновления ключевых параметров: %v", err)
		c.JSON(http.StatusBadRequest, errorResponse(err))
		return
	}

	// --- 4) Сервисный слой: упрощенное обновление ключевых параметров ---
	err := s.lotService.UpdateLotKeyParametersDirectly(
		c.Request.Context(),
		lotID,
		payload.LotKeyParameters,
	)
	if err != nil {
		// Проверяем, является ли это ошибкой "не найдено"
		if strings.Contains(err.Error(), "не найден") {
			logger.Warnf("Лот не найден: %v", err)
			c.JSON(http.StatusNotFound, errorResponse(err))
			return
		}

		// Все остальные ошибки считаем внутренними
		logger.Errorf("Ошибка обновления ключевых параметров: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	logger.Infof("AI результаты успешно обработаны для лота %s", lotID)

	// --- 5) Успешный ответ ---
	c.JSON(http.StatusOK, gin.H{
		"message":    "AI результаты успешно обработаны",
		"lot_id":     lotID,
		"updated_at": "now",
	})
}
