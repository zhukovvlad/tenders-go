package server

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"golang.org/x/sync/errgroup"
)

// Response Structures
type ProposalFullDetailsResponse struct {
	Meta      ProposalMetaResponse           `json:"meta"`
	Summaries []SummaryLineResponse          `json:"summaries"`
	Info      map[string]string              `json:"info"`
	Positions []ProposalPositionItemResponse `json:"positions"`
}

type SummaryLineResponse struct {
	SummaryKey string               `json:"summary_key"`
	JobTitle   string               `json:"job_title"`
	TotalCost  SummaryCostBreakdown `json:"total_cost"`
}

type SummaryCostBreakdown struct {
	Materials     *string `json:"materials"`
	Works         *string `json:"works"`
	IndirectCosts *string `json:"indirect_costs"`
	Total         *string `json:"total"`
}

type ProposalMetaResponse struct {
	ID             int64  `json:"id"`
	ContractorName string `json:"contractor_name"`
	ContractorInn  string `json:"contractor_inn"`
	TenderTitle    string `json:"tender_title"`
	TenderEtpID    string `json:"tender_etp_id"`
	LotTitle       string `json:"lot_title"`
	IsBaseline     bool   `json:"is_baseline"`
}

type ProposalPositionItemResponse struct {
	ID            int64   `json:"id"`
	Number        *string `json:"number"`
	ChapterNumber *string `json:"chapter_number_in_proposal,omitempty"` // Номер главы из JSON
	Title         string  `json:"title"`
	IsChapter     bool    `json:"is_chapter"`
	UnitName      *string `json:"unit,omitempty"`
	Quantity      *string `json:"quantity,omitempty"`

	// Новые поля детализации
	PriceTotal    *string `json:"price_total,omitempty"`    // unit_cost_total
	CostTotal     *string `json:"cost_total,omitempty"`     // total_cost_total
	CostMaterials *string `json:"cost_materials,omitempty"` // total_cost_materials
	CostWorks     *string `json:"cost_works,omitempty"`     // total_cost_works

	CommentContractor *string `json:"comment_contractor,omitempty"`
	CatalogName       *string `json:"catalog_name,omitempty"`
}

// GET /api/v1/proposals/:id/details
func (s *Server) getProposalFullDetailsHandler(c *gin.Context) {
	proposalID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(fmt.Errorf("неверный ID предложения")))
		return
	}

	var (
		meta      db.GetProposalMetaRow
		summaries []db.ProposalSummaryLine
		infoList  []db.ProposalAdditionalInfo
		// ИСПРАВЛЕНИЕ 1: Используем тип, сгенерированный новым запросом ListPositionsForEstimate
		dbPositions []db.ListPositionsForEstimateRow
	)

	g, ctx := errgroup.WithContext(c.Request.Context())

	// 1. Мета-данные
	g.Go(func() error {
		var err error
		meta, err = s.store.GetProposalMeta(ctx, proposalID)
		if err != nil {
			if err == sql.ErrNoRows {
				return sql.ErrNoRows
			}
			return err
		}
		return nil
	})

	// 2. Итоги
	g.Go(func() error {
		var err error
		summaries, err = s.store.ListProposalSummaryLinesByProposalID(ctx, db.ListProposalSummaryLinesByProposalIDParams{
			ProposalID: proposalID,
			Limit:      1000, // Увеличен лимит для избежания усечения данных
			Offset:     0,
		})
		return err
	})

	// 3. Доп. информация
	g.Go(func() error {
		var err error
		infoList, err = s.store.ListProposalAdditionalInfoByProposalID(ctx, db.ListProposalAdditionalInfoByProposalIDParams{
			ProposalID: proposalID,
			Limit:      1000, // Увеличен лимит для избежания усечения данных
			Offset:     0,
		})
		return err
	})

	// 4. Позиции сметы
	g.Go(func() error {
		var err error
		// ИСПРАВЛЕНИЕ 2: Вызываем новый метод ListPositionsForEstimate
		dbPositions, err = s.store.ListPositionsForEstimate(ctx, proposalID)
		return err
	})

	if err := g.Wait(); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, errorResponse(fmt.Errorf("предложение не найдено")))
			return
		}
		c.JSON(http.StatusInternalServerError, errorResponse(err))
		return
	}

	// --- Сборка ответа ---

	infoMap := make(map[string]string)
	for _, item := range infoList {
		if item.InfoValue.Valid {
			infoMap[item.InfoKey] = item.InfoValue.String
		}
	}

	apiPositions := make([]ProposalPositionItemResponse, len(dbPositions))
	for i, p := range dbPositions {
		// Подготовка указателей для Nullable полей
		var itemNum, chapterNum, unitName, qty, price, cost, catName, costMat, costWorks, comment *string

		// Базовые поля
		if p.ItemNumberInProposal.Valid {
			itemNum = &p.ItemNumberInProposal.String
		}
		if p.ChapterNumberInProposal.Valid {
			chapterNum = &p.ChapterNumberInProposal.String
		}
		if p.UnitName.Valid {
			unitName = &p.UnitName.String
		}
		if p.Quantity.Valid {
			q := p.Quantity.String
			qty = &q
		}
		if p.UnitCostTotal.Valid {
			pr := p.UnitCostTotal.String
			price = &pr
		}
		if p.TotalCostTotal.Valid {
			co := p.TotalCostTotal.String
			cost = &co
		}
		if p.CatalogName.Valid {
			catName = &p.CatalogName.String
		}

		// ИСПРАВЛЕНИЕ 3: Маппинг новых полей (материалы, работы, комментарии)
		// Убедись, что sqlc сгенерировал именно такие имена полей (обычно CamelCase от snake_case в SQL)
		if p.TotalCostMaterials.Valid {
			cm := p.TotalCostMaterials.String
			costMat = &cm
		}
		if p.TotalCostWorks.Valid {
			cw := p.TotalCostWorks.String
			costWorks = &cw
		}
		if p.CommentContractor.Valid {
			cmt := p.CommentContractor.String
			comment = &cmt
		}

		apiPositions[i] = ProposalPositionItemResponse{
			ID:            p.ID,
			Number:        itemNum,
			ChapterNumber: chapterNum,
			Title:         p.JobTitleInProposal,
			IsChapter:     p.IsChapter,
			UnitName:      unitName,
			Quantity:      qty,
			// ИСПРАВЛЕНИЕ 4: Используем правильные имена полей структуры (PriceTotal, а не Price)
			PriceTotal:        price,
			CostTotal:         cost,
			CostMaterials:     costMat,
			CostWorks:         costWorks,
			CommentContractor: comment,
			CatalogName:       catName,
		}
	}

	// Маппинг summaries в API response структуру
	apiSummaries := make([]SummaryLineResponse, len(summaries))
	for i, summ := range summaries {
		var materials, works, indirectCosts, total *string

		if summ.MaterialsCost.Valid {
			val := summ.MaterialsCost.String
			materials = &val
		}
		if summ.WorksCost.Valid {
			val := summ.WorksCost.String
			works = &val
		}
		if summ.IndirectCostsCost.Valid {
			val := summ.IndirectCostsCost.String
			indirectCosts = &val
		}
		if summ.TotalCost.Valid {
			val := summ.TotalCost.String
			total = &val
		}

		apiSummaries[i] = SummaryLineResponse{
			SummaryKey: summ.SummaryKey,
			JobTitle:   summ.JobTitle,
			TotalCost: SummaryCostBreakdown{
				Materials:     materials,
				Works:         works,
				IndirectCosts: indirectCosts,
				Total:         total,
			},
		}
	}

	response := ProposalFullDetailsResponse{
		Meta: ProposalMetaResponse{
			ID:             meta.ID,
			ContractorName: meta.ContractorName,
			ContractorInn:  meta.ContractorInn,
			TenderTitle:    meta.TenderTitle,
			TenderEtpID:    meta.TenderEtpID,
			LotTitle:       meta.LotTitle,
			IsBaseline:     meta.IsBaseline,
		},
		Summaries: apiSummaries,
		Info:      infoMap,
		Positions: apiPositions,
	}

	c.JSON(http.StatusOK, response)
}
