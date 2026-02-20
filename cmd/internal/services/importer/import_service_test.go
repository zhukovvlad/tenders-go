// Purpose: Protects the full tender import pipeline from regressions.
// Ensures that ImportFullTender correctly orchestrates the creation of tenders,
// lots, proposals, positions, summaries, and raw JSON within a single transaction.
// Also verifies correct error propagation and pure mapper functions.

package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/entities"
	"github.com/zhukovvlad/tenders-go/cmd/internal/testutil"
)

/*
BEHAVIORAL SCENARIOS FOR TENDER IMPORT SERVICE (Unit Tests)

What user problems does this protect us from?
================================================================================
1. Full import pipeline — tender data must be atomically imported (all-or-nothing)
2. Entity orchestration — objects, executors, contractors, lots, proposals must be
   created/updated in the correct order within a single transaction
3. Raw JSON preservation — original JSON must be saved alongside structured data
4. Error propagation — failures at any step must abort the transaction and return
   meaningful wrapped errors
5. Position catalog matching — positions must correctly interact with matching_cache
6. Mapper correctness — pure mapping functions must correctly convert API models to DB params

GIVEN / WHEN / THEN Scenarios:
================================================================================

SCENARIO 1: ImportFullTender — Successful import with 1 lot
- GIVEN a valid FullTenderData with 1 lot, 1 baseline proposal, 1 position, 1 summary
  WHEN ImportFullTender is called
  THEN all entities are created in sequence, raw JSON is saved, tenderID and lotIDs are returned

SCENARIO 2: ImportFullTender — Object creation fails
- GIVEN a valid payload but GetObjectByTitle returns an unexpected DB error
  WHEN ImportFullTender is called
  THEN the transaction is aborted and a wrapped error is returned

SCENARIO 3: ImportFullTender — Executor creation fails
- GIVEN a valid payload, object exists, but CreateExecutor returns an error
  WHEN ImportFullTender is called
  THEN the transaction is aborted and a wrapped error is returned

SCENARIO 4: ImportFullTender — UpsertTender fails
- GIVEN a valid payload, object and executor exist, but UpsertTender returns an error
  WHEN ImportFullTender is called
  THEN the transaction is aborted and a wrapped error is returned

SCENARIO 5: ImportFullTender — UpsertLot fails
- GIVEN a valid payload but UpsertLot returns an error
  WHEN ImportFullTender is called
  THEN the transaction is aborted with a wrapped lot error

SCENARIO 6: ImportFullTender — UpsertProposal fails
- GIVEN valid tender and lot, but UpsertProposal for baseline returns an error
  WHEN ImportFullTender is called
  THEN the transaction is aborted and a wrapped error is returned

SCENARIO 7: ImportFullTender — UpsertTenderRawData fails
- GIVEN the entire import succeeded but UpsertTenderRawData fails
  WHEN ImportFullTender is called
  THEN a wrapped transaction error is returned

SCENARIO 8: ImportFullTender — ExecTx itself fails
- GIVEN ExecTx returns an error before calling the callback
  WHEN ImportFullTender is called
  THEN a wrapped transaction error is returned

SCENARIO 9: ImportFullTender — Tender with no lots
- GIVEN a valid payload with LotsData as empty map
  WHEN ImportFullTender is called
  THEN the tender is created, raw JSON is saved, and empty lotIDs map is returned

SCENARIO 10: ImportFullTender — Position with cache hit
- GIVEN a position where matching_cache returns a cached catalog_position_id
  WHEN ImportFullTender is called
  THEN the cached catalog_position_id is used for the position item

SCENARIO 11: ImportFullTender — Contractor proposal with additional info
- GIVEN a lot with a contractor proposal containing additional_info
  WHEN ImportFullTender is called
  THEN DeleteAllAdditionalInfoForProposal + UpsertProposalAdditionalInfo are called

SCENARIO 12: mapApiPositionToDbParams — pure mapper
- GIVEN an API PositionItem with various fields
  WHEN mapApiPositionToDbParams is called
  THEN the resulting DB params have correctly mapped fields

SCENARIO 13: mapApiSummaryToDbParams — pure mapper
- GIVEN an API SummaryLine with various fields
  WHEN mapApiSummaryToDbParams is called
  THEN the resulting DB params have correctly mapped fields

SCENARIO 14: NewTenderImportService — constructor
- GIVEN valid dependencies (store, logger, entityManager)
  WHEN NewTenderImportService is called
  THEN the service is properly initialized with all dependencies
*/

// ============================================================================
// Test Helpers
// ============================================================================

var now = time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC)

// SQL result column sets for sqlmock row builders
var (
	objectColumns        = []string{"id", "title", "address", "created_at", "updated_at"}
	executorColumns      = []string{"id", "name", "phone", "created_at", "updated_at"}
	tenderColumns        = []string{"id", "etp_id", "title", "category_id", "object_id", "executor_id", "data_prepared_on_date", "created_at", "updated_at"}
	lotColumns           = []string{"id", "lot_key", "lot_title", "lot_key_parameters", "tender_id", "created_at", "updated_at"}
	contractorColumns    = []string{"id", "title", "inn", "address", "accreditation", "created_at", "updated_at"}
	proposalColumns      = []string{"id", "lot_id", "contractor_id", "is_baseline", "contractor_coordinate", "contractor_width", "contractor_height", "created_at", "updated_at"}
	unitColumns          = []string{"id", "normalized_name", "full_name", "description", "created_at", "updated_at"}
	catalogPosColumns    = []string{"id", "standard_job_title", "description", "embedding", "kind", "status", "unit_id", "created_at", "updated_at", "fts_vector"}
	matchingCacheColumns = []string{"job_title_hash", "norm_version", "job_title_text", "catalog_position_id", "created_at", "expires_at"}
	positionItemColumns  = []string{
		"id", "proposal_id", "catalog_position_id", "position_key_in_proposal",
		"comment_organazier", "comment_contractor", "item_number_in_proposal",
		"chapter_number_in_proposal", "job_title_in_proposal", "unit_id",
		"quantity", "suggested_quantity", "total_cost_for_organizer_quantity",
		"unit_cost_materials", "unit_cost_works", "unit_cost_indirect_costs", "unit_cost_total",
		"total_cost_materials", "total_cost_works", "total_cost_indirect_costs", "total_cost_total",
		"deviation_from_baseline_cost", "is_chapter", "chapter_ref_in_proposal",
		"created_at", "updated_at",
	}
	summaryLineColumns    = []string{"id", "proposal_id", "summary_key", "job_title", "materials_cost", "works_cost", "indirect_costs_cost", "total_cost", "created_at", "updated_at"}
	tenderRawColumns      = []string{"tender_id", "raw_data", "created_at", "updated_at"}
	additionalInfoColumns = []string{"id", "proposal_id", "info_key", "info_value", "created_at", "updated_at"}
)

// setupTestService creates a TenderImportService with a mock store for unit testing.
func setupTestService(t *testing.T) (*TenderImportService, *db.MockStore) {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockStore := db.NewMockStore(ctrl)
	logger := testutil.NewMockLogger()
	entityManager := entities.NewEntityManager(logger)
	service := NewTenderImportService(mockStore, logger, entityManager)
	return service, mockStore
}

// newMockQueries creates a sqlmock-backed *db.Queries for use inside ExecTx DoAndReturn.
func newMockQueries(t *testing.T) (sqlmock.Sqlmock, *db.Queries, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	q := db.New(sqlDB)
	cleanup := func() {
		err := mock.ExpectationsWereMet()
		assert.NoError(t, err, "sqlmock: there were unmet expectations")
		sqlDB.Close()
	}
	return mock, q, cleanup
}

// execTxDoAndReturn creates a DoAndReturn func that executes the ExecTx callback
// with a sqlmock-backed *Queries. setupFn sets up sqlmock expectations.
func execTxDoAndReturn(t *testing.T, setupFn func(mock sqlmock.Sqlmock)) func(ctx context.Context, fn func(*db.Queries) error) error {
	t.Helper()
	return func(ctx context.Context, fn func(*db.Queries) error) error {
		mock, q, cleanup := newMockQueries(t)
		defer cleanup()
		setupFn(mock)
		return fn(q)
	}
}

// makeMinimalPayload creates a minimal valid FullTenderData for testing.
func makeMinimalPayload() *api_models.FullTenderData {
	return &api_models.FullTenderData{
		TenderID:      "ETP-TEST-001",
		TenderTitle:   "Тестовый тендер",
		TenderObject:  "Строительство",
		TenderAddress: "г. Москва, ул. Тестовая, 1",
		ExecutorData: api_models.Executor{
			ExecutorName:  "Иванов И.И.",
			ExecutorPhone: "+7-999-000-0000",
			ExecutorDate:  "01.01.2025",
		},
		LotsData: map[string]api_models.Lot{},
	}
}

// makePayloadWithOneLot creates a payload with 1 lot, 1 baseline proposal with 1 position and 1 summary.
func makePayloadWithOneLot() *api_models.FullTenderData {
	unitName := "м2"
	quantity := 100.0
	totalMaterials := 5000.0
	totalWorks := 3000.0
	totalTotal := 8000.0
	jobTitleNorm := "устройство полов"

	payload := makeMinimalPayload()
	payload.LotsData = map[string]api_models.Lot{
		"lot-1": {
			LotTitle: "Лот №1 — Отделочные работы",
			BaseLineProposal: api_models.ContractorProposalDetails{
				Title: "Initiator",
				ContractorItems: api_models.ContractorItemsContainer{
					Positions: map[string]api_models.PositionItem{
						"pos-1": {
							Number:             "1",
							JobTitle:           "Устройство полов",
							JobTitleNormalized: &jobTitleNorm,
							Unit:               &unitName,
							Quantity:           &quantity,
							TotalCost: api_models.Cost{
								Materials: &totalMaterials,
								Works:     &totalWorks,
								Total:     &totalTotal,
							},
							UnitCost:  api_models.Cost{},
							IsChapter: false,
						},
					},
					Summary: map[string]api_models.SummaryLine{
						"sum-1": {
							JobTitle: "Итого по лоту",
							TotalCost: api_models.Cost{
								Total: &totalTotal,
							},
						},
					},
				},
			},
			ProposalData: map[string]api_models.ContractorProposalDetails{},
		},
	}
	return payload
}

// setupCoreTenderExpectations sets up sqlmock expectations for processCoreTenderData:
// GetObjectByTitle (not found) → CreateObject → GetExecutorByName (not found) → CreateExecutor → UpsertTender
func setupCoreTenderExpectations(mock sqlmock.Sqlmock) {
	// GetObjectByTitle → not found
	mock.ExpectQuery("SELECT .+ FROM objects WHERE title").
		WithArgs("Строительство").
		WillReturnError(sql.ErrNoRows)
	// CreateObject
	mock.ExpectQuery("INSERT INTO objects").
		WithArgs("Строительство", "г. Москва, ул. Тестовая, 1").
		WillReturnRows(sqlmock.NewRows(objectColumns).
			AddRow(int64(1), "Строительство", "г. Москва, ул. Тестовая, 1", now, now))
	// GetExecutorByName → not found
	mock.ExpectQuery("SELECT .+ FROM executors WHERE name").
		WithArgs("Иванов И.И.").
		WillReturnError(sql.ErrNoRows)
	// CreateExecutor
	mock.ExpectQuery("INSERT INTO executors").
		WithArgs("Иванов И.И.", "+7-999-000-0000").
		WillReturnRows(sqlmock.NewRows(executorColumns).
			AddRow(int64(1), "Иванов И.И.", "+7-999-000-0000", now, now))
	// UpsertTender
	mock.ExpectQuery("INSERT INTO tenders").
		WillReturnRows(sqlmock.NewRows(tenderColumns).
			AddRow(int64(100), "ETP-TEST-001", "Тестовый тендер", nil, int64(1), int64(1), nil, now, now))
}

// setupBaselineProposalExpectations sets up expectations for baseline proposal processing:
// GetContractorByINN("0000000000") → not found → CreateContractor → UpsertProposal
func setupBaselineProposalExpectations(mock sqlmock.Sqlmock, lotDBID int64) int64 {
	proposalDBID := int64(200)
	// GetContractorByINN for baseline ("0000000000")
	mock.ExpectQuery("SELECT .+ FROM contractors WHERE inn").
		WithArgs("0000000000").
		WillReturnError(sql.ErrNoRows)
	// CreateContractor for baseline
	mock.ExpectQuery("INSERT INTO contractors").
		WithArgs("Initiator", "0000000000", "N/A", "N/A").
		WillReturnRows(sqlmock.NewRows(contractorColumns).
			AddRow(int64(50), "Initiator", "0000000000", "N/A", "N/A", now, now))
	// UpsertProposal for baseline
	mock.ExpectQuery("INSERT INTO proposals").
		WillReturnRows(sqlmock.NewRows(proposalColumns).
			AddRow(proposalDBID, lotDBID, int64(50), true, nil, nil, nil, now, now))

	return proposalDBID
}

// setupPositionExpectations sets up expectations for a single position with cache miss:
// GetUnitByNormalizedName → not found → CreateUnit →
// GetCatalogPositionByTitleAndUnit → not found → CreateCatalogPosition →
// GetMatchingCache → cache miss → UpsertPositionItem
func setupPositionExpectations(mock sqlmock.Sqlmock, proposalDBID int64) {
	// GetUnitOfMeasurementByNormalizedName → not found
	mock.ExpectQuery("SELECT .+ FROM units_of_measurement WHERE normalized_name").
		WithArgs("м2").
		WillReturnError(sql.ErrNoRows)
	// CreateUnitOfMeasurement
	mock.ExpectQuery("INSERT INTO units_of_measurement").
		WithArgs("м2", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(unitColumns).
			AddRow(int64(10), "м2", sql.NullString{String: "м2", Valid: true}, sql.NullString{Valid: false}, now, now))
	// GetCatalogPositionByTitleAndUnit → not found
	mock.ExpectQuery("SELECT .+ FROM catalog_positions").
		WillReturnError(sql.ErrNoRows)
	// CreateCatalogPosition
	mock.ExpectQuery("INSERT INTO catalog_positions").
		WillReturnRows(sqlmock.NewRows(catalogPosColumns).
			AddRow(int64(300), "устройство полов", sql.NullString{String: "Устройство полов", Valid: true}, nil, "POSITION", "pending_indexing", sql.NullInt64{Int64: 10, Valid: true}, now, now, nil))
	// GetMatchingCache → cache miss
	mock.ExpectQuery("SELECT .+ FROM matching_cache").
		WillReturnError(sql.ErrNoRows)
	// UpsertPositionItem
	mock.ExpectQuery("INSERT INTO position_items").
		WillReturnRows(sqlmock.NewRows(positionItemColumns).
			AddRow(
				int64(400), proposalDBID, sql.NullInt64{Int64: 300, Valid: true}, "pos-1",
				sql.NullString{}, sql.NullString{}, sql.NullString{String: "1", Valid: true},
				sql.NullString{}, "Устройство полов", sql.NullInt64{Int64: 10, Valid: true},
				sql.NullString{}, sql.NullString{}, sql.NullString{},
				sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{},
				sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{},
				sql.NullString{}, false, sql.NullString{},
				now, now,
			))
}

// setupSummaryExpectations sets up expectations for a single summary line.
func setupSummaryExpectations(mock sqlmock.Sqlmock, proposalDBID int64) {
	mock.ExpectQuery("INSERT INTO proposal_summary_lines").
		WillReturnRows(sqlmock.NewRows(summaryLineColumns).
			AddRow(int64(500), proposalDBID, "sum-1", "Итого по лоту", nil, nil, nil, sql.NullString{String: "8000", Valid: true}, now, now))
}

// setupRawDataExpectations sets up expectations for UpsertTenderRawData.
func setupRawDataExpectations(mock sqlmock.Sqlmock, tenderDBID int64) {
	mock.ExpectQuery("INSERT INTO tender_raw_data").
		WillReturnRows(sqlmock.NewRows(tenderRawColumns).
			AddRow(tenderDBID, json.RawMessage(`{}`), now, now))
}

// ============================================================================
// NewTenderImportService TESTS
// ============================================================================

func TestNewTenderImportService(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := db.NewMockStore(ctrl)
	logger := testutil.NewMockLogger()
	em := entities.NewEntityManager(logger)

	// WHEN
	service := NewTenderImportService(mockStore, logger, em)

	// THEN
	require.NotNil(t, service)
	assert.NotNil(t, service.store)
	assert.NotNil(t, service.logger)
	assert.NotNil(t, service.Entities)
	assert.Equal(t, em, service.Entities)
}

// ============================================================================
// ImportFullTender TESTS
// ============================================================================

func TestImportFullTender_Success_NoLots(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN a valid payload with no lots
	payload := makeMinimalPayload()
	rawJSON := []byte(`{"tender_id":"ETP-TEST-001"}`)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			setupCoreTenderExpectations(mock)
			// No lots to process
			setupRawDataExpectations(mock, 100)
		}),
	)

	// WHEN
	tenderID, lotIDs, anyNewPending, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.NoError(t, err)
	assert.Equal(t, int64(100), tenderID)
	assert.Empty(t, lotIDs)
	assert.False(t, anyNewPending)
}

func TestImportFullTender_Success_WithOneLot(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN a valid payload with 1 lot
	payload := makePayloadWithOneLot()
	rawJSON := []byte(`{"tender_id":"ETP-TEST-001","lots":{}}`)

	lotDBID := int64(150)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// Step 1: Core tender data
			setupCoreTenderExpectations(mock)
			// Step 2: Lot processing
			// UpsertLot
			mock.ExpectQuery("INSERT INTO lots").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(lotDBID, "lot-1", "Лот №1 — Отделочные работы", nil, int64(100), now, now))
			// Baseline proposal
			proposalDBID := setupBaselineProposalExpectations(mock, lotDBID)
			// Baseline proposal: skip additional info (isBaseline=true)
			// Baseline proposal: process positions
			setupPositionExpectations(mock, proposalDBID)
			// Baseline proposal: process summary
			setupSummaryExpectations(mock, proposalDBID)
			// Step 3: Save raw JSON
			setupRawDataExpectations(mock, 100)
		}),
	)

	// WHEN
	tenderID, lotIDs, anyNewPending, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.NoError(t, err)
	assert.Equal(t, int64(100), tenderID)
	assert.Equal(t, map[string]int64{"lot-1": lotDBID}, lotIDs)
	assert.True(t, anyNewPending)
	// New catalog position created with kind=POSITION → anyNewPending = true
}

func TestImportFullTender_Success_MatchingCacheHit(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN a payload where matching_cache returns a cached result
	payload := makePayloadWithOneLot()
	rawJSON := []byte(`{}`)
	lotDBID := int64(150)
	proposalDBID := int64(200)
	cachedCatalogPosID := int64(999) // Cached from previous matching

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			setupCoreTenderExpectations(mock)
			// UpsertLot
			mock.ExpectQuery("INSERT INTO lots").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(lotDBID, "lot-1", "Лот №1 — Отделочные работы", nil, int64(100), now, now))
			// Baseline proposal
			// GetContractorByINN("0000000000") → not found → CreateContractor
			mock.ExpectQuery("SELECT .+ FROM contractors WHERE inn").
				WithArgs("0000000000").
				WillReturnError(sql.ErrNoRows)
			mock.ExpectQuery("INSERT INTO contractors").
				WithArgs("Initiator", "0000000000", "N/A", "N/A").
				WillReturnRows(sqlmock.NewRows(contractorColumns).
					AddRow(int64(50), "Initiator", "0000000000", "N/A", "N/A", now, now))
			mock.ExpectQuery("INSERT INTO proposals").
				WillReturnRows(sqlmock.NewRows(proposalColumns).
					AddRow(proposalDBID, lotDBID, int64(50), true, nil, nil, nil, now, now))
			// Position: unit exists
			mock.ExpectQuery("SELECT .+ FROM units_of_measurement WHERE normalized_name").
				WithArgs("м2").
				WillReturnRows(sqlmock.NewRows(unitColumns).
					AddRow(int64(10), "м2", sql.NullString{String: "м2", Valid: true}, sql.NullString{}, now, now))
			// Position: catalog position exists
			mock.ExpectQuery("SELECT .+ FROM catalog_positions").
				WillReturnRows(sqlmock.NewRows(catalogPosColumns).
					AddRow(int64(300), "устройство полов", sql.NullString{String: "Устройство полов", Valid: true}, nil, "POSITION", "active", sql.NullInt64{Int64: 10, Valid: true}, now, now, nil))
			// Position: CACHE HIT → GetMatchingCache returns cached result
			mock.ExpectQuery("SELECT .+ FROM matching_cache").
				WillReturnRows(sqlmock.NewRows(matchingCacheColumns).
					AddRow("somehash", int16(1), sql.NullString{String: "устройство полов", Valid: true}, cachedCatalogPosID, now, sql.NullTime{}))
			// UpsertPositionItem (with cached catalog_position_id)
			mock.ExpectQuery("INSERT INTO position_items").
				WillReturnRows(sqlmock.NewRows(positionItemColumns).
					AddRow(
						int64(400), proposalDBID, sql.NullInt64{Int64: cachedCatalogPosID, Valid: true}, "pos-1",
						sql.NullString{}, sql.NullString{}, sql.NullString{String: "1", Valid: true},
						sql.NullString{}, "Устройство полов", sql.NullInt64{Int64: 10, Valid: true},
						sql.NullString{}, sql.NullString{}, sql.NullString{},
						sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{},
						sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{},
						sql.NullString{}, false, sql.NullString{},
						now, now,
					))
			// Summary
			setupSummaryExpectations(mock, proposalDBID)
			// Raw data
			setupRawDataExpectations(mock, 100)
		}),
	)

	// WHEN
	tenderID, lotIDs, anyNewPending, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.NoError(t, err)
	assert.Equal(t, int64(100), tenderID)
	assert.Equal(t, map[string]int64{"lot-1": lotDBID}, lotIDs)
	assert.False(t, anyNewPending)
	// Catalog position already existed (not newly created) → isNewPendingItem = false
}

func TestImportFullTender_Success_ContractorProposalWithAdditionalInfo(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN a payload with 1 lot: baseline + 1 contractor proposal with additional_info
	infoVal := "30 дней"
	payload := makeMinimalPayload()
	payload.LotsData = map[string]api_models.Lot{
		"lot-1": {
			LotTitle: "Лот с подрядчиком",
			BaseLineProposal: api_models.ContractorProposalDetails{
				Title:           "Initiator",
				ContractorItems: api_models.ContractorItemsContainer{},
			},
			ProposalData: map[string]api_models.ContractorProposalDetails{
				"contractor-1": {
					Title:                "ООО Строитель",
					Inn:                  "1234567890",
					Address:              "г. Москва",
					Accreditation:        "Аккредитован",
					ContractorCoordinate: "A1",
					ContractorItems:      api_models.ContractorItemsContainer{},
					AdditionalInfo: map[string]*string{
						"срок_выполнения": &infoVal,
					},
				},
			},
		},
	}

	rawJSON := []byte(`{}`)
	lotDBID := int64(150)
	baselineProposalID := int64(200)
	contractorProposalID := int64(201)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			setupCoreTenderExpectations(mock)
			// UpsertLot
			mock.ExpectQuery("INSERT INTO lots").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(lotDBID, "lot-1", "Лот с подрядчиком", nil, int64(100), now, now))
			// Baseline proposal
			// Baseline: GetContractorByINN → not found → CreateContractor → UpsertProposal
			mock.ExpectQuery("SELECT .+ FROM contractors WHERE inn").
				WithArgs("0000000000").
				WillReturnError(sql.ErrNoRows)
			mock.ExpectQuery("INSERT INTO contractors").
				WithArgs("Initiator", "0000000000", "N/A", "N/A").
				WillReturnRows(sqlmock.NewRows(contractorColumns).
					AddRow(int64(50), "Initiator", "0000000000", "N/A", "N/A", now, now))
			mock.ExpectQuery("INSERT INTO proposals").
				WillReturnRows(sqlmock.NewRows(proposalColumns).
					AddRow(baselineProposalID, lotDBID, int64(50), true, nil, nil, nil, now, now))
			// Baseline: skip additional info, no positions, no summary

			// Contractor proposal
			mock.ExpectQuery("SELECT .+ FROM contractors WHERE inn").
				WithArgs("1234567890").
				WillReturnError(sql.ErrNoRows)
			mock.ExpectQuery("INSERT INTO contractors").
				WithArgs("ООО Строитель", "1234567890", "г. Москва", "Аккредитован").
				WillReturnRows(sqlmock.NewRows(contractorColumns).
					AddRow(int64(51), "ООО Строитель", "1234567890", "г. Москва", "Аккредитован", now, now))
			mock.ExpectQuery("INSERT INTO proposals").
				WillReturnRows(sqlmock.NewRows(proposalColumns).
					AddRow(contractorProposalID, lotDBID, int64(51), false, sql.NullString{String: "A1", Valid: true}, nil, nil, now, now))
			// Contractor proposal: additional info (NOT baseline)
			// DeleteAllAdditionalInfoForProposal
			mock.ExpectExec("DELETE FROM proposal_additional_info").
				WithArgs(contractorProposalID).
				WillReturnResult(sqlmock.NewResult(0, 0))
			// UpsertProposalAdditionalInfo
			mock.ExpectQuery("INSERT INTO proposal_additional_info").
				WillReturnRows(sqlmock.NewRows(additionalInfoColumns).
					AddRow(int64(600), contractorProposalID, "срок_выполнения", sql.NullString{String: "30 дней", Valid: true}, now, now))
			// No positions, no summary for contractor proposal

			// Save raw JSON
			setupRawDataExpectations(mock, 100)
		}),
	)

	// WHEN
	tenderID, lotIDs, _, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.NoError(t, err)
	assert.Equal(t, int64(100), tenderID)
	assert.Equal(t, map[string]int64{"lot-1": lotDBID}, lotIDs)
}

// ============================================================================
// Error Scenarios
// ============================================================================

func TestImportFullTender_ExecTxFails(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN ExecTx returns an error (e.g., transaction begin failed)
	payload := makeMinimalPayload()
	rawJSON := []byte(`{}`)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).
		Return(errors.New("connection refused"))

	// WHEN
	tenderID, lotIDs, anyNewPending, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "транзакция импорта тендера провалена")
	assert.Contains(t, err.Error(), "connection refused")
	assert.Equal(t, int64(0), tenderID)
	assert.Nil(t, lotIDs)
	assert.False(t, anyNewPending)
}

func TestImportFullTender_ObjectCreationFails(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN GetObjectByTitle returns an unexpected DB error (not sql.ErrNoRows)
	payload := makeMinimalPayload()
	rawJSON := []byte(`{}`)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("SELECT .+ FROM objects WHERE title").
				WithArgs("Строительство").
				WillReturnError(errors.New("deadlock detected"))
		}),
	)

	// WHEN
	tenderID, _, _, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deadlock detected")
	assert.Equal(t, int64(0), tenderID)
}

func TestImportFullTender_ExecutorCreationFails(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN object exists but CreateExecutor fails
	payload := makeMinimalPayload()
	rawJSON := []byte(`{}`)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// Object found
			mock.ExpectQuery("SELECT .+ FROM objects WHERE title").
				WithArgs("Строительство").
				WillReturnRows(sqlmock.NewRows(objectColumns).
					AddRow(int64(1), "Строительство", "г. Москва, ул. Тестовая, 1", now, now))
			// Executor not found
			mock.ExpectQuery("SELECT .+ FROM executors WHERE name").
				WithArgs("Иванов И.И.").
				WillReturnError(sql.ErrNoRows)
			// CreateExecutor fails
			mock.ExpectQuery("INSERT INTO executors").
				WillReturnError(errors.New("unique_violation"))
		}),
	)

	// WHEN
	tenderID, _, _, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.Error(t, err)
	assert.Equal(t, int64(0), tenderID)
}

func TestImportFullTender_UpsertTenderFails(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN object and executor exist but UpsertTender fails
	payload := makeMinimalPayload()
	rawJSON := []byte(`{}`)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// Object found
			mock.ExpectQuery("SELECT .+ FROM objects WHERE title").
				WithArgs("Строительство").
				WillReturnRows(sqlmock.NewRows(objectColumns).
					AddRow(int64(1), "Строительство", "г. Москва, ул. Тестовая, 1", now, now))
			// Executor found
			mock.ExpectQuery("SELECT .+ FROM executors WHERE name").
				WithArgs("Иванов И.И.").
				WillReturnRows(sqlmock.NewRows(executorColumns).
					AddRow(int64(1), "Иванов И.И.", "+7-999-000-0000", now, now))
			// UpsertTender fails
			mock.ExpectQuery("INSERT INTO tenders").
				WillReturnError(errors.New("FK violation: object_id"))
		}),
	)

	// WHEN
	tenderID, _, _, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "не удалось сохранить тендер")
	assert.Equal(t, int64(0), tenderID)
}

func TestImportFullTender_UpsertLotFails(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN tender created successfully but UpsertLot fails
	payload := makePayloadWithOneLot()
	rawJSON := []byte(`{}`)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			setupCoreTenderExpectations(mock)
			// UpsertLot fails
			mock.ExpectQuery("INSERT INTO lots").
				WillReturnError(errors.New("constraint_violation"))
		}),
	)

	// WHEN
	tenderID, _, _, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "не удалось сохранить лот")
	assert.Equal(t, int64(0), tenderID)
}

func TestImportFullTender_UpsertProposalFails(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN lot created but baseline UpsertProposal fails
	payload := makePayloadWithOneLot()
	rawJSON := []byte(`{}`)
	lotDBID := int64(150)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			setupCoreTenderExpectations(mock)
			// UpsertLot succeeds
			mock.ExpectQuery("INSERT INTO lots").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(lotDBID, "lot-1", "Лот №1 — Отделочные работы", nil, int64(100), now, now))
			// GetContractorByINN → found (Initiator already exists)
			mock.ExpectQuery("SELECT .+ FROM contractors WHERE inn").
				WithArgs("0000000000").
				WillReturnRows(sqlmock.NewRows(contractorColumns).
					AddRow(int64(50), "Initiator", "0000000000", "N/A", "N/A", now, now))
			// UpsertProposal fails
			mock.ExpectQuery("INSERT INTO proposals").
				WillReturnError(errors.New("serialization failure"))
		}),
	)

	// WHEN
	tenderID, _, _, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "не удалось сохранить предложение")
	assert.Equal(t, int64(0), tenderID)
}

func TestImportFullTender_UpsertPositionItemFails(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN proposals created but UpsertPositionItem fails
	payload := makePayloadWithOneLot()
	rawJSON := []byte(`{}`)
	lotDBID := int64(150)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			setupCoreTenderExpectations(mock)
			// UpsertLot
			mock.ExpectQuery("INSERT INTO lots").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(lotDBID, "lot-1", "Лот №1 — Отделочные работы", nil, int64(100), now, now))
			// Baseline proposal
			setupBaselineProposalExpectations(mock, lotDBID)
			// Position: unit exists
			mock.ExpectQuery("SELECT .+ FROM units_of_measurement WHERE normalized_name").
				WithArgs("м2").
				WillReturnRows(sqlmock.NewRows(unitColumns).
					AddRow(int64(10), "м2", sql.NullString{String: "м2", Valid: true}, sql.NullString{}, now, now))
			// Position: catalog position exists
			mock.ExpectQuery("SELECT .+ FROM catalog_positions").
				WillReturnRows(sqlmock.NewRows(catalogPosColumns).
					AddRow(int64(300), "устройство полов", sql.NullString{String: "Устройство полов", Valid: true}, nil, "POSITION", "active", sql.NullInt64{Int64: 10, Valid: true}, now, now, nil))
			// GetMatchingCache → cache miss
			mock.ExpectQuery("SELECT .+ FROM matching_cache").
				WillReturnError(sql.ErrNoRows)
			// UpsertPositionItem fails
			mock.ExpectQuery("INSERT INTO position_items").
				WillReturnError(errors.New("disk full"))
		}),
	)

	// WHEN
	tenderID, _, _, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "не удалось сохранить позицию")
	assert.Equal(t, int64(0), tenderID)
}

func TestImportFullTender_UpsertTenderRawDataFails(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN everything succeeded but UpsertTenderRawData fails
	payload := makePayloadWithOneLot()
	rawJSON := []byte(`{}`)
	lotDBID := int64(150)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			setupCoreTenderExpectations(mock)
			// UpsertLot
			mock.ExpectQuery("INSERT INTO lots").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(lotDBID, "lot-1", "Лот №1 — Отделочные работы", nil, int64(100), now, now))
			// Baseline proposal (full flow)
			proposalDBID := setupBaselineProposalExpectations(mock, lotDBID)
			setupPositionExpectations(mock, proposalDBID)
			setupSummaryExpectations(mock, proposalDBID)
			// UpsertTenderRawData fails
			mock.ExpectQuery("INSERT INTO tender_raw_data").
				WillReturnError(errors.New("jsonb parse error"))
		}),
	)

	// WHEN
	tenderID, _, _, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tender_raw_data")
	assert.Equal(t, int64(0), tenderID)
}

func TestImportFullTender_UnitCreationFails(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN unit of measurement creation fails
	payload := makePayloadWithOneLot()
	rawJSON := []byte(`{}`)
	lotDBID := int64(150)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			setupCoreTenderExpectations(mock)
			mock.ExpectQuery("INSERT INTO lots").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(lotDBID, "lot-1", "Лот №1 — Отделочные работы", nil, int64(100), now, now))
			setupBaselineProposalExpectations(mock, lotDBID)
			// GetUnit → not found
			mock.ExpectQuery("SELECT .+ FROM units_of_measurement WHERE normalized_name").
				WithArgs("м2").
				WillReturnError(sql.ErrNoRows)
			// CreateUnit → fails
			mock.ExpectQuery("INSERT INTO units_of_measurement").
				WillReturnError(errors.New("insert conflict"))
			// Retry lookup also fails (race condition path)
			mock.ExpectQuery("SELECT .+ FROM units_of_measurement WHERE normalized_name").
				WithArgs("м2").
				WillReturnError(sql.ErrNoRows)
		}),
	)

	// WHEN
	tenderID, _, _, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "единицы измерения")
	assert.Equal(t, int64(0), tenderID)
}

func TestImportFullTender_CatalogPositionCreationFails(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN catalog position creation fails
	payload := makePayloadWithOneLot()
	rawJSON := []byte(`{}`)
	lotDBID := int64(150)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			setupCoreTenderExpectations(mock)
			mock.ExpectQuery("INSERT INTO lots").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(lotDBID, "lot-1", "Лот №1 — Отделочные работы", nil, int64(100), now, now))
			setupBaselineProposalExpectations(mock, lotDBID)
			// Unit found
			mock.ExpectQuery("SELECT .+ FROM units_of_measurement WHERE normalized_name").
				WithArgs("м2").
				WillReturnRows(sqlmock.NewRows(unitColumns).
					AddRow(int64(10), "м2", sql.NullString{String: "м2", Valid: true}, sql.NullString{}, now, now))
			// Catalog position not found
			mock.ExpectQuery("SELECT .+ FROM catalog_positions").
				WillReturnError(sql.ErrNoRows)
			// CreateCatalogPosition fails
			mock.ExpectQuery("INSERT INTO catalog_positions").
				WillReturnError(errors.New("catalog insert error"))
		}),
	)

	// WHEN
	tenderID, _, _, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "позицию каталога")
	assert.Equal(t, int64(0), tenderID)
}

func TestImportFullTender_MatchingCacheDBError(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN GetMatchingCache returns an unexpected DB error (not sql.ErrNoRows)
	payload := makePayloadWithOneLot()
	rawJSON := []byte(`{}`)
	lotDBID := int64(150)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			setupCoreTenderExpectations(mock)
			mock.ExpectQuery("INSERT INTO lots").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(lotDBID, "lot-1", "Лот №1 — Отделочные работы", nil, int64(100), now, now))
			setupBaselineProposalExpectations(mock, lotDBID)
			// Unit found
			mock.ExpectQuery("SELECT .+ FROM units_of_measurement WHERE normalized_name").
				WithArgs("м2").
				WillReturnRows(sqlmock.NewRows(unitColumns).
					AddRow(int64(10), "м2", sql.NullString{String: "м2", Valid: true}, sql.NullString{}, now, now))
			// Catalog position found (kind=POSITION)
			mock.ExpectQuery("SELECT .+ FROM catalog_positions").
				WillReturnRows(sqlmock.NewRows(catalogPosColumns).
					AddRow(int64(300), "устройство полов", sql.NullString{String: "Устройство полов", Valid: true}, nil, "POSITION", "active", sql.NullInt64{Int64: 10, Valid: true}, now, now, nil))
			// GetMatchingCache returns real DB error
			mock.ExpectQuery("SELECT .+ FROM matching_cache").
				WillReturnError(errors.New("connection lost"))
		}),
	)

	// WHEN
	tenderID, _, _, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "matching_cache")
	assert.Equal(t, int64(0), tenderID)
}

func TestImportFullTender_SummaryLineFails(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN position succeeds but summary line upsert fails
	payload := makePayloadWithOneLot()
	rawJSON := []byte(`{}`)
	lotDBID := int64(150)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			setupCoreTenderExpectations(mock)
			mock.ExpectQuery("INSERT INTO lots").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(lotDBID, "lot-1", "Лот №1 — Отделочные работы", nil, int64(100), now, now))
			proposalDBID := setupBaselineProposalExpectations(mock, lotDBID)
			setupPositionExpectations(mock, proposalDBID)
			// Summary line fails
			mock.ExpectQuery("INSERT INTO proposal_summary_lines").
				WillReturnError(errors.New("summary insert error"))
		}),
	)

	// WHEN
	tenderID, _, _, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "строки итога")
	assert.Equal(t, int64(0), tenderID)
}

func TestImportFullTender_AdditionalInfoDeleteFails(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN contractor proposal with additional info, but delete old info fails
	infoVal := "test"
	payload := makeMinimalPayload()
	payload.LotsData = map[string]api_models.Lot{
		"lot-1": {
			LotTitle: "Лот",
			BaseLineProposal: api_models.ContractorProposalDetails{
				Title:           "Initiator",
				ContractorItems: api_models.ContractorItemsContainer{},
			},
			ProposalData: map[string]api_models.ContractorProposalDetails{
				"c1": {
					Title:                "Подрядчик",
					Inn:                  "1111111111",
					Address:              "Адрес",
					Accreditation:        "Да",
					ContractorCoordinate: "B2",
					ContractorItems:      api_models.ContractorItemsContainer{},
					AdditionalInfo:       map[string]*string{"key1": &infoVal},
				},
			},
		},
	}

	rawJSON := []byte(`{}`)
	lotDBID := int64(150)
	contractorProposalID := int64(201)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			setupCoreTenderExpectations(mock)
			mock.ExpectQuery("INSERT INTO lots").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(lotDBID, "lot-1", "Лот", nil, int64(100), now, now))
			// Baseline
			mock.ExpectQuery("SELECT .+ FROM contractors WHERE inn").
				WithArgs("0000000000").
				WillReturnRows(sqlmock.NewRows(contractorColumns).
					AddRow(int64(50), "Initiator", "0000000000", "N/A", "N/A", now, now))
			mock.ExpectQuery("INSERT INTO proposals").
				WillReturnRows(sqlmock.NewRows(proposalColumns).
					AddRow(int64(200), lotDBID, int64(50), true, nil, nil, nil, now, now))
			// Contractor
			mock.ExpectQuery("SELECT .+ FROM contractors WHERE inn").
				WithArgs("1111111111").
				WillReturnError(sql.ErrNoRows)
			mock.ExpectQuery("INSERT INTO contractors").
				WillReturnRows(sqlmock.NewRows(contractorColumns).
					AddRow(int64(51), "Подрядчик", "1111111111", "Адрес", "Да", now, now))
			mock.ExpectQuery("INSERT INTO proposals").
				WillReturnRows(sqlmock.NewRows(proposalColumns).
					AddRow(contractorProposalID, lotDBID, int64(51), false, sql.NullString{String: "B2", Valid: true}, nil, nil, now, now))
			// DeleteAllAdditionalInfoForProposal fails
			mock.ExpectExec("DELETE FROM proposal_additional_info").
				WithArgs(contractorProposalID).
				WillReturnError(errors.New("cannot delete"))
		}),
	)

	// WHEN
	tenderID, _, _, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "дополнительной информации")
	assert.Equal(t, int64(0), tenderID)
}

// ============================================================================
// Position with Header kind (skips matching_cache)
// ============================================================================

func TestImportFullTender_HeaderPosition_SkipsCache(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN a position that is a chapter header (isChapter=true)
	jobTitleNorm := "глава 1 общестроительные работы"
	payload := makeMinimalPayload()
	payload.LotsData = map[string]api_models.Lot{
		"lot-1": {
			LotTitle: "Лот №1",
			BaseLineProposal: api_models.ContractorProposalDetails{
				Title: "Initiator",
				ContractorItems: api_models.ContractorItemsContainer{
					Positions: map[string]api_models.PositionItem{
						"h-1": {
							Number:             "1",
							JobTitle:           "Глава 1 Общестроительные работы",
							JobTitleNormalized: &jobTitleNorm,
							IsChapter:          true,
						},
					},
				},
			},
			ProposalData: map[string]api_models.ContractorProposalDetails{},
		},
	}

	rawJSON := []byte(`{}`)
	lotDBID := int64(150)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			setupCoreTenderExpectations(mock)
			mock.ExpectQuery("INSERT INTO lots").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(lotDBID, "lot-1", "Лот №1", nil, int64(100), now, now))
			setupBaselineProposalExpectations(mock, lotDBID)
			// No unit for header (unit is nil)
			// GetCatalogPositionByTitleAndUnit → not found
			mock.ExpectQuery("SELECT .+ FROM catalog_positions").
				WillReturnError(sql.ErrNoRows)
			// CreateCatalogPosition with kind=HEADER
			mock.ExpectQuery("INSERT INTO catalog_positions").
				WillReturnRows(sqlmock.NewRows(catalogPosColumns).
					AddRow(int64(300), "глава 1 общестроительные работы", sql.NullString{String: "Глава 1 Общестроительные работы", Valid: true}, nil, "HEADER", "pending_indexing", sql.NullInt64{}, now, now, nil))
			// For HEADER kind: no GetMatchingCache call (skipped)
			// Directly UpsertPositionItem with catalogPositionID set
			mock.ExpectQuery("INSERT INTO position_items").
				WillReturnRows(sqlmock.NewRows(positionItemColumns).
					AddRow(
						int64(400), int64(200), sql.NullInt64{Int64: 300, Valid: true}, "h-1",
						sql.NullString{}, sql.NullString{}, sql.NullString{String: "1", Valid: true},
						sql.NullString{}, "Глава 1 Общестроительные работы", sql.NullInt64{},
						sql.NullString{}, sql.NullString{}, sql.NullString{},
						sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{},
						sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{},
						sql.NullString{}, true, sql.NullString{},
						now, now,
					))
			setupRawDataExpectations(mock, 100)
		}),
	)

	// WHEN
	tenderID, lotIDs, anyNewPending, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.NoError(t, err)
	assert.Equal(t, int64(100), tenderID)
	assert.Equal(t, map[string]int64{"lot-1": lotDBID}, lotIDs)
	assert.False(t, anyNewPending)
	// HEADER positions → isNewPendingItem = false
}

// ============================================================================
// Pure Mapper Functions TESTS
// ============================================================================

func TestMapApiPositionToDbParams(t *testing.T) {
	// GIVEN a fully populated API PositionItem
	quantity := 10.5
	suggestedQty := 12.0
	totalOrgCost := 999.99
	unitMaterials := 50.0
	unitWorks := 30.0
	unitIndirect := 10.0
	unitTotal := 90.0
	totalMaterials := 500.0
	totalWorks := 300.0
	totalIndirect := 100.0
	totalTotal := 900.0
	comment := "организатор"
	commentContractor := "подрядчик"
	chapterNum := "3"
	chapterRef := "ch-1"

	posAPI := api_models.PositionItem{
		Number:                        "42",
		ChapterNumber:                 &chapterNum,
		JobTitle:                      "Монтаж конструкций",
		CommentOrganizer:              &comment,
		CommentContractor:             &commentContractor,
		Quantity:                      &quantity,
		SuggestedQuantity:             &suggestedQty,
		TotalCostForOrganizerQuantity: &totalOrgCost,
		UnitCost: api_models.Cost{
			Materials:     &unitMaterials,
			Works:         &unitWorks,
			IndirectCosts: &unitIndirect,
			Total:         &unitTotal,
		},
		TotalCost: api_models.Cost{
			Materials:     &totalMaterials,
			Works:         &totalWorks,
			IndirectCosts: &totalIndirect,
			Total:         &totalTotal,
		},
		IsChapter:  false,
		ChapterRef: &chapterRef,
	}

	proposalID := int64(100)
	positionKey := "pos-42"
	catalogPosID := sql.NullInt64{Int64: 999, Valid: true}
	unitID := sql.NullInt64{Int64: 5, Valid: true}

	// WHEN
	result := mapApiPositionToDbParams(proposalID, positionKey, catalogPosID, unitID, posAPI)

	// THEN
	assert.Equal(t, proposalID, result.ProposalID)
	assert.Equal(t, positionKey, result.PositionKeyInProposal)
	assert.Equal(t, catalogPosID, result.CatalogPositionID)
	assert.Equal(t, unitID, result.UnitID)
	assert.Equal(t, "Монтаж конструкций", result.JobTitleInProposal)
	assert.Equal(t, false, result.IsChapter)

	// Verify nullable string fields are populated
	assert.True(t, result.ItemNumberInProposal.Valid)
	assert.Equal(t, "42", result.ItemNumberInProposal.String)
	assert.True(t, result.ChapterNumberInProposal.Valid)
	assert.Equal(t, "3", result.ChapterNumberInProposal.String)
	assert.True(t, result.CommentOrganazier.Valid)
	assert.Equal(t, "организатор", result.CommentOrganazier.String)
	assert.True(t, result.CommentContractor.Valid)
	assert.Equal(t, "подрядчик", result.CommentContractor.String)
	assert.True(t, result.ChapterRefInProposal.Valid)
	assert.Equal(t, "ch-1", result.ChapterRefInProposal.String)

	// Numeric fields converted to NullString
	assert.True(t, result.Quantity.Valid)
	assert.True(t, result.SuggestedQuantity.Valid)
	assert.True(t, result.TotalCostForOrganizerQuantity.Valid)
	assert.True(t, result.UnitCostMaterials.Valid)
	assert.True(t, result.UnitCostWorks.Valid)
	assert.True(t, result.UnitCostIndirectCosts.Valid)
	assert.True(t, result.UnitCostTotal.Valid)
	assert.True(t, result.TotalCostMaterials.Valid)
	assert.True(t, result.TotalCostWorks.Valid)
	assert.True(t, result.TotalCostIndirectCosts.Valid)
	assert.True(t, result.TotalCostTotal.Valid)
}

func TestMapApiPositionToDbParams_NilFields(t *testing.T) {
	// GIVEN a PositionItem with all nil optional fields
	posAPI := api_models.PositionItem{
		Number:   "1",
		JobTitle: "Простая работа",
	}

	proposalID := int64(100)
	positionKey := "pos-1"
	catalogPosID := sql.NullInt64{Valid: false}
	unitID := sql.NullInt64{Valid: false}

	// WHEN
	result := mapApiPositionToDbParams(proposalID, positionKey, catalogPosID, unitID, posAPI)

	// THEN
	assert.Equal(t, proposalID, result.ProposalID)
	assert.Equal(t, positionKey, result.PositionKeyInProposal)
	assert.False(t, result.CatalogPositionID.Valid)
	assert.False(t, result.UnitID.Valid)
	assert.Equal(t, "Простая работа", result.JobTitleInProposal)

	// All nullable fields should be invalid
	assert.False(t, result.CommentOrganazier.Valid)
	assert.False(t, result.CommentContractor.Valid)
	assert.False(t, result.ChapterNumberInProposal.Valid)
	assert.False(t, result.ChapterRefInProposal.Valid)
	assert.False(t, result.Quantity.Valid)
	assert.False(t, result.SuggestedQuantity.Valid)
	assert.False(t, result.TotalCostTotal.Valid)
	assert.False(t, result.UnitCostMaterials.Valid)
}

func TestMapApiSummaryToDbParams(t *testing.T) {
	// GIVEN a fully populated SummaryLine
	materials := 1000.0
	works := 2000.0
	indirect := 500.0
	total := 3500.0

	sumAPI := api_models.SummaryLine{
		JobTitle: "Итого по разделу",
		TotalCost: api_models.Cost{
			Materials:     &materials,
			Works:         &works,
			IndirectCosts: &indirect,
			Total:         &total,
		},
	}

	proposalID := int64(100)
	summaryKey := "sum-totals"

	// WHEN
	result := mapApiSummaryToDbParams(proposalID, summaryKey, sumAPI)

	// THEN
	assert.Equal(t, proposalID, result.ProposalID)
	assert.Equal(t, summaryKey, result.SummaryKey)
	assert.Equal(t, "Итого по разделу", result.JobTitle)
	assert.True(t, result.MaterialsCost.Valid)
	assert.True(t, result.WorksCost.Valid)
	assert.True(t, result.IndirectCostsCost.Valid)
	assert.True(t, result.TotalCost.Valid)
}

func TestMapApiSummaryToDbParams_NilCosts(t *testing.T) {
	// GIVEN a SummaryLine with nil cost fields
	sumAPI := api_models.SummaryLine{
		JobTitle:  "Пустой итог",
		TotalCost: api_models.Cost{},
	}

	// WHEN
	result := mapApiSummaryToDbParams(int64(1), "sum-empty", sumAPI)

	// THEN
	assert.Equal(t, "Пустой итог", result.JobTitle)
	assert.False(t, result.MaterialsCost.Valid)
	assert.False(t, result.WorksCost.Valid)
	assert.False(t, result.IndirectCostsCost.Valid)
	assert.False(t, result.TotalCost.Valid)
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestImportFullTender_PositionWithEmptyJobTitle(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN a position with empty job_title (GetOrCreateCatalogPosition returns zero ID)
	payload := makeMinimalPayload()
	payload.LotsData = map[string]api_models.Lot{
		"lot-1": {
			LotTitle: "Лот №1",
			BaseLineProposal: api_models.ContractorProposalDetails{
				Title: "Initiator",
				ContractorItems: api_models.ContractorItemsContainer{
					Positions: map[string]api_models.PositionItem{
						"pos-empty": {
							Number:   "1",
							JobTitle: "",
						},
					},
				},
			},
			ProposalData: map[string]api_models.ContractorProposalDetails{},
		},
	}

	rawJSON := []byte(`{}`)
	lotDBID := int64(150)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			setupCoreTenderExpectations(mock)
			mock.ExpectQuery("INSERT INTO lots").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(lotDBID, "lot-1", "Лот №1", nil, int64(100), now, now))
			setupBaselineProposalExpectations(mock, lotDBID)
			// Empty job_title → GetOrCreateCatalogPosition returns zero ID
			// processSinglePosition skips (no DB calls for catalog/position)
			// No summary
			setupRawDataExpectations(mock, 100)
		}),
	)

	// WHEN
	tenderID, lotIDs, _, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.NoError(t, err)
	assert.Equal(t, int64(100), tenderID)
	assert.Equal(t, map[string]int64{"lot-1": lotDBID}, lotIDs)
}

func TestImportFullTender_NilPositionsAndSummary(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN a proposal with nil Positions and nil Summary
	payload := makeMinimalPayload()
	payload.LotsData = map[string]api_models.Lot{
		"lot-1": {
			LotTitle: "Лот без позиций",
			BaseLineProposal: api_models.ContractorProposalDetails{
				Title:           "Initiator",
				ContractorItems: api_models.ContractorItemsContainer{}, // nil maps
			},
			ProposalData: map[string]api_models.ContractorProposalDetails{},
		},
	}

	rawJSON := []byte(`{}`)
	lotDBID := int64(150)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			setupCoreTenderExpectations(mock)
			mock.ExpectQuery("INSERT INTO lots").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(lotDBID, "lot-1", "Лот без позиций", nil, int64(100), now, now))
			setupBaselineProposalExpectations(mock, lotDBID)
			// No positions or summary to process
			setupRawDataExpectations(mock, 100)
		}),
	)

	// WHEN
	tenderID, lotIDs, anyNewPending, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.NoError(t, err)
	assert.Equal(t, int64(100), tenderID)
	assert.Equal(t, map[string]int64{"lot-1": lotDBID}, lotIDs)
	assert.False(t, anyNewPending)
}

func TestImportFullTender_ExistingEntitiesReused(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN object and executor already exist in DB (no creation needed)
	payload := makeMinimalPayload()
	rawJSON := []byte(`{}`)

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// Object already exists
			mock.ExpectQuery("SELECT .+ FROM objects WHERE title").
				WithArgs("Строительство").
				WillReturnRows(sqlmock.NewRows(objectColumns).
					AddRow(int64(1), "Строительство", "г. Москва, ул. Тестовая, 1", now, now))
			// Executor already exists
			mock.ExpectQuery("SELECT .+ FROM executors WHERE name").
				WithArgs("Иванов И.И.").
				WillReturnRows(sqlmock.NewRows(executorColumns).
					AddRow(int64(2), "Иванов И.И.", "+7-999-000-0000", now, now))
			// UpsertTender
			mock.ExpectQuery("INSERT INTO tenders").
				WillReturnRows(sqlmock.NewRows(tenderColumns).
					AddRow(int64(100), "ETP-TEST-001", "Тестовый тендер", nil, int64(1), int64(2), nil, now, now))
			setupRawDataExpectations(mock, 100)
		}),
	)

	// WHEN
	tenderID, _, _, err := service.ImportFullTender(ctx, payload, rawJSON)

	// THEN
	require.NoError(t, err)
	assert.Equal(t, int64(100), tenderID)
}
