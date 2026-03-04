// Package catalog предоставляет сервисный слой для работы с каталогом позиций.
//
// Этот пакет реализует бизнес-логику для управления справочником catalog_positions,
// который служит "золотым стандартом" для дедупликации и нормализации строительных работ.
//
// # Архитектурная роль
//
// CatalogService является центральным компонентом RAG-воркфлоу и отвечает за:
//
//  1. Event-Driven RAG-индексацию (Процесс 3А):
//     - Предоставление новых позиций (status='pending_indexing') для Google File Search
//     - Обновление статуса проиндексированных позиций на 'active'
//
//  2. Поиск дубликатов (Процесс 3Б):
//     - Пагинированное получение активных позиций для векторного поиска
//     - Регистрация предложений о слиянии дубликатов
//
// # Принцип работы с контекстом для RAG
//
// Для векторного поиска используется принцип "чистых описаний":
//   - Приоритет отдается оригинальному описанию (с естественными формами слов)
//   - Fallback на лемматизированную версию при отсутствии описания
//   - Метаданные (лот, тендер, подрядчик) НЕ включаются в контекст
//
// Обоснование: Google эмбеддинги обучены на естественном языке и лучше работают
// с семантически чистым контентом без избыточных метаданных.
//
// # Связь с другими компонентами
//
//   - importer.TenderImportService: создает записи со статусом 'pending_indexing'
//   - Python RAG-воркер: использует GET /catalog/unindexed и POST /catalog/indexed
//   - Python Duplicate Finder: использует GET /catalog/active и POST /merges/suggest
//
// # Используемые таблицы БД
//
//   - catalog_positions: основная таблица справочника (поля: id, standard_job_title, description, kind, status)
//   - suggested_merges: таблица предложений о слиянии дубликатов
package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/lib/pq"
	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/apierrors"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

// CatalogService управляет операциями с каталогом позиций.
//
// Этот сервис предоставляет высокоуровневые методы для работы с catalog_positions,
// скрывая детали работы с БД и реализуя бизнес-логику валидации и обработки данных.
type CatalogService struct {
	store  db.Store       // Интерфейс для доступа к БД (SQLC-сгенерированные запросы)
	logger logging.Logger // Логгер для отслеживания операций (интерфейс для тестируемости)
}

// NewCatalogService создает новый экземпляр CatalogService.
//
// Параметры:
//   - store: интерфейс db.Store для выполнения операций с БД
//   - logger: экземпляр логгера для записи событий и ошибок
//
// Возвращает готовый к использованию сервис каталога.
func NewCatalogService(store db.Store, logger logging.Logger) *CatalogService {
	return &CatalogService{
		store:  store,
		logger: logger,
	}
}

// buildContextString формирует строку контекста для RAG-индекса.
//
// Использует приоритет оригинального описания (с естественными формами слов)
// над лемматизированной версией для лучшей работы с Google эмбеддингами.
//
// Параметры:
//   - description: nullable поле с оригинальным описанием работы
//   - standardJobTitle: лемматизированное название (fallback)
//
// Возвращает очищенную от лишних пробелов строку контекста.
func buildContextString(description sql.NullString, standardJobTitle string) string {
	if description.Valid && strings.TrimSpace(description.String) != "" {
		return strings.TrimSpace(description.String)
	}
	return standardJobTitle
}

// GetUnindexedCatalogItems реализует GET /api/v1/catalog/unindexed.
//
// # Назначение
//
// Этот метод обслуживает Процесс 3А (Event-Driven RAG-индексация).
// Он предоставляет Python RAG-воркеру список новых позиций каталога,
// которые требуют индексации в Google File Search Store.
//
// # Workflow интеграция
//
//  1. Go Import Service создает новую запись catalog_positions со статусом 'pending_indexing'
//  2. Go возвращает флаг new_catalog_items_pending=true в ответе на POST /import-tender
//  3. Python-парсер вызывает Celery-задачу для индексации
//  4. Celery-воркер вызывает этот endpoint для получения очереди
//  5. После загрузки в Google, воркер вызывает POST /catalog/indexed
//
// # Формат контекста
//
// Для каждой позиции формируется "чистая" строка контекста:
//   - Приоритет: оригинальное описание (с падежами и естественными формами)
//   - Fallback: лемматизированная версия (standard_job_title)
//   - Метаданные (лот/тендер/подрядчик) НЕ включаются
//
// Обоснование: Google эмбеддинги лучше работают с семантически чистым,
// естественным языком без избыточных тегов и структурированных данных.
//
// # Параметры
//
//   - ctx: контекст выполнения запроса
//   - limit: максимальное количество позиций в ответе (для батчинга)
//
// # Возвращаемое значение
//
//   - []UnmatchedPositionResponse: массив позиций для индексации
//   - error: ошибка БД или nil при успехе
//
// # Важные детали
//
//   - Переиспользуется DTO UnmatchedPositionResponse (изначально для Процесса 2)
//   - PositionItemID содержит catalog_id (не position_item.id!)
//   - Возвращаются только записи с kind='POSITION' (исключаются HEADER, LOT_HEADER и т.д.)
func (s *CatalogService) GetUnindexedCatalogItems(
	ctx context.Context,
	limit int32,
) ([]api_models.UnmatchedPositionResponse, error) {

	// Validate parameters
	if limit <= 0 {
		s.logger.Warnf("Получен некорректный limit: %d (должен быть > 0)", limit)
		return nil, apierrors.NewValidationError("параметр limit должен быть положительным числом, получено: %d", limit)
	}

	// 1. Вызываем наш SQLC-запрос
	dbRows, err := s.store.ListCatalogPositionsForEmbedding(ctx, limit)
	if err != nil {
		s.logger.Errorf("Ошибка ListCatalogPositionsForEmbedding: %v", err)
		return nil, fmt.Errorf("ошибка БД: %w", err)
	}

	response := make([]api_models.UnmatchedPositionResponse, 0, len(dbRows))

	// 2. Формируем контекст для RAG-индекса
	// Принцип: Отправляем ПРОСТОЕ описание, без метаданных
	// (Google эмбеддинги лучше работают с естественным языком)
	for _, row := range dbRows {
		response = append(response, api_models.UnmatchedPositionResponse{
			// Python-воркеру нужен 'catalog_id'
			PositionItemID:     row.ID,
			JobTitleInProposal: row.StandardJobTitle,
			RichContextString:  buildContextString(row.Description, row.StandardJobTitle),
		})
	}

	s.logger.Infof("Найдено %d неиндексированных записей каталога для RAG", len(response))
	return response, nil
}

// MarkCatalogItemsAsActive реализует POST /api/v1/catalog/indexed.
//
// # Назначение
//
// Завершает Процесс 3А (Event-Driven RAG-индексация), устанавливая статус 'active'
// для позиций, которые были успешно загружены в Google File Search Store.
//
// # Workflow интеграция
//
//  1. Python RAG-воркер получает позиции через GET /catalog/unindexed
//  2. Загружает их в Google File Search (создает документы с эмбеддингами)
//  3. Вызывает этот endpoint с массивом успешно проиндексированных catalog_ids
//  4. Go обновляет статус: 'pending_indexing' -> 'active'
//
// После этого позиции становятся доступны для:
//   - Поиска дубликатов (Процесс 3Б через GET /catalog/active)
//   - RAG-матчинга новых позиций (Процесс 2)
//
// # Параметры
//
//   - ctx: контекст выполнения запроса
//   - catalogIDs: массив ID записей catalog_positions для обновления статуса
//
// # Возвращаемое значение
//
//   - error: ошибка БД или nil при успехе
//
// # Важные детали
//
//   - Пустой массив catalogIDs не считается ошибкой (просто логируется warning)
//   - Операция идемпотентна: повторный вызов с теми же ID безопасен
//   - Обновление атомарно (все ID в одной транзакции)
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

// SuggestMerge реализует POST /api/v1/merges/suggest.
//
// # Назначение
//
// Регистрирует предложение о слиянии дубликатов позиций каталога.
// Используется в Процессе 3Б (Поиск дубликатов) для накопления кандидатов
// на объединение перед финальным решением администратора.
//
// # Workflow интеграция
//
//  1. Python Duplicate Finder получает активные позиции через GET /catalog/active
//  2. Выполняет векторный поиск похожих позиций в Google File Search
//  3. Для каждой пары с высоким similarity_score вызывает этот endpoint
//  4. Предложения накапливаются в таблице suggested_merges
//  5. Администратор позже просматривает их через UI и принимает решение
//
// # Параметры
//
//   - ctx: контекст выполнения запроса
//   - req: структура с полями:
//   - MainPositionID: ID основной (канонической) позиции
//   - DuplicatePositionID: ID дубликата
//   - SimilarityScore: метрика схожести (0.0-1.0)
//
// # Возвращаемое значение
//
//   - error: ошибка БД или nil при успехе
//
// # Важные детали
//
//   - Защита от self-merge: позиция не может быть слита сама с собой
//   - Используется UPSERT: повторные предложения обновляют similarity_score
//   - Не выполняет фактическое слияние - только регистрирует предложение
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

// ListPendingMerges реализует GET /api/v1/admin/suggested_merges.
//
// # Назначение
//
// Возвращает список PENDING merge-предложений, сгруппированных по main_position_id.
// Каждая группа содержит «мастер-позицию» и массив её дубликатов.
//
// # Параметры
//
//   - ctx: контекст выполнения
//   - page: номер страницы (>= 1)
//   - pageSize: размер страницы (1–500)
//
// # Возвращаемое значение
//
//   - *api_models.ListSuggestedMergesResponse: группированный список
//   - error: ValidationError при некорректных параметрах, или ошибка БД
func (s *CatalogService) ListPendingMerges(
	ctx context.Context,
	page int32,
	pageSize int32,
) (*api_models.ListSuggestedMergesResponse, error) {

	if page < 1 {
		return nil, apierrors.NewValidationError("page должен быть >= 1")
	}
	if pageSize < 1 || pageSize > 500 {
		return nil, apierrors.NewValidationError("page_size должен быть от 1 до 500")
	}

	// Вычисляем offset в int64, чтобы избежать переполнения int32
	// при больших значениях page (например, page=2_000_000_000, pageSize=500).
	offset64 := (int64(page) - 1) * int64(pageSize)
	if offset64 > math.MaxInt32 {
		return nil, apierrors.NewValidationError("page слишком велик для данного page_size")
	}
	offset := int32(offset64)

	// Получаем общее количество PENDING merge-записей и уникальных групп для пагинации
	totalCount, err := s.store.CountPendingMerges(ctx)
	if err != nil {
		s.logger.Errorf("Ошибка CountPendingMerges: %v", err)
		return nil, fmt.Errorf("ошибка CountPendingMerges: %w", err)
	}

	totalGroups, err := s.store.CountPendingMergeGroups(ctx)
	if err != nil {
		s.logger.Errorf("Ошибка CountPendingMergeGroups: %v", err)
		return nil, fmt.Errorf("ошибка CountPendingMergeGroups: %w", err)
	}

	rows, err := s.store.ListPendingMerges(ctx, db.ListPendingMergesParams{
		Limit:  pageSize,
		Offset: offset,
	})
	if err != nil {
		s.logger.Errorf("Ошибка ListPendingMerges: %v", err)
		return nil, fmt.Errorf("ошибка ListPendingMerges: %w", err)
	}

	// Группируем по main_position_id, сохраняя порядок появления
	groupOrder := []int64{}
	groupMap := map[int64]*api_models.SuggestedMergeGroup{}

	for _, row := range rows {
		mainID := row.SuggestedMerge.MainPositionID

		grp, exists := groupMap[mainID]
		if !exists {
			grp = &api_models.SuggestedMergeGroup{
				MainPosition: catalogPositionToSummary(row.CatalogPosition),
			}
			groupMap[mainID] = grp
			groupOrder = append(groupOrder, mainID)
		}

		grp.Merges = append(grp.Merges, api_models.SuggestedMergeItem{
			MergeID:         row.SuggestedMerge.ID,
			SimilarityScore: row.SuggestedMerge.SimilarityScore,
			Duplicate:       catalogPositionToSummary(row.CatalogPosition_2),
			CreatedAt:       row.SuggestedMerge.CreatedAt,
		})
	}

	groups := make([]api_models.SuggestedMergeGroup, 0, len(groupOrder))
	for _, id := range groupOrder {
		groups = append(groups, *groupMap[id])
	}

	return &api_models.ListSuggestedMergesResponse{
		Groups:      groups,
		Total:       int(totalCount),
		TotalGroups: int(totalGroups),
	}, nil
}

// catalogPositionToSummary конвертирует db.CatalogPosition в краткую API-модель.
func catalogPositionToSummary(pos db.CatalogPosition) api_models.CatalogPositionSummary {
	var desc *string
	if pos.Description.Valid && strings.TrimSpace(pos.Description.String) != "" {
		s := pos.Description.String
		desc = &s
	}
	return api_models.CatalogPositionSummary{
		ID:               pos.ID,
		StandardJobTitle: pos.StandardJobTitle,
		Description:      desc,
		Kind:             pos.Kind,
		Status:           pos.Status,
	}
}

// RejectMerge реализует PATCH /api/v1/admin/merges/:id/reject.
//
// Атомарно переводит предложение о слиянии из PENDING в REJECTED.
// Использует RejectPendingMerge SQL с guard `AND status = 'PENDING'`
// для защиты от race condition (аналогично ExecuteMerge).
//
// Параметры:
//   - ctx: контекст выполнения запроса
//   - mergeID: ID записи в таблице suggested_merges
//   - rejectedBy: ID оператора, отклонившего предложение
//
// Возвращаемое значение:
//   - error: ValidationError при некорректных параметрах (пустой rejectedBy) или
//     если merge уже не в статусе PENDING, NotFoundError если запись не найдена,
//     или ошибка БД
func (s *CatalogService) RejectMerge(ctx context.Context, mergeID int64, rejectedBy string) error {
	logger := s.logger.WithField("method", "RejectMerge").WithField("merge_id", mergeID)

	if mergeID <= 0 {
		return apierrors.NewValidationError("mergeID должен быть положительным")
	}
	rejectedBy = strings.TrimSpace(rejectedBy)
	if rejectedBy == "" {
		return apierrors.NewValidationError("rejectedBy не может быть пустым")
	}

	// Атомарный UPDATE с guard status = 'PENDING' (защита от race condition).
	// Если merge не найден или статус != PENDING — возвращает sql.ErrNoRows.
	_, err := s.store.RejectPendingMerge(ctx, db.RejectPendingMergeParams{
		ResolvedBy: sql.NullString{String: rejectedBy, Valid: true},
		ID:         mergeID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Различаем "не найден" и "не в PENDING" для информативного ответа
			merge, getErr := s.store.GetSuggestedMergeByID(ctx, mergeID)
			if getErr != nil {
				if errors.Is(getErr, sql.ErrNoRows) {
					return apierrors.NewNotFoundError("suggested_merge с id=%d не найден", mergeID)
				}
				return fmt.Errorf("ошибка получения merge %d: %w", mergeID, getErr)
			}
			return apierrors.NewValidationError(
				"нельзя отклонить merge в статусе %s (ожидается PENDING)", merge.Status,
			)
		}
		return fmt.Errorf("ошибка RejectPendingMerge для merge %d: %w", mergeID, err)
	}

	logger.Infof("Merge %d отклонён пользователем %s", mergeID, rejectedBy)
	return nil
}

// ExecuteMerge реализует POST /api/v1/merges/:id/execute.
//
// # Назначение
//
// Выполняет фактическое слияние дубликата в мастер-позицию.
// Предложения в статусе PENDING или APPROVED могут быть выполнены (one-click merge).
//
// # Сценарии
//
//   - Сценарий 1 (Default Merge): newMainTitle == "" → B вливается в A.
//     A остаётся active. B получает merged_into_id = A, status = 'deprecated'.
//
//   - Сценарий 2 (Merge to New): newMainTitle != "" → Создаётся новая позиция C.
//     A и B получают merged_into_id = C, status = 'deprecated'.
//
// # Логика выполнения (целиком в транзакции)
//
//  1. Атомарно переводит suggested_merge из PENDING/APPROVED в EXECUTED
//  2. В зависимости от сценария: помечает дубликат(ы) как deprecated
//
// # Параметры
//
//   - ctx: контекст выполнения запроса
//   - mergeID: ID записи в таблице suggested_merges
//   - executedBy: ID оператора, выполнившего слияние
//   - newMainTitle: новое имя (пустая строка = Сценарий 1)
//
// # Возвращаемое значение
//
//   - *api_models.ExecuteMergeResponse: данные о выполненном слиянии
//   - error: ValidationError, NotFoundError или ошибка БД
func (s *CatalogService) ExecuteMerge(
	ctx context.Context,
	mergeID int64,
	executedBy string,
	newMainTitle string,
) (*api_models.ExecuteMergeResponse, error) {
	logger := s.logger.WithField("method", "ExecuteMerge").WithField("merge_id", mergeID)

	// Валидация: executedBy обязателен (гарантируется JWT-middleware, но проверяем для безопасности)
	if executedBy == "" {
		return nil, apierrors.NewValidationError("executedBy не может быть пустым")
	}

	// Определяем сценарий
	newMainTitle = strings.TrimSpace(newMainTitle)
	isRename := newMainTitle != ""

	// Вся логика внутри транзакции для защиты от race condition
	var merge db.SuggestedMerge
	var resultingPositionID int64
	var resultingPositionStatus string
	var deprecatedPositionIDs []int64
	var mergedPositionID int64 // B (дубликат) — deprecated в обоих сценариях
	var mergedStatus string
	var scenario string

	err := s.store.ExecTx(ctx, func(q *db.Queries) error {
		// 1. Атомарно переводим PENDING/APPROVED → EXECUTED (one-click merge)
		var txErr error
		merge, txErr = q.ExecuteMerge(ctx, db.ExecuteMergeParams{
			ResolvedBy: sql.NullString{String: executedBy, Valid: true},
			ID:         mergeID,
		})
		if txErr != nil {
			if errors.Is(txErr, sql.ErrNoRows) {
				// Нужно определить причину: не найдено или неверный статус
				existing, checkErr := q.GetSuggestedMergeByID(ctx, mergeID)
				if checkErr != nil {
					if errors.Is(checkErr, sql.ErrNoRows) {
						return apierrors.NewNotFoundError("предложение о слиянии с ID %d не найдено", mergeID)
					}
					return fmt.Errorf("ошибка GetSuggestedMergeByID: %w", checkErr)
				}
				return apierrors.NewValidationError(
					"слияние %d не может быть выполнено: текущий статус=%s (ожидается PENDING/APPROVED)",
					mergeID, existing.Status,
				)
			}
			return fmt.Errorf("ошибка ExecuteMerge: %w", txErr)
		}

		if !isRename {
			// ===== Сценарий 1: Default Merge (B → A) =====
			scenario = api_models.MergeScenarioDefault

			mergedPos, mergeErr := q.MergeCatalogPosition(ctx, db.MergeCatalogPositionParams{
				MasterID:    sql.NullInt64{Int64: merge.MainPositionID, Valid: true},
				DuplicateID: merge.DuplicatePositionID,
			})
			if mergeErr != nil {
				if errors.Is(mergeErr, sql.ErrNoRows) {
					return diagnoseMergeFailure(ctx, q, merge)
				}
				return fmt.Errorf("ошибка MergeCatalogPosition: %w", mergeErr)
			}

			resultingPositionID = merge.MainPositionID // A остаётся активной
			resultingPositionStatus = "active"         // WHERE clause гарантирует: master.status = 'active'
			deprecatedPositionIDs = []int64{mergedPos.ID}
			mergedPositionID = mergedPos.ID
			mergedStatus = mergedPos.Status
		} else {
			// ===== Сценарий 2: Merge to New (A,B → C) =====
			scenario = api_models.MergeScenarioMergeToNew

			// 2a. Создаём новую позицию C
			newPos, createErr := q.CreateSimpleCatalogPosition(ctx, newMainTitle)
			if createErr != nil {
				var pqErr *pq.Error
				if errors.As(createErr, &pqErr) && pqErr.Code == "23505" {
					return apierrors.NewValidationError(
						"позиция с названием %q уже существует в каталоге", newMainTitle,
					)
				}
				return fmt.Errorf("ошибка CreateSimpleCatalogPosition: %w", createErr)
			}
			resultingPositionID = newPos.ID

			// 2b. A → deprecated, merged_into_id = C
			mergedA, mergeAErr := q.SetPositionMerged(ctx, db.SetPositionMergedParams{
				TargetID:   sql.NullInt64{Int64: newPos.ID, Valid: true},
				PositionID: merge.MainPositionID,
			})
			if mergeAErr != nil {
				if errors.Is(mergeAErr, sql.ErrNoRows) {
					return apierrors.NewValidationError(
						"слияние невозможно: мастер-позиция %d уже deprecated или влита",
						merge.MainPositionID,
					)
				}
				return fmt.Errorf("ошибка SetPositionMerged (A=%d): %w", merge.MainPositionID, mergeAErr)
			}

			// 2c. B → deprecated, merged_into_id = C
			mergedB, mergeBErr := q.SetPositionMerged(ctx, db.SetPositionMergedParams{
				TargetID:   sql.NullInt64{Int64: newPos.ID, Valid: true},
				PositionID: merge.DuplicatePositionID,
			})
			if mergeBErr != nil {
				if errors.Is(mergeBErr, sql.ErrNoRows) {
					return apierrors.NewValidationError(
						"слияние невозможно: дубликат %d уже deprecated или влит",
						merge.DuplicatePositionID,
					)
				}
				return fmt.Errorf("ошибка SetPositionMerged (B=%d): %w", merge.DuplicatePositionID, mergeBErr)
			}

			resultingPositionStatus = newPos.Status // "pending_indexing" для C
			deprecatedPositionIDs = []int64{mergedA.ID, mergedB.ID}
			mergedPositionID = mergedB.ID
			mergedStatus = mergedB.Status
		}

		// Инвалидируем все связанные заявки (PENDING/APPROVED), где участвуют deprecated-позиции
		if err := invalidateActionableMerges(ctx, q, deprecatedPositionIDs); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		logger.Errorf("Ошибка выполнения слияния: %v", err)
		return nil, err
	}

	if isRename {
		logger.Infof("Merge-to-New: позиции %d и %d влиты в новую %d (%q)",
			merge.MainPositionID, merge.DuplicatePositionID, resultingPositionID, newMainTitle)
	} else {
		logger.Infof("Default Merge: позиция %d влита в %d",
			merge.DuplicatePositionID, merge.MainPositionID)
	}

	return &api_models.ExecuteMergeResponse{
		MergeID:                 mergeID,
		MainPositionID:          merge.MainPositionID,
		MergedPositionID:        mergedPositionID,
		ResultingPositionID:     resultingPositionID,
		ResultingPositionStatus: resultingPositionStatus,
		DeprecatedPositionIDs:   deprecatedPositionIDs,
		Scenario:                scenario,
		Status:                  mergedStatus,
		ResolvedAt:              merge.ResolvedAt.Time,
	}, nil
}

// ExecuteBatchMerge реализует POST /api/v1/admin/merges/execute-batch.
//
// # Назначение
//
// Выполняет групповое слияние дубликатов каталога в одной транзакции.
// Решает проблему связных компонентов — когда позиция одновременно является
// мастером в одних merge-записях и дубликатом в других.
//
// # Сценарии
//
//   - Сценарий 1 (Default Batch): NewMainTitle == "" → TargetPositionID остаётся active,
//     все остальные позиции deprecated. RenameTitle позволяет переименовать выжившую.
//
//   - Сценарий 2 (Batch Merge-to-New): NewMainTitle != "" → создаётся C,
//     все позиции из группы deprecated.
//
// # Параметры
//
//   - ctx: контекст выполнения запроса
//   - req: ExecuteBatchMergeRequest с merge ID, target, rename и new_main_title
//   - executedBy: ID оператора
//
// # Возвращаемое значение
//
//   - *api_models.ExecuteBatchMergeResponse: результат батча
//   - error: ValidationError, NotFoundError или ошибка БД
func (s *CatalogService) ExecuteBatchMerge(
	ctx context.Context,
	req api_models.ExecuteBatchMergeRequest,
	executedBy string,
) (*api_models.ExecuteBatchMergeResponse, error) {
	logger := s.logger.WithField("method", "ExecuteBatchMerge").
		WithField("merge_ids_count", len(req.MergeIDs))

	// === Валидация входных данных ===
	if executedBy == "" {
		return nil, apierrors.NewValidationError("executedBy не может быть пустым")
	}
	if len(req.MergeIDs) == 0 {
		return nil, apierrors.NewValidationError("merge_ids не может быть пустым")
	}

	// Проверка на дубликаты в merge_ids
	seen := make(map[int64]struct{}, len(req.MergeIDs))
	for _, id := range req.MergeIDs {
		if _, exists := seen[id]; exists {
			return nil, apierrors.NewValidationError("дубликат merge_id: %d", id)
		}
		seen[id] = struct{}{}
	}

	// Определяем сценарий
	newMainTitle := strings.TrimSpace(req.NewMainTitle)
	renameTitle := strings.TrimSpace(req.RenameTitle)
	isMergeToNew := newMainTitle != ""

	if !isMergeToNew && req.TargetPositionID == 0 {
		return nil, apierrors.NewValidationError(
			"target_position_id обязателен для Сценария 1 (без new_main_title)")
	}

	// === Транзакция ===
	var resultingPositionID int64
	var resultingPositionStatus string
	var deprecatedPositionIDs []int64
	var scenario string
	var resolvedAt sql.NullTime

	err := s.store.ExecTx(ctx, func(q *db.Queries) error {
		// 1. Bulk-переводим PENDING/APPROVED → EXECUTED
		executedMerges, txErr := q.ExecuteMergeBatch(ctx, db.ExecuteMergeBatchParams{
			ResolvedBy: sql.NullString{String: executedBy, Valid: true},
			Ids:        req.MergeIDs,
		})
		if txErr != nil {
			return fmt.Errorf("ошибка ExecuteMergeBatch: %w", txErr)
		}

		// Проверяем что ВСЕ merge_ids были обновлены
		if len(executedMerges) != len(req.MergeIDs) {
			// Определяем какие merge_ids не прошли
			executedSet := make(map[int64]struct{}, len(executedMerges))
			for _, m := range executedMerges {
				executedSet[m.ID] = struct{}{}
			}
			var failedIDs []int64
			for _, id := range req.MergeIDs {
				if _, ok := executedSet[id]; !ok {
					failedIDs = append(failedIDs, id)
				}
			}
			return apierrors.NewValidationError(
				"не удалось выполнить merge_ids %v: не найдены или имеют неверный статус",
				failedIDs,
			)
		}

		// Сохраняем resolved_at из первого merge
		resolvedAt = executedMerges[0].ResolvedAt

		// 2. Собираем уникальные position ID из всех merge-записей
		positionSet := make(map[int64]struct{})
		for _, m := range executedMerges {
			positionSet[m.MainPositionID] = struct{}{}
			positionSet[m.DuplicatePositionID] = struct{}{}
		}

		// Сортируем один раз до ветвления для детерминированного порядка UPDATE-ов
		// (стабильность тестов + отсутствие дедлоков при конкурентных batch-merge).
		sortedPositionIDs := make([]int64, 0, len(positionSet))
		for posID := range positionSet {
			sortedPositionIDs = append(sortedPositionIDs, posID)
		}
		slices.Sort(sortedPositionIDs)

		if !isMergeToNew {
			// ===== Сценарий 1: Default Batch Merge =====
			scenario = api_models.MergeScenarioDefault

			// Проверяем что target входит в собранные позиции
			if _, ok := positionSet[req.TargetPositionID]; !ok {
				return apierrors.NewValidationError(
					"target_position_id=%d не входит в позиции группы merge-записей",
					req.TargetPositionID,
				)
			}

			// Проверяем что target-позиция активна и не deprecated/merged
			targetPos, targetErr := q.GetCatalogPositionByID(ctx, req.TargetPositionID)
			if targetErr != nil {
				if errors.Is(targetErr, sql.ErrNoRows) {
					return apierrors.NewValidationError(
						"target_position_id=%d не найден в каталоге", req.TargetPositionID,
					)
				}
				return fmt.Errorf("ошибка проверки target_position_id=%d: %w", req.TargetPositionID, targetErr)
			}
			if targetPos.Status != "active" || targetPos.MergedIntoID.Valid {
				return apierrors.NewValidationError(
					"target_position_id=%d имеет невалидный статус %q (merged_into_id=%v)",
					req.TargetPositionID, targetPos.Status, targetPos.MergedIntoID,
				)
			}

			// Deprecate все позиции, кроме target
			for _, posID := range sortedPositionIDs {
				if posID == req.TargetPositionID {
					continue
				}
				_, mergeErr := q.SetPositionMerged(ctx, db.SetPositionMergedParams{
					TargetID:   sql.NullInt64{Int64: req.TargetPositionID, Valid: true},
					PositionID: posID,
				})
				if mergeErr != nil {
					if errors.Is(mergeErr, sql.ErrNoRows) {
						return apierrors.NewValidationError(
							"позиция %d уже deprecated или влита", posID,
						)
					}
					return fmt.Errorf("ошибка SetPositionMerged (pos=%d): %w", posID, mergeErr)
				}
				deprecatedPositionIDs = append(deprecatedPositionIDs, posID)
			}

			resultingPositionID = req.TargetPositionID

			// Опциональное переименование target
			if renameTitle != "" {
				_, renameErr := q.UpdateCatalogPositionDetails(ctx, db.UpdateCatalogPositionDetailsParams{
					StandardJobTitle: sql.NullString{String: renameTitle, Valid: true},
					Description:      sql.NullString{Valid: false},
					UnitID:           sql.NullInt64{Valid: false},
					ID:               req.TargetPositionID,
				})
				if renameErr != nil {
					if errors.Is(renameErr, sql.ErrNoRows) {
						// Если title не изменился — не ошибка (COALESCE + IS DISTINCT FROM → no-op)
						resultingPositionStatus = "active"
					} else {
						return fmt.Errorf("ошибка UpdateCatalogPositionDetails (target=%d): %w",
							req.TargetPositionID, renameErr)
					}
				} else {
					resultingPositionStatus = "pending_indexing" // rename → нужна переиндексация
				}
			} else {
				resultingPositionStatus = "active"
			}

		} else {
			// ===== Сценарий 2: Batch Merge-to-New =====
			scenario = api_models.MergeScenarioMergeToNew

			// Создаём новую позицию C
			newPos, createErr := q.CreateSimpleCatalogPosition(ctx, newMainTitle)
			if createErr != nil {
				var pqErr *pq.Error
				if errors.As(createErr, &pqErr) && pqErr.Code == "23505" {
					return apierrors.NewValidationError(
						"позиция с названием %q уже существует в каталоге", newMainTitle,
					)
				}
				return fmt.Errorf("ошибка CreateSimpleCatalogPosition: %w", createErr)
			}
			resultingPositionID = newPos.ID
			resultingPositionStatus = newPos.Status // "pending_indexing"

			// Deprecate все позиции из группы
			for _, posID := range sortedPositionIDs {
				_, mergeErr := q.SetPositionMerged(ctx, db.SetPositionMergedParams{
					TargetID:   sql.NullInt64{Int64: newPos.ID, Valid: true},
					PositionID: posID,
				})
				if mergeErr != nil {
					if errors.Is(mergeErr, sql.ErrNoRows) {
						return apierrors.NewValidationError(
							"позиция %d уже deprecated или влита", posID,
						)
					}
					return fmt.Errorf("ошибка SetPositionMerged (pos=%d): %w", posID, mergeErr)
				}
				deprecatedPositionIDs = append(deprecatedPositionIDs, posID)
			}
		}

		// Инвалидируем все связанные заявки (PENDING/APPROVED), где участвуют deprecated-позиции
		if err := invalidateActionableMerges(ctx, q, deprecatedPositionIDs); err != nil {
			return err
		}

		// Сортируем для детерминированного ответа API
		slices.Sort(deprecatedPositionIDs)

		return nil
	})

	if err != nil {
		logger.Errorf("Ошибка батч-слияния: %v", err)
		return nil, err
	}

	logger.Infof("Batch merge выполнен: scenario=%s, resulting=%d, deprecated=%v",
		scenario, resultingPositionID, deprecatedPositionIDs)

	return &api_models.ExecuteBatchMergeResponse{
		MergeIDs:                req.MergeIDs,
		ResultingPositionID:     resultingPositionID,
		ResultingPositionStatus: resultingPositionStatus,
		DeprecatedPositionIDs:   deprecatedPositionIDs,
		Scenario:                scenario,
		ResolvedAt:              resolvedAt.Time,
	}, nil
}

// invalidateActionableMerges отклоняет все незавершённые (PENDING/APPROVED) заявки
// на слияние, где участвует хотя бы одна из deprecated-позиций ("мёртвые души").
// Вызывается внутри транзакций ExecuteMerge и ExecuteBatchMerge.
func invalidateActionableMerges(ctx context.Context, q *db.Queries, deprecatedPositionIDs []int64) error {
	if len(deprecatedPositionIDs) == 0 {
		return nil
	}
	if err := q.InvalidateRelatedActionableMerges(ctx, deprecatedPositionIDs); err != nil {
		return fmt.Errorf("ошибка InvalidateRelatedActionableMerges: %w", err)
	}
	return nil
}

// diagnoseMergeFailure определяет конкретную причину отказа MergeCatalogPosition
// при получении sql.ErrNoRows. Возвращает информативную ValidationError
// или пробрасывает реальную ошибку БД.
func diagnoseMergeFailure(
	ctx context.Context,
	q *db.Queries,
	merge db.SuggestedMerge,
) error {
	dup, dupErr := q.GetCatalogPositionByID(ctx, merge.DuplicatePositionID)
	if dupErr != nil && !errors.Is(dupErr, sql.ErrNoRows) {
		// Реальная ошибка БД — пробрасываем
		return fmt.Errorf("ошибка GetCatalogPositionByID (dup=%d): %w", merge.DuplicatePositionID, dupErr)
	}
	if dupErr == nil && (dup.MergedIntoID.Valid || dup.Status == "deprecated") {
		return apierrors.NewValidationError(
			"слияние невозможно: дубликат %d уже влит в другую позицию",
			merge.DuplicatePositionID,
		)
	}

	master, masterErr := q.GetCatalogPositionByID(ctx, merge.MainPositionID)
	if masterErr != nil && !errors.Is(masterErr, sql.ErrNoRows) {
		// Реальная ошибка БД — пробрасываем
		return fmt.Errorf("ошибка GetCatalogPositionByID (master=%d): %w", merge.MainPositionID, masterErr)
	}
	if masterErr == nil && (master.MergedIntoID.Valid || master.Status != "active") {
		return apierrors.NewValidationError(
			"слияние невозможно: мастер-позиция %d неактивна или влита в другую",
			merge.MainPositionID,
		)
	}

	return apierrors.NewValidationError(
		"слияние невозможно: дубликат %d или мастер-позиция %d не удовлетворяют условиям",
		merge.DuplicatePositionID, merge.MainPositionID,
	)
}

// GetAllActiveCatalogItems реализует GET /api/v1/catalog/active.
//
// # Назначение
//
// Предоставляет пагинированный список активных позиций каталога для Процесса 3Б
// (Поиск дубликатов). Позиции со статусом 'active' уже проиндексированы в
// Google File Search и готовы для векторного поиска похожих записей.
//
// # Workflow интеграция
//
//  1. Python Duplicate Finder запрашивает активные позиции батчами (например, по 100 шт)
//  2. Для каждой позиции выполняет векторный поиск в Google File Search
//  3. Если найдены похожие позиции с высоким similarity_score (>0.85),
//     вызывает POST /merges/suggest для регистрации потенциального дубликата
//  4. Переходит к следующему батчу (увеличивает offset)
//
// # Формат контекста
//
// Использует ТУ ЖЕ логику формирования контекста, что и GetUnindexedCatalogItems:
//   - Приоритет: оригинальное описание (естественный язык)
//   - Fallback: лемматизированная версия
//   - Без метаданных (лот/тендер/подрядчик)
//
// Это обеспечивает консистентность между:
//   - Данными, загруженными в Google (через /catalog/unindexed)
//   - Данными, используемыми для поиска дубликатов (через /catalog/active)
//
// # Параметры
//
//   - ctx: контекст выполнения запроса
//   - limit: количество записей на страницу (должно быть > 0)
//   - offset: смещение от начала выборки (должно быть >= 0)
//
// # Возвращаемое значение
//
//   - []UnmatchedPositionResponse: массив активных позиций
//   - error: ValidationError при некорректных параметрах, ошибка БД или nil при успехе
//
// # Важные детали
//
//   - Валидация параметров: limit > 0, offset >= 0
//   - Переиспользуется DTO UnmatchedPositionResponse
//   - PositionItemID содержит catalog_id
//   - Возвращаются только записи с kind='POSITION' и status='active'
//   - Сортировка по id для детерминированной пагинации
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

	// 2. Формируем контекст (используем ту же логику, что и в GetUnindexedCatalogItems)
	for _, row := range dbRows {
		response = append(response, api_models.UnmatchedPositionResponse{
			PositionItemID:     row.CatalogID, // 👈 Передаем ID каталога
			JobTitleInProposal: row.StandardJobTitle,
			RichContextString:  buildContextString(row.Description, row.StandardJobTitle), // <-- Чистая строка для чистого поиска
		})
	}

	s.logger.Infof("Найдено %d АКТИВНЫХ записей каталога для поиска дубликатов (Limit: %d, Offset: %d)",
		len(response), limit, offset)
	return response, nil
}
