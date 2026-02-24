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
	"fmt"
	"strings"

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

// ExecuteMerge реализует POST /api/v1/merges/:id/execute.
//
// # Назначение
//
// Выполняет фактическое слияние дубликата в мастер-позицию.
// Только одобренные (APPROVED) предложения могут быть выполнены.
//
// # Логика выполнения (целиком в транзакции)
//
//  1. Атомарно переводит suggested_merge из APPROVED в EXECUTED (защита от race condition)
//  2. Помечает дубликат: merged_into_id = master, status = 'deprecated'
//
// # Параметры
//
//   - ctx: контекст выполнения запроса
//   - mergeID: ID записи в таблице suggested_merges
//   - executedBy: ID оператора, выполнившего слияние
//
// # Возвращаемое значение
//
//   - *api_models.ExecuteMergeResponse: данные о выполненном слиянии
//   - error: ValidationError, NotFoundError или ошибка БД
func (s *CatalogService) ExecuteMerge(
	ctx context.Context,
	mergeID int64,
	executedBy string,
) (*api_models.ExecuteMergeResponse, error) {
	logger := s.logger.WithField("method", "ExecuteMerge").WithField("merge_id", mergeID)

	// Вся логика внутри транзакции для защиты от race condition
	var mergedPos db.CatalogPosition
	var merge db.SuggestedMerge

	err := s.store.ExecTx(ctx, func(q *db.Queries) error {
		// 1. Атомарно переводим APPROVED → EXECUTED
		// Если статус != APPROVED, UPDATE не затронет строк → sql.ErrNoRows
		var txErr error
		merge, txErr = q.ExecuteApprovedMerge(ctx, db.ExecuteApprovedMergeParams{
			ExecutedBy: sql.NullString{String: executedBy, Valid: executedBy != ""},
			ID:         mergeID,
		})
		if txErr != nil {
			if txErr == sql.ErrNoRows {
				// Нужно определить причину: не найдено или неверный статус
				_, checkErr := q.GetSuggestedMergeByID(ctx, mergeID)
				if checkErr == sql.ErrNoRows {
					return apierrors.NewNotFoundError("предложение о слиянии с ID %d не найдено", mergeID)
				}
				// Запись существует, но статус != APPROVED
				return apierrors.NewValidationError(
					"слияние %d не может быть выполнено: статус не APPROVED (возможно, уже выполнено или отклонено)",
					mergeID,
				)
			}
			return fmt.Errorf("ошибка ExecuteApprovedMerge: %w", txErr)
		}

		// 2. Помечаем дубликат: merged_into_id = master, status = 'deprecated'
		mergedPos, txErr = q.MergeCatalogPosition(ctx, db.MergeCatalogPositionParams{
			MasterID:    sql.NullInt64{Int64: merge.MainPositionID, Valid: true},
			DuplicateID: merge.DuplicatePositionID,
		})
		if txErr != nil {
			if txErr == sql.ErrNoRows {
				return apierrors.NewValidationError(
					"позиция %d уже была влита ранее или не существует",
					merge.DuplicatePositionID,
				)
			}
			return fmt.Errorf("ошибка MergeCatalogPosition: %w", txErr)
		}

		return nil
	})

	if err != nil {
		logger.Errorf("Ошибка выполнения слияния: %v", err)
		return nil, err
	}

	logger.Infof("Слияние выполнено: позиция %d влита в %d",
		merge.DuplicatePositionID, merge.MainPositionID)

	return &api_models.ExecuteMergeResponse{
		MergeID:          mergeID,
		MainPositionID:   merge.MainPositionID,
		MergedPositionID: mergedPos.ID,
	}, nil
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
