package api_models

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
