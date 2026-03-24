package server

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/apierrors"
)

// GetPositionPricingHandler обрабатывает GET /api/v1/catalog/positions/:id/pricing.
// Возвращает агрегированную статистику цен по каталожной позиции,
// сгруппированную по единицам измерения.
func (s *Server) GetPositionPricingHandler(c *gin.Context) {
	logger := s.logger.WithField("handler", "GetPositionPricingHandler")

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("невалидный id каталожной позиции")))
		return
	}

	result, err := s.catalogService.GetPositionPricingStats(c.Request.Context(), id)
	if err != nil {
		switch err.(type) {
		case *apierrors.ValidationError:
			c.JSON(http.StatusBadRequest, errorResponse(err))
		case *apierrors.NotFoundError:
			c.JSON(http.StatusNotFound, errorResponse(err))
		default:
			logger.WithError(err).Errorf("failed to get pricing stats for position=%d", id)
			c.JSON(http.StatusInternalServerError, errorResponse(fmt.Errorf("ошибка получения аналитики цен")))
		}
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetGroupPricingHandler обрабатывает GET /api/v1/catalog/groups/:id/pricing.
// Возвращает агрегированную статистику цен по группе каталожных позиций (рекурсивно),
// сгруппированную по единицам измерения.
func (s *Server) GetGroupPricingHandler(c *gin.Context) {
	logger := s.logger.WithField("handler", "GetGroupPricingHandler")

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("невалидный id группы")))
		return
	}

	result, err := s.catalogService.GetGroupPricingStats(c.Request.Context(), id)
	if err != nil {
		switch err.(type) {
		case *apierrors.ValidationError:
			c.JSON(http.StatusBadRequest, errorResponse(err))
		case *apierrors.NotFoundError:
			c.JSON(http.StatusNotFound, errorResponse(err))
		default:
			logger.WithError(err).Errorf("failed to get pricing stats for group=%d", id)
			c.JSON(http.StatusInternalServerError, errorResponse(fmt.Errorf("ошибка получения аналитики цен группы")))
		}
		return
	}

	c.JSON(http.StatusOK, result)
}
