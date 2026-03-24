package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/apierrors"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

// unitAccumulator — промежуточный аккумулятор для вычисления агрегатов по ед. изм.
type unitAccumulator struct {
	stats          *api_models.UnitPricingStats
	sumTotalCost   float64
	validCostCount int
}

// pricingRowData — общие поля из строк position_pricing и group_pricing запросов.
// Используется для дедупликации маппинга между GetPositionPricingStats и GetGroupPricingStats.
type pricingRowData struct {
	TenderNumber          string
	LotKey                string
	ContractorTitle       string
	ContractorInn         string
	OriginalTitle         string
	ChildPositionTitle    string
	Quantity              sql.NullString
	UnitCostMaterials     sql.NullString
	UnitCostWorks         sql.NullString
	UnitCostIndirectCosts sql.NullString
	UnitCostTotal         sql.NullString
	UnitName              string
	WinnerRank            sql.NullInt32
	WinnerShare           sql.NullString
	WinnerID              sql.NullInt64
	CreatedAt             time.Time
}

// mapRowToItem конвертирует общие поля строки в PositionPriceItem.
func mapRowToItem(row pricingRowData, logger logging.Logger) api_models.PositionPriceItem {
	item := api_models.PositionPriceItem{
		TenderNumber:       row.TenderNumber,
		LotKey:             row.LotKey,
		ContractorTitle:    row.ContractorTitle,
		ContractorInn:      row.ContractorInn,
		OriginalTitle:      row.OriginalTitle,
		ChildPositionTitle: row.ChildPositionTitle,
		Quantity:           parseNullNumeric(row.Quantity.String, row.Quantity.Valid, logger),
		UnitCostMaterials:  parseNullNumeric(row.UnitCostMaterials.String, row.UnitCostMaterials.Valid, logger),
		UnitCostWorks:      parseNullNumeric(row.UnitCostWorks.String, row.UnitCostWorks.Valid, logger),
		UnitCostIndirect:   parseNullNumeric(row.UnitCostIndirectCosts.String, row.UnitCostIndirectCosts.Valid, logger),
		UnitCostTotal:      parseNullNumeric(row.UnitCostTotal.String, row.UnitCostTotal.Valid, logger),
		CreatedAt:          row.CreatedAt.Format(time.RFC3339),
	}

	if row.WinnerRank.Valid {
		rank := int(row.WinnerRank.Int32)
		item.WinnerRank = &rank
	}
	if row.WinnerShare.Valid {
		item.WinnerShare = parseNullNumeric(row.WinnerShare.String, true, logger)
	}

	return item
}

// accumulateItem добавляет элемент в аккумулятор и обновляет агрегаты (min/max/sum).
// hasWinner определяется по наличию записи в таблице winners (winner_id IS NOT NULL),
// а не по наличию rank, т.к. rank nullable.
func accumulateItem(accumulators map[string]*unitAccumulator, unitName string, item api_models.PositionPriceItem, hasWinner bool) {
	acc, ok := accumulators[unitName]
	if !ok {
		acc = &unitAccumulator{
			stats: &api_models.UnitPricingStats{
				UnitName: unitName,
				Items:    []api_models.PositionPriceItem{},
			},
		}
		accumulators[unitName] = acc
	}

	acc.stats.Items = append(acc.stats.Items, item)
	acc.stats.TotalCount++

	if hasWinner {
		acc.stats.WinnerCount++
	}

	if item.UnitCostTotal != nil {
		cost := *item.UnitCostTotal

		if acc.stats.MinUnitCost == nil || cost < *acc.stats.MinUnitCost {
			minVal := cost
			acc.stats.MinUnitCost = &minVal
		}
		if acc.stats.MaxUnitCost == nil || cost > *acc.stats.MaxUnitCost {
			maxVal := cost
			acc.stats.MaxUnitCost = &maxVal
		}

		acc.sumTotalCost += cost
		acc.validCostCount++
	}
}

// finalizeStats вычисляет среднюю цену и собирает итоговую карту статистик.
func finalizeStats(accumulators map[string]*unitAccumulator) map[string]*api_models.UnitPricingStats {
	statsByUnit := make(map[string]*api_models.UnitPricingStats, len(accumulators))
	for unitName, acc := range accumulators {
		if acc.stats.MinUnitCost != nil {
			v := math.Round(*acc.stats.MinUnitCost*100) / 100
			acc.stats.MinUnitCost = &v
		}
		if acc.stats.MaxUnitCost != nil {
			v := math.Round(*acc.stats.MaxUnitCost*100) / 100
			acc.stats.MaxUnitCost = &v
		}
		if acc.validCostCount > 0 {
			avg := acc.sumTotalCost / float64(acc.validCostCount)
			avg = math.Round(avg*100) / 100
			acc.stats.AvgUnitCost = &avg
		}
		statsByUnit[unitName] = acc.stats
	}
	return statsByUnit
}

// GetPositionPricingStats возвращает агрегированную статистику цен
// по всем предложениям подрядчиков для указанной каталожной позиции,
// сгруппированную по единицам измерения.
func (s *CatalogService) GetPositionPricingStats(ctx context.Context, catalogPositionID int64) (*api_models.PositionPricingResponse, error) {
	logger := s.logger.WithField("method", "GetPositionPricingStats")

	if catalogPositionID <= 0 {
		return nil, apierrors.NewValidationError("id должен быть положительным")
	}

	// Проверяем существование позиции
	_, err := s.store.GetCatalogPositionByID(ctx, catalogPositionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierrors.NewNotFoundError("каталожная позиция %d не найдена", catalogPositionID)
		}
		logger.WithError(err).Errorf("failed to check catalog position existence for id=%d", catalogPositionID)
		return nil, fmt.Errorf("ошибка проверки каталожной позиции: %w", err)
	}

	rows, err := s.store.GetPositionPricingData(ctx, catalogPositionID)
	if err != nil {
		logger.WithError(err).Errorf("failed to get pricing data for catalog_position_id=%d", catalogPositionID)
		return nil, fmt.Errorf("ошибка GetPositionPricingData: %w", err)
	}

	accumulators := make(map[string]*unitAccumulator)

	for _, row := range rows {
		common := pricingRowData{
			TenderNumber:          row.TenderNumber,
			LotKey:                row.LotKey,
			ContractorTitle:       row.ContractorTitle,
			ContractorInn:         row.ContractorInn,
			OriginalTitle:         row.OriginalTitle,
			Quantity:              row.Quantity,
			UnitCostMaterials:     row.UnitCostMaterials,
			UnitCostWorks:         row.UnitCostWorks,
			UnitCostIndirectCosts: row.UnitCostIndirectCosts,
			UnitCostTotal:         row.UnitCostTotal,
			UnitName:              row.UnitName,
			WinnerRank:            row.WinnerRank,
			WinnerShare:           row.WinnerShare,
			WinnerID:              row.WinnerID,
			CreatedAt:             row.CreatedAt,
		}
		item := mapRowToItem(common, logger)
		accumulateItem(accumulators, common.UnitName, item, common.WinnerID.Valid)
	}

	return &api_models.PositionPricingResponse{
		CatalogPositionID: catalogPositionID,
		StatsByUnit:       finalizeStats(accumulators),
	}, nil
}

// GetGroupPricingStats возвращает агрегированную статистику цен
// по всем предложениям подрядчиков для группы каталожных позиций (рекурсивно),
// сгруппированную по единицам измерения.
func (s *CatalogService) GetGroupPricingStats(ctx context.Context, groupID int64) (*api_models.GroupPricingResponse, error) {
	logger := s.logger.WithField("method", "GetGroupPricingStats")

	if groupID <= 0 {
		return nil, apierrors.NewValidationError("id должен быть положительным")
	}

	// Проверяем существование и тип записи
	pos, err := s.store.GetCatalogPositionByID(ctx, groupID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apierrors.NewNotFoundError("группа %d не найдена", groupID)
		}
		logger.WithError(err).Errorf("failed to check catalog position existence for id=%d", groupID)
		return nil, fmt.Errorf("ошибка проверки каталожной позиции: %w", err)
	}
	if pos.Kind != "GROUP_TITLE" {
		return nil, apierrors.NewValidationError("позиция %d не является группой (kind=%s)", groupID, pos.Kind)
	}

	rows, err := s.store.GetGroupPricingData(ctx, groupID)
	if err != nil {
		logger.WithError(err).Errorf("failed to get pricing data for group_id=%d", groupID)
		return nil, fmt.Errorf("ошибка GetGroupPricingData: %w", err)
	}

	accumulators := make(map[string]*unitAccumulator)

	for _, row := range rows {
		common := pricingRowData{
			TenderNumber:          row.TenderNumber,
			LotKey:                row.LotKey,
			ContractorTitle:       row.ContractorTitle,
			ContractorInn:         row.ContractorInn,
			OriginalTitle:         row.OriginalTitle,
			ChildPositionTitle:    row.ChildPositionTitle,
			Quantity:              row.Quantity,
			UnitCostMaterials:     row.UnitCostMaterials,
			UnitCostWorks:         row.UnitCostWorks,
			UnitCostIndirectCosts: row.UnitCostIndirectCosts,
			UnitCostTotal:         row.UnitCostTotal,
			UnitName:              row.UnitName,
			WinnerRank:            row.WinnerRank,
			WinnerShare:           row.WinnerShare,
			WinnerID:              row.WinnerID,
			CreatedAt:             row.CreatedAt,
		}
		item := mapRowToItem(common, logger)
		accumulateItem(accumulators, common.UnitName, item, common.WinnerID.Valid)
	}

	return &api_models.GroupPricingResponse{
		GroupID:     groupID,
		StatsByUnit: finalizeStats(accumulators),
	}, nil
}

// parseNullNumeric конвертирует строковое значение PostgreSQL numeric в *float64.
// При ошибке парсинга логирует предупреждение через структурированный логгер.
func parseNullNumeric(s string, valid bool, logger logging.Logger) *float64 {
	if !valid {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		logger.Warnf("parseNullNumeric: не удалось распарсить numeric значение %q: %v", s, err)
		return nil
	}
	return &v
}
