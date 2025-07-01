package services

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/util"
)

func getOrCreateOrUpdate[T any, P any](
	ctx context.Context,
	qtx db.Querier,
	// Функция для получения существующей сущности
	getFn func() (T, error),
	// Функция для создания новой сущности
	createFn func() (T, error),
	// Функция, которая проверяет, нужно ли обновление.
	// Возвращает:
	// 1. bool - нужно ли обновление.
	// 2. P - параметры для обновления.
	// 3. error - если ошибка.
	diffFn func(existing T) (bool, P, error),
	// Функция для выполнения обновления
	updateFn func(params P) (T, error),
) (T, error) {
	existing, err := getFn()
	if err != nil {
		if err == sql.ErrNoRows {
			// Сущность не найдена, создаем новую
			return createFn()
		}

		var zero T
		return zero, err
	}

	// Сущность найдена, проверяем необходимость обновления
	needsUpdate, updateParams, err := diffFn(existing)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("ошибка при проверке необходимости обновления: %w", err)
	}

	if needsUpdate {
		return updateFn(updateParams)
	}

	return existing, nil
}

func mapApiPositionToDbParams(
	proposalID int64,
	positionKey string,
	catalogPositionID int64,
	unitID sql.NullInt64,
	posAPI api_models.PositionItem,
) db.UpsertPositionItemParams {
	return db.UpsertPositionItemParams{
		ProposalID:                    proposalID,
		CatalogPositionID:             catalogPositionID,
		PositionKeyInProposal:         positionKey,
		CommentOrganazier:             util.NullableString(posAPI.CommentOrganizer),
		CommentContractor:             util.NullableString(posAPI.CommentContractor),
		ItemNumberInProposal:          util.NullableString(&posAPI.Number), // Number - string, not *string в api_models
		ChapterNumberInProposal:       util.NullableString(posAPI.ChapterNumber),
		JobTitleInProposal:            posAPI.JobTitle,
		UnitID:                        unitID, // sql.NullInt64
		Quantity:                      util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.Quantity)),
		SuggestedQuantity:             util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.SuggestedQuantity)),
		TotalCostForOrganizerQuantity: util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.TotalCostForOrganizerQuantity)),
		UnitCostMaterials:             util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.UnitCost.Materials)),
		UnitCostWorks:                 util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.UnitCost.Works)),
		UnitCostIndirectCosts:         util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.UnitCost.IndirectCosts)),
		UnitCostTotal:                 util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.UnitCost.Total)),
		TotalCostMaterials:            util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.TotalCost.Materials)),
		TotalCostWorks:                util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.TotalCost.Works)),
		TotalCostIndirectCosts:        util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.TotalCost.IndirectCosts)),
		TotalCostTotal:                util.ConvertNullFloat64ToNullString(util.NullableFloat64(posAPI.TotalCost.Total)), // Убедитесь, что это поле nullable в таблице
		DeviationFromBaselineCost:     util.ConvertNullFloat64ToNullString(util.NullableFloat64(nil)),                    // Заполните из posAPI, если есть
		IsChapter:                     posAPI.IsChapter,
		ChapterRefInProposal:          util.NullableString(posAPI.ChapterRef),
	}
}

// mapApiSummaryToDbParams преобразует API-модель строки итога в параметры для sqlc.
// Это чистая функция без побочных эффектов.
func mapApiSummaryToDbParams(
	proposalID int64,
	summaryKey string,
	sumLineAPI api_models.SummaryLine,
) db.UpsertProposalSummaryLineParams {
	return db.UpsertProposalSummaryLineParams{
		// Основные идентификаторы
		ProposalID: proposalID,
		SummaryKey: summaryKey,

		// Основные данные
		JobTitle: sumLineAPI.JobTitle,

		// Данные из TotalCost (для summary обычно используется TotalCost, а не UnitCost)
		MaterialsCost:     util.ConvertNullFloat64ToNullString(util.NullableFloat64(sumLineAPI.TotalCost.Materials)),
		WorksCost:         util.ConvertNullFloat64ToNullString(util.NullableFloat64(sumLineAPI.TotalCost.Works)),
		IndirectCostsCost: util.ConvertNullFloat64ToNullString(util.NullableFloat64(sumLineAPI.TotalCost.IndirectCosts)),
		TotalCost:         util.ConvertNullFloat64ToNullString(util.NullableFloat64(sumLineAPI.TotalCost.Total)),
	}
}
