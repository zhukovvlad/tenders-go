package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/apierrors"
)

// listUsersHandler обрабатывает GET /api/v1/admin/users
// Список всех пользователей (только для admin)
func (s *Server) listUsersHandler(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": "not implemented yet",
	})
}

// updateUserRoleHandler обрабатывает PATCH /api/v1/admin/users/:id/role
// Изменение роли пользователя (только для admin)
func (s *Server) updateUserRoleHandler(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": "not implemented yet",
	})
}

// HandleUpdateSystemSetting обрабатывает PUT /api/v1/admin/settings.
//
// Обновляет системную настройку. Ожидает JSON-body с ключом и ровно одним значением.
// При обновлении "dedup_distance_threshold" автоматически удаляет
// устаревшие PENDING merge-заявки.
//
// Request:  UpdateSystemSettingRequest (strict JSON: DisallowUnknownFields)
// Response: 200 + SystemSettingResponse
// Errors:   400 (валидация), 401 (не аутентифицирован), 403 (не admin), 500 (БД)
func (s *Server) HandleUpdateSystemSetting(c *gin.Context) {
	logger := s.logger.WithField("handler", "HandleUpdateSystemSetting")

	// 1. Strict JSON decode (DisallowUnknownFields)
	var req api_models.UpdateSystemSettingRequest
	decoder := json.NewDecoder(bytes.NewReader(readBody(c)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		logger.Errorf("Ошибка парсинга JSON: %v", err)
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("некорректный JSON: %v", err)))
		return
	}

	// 2. Извлекаем user_id из JWT-контекста
	userID, exists := c.Get("user_id")
	if !exists {
		logger.Errorf("user_id отсутствует в контексте")
		c.JSON(http.StatusUnauthorized, errorResponse(fmt.Errorf("user not authenticated")))
		return
	}
	uid, ok := userID.(int64)
	if !ok {
		logger.Errorf("user_id имеет неожиданный тип: %T", userID)
		c.JSON(http.StatusInternalServerError, errorResponse(fmt.Errorf("invalid user_id type")))
		return
	}
	updatedBy := strconv.FormatInt(uid, 10)

	// 3. Вызываем сервис
	result, err := s.settingsService.UpdateSetting(c.Request.Context(), req, updatedBy)
	if err != nil {
		logger.Errorf("Ошибка UpdateSetting: %v", err)

		var validationErr *apierrors.ValidationError
		var notFoundErr *apierrors.NotFoundError
		switch {
		case errors.As(err, &validationErr):
			c.JSON(http.StatusBadRequest, errorResponse(err))
		case errors.As(err, &notFoundErr):
			c.JSON(http.StatusNotFound, errorResponse(err))
		default:
			c.JSON(http.StatusInternalServerError, errorResponse(err))
		}
		return
	}

	c.JSON(http.StatusOK, result)
}

// HandleListSystemSettings обрабатывает GET /api/v1/admin/settings.
// Возвращает все системные настройки.
func (s *Server) HandleListSystemSettings(c *gin.Context) {
	logger := s.logger.WithField("handler", "HandleListSystemSettings")

	settings, err := s.settingsService.ListSettings(c.Request.Context())
	if err != nil {
		logger.Errorf("Ошибка ListSettings: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	c.JSON(http.StatusOK, settings)
}

// HandleGetSystemSetting обрабатывает GET /api/v1/admin/settings/:key.
// Возвращает одну настройку по ключу.
func (s *Server) HandleGetSystemSetting(c *gin.Context) {
	logger := s.logger.WithField("handler", "HandleGetSystemSetting")

	key := c.Param("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("параметр key обязателен")))
		return
	}

	setting, err := s.settingsService.GetSetting(c.Request.Context(), key)
	if err != nil {
		logger.Errorf("Ошибка GetSetting(%s): %v", key, err)

		var notFoundErr *apierrors.NotFoundError
		if errors.As(err, &notFoundErr) {
			c.JSON(http.StatusNotFound, errorResponse(err))
		} else {
			c.JSON(http.StatusInternalServerError, errorResponse(err))
		}
		return
	}

	c.JSON(http.StatusOK, setting)
}

// readBody читает тело запроса и возвращает его как []byte.
// Gin буферизует body, поэтому читаем через gin.Context.
func readBody(c *gin.Context) []byte {
	body, _ := c.GetRawData()
	return body
}
