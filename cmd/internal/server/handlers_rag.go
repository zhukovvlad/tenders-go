package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services"
)

// === 1. GET /api/v1/positions/unmatched ===

// UnmatchedPositionsHandler - хендлер для GET /api/v1/positions/unmatched
func (s *Server) UnmatchedPositionsHandler(c *gin.Context) {
	logger := s.logger.WithField("handler", "UnmatchedPositionsHandler")

	// Получаем limit из query-параметров
	limitStr := c.DefaultQuery("limit", "100")
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		logger.Errorf("Некорректное значение limit: %s", limitStr)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("параметр limit должен быть целым числом")))
		return
	}

	// 1. Вызываем логику из tender_services (там есть валидация limit)
	//
	response, err := s.tenderService.GetUnmatchedPositions(c.Request.Context(), int32(limit))
	if err != nil {
		logger.Errorf("Ошибка GetUnmatchedPositions: %v", err)

		// Используем проверку типа для определения ошибок валидации
		var validationErr *services.ValidationError
		if errors.As(err, &validationErr) {
			// Ошибка валидации - возвращаем 400 Bad Request
			c.JSON(http.StatusBadRequest, errorResponse(err))
		} else {
			// Все остальные ошибки (БД и т.д.) - возвращаем 500 Internal Server Error
			c.JSON(http.StatusInternalServerError, errorResponse(err))
		}
		return
	}

	// Если ничего не найдено, возвращаем пустой массив, а не nil
	if response == nil {
		response = make([]api_models.UnmatchedPositionResponse, 0)
	}

	// 2. Отправляем JSON-ответ
	c.JSON(http.StatusOK, response)
}

// === 2. POST /api/v1/positions/match ===

// MatchPositionHandler - хендлер для POST /api/v1/positions/match
func (s *Server) MatchPositionHandler(c *gin.Context) {
	logger := s.logger.WithField("handler", "MatchPositionHandler")

	// 1. Биндинг JSON в DTO
	var payload api_models.MatchPositionRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		logger.Errorf("Ошибка парсинга JSON для MatchPosition: %v", err)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("некорректный JSON: %w", err)))
		return
	}

	// (Можно добавить Валидацию DTO, если нужно)

	// 2. Вызываем логику из tender_services
	//
	err := s.tenderService.MatchPosition(c.Request.Context(), payload)
	if err != nil {
		logger.Errorf("Ошибка MatchPosition: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	// 3. Успешный ответ
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// === 3. GET /api/v1/catalog/unindexed ===

// UnindexedCatalogItemsHandler - хендлер для GET /api/v1/catalog/unindexed
func (s *Server) UnindexedCatalogItemsHandler(c *gin.Context) {
	logger := s.logger.WithField("handler", "UnindexedCatalogItemsHandler")

	limitStr := c.DefaultQuery("limit", "1000")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 0 { // 0 - значит "без лимита"
		limit = 1000
	}

	// 1. Вызываем логику (этот метод нам еще нужно создать в tender_services.go)
	response, err := s.tenderService.GetUnindexedCatalogItems(c.Request.Context(), int32(limit))
	if err != nil {
		logger.Errorf("Ошибка GetUnindexedCatalogItems: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	if response == nil {
		response = make([]api_models.UnmatchedPositionResponse, 0) // Используем тот же DTO
	}

	c.JSON(http.StatusOK, response)
}

// === 4. POST /api/v1/catalog/indexed ===

// CatalogIndexedHandler - хендлер для POST /api/v1/catalog/indexed
func (s *Server) CatalogIndexedHandler(c *gin.Context) {
	logger := s.logger.WithField("handler", "CatalogIndexedHandler")

	var payload api_models.CatalogIndexedRequest // DTO: { CatalogIDs: []int64 }
	if err := c.ShouldBindJSON(&payload); err != nil {
		logger.Errorf("Ошибка парсинга JSON для CatalogIndexed: %v", err)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("некорректный JSON: %w", err)))
		return
	}

	// 1. Вызываем логику (этот метод нам еще нужно создать в tender_services.go)
	err := s.tenderService.MarkCatalogItemsAsActive(c.Request.Context(), payload.CatalogIDs)
	if err != nil {
		logger.Errorf("Ошибка MarkCatalogItemsAsActive: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "indexed_count": len(payload.CatalogIDs)})
}

// === 5. POST /api/v1/merges/suggest ===

// SuggestMergeHandler - хендлер для POST /api/v1/merges/suggest
func (s *Server) SuggestMergeHandler(c *gin.Context) {
	logger := s.logger.WithField("handler", "SuggestMergeHandler")

	var payload api_models.SuggestMergeRequest // DTO: { MainPositionID: ..., DuplicatePositionID: ..., ... }
	if err := c.ShouldBindJSON(&payload); err != nil {
		logger.Errorf("Ошибка парсинга JSON для SuggestMerge: %v", err)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("некорректный JSON: %w", err)))
		return
	}

	// 1. Вызываем логику (этот метод нам еще нужно создать в tender_services.go)
	err := s.tenderService.SuggestMerge(c.Request.Context(), payload)
	if err != nil {
		logger.Errorf("Ошибка SuggestMerge: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	c.JSON(http.StatusCreated, gin.H{"status": "suggestion_created"})
}
