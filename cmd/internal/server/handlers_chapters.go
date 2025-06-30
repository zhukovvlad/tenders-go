package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
)

// listTenderChaptersHandler - обработчик для получения списка разделов тендеров с пагинацией.
func (s *Server) listTenderChaptersHandler(c *gin.Context) {
	// Получаем параметры пагинации из URL
	pageIDStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("page_size", "20")

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

	params := db.ListTenderChaptersParams{
		Limit:  int32(pageSize),
		Offset: (int32(pageID) - 1) * int32(pageSize),
	}

	// Вызываем новую функцию из sqlc
	tenderChapters, err := s.store.ListTenderChapters(c.Request.Context(), params)
	if err != nil {
		s.logger.Errorf("ошибка получения списка разделов тендеров: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	if tenderChapters == nil {
		tenderChapters = make([]db.ListTenderChaptersRow, 0)
	}

	c.JSON(http.StatusOK, tenderChapters)
}

// Структура для входящего JSON остается той же
type createTenderChapterRequest struct {
	Title        string `json:"title" binding:"required"`
	TenderTypeID int64  `json:"tender_type_id" binding:"required,min=1"`
}

// createTenderChapterHandler - обработчик для создания/обновления раздела тендера
func (s *Server) createTenderChapterHandler(c *gin.Context) {
	var req createTenderChapterRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err))
		return
	}

	// --- ИЗМЕНЕНИЕ: Вызываем UpsertTenderChapter вместо CreateTenderChapter ---
	params := db.UpsertTenderChapterParams{ // sqlc сгенерирует эту структуру для Upsert
		Title:        req.Title,
		TenderTypeID: req.TenderTypeID,
	}

	// Вызываем более надежный метод Upsert
	chapter, err := s.store.UpsertTenderChapter(c.Request.Context(), params)
	if err != nil {
		s.logger.Errorf("ошибка при создании/обновлении раздела тендера: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	// Возвращаем созданный/обновленный объект
	c.JSON(http.StatusCreated, chapter)
}

// Структура для входящего JSON при обновлении раздела
type updateTenderChapterRequest struct {
	Title        string `json:"title" binding:"required"`
	TenderTypeID int64  `json:"tender_type_id" binding:"required,min=1"`
}

// updateTenderChapterHandler - обновляет существующий раздел тендера
func (s *Server) updateTenderChapterHandler(c *gin.Context) {
	// Получаем ID из URL
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID раздела")))
		return
	}

	var req updateTenderChapterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err))
		return
	}

	params := db.UpdateTenderChapterParams{
		ID:           id,
		Title:        sql.NullString{String: req.Title, Valid: true},
		TenderTypeID: sql.NullInt64{Int64: req.TenderTypeID, Valid: true},
	}

	updatedChapter, err := s.store.UpdateTenderChapter(c.Request.Context(), params)
	if err != nil {
		s.logger.Errorf("ошибка обновления раздела тендера: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	c.JSON(http.StatusOK, updatedChapter)
}

// deleteTenderChapterHandler - удаляет раздел тендера
func (s *Server) deleteTenderChapterHandler(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID раздела")))
		return
	}

	err = s.store.DeleteTenderChapter(c.Request.Context(), id)
	if err != nil {
		s.logger.Errorf("ошибка удаления раздела тендера: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	c.Status(http.StatusNoContent)
}

// listChaptersByTypeHandler - получает список разделов для конкретного типа тендера
func (s *Server) listChaptersByTypeHandler(c *gin.Context) {
	// Получаем ID типа из URL, например /api/v1/tender-types/1/chapters
	typeID, err := strconv.ParseInt(c.Param("type_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID типа тендера")))
		return
	}

	// Пагинация (можно оставить для унификации, но для выпадающего списка обычно не нужна)
	pageIDStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("page_size", "100") // Берем с запасом

	pageID, _ := strconv.ParseInt(pageIDStr, 10, 32)
	pageSize, _ := strconv.ParseInt(pageSizeStr, 10, 32)
	if pageID < 1 {
		pageID = 1
	}
	if pageSize < 1 {
		pageSize = 100
	}

	params := db.ListTenderChaptersByTypeParams{
		TenderTypeID: typeID,
		Limit:        int32(pageSize),
		Offset:       (int32(pageID) - 1) * int32(pageSize),
	}

	chapters, err := s.store.ListTenderChaptersByType(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	if chapters == nil {
		chapters = make([]db.TenderChapter, 0)
	}

	c.JSON(http.StatusOK, chapters)
}