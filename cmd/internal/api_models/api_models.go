// Package api_models содержит структуры и методы валидации, используемые для представления
// полной информации о тендере, его лотах, предложениях подрядчиков и связанных данных.
package api_models

import (
	"fmt"
	"strings"
	"time"
)

// FullTenderData описывает полную структуру тендера, включая его метаданные,
// информацию об исполнителе и все лоты с предложениями подрядчиков.
type FullTenderData struct {
	TenderID      string         `json:"tender_id"`      // Уникальный идентификатор тендера
	TenderTitle   string         `json:"tender_title"`   // Название тендера
	TenderObject  string         `json:"tender_object"`  // Объект тендера (например, строительство, реконструкция и т.д.)
	TenderAddress string         `json:"tender_address"` // Адрес объекта
	ExecutorData  Executor       `json:"executor"`       // Данные об исполнителе, составившем тендер
	LotsData      map[string]Lot `json:"lots"`           // Список лотов тендера, где ключ — идентификатор лота
}

// Executor представляет информацию об исполнителе тендера.
type Executor struct {
	ExecutorName  string `json:"executor_name"`  // Имя исполнителя
	ExecutorPhone string `json:"executor_phone"` // Контактный телефон
	ExecutorDate  string `json:"executor_date"`  // Дата составления
}

// Lot описывает отдельный лот тендера, включая предложения подрядчиков.
type Lot struct {
	LotTitle         string                               `json:"lot_title"`         // Название лота
	ProposalData     map[string]ContractorProposalDetails `json:"proposals"`         // Предложения от подрядчиков
	BaseLineProposal ContractorProposalDetails            `json:"baseline_proposal"` // Базовое (ориентировочное) предложение от организатора
}

// ContractorProposalDetails содержит данные одного предложения от подрядчика.
type ContractorProposalDetails struct {
	Title                string                   `json:"title"`                     // Название подрядчика
	Inn                  string                   `json:"inn"`                       // ИНН
	Address              string                   `json:"address"`                   // Юридический адрес
	Accreditation        string                   `json:"accreditation"`             // Статус аккредитации
	ContractorCoordinate string                   `json:"contractor_coordinate"`     // Внутренние координаты для визуализации таблицы
	ContractorWidth      int                      `json:"contractor_width"`          // Ширина блока подрядчика
	ContractorHeight     int                      `json:"contractor_height"`         // Высота блока подрядчика
	ContractorItems      ContractorItemsContainer `json:"contractor_items"`          // Позиции и итоги предложения
	AdditionalInfo       map[string]*string       `json:"additional_info,omitempty"` // Дополнительная информация (например, сроки, условия)
}

// ContractorItemsContainer группирует позиции и сводные строки предложения.
type ContractorItemsContainer struct {
	Positions map[string]PositionItem `json:"positions"` // Позиции работ/материалов подрядчика
	Summary   map[string]SummaryLine  `json:"summary"`   // Итоговые строки по разделам
}

// PositionItem представляет одну позицию из предложения подрядчика.
type PositionItem struct {
	Number                        string   `json:"number"`                                      // Порядковый номер
	ChapterNumber                 *string  `json:"chapter_number,omitempty"`                    // Номер главы (если применимо)
	ArticleSMR                    *string  `json:"article_smr,omitempty"`                       // Артикул СМР
	JobTitle                      string   `json:"job_title"`                                   // Название работы
	CommentOrganizer              *string  `json:"comment_organizer,omitempty"`                 // Комментарий организатора
	Unit                          *string  `json:"unit,omitempty"`                              // Единица измерения
	Quantity                      *float64 `json:"quantity,omitempty"`                          // Количество по ТЗ организатора
	SuggestedQuantity             *float64 `json:"suggested_quantity,omitempty"`                // Предложенное количество от подрядчика
	UnitCost                      Cost     `json:"unit_cost"`                                   // Стоимость за единицу
	TotalCost                     Cost     `json:"total_cost"`                                  // Общая стоимость
	TotalCostForOrganizerQuantity *float64 `json:"total_cost_for_organizer_quantity,omitempty"` // Стоимость за объём по ТЗ, но по ценам подрядчика
	CommentContractor             *string  `json:"comment_contractor,omitempty"`                // Комментарий подрядчика
	JobTitleNormalized            *string  `json:"job_title_normalized,omitempty"`              // Нормализованное название работы
	IsChapter                     bool     `json:"is_chapter"`                                  // Является ли это заголовком главы
	ChapterRef                    *string  `json:"chapter_ref,omitempty"`                       // Ссылка на главу, если применимо
}

// Cost представляет разбивку стоимости по компонентам.
type Cost struct {
	Materials     *float64 `json:"materials"`      // Стоимость материалов
	Works         *float64 `json:"works"`          // Стоимость работ
	IndirectCosts *float64 `json:"indirect_costs"` // Накладные расходы
	Total         *float64 `json:"total"`          // Общая стоимость
}

// SummaryLine описывает итог по группе работ/разделу.
type SummaryLine struct {
	JobTitle               string   `json:"job_title"`                              // Заголовок итога
	SuggestedQuantity      *float64 `json:"suggested_quantity"`                     // Объём, предложенный подрядчиком
	UnitCost               Cost     `json:"unit_cost"`                              // Цена за единицу
	TotalCost              Cost     `json:"total_cost"`                             // Общая стоимость
	OrganizierQuantityCost *float64 `json:"total_cost_for_organizer_quantity"`      // Стоимость по исходному объёму
	CommentContractor      *string  `json:"comment_contractor,omitempty"`           // Комментарий подрядчика
	Deviation              *float64 `json:"deviation_from_baseline_cost,omitempty"` // Отклонение от базовой стоимости
}

// Validate проверяет корректность данных предложения подрядчика.
// В случае ошибки возвращает подробное описание проблемы.
// Аргумент isBaseline указывает, является ли это базовым предложением.
func (cpd *ContractorProposalDetails) Validate(isBaseline bool) error {
	if strings.TrimSpace(cpd.Title) == "" {
		return fmt.Errorf("название подрядчика (title) не может быть пустым")
	}
	if !isBaseline && strings.TrimSpace(cpd.Inn) == "" {
		return fmt.Errorf("ИНН подрядчика (inn) не может быть пустым для '%s'", cpd.Title)
	}
	if !isBaseline && strings.TrimSpace(cpd.Address) == "" {
		return fmt.Errorf("адрес подрядчика (address) не может быть пустым")
	}
	if !isBaseline && cpd.ContractorCoordinate == "" {
		return fmt.Errorf("координаты подрядчика (contractor_coordinate) не могут быть пустыми")
	}
	if !isBaseline && cpd.ContractorWidth <= 0 {
		return fmt.Errorf("ширина подрядчика (contractor_width) должна быть положительной")
	}
	if !isBaseline && cpd.ContractorHeight <= 0 {
		return fmt.Errorf("высота подрядчика (contractor_height) должна быть положительной")
	}
	if !isBaseline && len(cpd.ContractorItems.Positions) == 0 {
		return fmt.Errorf("необходимо указать хотя бы одну позицию")
	}
	return nil
}

// Validate проверяет корректность данных лота, включая базовое и подрядные предложения.
func (l *Lot) Validate() error {
	if strings.TrimSpace(l.LotTitle) == "" {
		return fmt.Errorf("название лота (lot_title) не может быть пустым")
	}
	if err := l.BaseLineProposal.Validate(true); err != nil {
		return fmt.Errorf("ошибка в базовом предложении лота '%s': %w", l.LotTitle, err)
	}
	for key, proposal := range l.ProposalData {
		if err := proposal.Validate(false); err != nil {
			return fmt.Errorf("ошибка в предложении '%s' лота '%s': %w", key, l.LotTitle, err)
		}
	}
	return nil
}

// Validate проверяет корректность данных исполнителя.
func (e *Executor) Validate() error {
	if strings.TrimSpace(e.ExecutorName) == "" {
		return fmt.Errorf("имя исполнителя (executor_name) не может быть пустым")
	}
	if strings.TrimSpace(e.ExecutorPhone) == "" {
		return fmt.Errorf("телефон исполнителя (executor_phone) не может быть пустым")
	}
	return nil
}

// Validate проверяет полную структуру тендера, включая исполнителя и все лоты.
func (ftd *FullTenderData) Validate() error {
	if strings.TrimSpace(ftd.TenderID) == "" {
		return fmt.Errorf("ID тендера (tender_id) не может быть пустым")
	}
	if strings.TrimSpace(ftd.TenderTitle) == "" {
		return fmt.Errorf("название тендера (tender_title) не может быть пустым")
	}
	if strings.TrimSpace(ftd.TenderObject) == "" {
		return fmt.Errorf("объект тендера (tender_object) не может быть пустым")
	}
	if strings.TrimSpace(ftd.TenderAddress) == "" {
		return fmt.Errorf("адрес тендера (tender_address) не может быть пустым")
	}
	if ftd.ExecutorData == (Executor{}) {
		return fmt.Errorf("данные исполнителя (executor) не могут быть пустыми")
	}
	if len(ftd.LotsData) == 0 {
		return fmt.Errorf("необходимо указать хотя бы один лот (lots)")
	}
	if err := ftd.ExecutorData.Validate(); err != nil {
		return err
	}
	for key, lot := range ftd.LotsData {
		if err := lot.Validate(); err != nil {
			return fmt.Errorf("ошибка в лоте '%s': %w", key, err)
		}
	}
	return nil
}

// SimpleLotAIResult представляет упрощенный результат AI обработки только с lot_id
type SimpleLotAIResult struct {
	LotKeyParameters map[string]interface{} `json:"lot_key_parameters" binding:"required"` // Ключевые параметры, извлеченные AI
}

// Validate проверяет корректность данных упрощенного AI результата
func (slar *SimpleLotAIResult) Validate() error {
	if len(slar.LotKeyParameters) == 0 {
		return fmt.Errorf("ключевые параметры (lot_key_parameters) не могут быть пустыми")
	}
	return nil
}

// MatchPositionRequest - это JSON, который Go-сервер ожидает от Python-воркера
// при вызове POST /api/v1/positions/match
type MatchPositionRequest struct {
	PositionItemID    int64  `json:"position_item_id" binding:"required"`
	CatalogPositionID int64  `json:"catalog_position_id" binding:"required"`
	Hash              string `json:"hash" binding:"required"`
	NormVersion       int    `json:"norm_version"` // Опционально, сервис Go подставит '1' по умолчанию
}

// CatalogIndexedRequest - это JSON для POST /api/v1/catalog/indexed
// Сообщает Go-серверу, какие ID каталога были успешно проиндексированы.
type CatalogIndexedRequest struct {
	CatalogIDs []int64 `json:"catalog_ids" binding:"required"`
}

// --- System Settings DTOs ---

// UpdateSystemSettingRequest - это DTO запроса для PUT /api/v1/admin/settings.
// Поддерживает обновление числовых, строковых и булевых настроек.
// Ровно одно из полей ValueNumeric / ValueString / ValueBoolean должно быть задано.
type UpdateSystemSettingRequest struct {
	Key          string   `json:"key" binding:"required"`  // Ключ настройки (например, "dedup_distance_threshold")
	ValueNumeric *float64 `json:"value_numeric,omitempty"` // Числовое значение (опционально)
	ValueString  *string  `json:"value_string,omitempty"`  // Строковое значение (опционально)
	ValueBoolean *bool    `json:"value_boolean,omitempty"` // Булево значение (опционально)
	Description  string   `json:"description,omitempty"`   // Описание (опционально, сохраняется через COALESCE)
}

// SystemSettingResponse - это DTO ответа с данными системной настройки.
type SystemSettingResponse struct {
	Key          string   `json:"key"`
	ValueNumeric *float64 `json:"value_numeric,omitempty"`
	ValueString  *string  `json:"value_string,omitempty"`
	ValueBoolean *bool    `json:"value_boolean,omitempty"`
	Description  *string  `json:"description,omitempty"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
	UpdatedBy    string   `json:"updated_by"`
}

// ImportTenderResponse - это DTO ответа для POST /api/v1/import-tender
// Возвращает информацию о результате импорта тендера.
type ImportTenderResponse struct {
	TenderDBID             int64            `json:"tender_db_id"`
	LotIDsMap              map[string]int64 `json:"lot_ids_map"`
	NewCatalogItemsPending bool             `json:"new_catalog_items_pending"`
}

// SuggestMergeRequest - это JSON для POST /api/v1/merges/suggest
// Создает новую задачу на слияние дубликатов в таблице suggested_merges.
type SuggestMergeRequest struct {
	MainPositionID      int64   `json:"main_position_id" binding:"required"`
	DuplicatePositionID int64   `json:"duplicate_position_id" binding:"required"`
	SimilarityScore     float64 `json:"similarity_score" binding:"required"`
}

// UnmatchedPositionResponse - это DTO, которое Go-сервер возвращает
// в ответ на GET /api/v1/positions/unmatched
type UnmatchedPositionResponse struct {
	PositionItemID     int64  `json:"position_item_id"`
	JobTitleInProposal string `json:"job_title_in_proposal"`
	RichContextString  string `json:"rich_context_string"`
	DraftCatalogID     *int64 `json:"draft_catalog_id,omitempty"` // ID записи catalog_positions со статусом pending (fallback для RAG)
	StandardJobTitle   string `json:"standard_job_title"`         // Лемматизированная версия для поиска в catalog_positions
}

// MergeScenario — тип сценария слияния.
type MergeScenario = string

const (
	// MergeScenarioDefault — B вливается в A (Default Merge).
	MergeScenarioDefault MergeScenario = "default"
	// MergeScenarioMergeToNew — создаётся C, A и B вливаются в C (Merge to New).
	MergeScenarioMergeToNew MergeScenario = "merge_to_new"
)

// ExecuteMergeRequest - это DTO запроса для POST /api/v1/merges/:id/execute
// Если NewMainTitle пуст — Сценарий 1 (Default Merge): B вливается в A.
// Если NewMainTitle задан — Сценарий 2 (Merge to New): создаётся C, A и B вливаются в C.
type ExecuteMergeRequest struct {
	NewMainTitle string `json:"new_main_title,omitempty"`
}

// ExecuteMergeResponse - это DTO ответа для POST /api/v1/merges/:id/execute
// Возвращает информацию о выполненном слиянии.
type ExecuteMergeResponse struct {
	MergeID                 int64         `json:"merge_id"`                  // ID записи suggested_merges
	MainPositionID          int64         `json:"main_position_id"`          // Исходная мастер-позиция (A)
	MergedPositionID        int64         `json:"merged_position_id"`        // Дубликат (B), стал deprecated
	ResultingPositionID     int64         `json:"resulting_position_id"`     // Итоговая позиция: A (Scenario 1) или C (Scenario 2)
	ResultingPositionStatus string        `json:"resulting_position_status"` // Статус итоговой позиции: "active" (S1) или "pending_indexing" (S2)
	DeprecatedPositionIDs   []int64       `json:"deprecated_position_ids"`   // ID всех deprecated-позиций: [B] (S1) или [A,B] (S2)
	Scenario                MergeScenario `json:"scenario"`                  // MergeScenarioDefault или MergeScenarioMergeToNew
	Status                  string        `json:"status"`                    // Новый статус дубликата ("deprecated")
	ResolvedAt              time.Time     `json:"resolved_at"`               // Время выполнения слияния
}

// ExecuteBatchMergeRequest - это DTO запроса для POST /api/v1/admin/merges/execute-batch
//
// Выбор сценария:
//   - NewMainTitle задан → Scenario 2: все позиции → deprecated, создаётся C.
//   - NewMainTitle пуст  → Scenario 1: TargetPositionID остаётся active, остальные → deprecated.
//     RenameTitle позволяет переименовать выжившую позицию.
type ExecuteBatchMergeRequest struct {
	MergeIDs         []int64 `json:"merge_ids" binding:"required"` // ID записей suggested_merges
	TargetPositionID int64   `json:"target_position_id,omitempty"` // Scenario 1: кто выживает
	RenameTitle      string  `json:"rename_title,omitempty"`       // Scenario 1: переименовать выжившую
	NewMainTitle     string  `json:"new_main_title,omitempty"`     // Scenario 2: имя новой позиции C
}

// ExecuteBatchMergeResponse - это DTO ответа для POST /api/v1/admin/merges/execute-batch
type ExecuteBatchMergeResponse struct {
	MergeIDs                []int64       `json:"merge_ids"`                 // Обработанные merge ID
	ResultingPositionID     int64         `json:"resulting_position_id"`     // Итоговая позиция: target (S1) или C (S2)
	ResultingPositionStatus string        `json:"resulting_position_status"` // Статус итоговой: "active"/"pending_indexing"
	DeprecatedPositionIDs   []int64       `json:"deprecated_position_ids"`   // Все deprecated-позиции
	Scenario                MergeScenario `json:"scenario"`                  // "default" или "merge_to_new"
	ResolvedAt              time.Time     `json:"resolved_at"`               // Время выполнения
}

// === Suggested Merges (GET /api/v1/admin/suggested_merges) ===

// CatalogPositionSummary — краткая информация о позиции каталога (без embedding/fts_vector).
type CatalogPositionSummary struct {
	ID               int64   `json:"id"`
	StandardJobTitle string  `json:"standard_job_title"`
	Description      *string `json:"description,omitempty"`
	Kind             string  `json:"kind"`
	Status           string  `json:"status"`
}

// SuggestedMergeItem — одно предложение о слиянии с краткой информацией о дубликате.
type SuggestedMergeItem struct {
	MergeID         int64                  `json:"merge_id"`
	SimilarityScore float32                `json:"similarity_score"`
	Duplicate       CatalogPositionSummary `json:"duplicate"`
	CreatedAt       time.Time              `json:"created_at"`
}

// SuggestedMergeGroup — группа предложений, объединённых по main_position_id.
type SuggestedMergeGroup struct {
	MainPosition CatalogPositionSummary `json:"main_position"`
	Merges       []SuggestedMergeItem   `json:"merges"`
}

// === Catalog Groups (GET /api/v1/admin/catalog/groups) ===

// GroupSummary — краткая информация о родительской группе (kind='GROUP_TITLE').
type GroupSummary struct {
	ID               int64     `json:"id"`
	StandardJobTitle string    `json:"standard_job_title"`
	Description      *string   `json:"description,omitempty"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	ChildrenCount    int       `json:"children_count"`
}

// ListGroupsResponse — ответ GET /api/v1/admin/catalog/groups.
type ListGroupsResponse struct {
	Groups []GroupSummary `json:"groups"`
	Total  int            `json:"total"`
}

// ListGroupChildrenResponse — ответ GET /api/v1/admin/catalog/groups/:id/children.
type ListGroupChildrenResponse struct {
	Children []CatalogPositionSummary `json:"children"`
	ParentID int64                    `json:"parent_id"`
}

// ListSuggestedMergesResponse — ответ GET /api/v1/admin/suggested_merges.
type ListSuggestedMergesResponse struct {
	Groups      []SuggestedMergeGroup `json:"groups"`
	Total       int                   `json:"total"`        // Общее количество PENDING merge-записей
	TotalGroups int                   `json:"total_groups"` // Общее количество уникальных main_position_id
}

// === Group Positions (POST /api/v1/admin/merges/:id/group) ===

// GroupPositionsRequest — DTO запроса для группировки позиций под родителем.
// Ровно одно из полей ParentID / NewParentTitle должно быть задано.
type GroupPositionsRequest struct {
	// Идентификатор существующего родителя (если выбран из справочника)
	ParentID int64 `json:"parent_id,omitempty"`
	// Название для нового родителя (если нужно создать на лету)
	NewParentTitle string `json:"new_parent_title,omitempty"`
	// Принудительная перезапись parent_id для позиций, уже входящих в другую группу
	Force bool `json:"force,omitempty"`
}

// GroupPositionsResponse — DTO ответа для группировки позиций.
type GroupPositionsResponse struct {
	MergeID    int64     `json:"merge_id"`
	ParentID   int64     `json:"parent_id"`
	Status     string    `json:"status"` // expected: "GROUPED"
	ResolvedAt time.Time `json:"resolved_at"`
}

// GroupBatchPositionsRequest — DTO запроса для батч-группировки позиций.
// Ровно одно из полей ParentID / NewParentTitle должно быть задано.
type GroupBatchPositionsRequest struct {
	MergeIDs       []int64 `json:"merge_ids" binding:"required"`
	ParentID       int64   `json:"parent_id,omitempty"`
	NewParentTitle string  `json:"new_parent_title,omitempty"`
	Force          bool    `json:"force,omitempty"`
}

// GroupBatchPositionsResponse — DTO ответа для батч-группировки позиций.
type GroupBatchPositionsResponse struct {
	MergeIDs    []int64   `json:"merge_ids"`
	ParentID    int64     `json:"parent_id"`
	PositionIDs []int64   `json:"position_ids"` // все уникальные позиции, привязанные к parent
	Status      string    `json:"status"`       // "GROUPED"
	ResolvedAt  time.Time `json:"resolved_at"`
}

// GroupConflict — описание конфликта при группировке: позиция уже в другой группе.
type GroupConflict struct {
	PositionID         int64  `json:"position_id"`
	PositionTitle      string `json:"position_title"`
	CurrentParentID    int64  `json:"current_parent_id"`
	CurrentParentTitle string `json:"current_parent_title"`
	SiblingsCount      int64  `json:"siblings_count"`
}
