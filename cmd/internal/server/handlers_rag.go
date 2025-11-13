package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/apierrors"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/matching"
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
	response, err := s.matchingService.GetUnmatchedPositions(c.Request.Context(), int32(limit))
	if err != nil {
		logger.Errorf("Ошибка GetUnmatchedPositions: %v", err)

		// Используем проверку типа для определения ошибок валидации
		var validationErr *apierrors.ValidationError
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
	err := s.matchingService.MatchPosition(c.Request.Context(), payload)
	if err != nil {
		logger.Errorf("Ошибка MatchPosition: %v", err)
		var validationErr *apierrors.ValidationError
		if errors.As(err, &validationErr) {
			c.JSON(http.StatusBadRequest, errorResponse(err))
		} else {
			c.JSON(http.StatusInternalServerError, errorResponse(err))
		}
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
	if err != nil {
		logger.Errorf("Некорректное значение limit: %s", limitStr)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("параметр limit должен быть целым числом")))
		return
	}
	if limit < 0 {
		logger.Errorf("Некорректное значение limit: %d (должно быть >= 0)", limit)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("параметр limit должен быть >= 0")))
		return
	}

	// 1. Вызываем логику (этот метод нам еще нужно создать в tender_services.go)
	response, err := s.catalogService.GetUnindexedCatalogItems(c.Request.Context(), int32(limit))
	if err != nil {
		logger.Errorf("Ошибка GetUnindexedCatalogItems: %v", err)
		var validationErr *apierrors.ValidationError
		if errors.As(err, &validationErr) {
			c.JSON(http.StatusBadRequest, errorResponse(err))
		} else {
			c.JSON(http.StatusInternalServerError, errorResponse(err))
		}
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
	err := s.catalogService.MarkCatalogItemsAsActive(c.Request.Context(), payload.CatalogIDs)
	if err != nil {
		logger.Errorf("Ошибка MarkCatalogItemsAsActive: %v", err)
		var validationErr *apierrors.ValidationError
		if errors.As(err, &validationErr) {
			c.JSON(http.StatusBadRequest, errorResponse(err))
		} else {
			c.JSON(http.StatusInternalServerError, errorResponse(err))
		}
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
	err := s.catalogService.SuggestMerge(c.Request.Context(), payload)
	if err != nil {
		logger.Errorf("Ошибка SuggestMerge: %v", err)
		var validationErr *apierrors.ValidationError
		if errors.As(err, &validationErr) {
			c.JSON(http.StatusBadRequest, errorResponse(err))
		} else {
			c.JSON(http.StatusInternalServerError, errorResponse(err))
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{"status": "suggestion_created"})
}

// === 6. GET /api/v1/catalog/active ===

// ActiveCatalogItemsHandler - хендлер для GET /api/v1/catalog/active (с пагинацией)
func (s *Server) ActiveCatalogItemsHandler(c *gin.Context) {
	logger := s.logger.WithField("handler", "ActiveCatalogItemsHandler")

	// --- Новая логика парсинга пагинации ---
	limitStr := c.DefaultQuery("limit", "1000") // Батч по 1000 по умолчанию
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		logger.Errorf("Некорректное значение limit: %s", limitStr)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("параметр limit должен быть целым числом")))
		return
	}
	if limit <= 0 {
		logger.Errorf("Некорректное значение limit: %d (должно быть > 0)", limit)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("параметр limit должен быть > 0")))
		return
	}

	// Ограничиваем limit максимальным значением
	if limit > matching.MaxUnmatchedPositionsLimit {
		logger.Warnf("Запрошено limit=%d, ограничиваем до MaxUnmatchedPositionsLimit=%d",
			limit, matching.MaxUnmatchedPositionsLimit)
		limit = matching.MaxUnmatchedPositionsLimit
	}

	offsetStr := c.DefaultQuery("offset", "0")
	offset, err := strconv.Atoi(offsetStr)
	if err != nil {
		logger.Errorf("Некорректное значение offset: %s", offsetStr)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("параметр offset должен быть целым числом")))
		return
	}
	if offset < 0 {
		logger.Errorf("Некорректное значение offset: %d (должно быть >= 0)", offset)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("параметр offset должен быть >= 0")))
		return
	}
	// --- Конец логики пагинации ---

	// 1. Вызываем новый сервисный метод
	response, err := s.catalogService.GetAllActiveCatalogItems(c.Request.Context(), int32(limit), int32(offset))
	if err != nil {
		logger.Errorf("Ошибка GetAllActiveCatalogItems: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	if response == nil {
		response = make([]api_models.UnmatchedPositionResponse, 0)
	}

	c.JSON(http.StatusOK, response)
}
