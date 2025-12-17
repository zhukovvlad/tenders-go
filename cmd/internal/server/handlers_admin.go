package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
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
