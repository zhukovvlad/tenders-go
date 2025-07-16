package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/sqlc-dev/pqtype"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
)

// PATCH /api/v1/lots/:id/key-parameters
func (s *Server) patchLotKeyParametersHandler(c *gin.Context) {
	lotID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID лота")))
		return
	}

	// Шаг 1. Парсим тело запроса
	var req struct {
		LotKeyParameters map[string]string `json:"lot_key_parameters"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный JSON: %v", err)))
		return
	}

	parsed := make(map[string]interface{})
	for k, strVal := range req.LotKeyParameters {
		if floatVal, err := strconv.ParseFloat(strVal, 64); err == nil {
			parsed[k] = floatVal
		} else {
			parsed[k] = strVal // <-- здесь используем исходное strVal, а не shadowed v
		}
	}

	// Шаг 2. Сериализуем map в json.RawMessage
	raw, err := json.Marshal(parsed)
	if err != nil {
		s.logger.Errorf("ошибка сериализации lot_key_parameters: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	// Шаг 3. Вызываем sqlc-метод обновления
	params := db.UpdateLotDetailsParams{
		ID:               lotID,
		LotKeyParameters: pqtype.NullRawMessage{RawMessage: raw, Valid: true},
	}

	updated, err := s.store.UpdateLotDetails(c.Request.Context(), params)
	if err != nil {
		s.logger.Errorf("ошибка обновления параметров лота %d: %v", lotID, err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	c.JSON(http.StatusOK, updated)
}
