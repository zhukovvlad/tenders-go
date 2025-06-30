package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
)

// Структура для JSON-запросов на создание/обновление категории
type tenderCategoryRequest struct {
	Title           string `json:"title" binding:"required"`
	TenderChapterID int64  `json:"tender_chapter_id" binding:"required,min=1"`
}

// listTenderCategoriesHandler получает список всех категорий
func (s *Server) listTenderCategoriesHandler(c *gin.Context) {
	// Логика пагинации
	pageIDStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("page_size", "100") // Берем побольше для справочника

	pageID, err := strconv.ParseInt(pageIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный параметр page")))
		return
	}

	pageSize, err := strconv.ParseInt(pageSizeStr, 10, 32)
	if err != nil || pageSize < 1 {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный параметр page_size")))
		return
	}

	params := db.ListTenderCategoriesParams{
		Limit:  int32(pageSize),
		Offset: (int32(pageID) - 1) * int32(pageSize),
	}

	categories, err := s.store.ListTenderCategories(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	// дополнительный проверка. Если нет категорий, возвращаем пустой массив
	// Это важно, чтобы фронт не ломался при отсутствии данных
	if categories == nil {
		categories = make([]db.ListTenderCategoriesRow, 0) // Возвращаем пустой массив, если нет данных
	}

	c.JSON(http.StatusOK, categories)
}

// createTenderCategoryHandler создает новую категорию
func (s *Server) createTenderCategoryHandler(c *gin.Context) {
	var req tenderCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err))
		return
	}
	// Используем ваш надежный Upsert
	params := db.UpsertTenderCategoryParams{
		Title:           req.Title,
		TenderChapterID: req.TenderChapterID,
	}
	category, err := s.store.UpsertTenderCategory(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}
	c.JSON(http.StatusCreated, category)
}

// updateTenderCategoryHandler обновляет категорию
func (s *Server) updateTenderCategoryHandler(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID")))
		return
	}
	var req tenderCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err))
		return
	}
	params := db.UpdateTenderCategoryParams{
		ID:              id,
		Title:           sql.NullString{String: req.Title, Valid: true},
		TenderChapterID: sql.NullInt64{Int64: req.TenderChapterID, Valid: true},
	}
	category, err := s.store.UpdateTenderCategory(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}
	c.JSON(http.StatusOK, category)
}

// deleteTenderCategoryHandler удаляет категорию
func (s *Server) deleteTenderCategoryHandler(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID")))
		return
	}
	err = s.store.DeleteTenderCategory(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) listCategoriesByChapterHandler(c *gin.Context) {
	chapterID, err := strconv.ParseInt(c.Param("chapter_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID раздела")))
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

	params := db.ListTenderCategoriesByChapterParams{
		TenderChapterID: chapterID,
		Limit:           int32(pageSize),
		Offset:          (int32(pageID) - 1) * int32(pageSize),
	}

	categories, err := s.store.ListTenderCategoriesByChapter(c.Request.Context(), params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	if categories == nil {
		categories = make([]db.TenderCategory, 0)
	}

	c.JSON(http.StatusOK, categories)
}
