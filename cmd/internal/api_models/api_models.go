// Package api_models содержит структуры и методы валидации, используемые для представления
// полной информации о тендере, его лотах, предложениях подрядчиков и связанных данных.
package api_models

import (
	"fmt"
	"strings"
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
