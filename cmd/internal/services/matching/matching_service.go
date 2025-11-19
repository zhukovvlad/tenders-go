package matching

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/apierrors"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

// MatchingService управляет операциями матчинга позиций
type MatchingService struct {
	store  db.Store
	logger *logging.Logger
}

// NewMatchingService создает новый экземпляр MatchingService
func NewMatchingService(store db.Store, logger *logging.Logger) *MatchingService {
	return &MatchingService{
		store:  store,
		logger: logger,
	}
}

const (
	// MaxUnmatchedPositionsLimit определяет максимальное количество позиций,
	// которое можно запросить за один вызов GetUnmatchedPositions.
	// Это ограничение предотвращает чрезмерную нагрузку на БД и память.
	MaxUnmatchedPositionsLimit = 1000
)

// GetUnmatchedPositions (Версия 3: БЕЗ lot_title)
// Возвращает позиции, для которых еще нет соответствия в catalog_positions.
// `rich_context_string` теперь состоит из:
//   - `job_title_normalized` (то есть лемма самой позиции).
//   - "Хлебных крошек" (breadcrumbs) — иерархии заголовков (HEADER и LOT_HEADER),
//     в которых эта позиция "вложена".
//
// Это позволит LLM/векторной модели находить семантически близкие работы,
// опираясь на контекст вложенности.
func (s *MatchingService) GetUnmatchedPositions(
	ctx context.Context,
	limit int32,
) ([]api_models.UnmatchedPositionResponse, error) {

	// Валидация параметра limit
	if limit <= 0 {
		s.logger.Warnf("Получен некорректный limit: %d (должен быть > 0)", limit)
		return nil, apierrors.NewValidationError("параметр limit должен быть положительным числом, получено: %d", limit)
	}

	// Ограничиваем максимальное значение
	if limit > MaxUnmatchedPositionsLimit {
		s.logger.Infof("Запрошено limit=%d, ограничиваем до MaxUnmatchedPositionsLimit=%d",
			limit, MaxUnmatchedPositionsLimit)
		limit = MaxUnmatchedPositionsLimit
	}

	// 1. Вызываем наш НОВЫЙ рекурсивный SQLC-запрос
	// (sqlc сгенерирует row.FullParentPath, но НЕ row.LotTitle)
	dbRows, err := s.store.GetUnmatchedPositions(ctx, limit)
	if err != nil {
		s.logger.Errorf("Ошибка GetUnmatchedPositions: %v", err)
		return nil, fmt.Errorf("ошибка БД: %w", err)
	}

	response := make([]api_models.UnmatchedPositionResponse, 0, len(dbRows))

	for _, row := range dbRows {
		var context string

		// 2. Собираем "богатую" строку (БЕЗ ЛОТА)
		// (SQL вернет '' (пустую строку), если разделов нет, благодаря COALESCE)
		if row.FullParentPath != "" {
			// Если есть "хлебные крошки"
			context = fmt.Sprintf("Раздел: %s | Позиция: %s",
				row.FullParentPath,
				row.JobTitleInProposal,
			)
		} else {
			// Если у позиции нет родительского раздела (лежит в корне)
			// Используем только ее собственный заголовок.
			context = fmt.Sprintf("Позиция: %s", row.JobTitleInProposal)
		}

		// Преобразуем sql.NullInt64 в *int64 для JSON
		var draftCatalogID *int64
		if row.DraftCatalogID.Valid {
			draftCatalogID = &row.DraftCatalogID.Int64
		}

		// Преобразуем sql.NullString в string (по умолчанию пустая строка)
		standardJobTitle := ""
		if row.StandardJobTitle.Valid {
			standardJobTitle = row.StandardJobTitle.String
		}

		response = append(response, api_models.UnmatchedPositionResponse{
			PositionItemID:     row.PositionItemID,
			JobTitleInProposal: row.JobTitleInProposal,
			RichContextString:  context,
			DraftCatalogID:     draftCatalogID,
			StandardJobTitle:   standardJobTitle,
		})
	}

	s.logger.Infof("Найдено %d не сопоставленных позиций для RAG-воркера", len(response))
	return response, nil
}

// MatchPosition обрабатывает POST /api/v1/positions/match
func (s *MatchingService) MatchPosition(
	ctx context.Context,
	req api_models.MatchPositionRequest,
) error {

	// Устанавливаем версию нормы по умолчанию, если Python ее не прислал
	normVersion := req.NormVersion
	if normVersion == 0 {
		normVersion = 1 // Версия по умолчанию
	}

	// Выполняем оба обновления в одной транзакции
	txErr := s.store.ExecTx(ctx, func(qtx *db.Queries) error {

		// 1. Обновляем position_items, "закрывая" NULL
		//
		err := qtx.SetCatalogPositionID(ctx, db.SetCatalogPositionIDParams{
			CatalogPositionID: sql.NullInt64{Int64: req.CatalogPositionID, Valid: true},
			ID:                req.PositionItemID,
		})
		if err != nil {
			s.logger.Errorf("MatchPosition: Ошибка SetCatalogPositionID: %v", err)
			return fmt.Errorf("ошибка обновления position_items: %w", err)
		}

		// 2. Обновляем matching_cache для будущих импортов
		// (Ищем "сырой" job_title, чтобы сохранить в кэш для отладки)
		posItem, err := qtx.GetPositionItemByID(ctx, req.PositionItemID)
		if err != nil {
			s.logger.Warnf("MatchPosition: не удалось найти %d для лога кэша: %v", req.PositionItemID, err)
			// Инициализируем пустой posItem для безопасного использования ниже
			posItem = db.PositionItem{}
		}

		// Устанавливаем TTL для кэша (например, 30 дней)
		expiresAt := sql.NullTime{
			Time:  time.Now().AddDate(0, 0, 30), // 30 дней от сейчас
			Valid: true,
		}

		// Определяем jobTitleText: используем реальное значение, если posItem загружен успешно
		jobTitleText := sql.NullString{String: "", Valid: false}
		if posItem.JobTitleInProposal != "" {
			jobTitleText = sql.NullString{String: posItem.JobTitleInProposal, Valid: true}
		}

		//
		err = qtx.UpsertMatchingCache(ctx, db.UpsertMatchingCacheParams{
			JobTitleHash:      req.Hash,
			NormVersion:       int16(normVersion), // (Убедитесь, что тип int16 в sqlc)
			JobTitleText:      jobTitleText,
			CatalogPositionID: req.CatalogPositionID,
			ExpiresAt:         expiresAt, // 👈 (ДОБАВЛЕНО ПОЛЕ)
		})
		if err != nil {
			s.logger.Errorf("MatchPosition: Ошибка UpsertMatchingCache: %v", err)
			return fmt.Errorf("ошибка обновления matching_cache: %w", err)
		}

		return nil // Commit транзакции
	})

	if txErr != nil {
		return txErr // Возвращаем ошибку транзакции
	}

	s.logger.Infof("Успешно сопоставлена позиция %d -> %d (hash: %s)",
		req.PositionItemID, req.CatalogPositionID, req.Hash)
	return nil
}
