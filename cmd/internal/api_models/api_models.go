package api_models

import (
	"fmt"
	"strings"
)

type FullTenderData struct {
	TenderID      string         `json:"tender_id"`
	TenderTitle   string         `json:"tender_title"`
	TenderObject  string         `json:"tender_object"`
	TenderAddress string         `json:"tender_address"`
	ExecutorData  Executor       `json:"executor"`
	LotsData      map[string]Lot `json:"lots"`
}

type Executor struct {
	ExecutorName  string `json:"executor_name"`
	ExecutorPhone string `json:"executor_phone"`
	ExecutorDate  string `json:"executor_date"`
}

type Lot struct {
	LotTitle         string                               `json:"lot_title"`
	ProposalData     map[string]ContractorProposalDetails `json:"proposals"`
	BaseLineProposal ContractorProposalDetails            `json:"baseline_proposal"`
}

type ContractorProposalDetails struct {
	Title                string                   `json:"title"`
	Inn                  string                   `json:"inn"`
	Address              string                   `json:"address"`
	Accreditation        string                   `json:"accreditation"`
	ContractorCoordinate string                   `json:"contractor_coordinate"`
	ContractorWidth      int                      `json:"contractor_width"`
	ContractorHeight     int                      `json:"contractor_height"`
	ContractorItems      ContractorItemsContainer `json:"contractor_items"`
	AdditionalInfo       map[string]*string       `json:"additional_info,omitempty"` // Дополнительная информация, если есть
}

type ContractorItemsContainer struct {
	Positions map[string]PositionItem `json:"positions"` // ИСПРАВЛЕНО: Используем PositionItem (определим ниже)
	Summary   map[string]SummaryLine  `json:"summary"`   // ИСПРАВЛЕНО: тип и тэг для summary
}

type PositionItem struct {
	Number                        string   `json:"number"`                                      // Номер позиции в предложении
	ChapterNumber                 *string  `json:"chapter_number,omitempty"`                    // Номер главы, к которой относится позиция
	ArticleSMR                    *string  `json:"article_smr,omitempty"`                       // Артикул СМР (если применимо)
	JobTitle                      string   `json:"job_title"`                                   // Наименование работы
	CommentOrganizer              *string  `json:"comment_organizer,omitempty"`                 // Комментарий организатора
	Unit                          *string  `json:"unit,omitempty"`                              // Единица измерения
	Quantity                      *float64 `json:"quantity,omitempty"`                          // Количество
	SuggestedQuantity             *float64 `json:"suggested_quantity,omitempty"`                // Предложенное количество
	UnitCost                      Cost     `json:"unit_cost"`                                   // Стоимость за единицу
	TotalCost                     Cost     `json:"total_cost"`                                  // Общая стоимость
	TotalCostForOrganizerQuantity *float64 `json:"total_cost_for_organizer_quantity,omitempty"` // Расчетная общая стоимость по ценам подрядчика, но для первоначального количества, указанного организатором
	CommentContractor             *string  `json:"comment_contractor,omitempty"`
	JobTitleNormalized            *string  `json:"job_title_normalized,omitempty"`
	IsChapter                     bool     `json:"is_chapter"`
	ChapterRef                    *string  `json:"chapter_ref,omitempty"`
}

type Cost struct {
	Materials     *float64 `json:"materials"`
	Works         *float64 `json:"works"`
	IndirectCosts *float64 `json:"indirect_costs"`
	Total         *float64 `json:"total"`
}

type SummaryLine struct {
	JobTitle               string   `json:"job_title"`
	SuggestedQuantity      *float64 `json:"suggested_quantity"`
	UnitCost               Cost     `json:"unit_cost"`
	TotalCost              Cost     `json:"total_cost"`
	OrganizierQuantityCost *float64 `json:"total_cost_for_organizer_quantity"`
	CommentContractor      *string  `json:"comment_contractor,omitempty"`
	Deviation              *float64 `json:"deviation_from_baseline_cost,omitempty"`
}

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

func (l *Lot) Validate() error {
	if strings.TrimSpace(l.LotTitle) == "" {
		return fmt.Errorf("название лота (lot_title) не может быть пустым")
	}

	// Валидируем базовое предложение
	if err := l.BaseLineProposal.Validate(true); err != nil {
		return fmt.Errorf("ошибка в базовом предложении лота '%s': %w", l.LotTitle, err)
	}

	// Валидируем все предложения подрядчиков в цикле
	for key, proposal := range l.ProposalData {
		if err := proposal.Validate(false); err != nil {
			return fmt.Errorf("ошибка в предложении '%s' лота '%s': %w", key, l.LotTitle, err)
		}
	}

	return nil
}

func (e *Executor) Validate() error {
	if strings.TrimSpace(e.ExecutorName) == "" {
		return fmt.Errorf("имя исполнителя (executor_name) не может быть пустым")
	}
	if strings.TrimSpace(e.ExecutorPhone) == "" {
		return fmt.Errorf("телефон исполнителя (executor_phone) не может быть пустым")
	}
	return nil
}

func (ftd *FullTenderData) Validate() error {
	// 1. Проверяем собственные поля
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

	// 2. Делегируем валидацию дочерним структурам
	if err := ftd.ExecutorData.Validate(); err != nil {
		return err // Ошибка уже содержит всю нужную информацию
	}

	// 3. Делегируем валидацию лотам в цикле
	for key, lot := range ftd.LotsData {
		if err := lot.Validate(); err != nil {
			return fmt.Errorf("ошибка в лоте '%s': %w", key, err)
		}
	}

	return nil
}