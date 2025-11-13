package catalog

import (
	"context"
	"fmt"

	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/apierrors"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

// CatalogService управляет операциями с каталогом позиций
type CatalogService struct {
	store  db.Store
	logger *logging.Logger
}

// NewCatalogService создает новый экземпляр CatalogService
func NewCatalogService(store db.Store, logger *logging.Logger) *CatalogService {
	return &CatalogService{
		store:  store,
		logger: logger,
	}
}

// GetUnindexedCatalogItems реализует GET /api/v1/catalog/unindexed
func (s *CatalogService) GetUnindexedCatalogItems(
	ctx context.Context,
	limit int32,
) ([]api_models.UnmatchedPositionResponse, error) {
	// (Мы переиспользуем DTO UnmatchedPositionResponse)

	// 1. Вызываем наш SQLC-запрос
	dbRows, err := s.store.GetUnindexedCatalogItems(ctx, limit)
	if err != nil {
		s.logger.Errorf("Ошибка GetUnindexedCatalogItems: %v", err)
		return nil, fmt.Errorf("ошибка БД: %w", err)
	}

	response := make([]api_models.UnmatchedPositionResponse, 0, len(dbRows))

	// 2. "Обогащаем" данные для RAG-индекса
	for _, row := range dbRows {

		// 3. Собираем "богатую" строку для ИНДЕКСА
		// (Индекс НЕ содержит "хлебных крошек",
		// он содержит только суть самой работы)
		description := ""
		if row.Description.Valid {
			description = row.Description.String
		}
		context := fmt.Sprintf("Работа: %s | Описание: %s",
			row.StandardJobTitle, // Лемма
			description,          // "Сырое" название
		)

		response = append(response, api_models.UnmatchedPositionResponse{
			// Python-воркеру нужен 'catalog_id'
			PositionItemID:     row.CatalogID, // 👈 Передаем ID каталога
			JobTitleInProposal: row.StandardJobTitle,
			RichContextString:  context,
		})
	}

	s.logger.Infof("Найдено %d неиндексированных записей каталога для RAG", len(response))
	return response, nil
}

// MarkCatalogItemsAsActive реализует POST /api/v1/catalog/indexed
func (s *CatalogService) MarkCatalogItemsAsActive(
	ctx context.Context,
	catalogIDs []int64,
) error {

	if len(catalogIDs) == 0 {
		s.logger.Warn("MarkCatalogItemsAsActive: получен пустой список ID, действие не требуется.")
		return nil
	}

	// 1. Вызываем наш SQLC-запрос
	err := s.store.SetCatalogStatusActive(ctx, catalogIDs)

	if err != nil {
		s.logger.Errorf("Ошибка MarkCatalogItemsAsActive: %v", err)
		return fmt.Errorf("ошибка БД: %w", err)
	}

	s.logger.Infof("Установлен статус 'active' для %d записей каталога", len(catalogIDs))
	return nil
}

// SuggestMerge реализует POST /api/v1/merges/suggest
func (s *CatalogService) SuggestMerge(
	ctx context.Context,
	req api_models.SuggestMergeRequest,
) error {

	// Защита: не предлагать слияние позиции с самой собой
	if req.MainPositionID == req.DuplicatePositionID {
		s.logger.Warnf("Попытка предложить слияние позиции %d с самой собой. Пропущено.", req.MainPositionID)
		return nil // Не ошибка, просто пропускаем
	}

	// 1. Вызываем наш SQLC-запрос
	err := s.store.UpsertSuggestedMerge(ctx, db.UpsertSuggestedMergeParams{
		MainPositionID:      req.MainPositionID,
		DuplicatePositionID: req.DuplicatePositionID,
		SimilarityScore:     float32(req.SimilarityScore),
	})

	if err != nil {
		s.logger.Errorf("Ошибка UpsertSuggestedMerge: %v", err)
		return fmt.Errorf("ошибка БД при создании предложения о слиянии: %w", err)
	}

	s.logger.Infof("Успешно предложено/обновлено слияние: %d -> %d (Score: %.2f)",
		req.DuplicatePositionID, req.MainPositionID, req.SimilarityScore)
	return nil
}

// GetAllActiveCatalogItems реализует GET /api/v1/catalog/active (с пагинацией)
// Он используется "Процессом 3 (Часть Б)" для поиска дубликатов.
func (s *CatalogService) GetAllActiveCatalogItems(
	ctx context.Context,
	limit int32,
	offset int32,
) ([]api_models.UnmatchedPositionResponse, error) {

	// Validate parameters
	if limit <= 0 {
		s.logger.Warnf("Получен некорректный limit: %d (должен быть > 0)", limit)
		return nil, apierrors.NewValidationError("параметр limit должен быть положительным числом, получено: %d", limit)
	}
	if offset < 0 {
		s.logger.Warnf("Получен некорректный offset: %d (должен быть >= 0)", offset)
		return nil, apierrors.NewValidationError("параметр offset не может быть отрицательным, получено: %d", offset)
	}

	// 1. Вызываем наш обновленный SQLC-запрос
	dbRows, err := s.store.GetActiveCatalogItems(ctx, db.GetActiveCatalogItemsParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		s.logger.Errorf("Ошибка GetActiveCatalogItems: %v", err)
		return nil, fmt.Errorf("ошибка БД: %w", err)
	}

	response := make([]api_models.UnmatchedPositionResponse, 0, len(dbRows))

	// 2. "Обогащаем" данные (точно так же, как для индексации)
	for _, row := range dbRows {

		// 3. Собираем "богатую" строку (context_string)
		description := ""
		if row.Description.Valid {
			description = row.Description.String
		}
		context := fmt.Sprintf("Работа: %s | Описание: %s",
			row.StandardJobTitle, // Лемма
			description,          // "Сырое" название
		)

		response = append(response, api_models.UnmatchedPositionResponse{
			PositionItemID:     row.CatalogID, // 👈 Передаем ID каталога
			JobTitleInProposal: row.StandardJobTitle,
			RichContextString:  context,
		})
	}

	s.logger.Infof("Найдено %d АКТИВНЫХ записей каталога для поиска дубликатов (Limit: %d, Offset: %d)",
		len(response), limit, offset)
	return response, nil
}
