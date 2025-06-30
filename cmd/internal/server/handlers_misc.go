package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Server) HomeHandler(c *gin.Context) {
	c.JSON(200, gin.H{
		"message": "Welcome to the Tenders API",
	})
}

func (s *Server) getStatsHandler(c *gin.Context) {
	count, err := s.store.GetTendersCount(c.Request.Context())
	if err != nil {
		s.logger.Errorf("Ошибка при получении количества тендеров: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"tenders_count": count,
		"message":       "Статистика успешно получена",
	})
}