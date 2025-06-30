package server

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
)

// listTenderTypesHandler - обработчик для получения списка типов тендеров с пагинацией.
func (s *Server) listTenderTypesHandler(c *gin.Context) {
	// Получаем параметры пагинации из URL
	pageIDStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("page_size", "20") // Можно задать другой размер страницы по умолчанию

	pageID, err := strconv.ParseInt(pageIDStr, 10, 32)
	if err != nil || pageID < 1 {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный параметр page")))
		return
	}

	pageSize, err := strconv.ParseInt(pageSizeStr, 10, 32)
	if err != nil || pageSize < 1 || pageSize > 100 {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный параметр page_size (допустимо от 1 до 100)")))
		return
	}

	// Создаем структуру с параметрами для sqlc
	params := db.ListTenderTypesParams{
		Limit:  int32(pageSize),
		Offset: (int32(pageID) - 1) * int32(pageSize),
	}

	// Вызываем метод из sqlc
	tenderTypes, err := s.store.ListTenderTypes(c.Request.Context(), params)
	if err != nil {
		s.logger.Errorf("ошибка получения списка типов тендеров: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	// В этот раз нам не нужно преобразовывать данные, так как структура `db.TenderType`
	// уже подходит для JSON-ответа. Возвращаем ее напрямую.
	if tenderTypes == nil {
		tenderTypes = make([]db.TenderType, 0)
	}

	c.JSON(http.StatusOK, tenderTypes)
}

// createTenderTypeRequest определяет структуру входящего JSON при создании типа
type createTenderTypeRequest struct {
	Title string `json:"title" binding:"required"`
}

// createTenderTypeHandler - обработчик для создания нового типа тендера
func (s *Server) createTenderTypeHandler(c *gin.Context) {
	var req createTenderTypeRequest

	// Парсим и валидируем входящий JSON
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err))
		return
	}

	// --- ИЗМЕНЕНИЕ ЗДЕСЬ ---
	// Вместо создания структуры UpsertTenderTypeParams,
	// мы передаем req.Title (тип string) напрямую,
	// как и ожидает сгенерированная sqlc функция.
	tenderType, err := s.store.UpsertTenderType(c.Request.Context(), req.Title)

	if err != nil {
		s.logger.Errorf("ошибка при создании/обновлении типа тендера: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	// Возвращаем созданный/найденный объект со статусом 201 Created
	c.JSON(http.StatusCreated, tenderType)
}

// Структура для входящего JSON при обновлении
type updateTenderTypeRequest struct {
	Title string `json:"title" binding:"required"`
}

// updateTenderTypeHandler - обновляет существующий тип тендера
func (s *Server) updateTenderTypeHandler(c *gin.Context) {
	// Получаем ID из URL
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID")))
		return
	}

	var req updateTenderTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err))
		return
	}

	params := db.UpdateTenderTypeParams{
		ID:    id,
		Title: req.Title,
	}

	updatedType, err := s.store.UpdateTenderType(c.Request.Context(), params)
	if err != nil {
		s.logger.Errorf("ошибка обновления типа тендера: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	c.JSON(http.StatusOK, updatedType)
}

// deleteTenderTypeHandler - удаляет тип тендера
func (s *Server) deleteTenderTypeHandler(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID")))
		return
	}

	err = s.store.DeleteTenderType(c.Request.Context(), id)
	if err != nil {
		s.logger.Errorf("ошибка удаления типа тендера: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	// При успешном удалении возвращаем статус 204 No Content
	c.Status(http.StatusNoContent)
}
