package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"strconv"
	"time"

	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/apierrors"
)

// unitAccumulator — промежуточный аккумулятор для вычисления агрегатов по ед. изм.
type unitAccumulator struct {
	stats          *api_models.UnitPricingStats
	sumTotalCost   float64
	validCostCount int
}

// accumulateItem добавляет элемент в аккумулятор и обновляет агрегаты (min/max/sum).
func accumulateItem(accumulators map[string]*unitAccumulator, unitName string, item api_models.PositionPriceItem) {
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

	if item.WinnerRank != nil {
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
		if err == sql.ErrNoRows {
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
		item := api_models.PositionPriceItem{
			TenderNumber:      row.TenderNumber,
			LotKey:            row.LotKey,
			ContractorTitle:   row.ContractorTitle,
			ContractorInn:     row.ContractorInn,
			OriginalTitle:     row.OriginalTitle,
			Quantity:          parseNullNumeric(row.Quantity.String, row.Quantity.Valid),
			UnitCostMaterials: parseNullNumeric(row.UnitCostMaterials.String, row.UnitCostMaterials.Valid),
			UnitCostWorks:     parseNullNumeric(row.UnitCostWorks.String, row.UnitCostWorks.Valid),
			UnitCostIndirect:  parseNullNumeric(row.UnitCostIndirectCosts.String, row.UnitCostIndirectCosts.Valid),
			UnitCostTotal:     parseNullNumeric(row.UnitCostTotal.String, row.UnitCostTotal.Valid),
			CreatedAt:         row.CreatedAt.Format(time.RFC3339),
		}

		if row.WinnerRank.Valid {
			rank := int(row.WinnerRank.Int32)
			item.WinnerRank = &rank
		}
		if row.WinnerShare.Valid {
			item.WinnerShare = parseNullNumeric(row.WinnerShare.String, true)
		}

		accumulateItem(accumulators, row.UnitName, item)
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
		if err == sql.ErrNoRows {
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
		item := api_models.PositionPriceItem{
			TenderNumber:       row.TenderNumber,
			LotKey:             row.LotKey,
			ContractorTitle:    row.ContractorTitle,
			ContractorInn:      row.ContractorInn,
			OriginalTitle:      row.OriginalTitle,
			ChildPositionTitle: row.ChildPositionTitle,
			Quantity:           parseNullNumeric(row.Quantity.String, row.Quantity.Valid),
			UnitCostMaterials:  parseNullNumeric(row.UnitCostMaterials.String, row.UnitCostMaterials.Valid),
			UnitCostWorks:      parseNullNumeric(row.UnitCostWorks.String, row.UnitCostWorks.Valid),
			UnitCostIndirect:   parseNullNumeric(row.UnitCostIndirectCosts.String, row.UnitCostIndirectCosts.Valid),
			UnitCostTotal:      parseNullNumeric(row.UnitCostTotal.String, row.UnitCostTotal.Valid),
			CreatedAt:          row.CreatedAt.Format(time.RFC3339),
		}

		if row.WinnerRank.Valid {
			rank := int(row.WinnerRank.Int32)
			item.WinnerRank = &rank
		}
		if row.WinnerShare.Valid {
			item.WinnerShare = parseNullNumeric(row.WinnerShare.String, true)
		}

		accumulateItem(accumulators, row.UnitName, item)
	}

	return &api_models.GroupPricingResponse{
		GroupID:     groupID,
		StatsByUnit: finalizeStats(accumulators),
	}, nil
}

// parseNullNumeric конвертирует строковое значение PostgreSQL numeric в *float64.
func parseNullNumeric(s string, valid bool) *float64 {
	if !valid {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		log.Printf("[WARN] parseNullNumeric: не удалось распарсить numeric значение %q: %v", s, err)
		return nil
	}
	return &v
}
