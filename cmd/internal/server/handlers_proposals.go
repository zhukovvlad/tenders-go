package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
)

// Обновляем структуру для API-ответа
type proposalResponse struct {
	ProposalID      int64    `json:"proposal_id"`
	ContractorID    int64    `json:"contractor_id"`
	ContractorTitle string   `json:"contractor_title"`
	ContractorInn   string   `json:"contractor_inn"`
	IsWinner        bool     `json:"is_winner"`
	TotalCost       *float64 `json:"total_cost"`
	// Добавляем поле для всего объекта additional_info
	AdditionalInfo json.RawMessage `json:"additional_info"`
}

// listProposalsHandler - обработчик для получения списка предложений по тендеру
func (s *Server) listProposalsHandler(c *gin.Context) {
	tenderID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID тендера")))
		return
	}

	pageIDStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("page_size", "20")

	pageID, err := strconv.ParseInt(pageIDStr, 10, 32)
	if err != nil || pageID < 1 {
		pageID = 1
	}

	pageSize, err := strconv.ParseInt(pageSizeStr, 10, 32)
	if err != nil || pageSize < 1 {
		pageSize = 20
	}

	params := db.ListProposalsForTenderParams{
		TenderID: tenderID,
		Limit:    int32(pageSize),
		Offset:   (int32(pageID) - 1) * int32(pageSize),
	}

	dbProposals, err := s.store.ListProposalsForTender(c.Request.Context(), params)
	if err != nil {
		s.logger.Errorf("ошибка получения списка предложений: %v", err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	// --- ЛОГИКА ПРЕОБРАЗОВАНИЯ ДАННЫХ ---
	apiResponse := make([]proposalResponse, 0, len(dbProposals))
	for _, p := range dbProposals {
		// Создаем экземпляр нашей чистой структуры для API
		apiProp := proposalResponse{
			ProposalID:      p.ProposalID,
			ContractorID:    p.ContractorID,
			ContractorTitle: p.ContractorTitle,
			ContractorInn:   p.ContractorInn,
			IsWinner:        p.IsWinner,
			AdditionalInfo:  p.AdditionalInfo,
		}

		// Проверяем, что строка с ценой не NULL
		if p.TotalCost.Valid {
			// Конвертируем строку (p.TotalCost.String) в float64
			cost, err := strconv.ParseFloat(p.TotalCost.String, 64)
			if err == nil { // Если конвертация прошла успешно
				apiProp.TotalCost = &cost // Присваиваем указатель на полученное число
			}
			// Если конвертация не удалась, поле останется nil, что тоже корректно
		}

		apiResponse = append(apiResponse, apiProp)
	}

	c.JSON(http.StatusOK, apiResponse)
}

// listProposalsForLotHandler - обработчик для получения списка предложений по ID лота
func (s *Server) listProposalsForLotHandler(c *gin.Context) {
	// Получаем ID лота из URL
	lotID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID лота")))
		return
	}

	pageIDStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("page_size", "20")

	pageID, err := strconv.ParseInt(pageIDStr, 10, 32)
	if err != nil || pageID < 1 {
		pageID = 1
	}

	pageSize, err := strconv.ParseInt(pageSizeStr, 10, 32)
	if err != nil || pageSize < 1 {
		pageSize = 20
	}

	params := db.ListRichProposalsForLotParams{ // <-- Используем правильный тип параметров
		LotID:  lotID,
		Limit:  int32(pageSize),
		Offset: (int32(pageID) - 1) * int32(pageSize),
	}

	// Вызываем новую функцию, сгенерированную sqlc
	dbProposals, err := s.store.ListRichProposalsForLot(c.Request.Context(), params)
	if err != nil {
		s.logger.Errorf("ошибка получения списка предложений для лота %d: %v", lotID, err)
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	// --- Логика преобразования данных (остается такой же, как мы делали раньше) ---
	// Она преобразует "сырые" данные из БД в "чистый" JSON для API
	apiResponse := make([]proposalResponse, 0, len(dbProposals))
	for _, p := range dbProposals {
		apiProp := proposalResponse{
			ProposalID:      p.ProposalID,
			ContractorID:    p.ContractorID,
			ContractorTitle: p.ContractorTitle,
			ContractorInn:   p.ContractorInn,
			IsWinner:        p.IsWinner,
			AdditionalInfo:  p.AdditionalInfo,
		}
		if p.TotalCost.Valid {
			// Конвертируем строку (p.TotalCost.String) в float64
			cost, err := strconv.ParseFloat(p.TotalCost.String, 64)
			if err == nil { // Если конвертация прошла успешно
				apiProp.TotalCost = &cost // Присваиваем указатель на полученное число
			}
			// Если конвертация не удалась, поле останется nil, что тоже корректно
		}
		apiResponse = append(apiResponse, apiProp)
	}
	// ------------------------------------------------------------------------

	c.JSON(http.StatusOK, apiResponse)
}