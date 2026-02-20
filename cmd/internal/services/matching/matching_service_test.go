// Purpose: Защита бизнес-логики матчинга позиций — корректное сопоставление
// position_items с catalog_positions, валидация входных параметров,
// транзакционная целостность и обработка ошибок БД.
package matching

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/apierrors"
	"github.com/zhukovvlad/tenders-go/cmd/internal/testutil"
)

/*
BEHAVIORAL SCENARIOS FOR MATCHING SERVICE (Unit Tests)

What user problems does this protect us from?
================================================================================
1. Data integrity — unmatched positions must be correctly mapped from DB rows
   to API responses with rich context strings (breadcrumbs)
2. Input validation — invalid limit (<=0) must be rejected with ValidationError
3. Limit capping — excessive limit values must be capped to MaxUnmatchedPositionsLimit
4. Error propagation — DB errors must be properly wrapped and returned
5. Transaction safety — MatchPosition updates position_items AND matching_cache
   atomically inside ExecTx
6. Default values — norm_version defaults to 1 when not provided
7. Context building — breadcrumbs (full_parent_path) vs root positions handled correctly

GIVEN / WHEN / THEN Scenarios:
================================================================================

SCENARIO 1: GetUnmatchedPositions
- GIVEN valid limit and unmatched positions in DB
  WHEN GetUnmatchedPositions is called
  THEN positions are returned with correctly built rich_context_string

- GIVEN positions with breadcrumbs (full_parent_path present)
  WHEN GetUnmatchedPositions is called
  THEN rich_context_string includes "Раздел: ... | Позиция: ..."

- GIVEN positions without breadcrumbs (root positions)
  WHEN GetUnmatchedPositions is called
  THEN rich_context_string is "Позиция: ..."

- GIVEN positions with draft_catalog_id (pending_indexing)
  WHEN GetUnmatchedPositions is called
  THEN DraftCatalogID is set in response

- GIVEN positions without draft_catalog_id (NULL)
  WHEN GetUnmatchedPositions is called
  THEN DraftCatalogID is nil in response

- GIVEN zero or negative limit
  WHEN GetUnmatchedPositions is called
  THEN ValidationError is returned without DB call

- GIVEN limit exceeding MaxUnmatchedPositionsLimit
  WHEN GetUnmatchedPositions is called
  THEN limit is capped and DB is called with MaxUnmatchedPositionsLimit

- GIVEN empty result from DB (no unmatched positions)
  WHEN GetUnmatchedPositions is called
  THEN empty slice is returned (not nil)

- GIVEN a DB error
  WHEN GetUnmatchedPositions is called
  THEN wrapped DB error is returned

SCENARIO 2: MatchPosition
- GIVEN valid request with position_item_id, catalog_position_id, and hash
  WHEN MatchPosition is called
  THEN SetCatalogPositionID and UpsertMatchingCache are called in transaction

- GIVEN valid request with norm_version = 0
  WHEN MatchPosition is called
  THEN norm_version defaults to 1

- GIVEN valid request with explicit norm_version = 3
  WHEN MatchPosition is called
  THEN norm_version 3 is used

- GIVEN SetCatalogPositionID fails inside transaction
  WHEN MatchPosition is called
  THEN transaction error is returned

- GIVEN GetPositionItemByID fails inside transaction
  WHEN MatchPosition is called
  THEN UpsertMatchingCache still proceeds with empty job_title_text

- GIVEN UpsertMatchingCache fails inside transaction
  WHEN MatchPosition is called
  THEN transaction error is returned

- GIVEN ExecTx itself fails (e.g., tx begin error)
  WHEN MatchPosition is called
  THEN error is returned
*/

// =============================================================================
// TEST HELPERS
// =============================================================================

// setupTestService creates a MatchingService with mock store for unit testing.
func setupTestService(t *testing.T) (*MatchingService, *db.MockStore) {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockStore := db.NewMockStore(ctrl)
	logger := testutil.NewMockLogger()

	service := &MatchingService{
		store:  mockStore,
		logger: logger,
	}

	return service, mockStore
}

// Helper: create a mock DB + Queries for use inside ExecTx DoAndReturn.
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

// execTxDoAndReturn returns a DoAndReturn function that executes
// the ExecTx callback with a sqlmock-backed *Queries.
func execTxDoAndReturn(t *testing.T, setupFn func(mock sqlmock.Sqlmock)) func(ctx context.Context, fn func(*db.Queries) error) error {
	t.Helper()
	return func(ctx context.Context, fn func(*db.Queries) error) error {
		mock, q, cleanup := newMockQueries(t)
		defer cleanup()
		setupFn(mock)
		return fn(q)
	}
}

// Helper: SQL column names for position_items (used with sqlmock)
var positionItemColumns = []string{
	"id", "proposal_id", "catalog_position_id", "position_key_in_proposal",
	"comment_organazier", "comment_contractor", "item_number_in_proposal",
	"chapter_number_in_proposal", "job_title_in_proposal", "unit_id",
	"quantity", "suggested_quantity", "total_cost_for_organizer_quantity",
	"unit_cost_materials", "unit_cost_works", "unit_cost_indirect_costs",
	"unit_cost_total", "total_cost_materials", "total_cost_works",
	"total_cost_indirect_costs", "total_cost_total", "deviation_from_baseline_cost",
	"is_chapter", "chapter_ref_in_proposal", "created_at", "updated_at",
}

// Helper: create a sqlmock row for position_items with given id and job_title
func positionItemRow(id int64, jobTitle string) []driver.Value {
	now := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	return []driver.Value{
		id,                                       // id
		int64(1),                                 // proposal_id
		sql.NullInt64{Int64: 0, Valid: false},    // catalog_position_id (NULL)
		"pos-key-1",                              // position_key_in_proposal
		sql.NullString{String: "", Valid: false}, // comment_organazier
		sql.NullString{String: "", Valid: false}, // comment_contractor
		sql.NullString{String: "1.1", Valid: true}, // item_number_in_proposal
		sql.NullString{String: "", Valid: false},   // chapter_number_in_proposal
		jobTitle,                                   // job_title_in_proposal
		sql.NullInt64{Int64: 0, Valid: false},      // unit_id
		sql.NullString{String: "10", Valid: true},  // quantity
		sql.NullString{String: "", Valid: false},   // suggested_quantity
		sql.NullString{String: "", Valid: false},   // total_cost_for_organizer_quantity
		sql.NullString{String: "", Valid: false},   // unit_cost_materials
		sql.NullString{String: "", Valid: false},   // unit_cost_works
		sql.NullString{String: "", Valid: false},   // unit_cost_indirect_costs
		sql.NullString{String: "", Valid: false},   // unit_cost_total
		sql.NullString{String: "", Valid: false},   // total_cost_materials
		sql.NullString{String: "", Valid: false},   // total_cost_works
		sql.NullString{String: "", Valid: false},   // total_cost_indirect_costs
		sql.NullString{String: "", Valid: false},   // total_cost_total
		sql.NullString{String: "", Valid: false},   // deviation_from_baseline_cost
		false,                                      // is_chapter
		sql.NullString{String: "", Valid: false},   // chapter_ref_in_proposal
		now,                                        // created_at
		now,                                        // updated_at
	}
}

// =============================================================================
// NewMatchingService TESTS
// =============================================================================

func TestNewMatchingService(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := db.NewMockStore(ctrl)
	logger := testutil.NewMockLogger()

	service := NewMatchingService(mockStore, logger)

	require.NotNil(t, service)
	assert.Equal(t, mockStore, service.store)
	assert.Equal(t, logger, service.logger)
}

// =============================================================================
// GetUnmatchedPositions TESTS
// =============================================================================

func TestGetUnmatchedPositions_Success_WithBreadcrumbs(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN positions with breadcrumbs (full_parent_path present)
	dbRows := []db.GetUnmatchedPositionsRow{
		{
			PositionItemID:     100,
			JobTitleInProposal: "Монтаж электропроводки",
			FullParentPath:     "Электромонтажные работы | Внутренние работы",
			DraftCatalogID:     sql.NullInt64{Int64: 0, Valid: false},
			StandardJobTitle:   sql.NullString{String: "", Valid: false},
		},
	}

	mockStore.EXPECT().
		GetUnmatchedPositions(ctx, int32(10)).
		Return(dbRows, nil)

	// WHEN
	result, err := service.GetUnmatchedPositions(ctx, 10)

	// THEN
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, int64(100), result[0].PositionItemID)
	assert.Equal(t, "Монтаж электропроводки", result[0].JobTitleInProposal)
	assert.Equal(t, "Раздел: Электромонтажные работы | Внутренние работы | Позиция: Монтаж электропроводки", result[0].RichContextString)
	assert.Nil(t, result[0].DraftCatalogID)
	assert.Empty(t, result[0].StandardJobTitle)
}

func TestGetUnmatchedPositions_Success_WithoutBreadcrumbs(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN positions without breadcrumbs (root positions, empty full_parent_path)
	dbRows := []db.GetUnmatchedPositionsRow{
		{
			PositionItemID:     200,
			JobTitleInProposal: "Устройство фундамента",
			FullParentPath:     "",
			DraftCatalogID:     sql.NullInt64{Int64: 0, Valid: false},
			StandardJobTitle:   sql.NullString{String: "", Valid: false},
		},
	}

	mockStore.EXPECT().
		GetUnmatchedPositions(ctx, int32(5)).
		Return(dbRows, nil)

	// WHEN
	result, err := service.GetUnmatchedPositions(ctx, 5)

	// THEN
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "Позиция: Устройство фундамента", result[0].RichContextString)
}

func TestGetUnmatchedPositions_Success_WithDraftCatalogID(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN position with draft_catalog_id (pending_indexing catalog entry)
	draftID := int64(42)
	dbRows := []db.GetUnmatchedPositionsRow{
		{
			PositionItemID:     300,
			JobTitleInProposal: "Штукатурка стен",
			FullParentPath:     "Отделочные работы",
			DraftCatalogID:     sql.NullInt64{Int64: draftID, Valid: true},
			StandardJobTitle:   sql.NullString{String: "штукатурка стена", Valid: true},
		},
	}

	mockStore.EXPECT().
		GetUnmatchedPositions(ctx, int32(10)).
		Return(dbRows, nil)

	// WHEN
	result, err := service.GetUnmatchedPositions(ctx, 10)

	// THEN
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.NotNil(t, result[0].DraftCatalogID)
	assert.Equal(t, draftID, *result[0].DraftCatalogID)
	assert.Equal(t, "штукатурка стена", result[0].StandardJobTitle)
}

func TestGetUnmatchedPositions_Success_MultiplePositions(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN multiple positions: one with breadcrumbs, one without, one with draft
	draftID := int64(99)
	dbRows := []db.GetUnmatchedPositionsRow{
		{
			PositionItemID:     1,
			JobTitleInProposal: "Монтаж труб",
			FullParentPath:     "Сантехнические работы",
			DraftCatalogID:     sql.NullInt64{Valid: false},
			StandardJobTitle:   sql.NullString{Valid: false},
		},
		{
			PositionItemID:     2,
			JobTitleInProposal: "Покраска стен",
			FullParentPath:     "",
			DraftCatalogID:     sql.NullInt64{Valid: false},
			StandardJobTitle:   sql.NullString{Valid: false},
		},
		{
			PositionItemID:     3,
			JobTitleInProposal: "Укладка плитки",
			FullParentPath:     "Отделка | Полы",
			DraftCatalogID:     sql.NullInt64{Int64: draftID, Valid: true},
			StandardJobTitle:   sql.NullString{String: "укладка плитка", Valid: true},
		},
	}

	mockStore.EXPECT().
		GetUnmatchedPositions(ctx, int32(50)).
		Return(dbRows, nil)

	// WHEN
	result, err := service.GetUnmatchedPositions(ctx, 50)

	// THEN
	require.NoError(t, err)
	require.Len(t, result, 3)

	// Position with breadcrumbs
	assert.Equal(t, "Раздел: Сантехнические работы | Позиция: Монтаж труб", result[0].RichContextString)
	assert.Nil(t, result[0].DraftCatalogID)

	// Root position (no breadcrumbs)
	assert.Equal(t, "Позиция: Покраска стен", result[1].RichContextString)
	assert.Nil(t, result[1].DraftCatalogID)

	// Position with draft catalog
	assert.Equal(t, "Раздел: Отделка | Полы | Позиция: Укладка плитки", result[2].RichContextString)
	require.NotNil(t, result[2].DraftCatalogID)
	assert.Equal(t, draftID, *result[2].DraftCatalogID)
	assert.Equal(t, "укладка плитка", result[2].StandardJobTitle)
}

func TestGetUnmatchedPositions_EmptyResult(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN no unmatched positions in DB
	mockStore.EXPECT().
		GetUnmatchedPositions(ctx, int32(10)).
		Return([]db.GetUnmatchedPositionsRow{}, nil)

	// WHEN
	result, err := service.GetUnmatchedPositions(ctx, 10)

	// THEN
	require.NoError(t, err)
	require.NotNil(t, result, "should return empty slice, not nil")
	assert.Len(t, result, 0)
}

func TestGetUnmatchedPositions_ValidationError_ZeroLimit(t *testing.T) {
	service, _ := setupTestService(t)
	ctx := context.Background()

	// GIVEN zero limit
	// WHEN
	result, err := service.GetUnmatchedPositions(ctx, 0)

	// THEN — ValidationError, no DB call
	require.Error(t, err)
	assert.Nil(t, result)

	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr), "expected ValidationError, got: %T", err)
}

func TestGetUnmatchedPositions_ValidationError_NegativeLimit(t *testing.T) {
	service, _ := setupTestService(t)
	ctx := context.Background()

	// GIVEN negative limit
	// WHEN
	result, err := service.GetUnmatchedPositions(ctx, -5)

	// THEN — ValidationError, no DB call
	require.Error(t, err)
	assert.Nil(t, result)

	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr), "expected ValidationError, got: %T", err)
}

func TestGetUnmatchedPositions_LimitCappedToMax(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN limit exceeding MaxUnmatchedPositionsLimit (1000)
	excessiveLimit := int32(5000)

	// THEN DB is called with capped limit
	mockStore.EXPECT().
		GetUnmatchedPositions(ctx, int32(MaxUnmatchedPositionsLimit)).
		Return([]db.GetUnmatchedPositionsRow{}, nil)

	// WHEN
	result, err := service.GetUnmatchedPositions(ctx, excessiveLimit)

	// THEN
	require.NoError(t, err)
	assert.Len(t, result, 0)
}

func TestGetUnmatchedPositions_LimitExactlyAtMax(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN limit exactly at MaxUnmatchedPositionsLimit
	mockStore.EXPECT().
		GetUnmatchedPositions(ctx, int32(MaxUnmatchedPositionsLimit)).
		Return([]db.GetUnmatchedPositionsRow{}, nil)

	// WHEN
	result, err := service.GetUnmatchedPositions(ctx, MaxUnmatchedPositionsLimit)

	// THEN — no capping, DB called with exact value
	require.NoError(t, err)
	assert.Len(t, result, 0)
}

func TestGetUnmatchedPositions_DBError(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN DB returns an error
	dbErr := errors.New("connection refused")
	mockStore.EXPECT().
		GetUnmatchedPositions(ctx, int32(10)).
		Return(nil, dbErr)

	// WHEN
	result, err := service.GetUnmatchedPositions(ctx, 10)

	// THEN — wrapped DB error
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "ошибка БД")
	assert.True(t, errors.Is(err, dbErr), "original error should be wrapped")
}

// =============================================================================
// MatchPosition TESTS
// =============================================================================

func TestMatchPosition_Success(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN valid match request
	req := api_models.MatchPositionRequest{
		PositionItemID:    100,
		CatalogPositionID: 42,
		Hash:              "abc123hash",
		NormVersion:       2,
	}

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// SetCatalogPositionID succeeds
			mock.ExpectExec("UPDATE position_items").
				WithArgs(sql.NullInt64{Int64: 42, Valid: true}, int64(100)).
				WillReturnResult(sqlmock.NewResult(0, 1))

			// GetPositionItemByID returns position
			mock.ExpectQuery("SELECT .+ FROM position_items WHERE id").
				WithArgs(int64(100)).
				WillReturnRows(sqlmock.NewRows(positionItemColumns).
					AddRow(positionItemRow(100, "Монтаж электропроводки")...))

			// UpsertMatchingCache succeeds
			mock.ExpectExec("INSERT INTO matching_cache").
				WithArgs(
					"abc123hash", // job_title_hash
					int16(2),     // norm_version
					sql.NullString{String: "Монтаж электропроводки", Valid: true}, // job_title_text
					int64(42),        // catalog_position_id
					sqlmock.AnyArg(), // expires_at (time-dependent)
				).
				WillReturnResult(sqlmock.NewResult(0, 1))
		}),
	)

	// WHEN
	err := service.MatchPosition(ctx, req)

	// THEN
	assert.NoError(t, err)
}

func TestMatchPosition_DefaultNormVersion(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN request with norm_version = 0 (should default to 1)
	req := api_models.MatchPositionRequest{
		PositionItemID:    100,
		CatalogPositionID: 42,
		Hash:              "hash456",
		NormVersion:       0,
	}

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectExec("UPDATE position_items").
				WithArgs(sql.NullInt64{Int64: 42, Valid: true}, int64(100)).
				WillReturnResult(sqlmock.NewResult(0, 1))

			mock.ExpectQuery("SELECT .+ FROM position_items WHERE id").
				WithArgs(int64(100)).
				WillReturnRows(sqlmock.NewRows(positionItemColumns).
					AddRow(positionItemRow(100, "Покраска стен")...))

			// norm_version should be 1 (default)
			mock.ExpectExec("INSERT INTO matching_cache").
				WithArgs(
					"hash456",
					int16(1), // default norm_version
					sql.NullString{String: "Покраска стен", Valid: true},
					int64(42),
					sqlmock.AnyArg(),
				).
				WillReturnResult(sqlmock.NewResult(0, 1))
		}),
	)

	// WHEN
	err := service.MatchPosition(ctx, req)

	// THEN
	assert.NoError(t, err)
}

func TestMatchPosition_ExplicitNormVersion(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN request with explicit norm_version = 3
	req := api_models.MatchPositionRequest{
		PositionItemID:    100,
		CatalogPositionID: 42,
		Hash:              "hash789",
		NormVersion:       3,
	}

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectExec("UPDATE position_items").
				WithArgs(sql.NullInt64{Int64: 42, Valid: true}, int64(100)).
				WillReturnResult(sqlmock.NewResult(0, 1))

			mock.ExpectQuery("SELECT .+ FROM position_items WHERE id").
				WithArgs(int64(100)).
				WillReturnRows(sqlmock.NewRows(positionItemColumns).
					AddRow(positionItemRow(100, "Работы по бетонированию")...))

			mock.ExpectExec("INSERT INTO matching_cache").
				WithArgs(
					"hash789",
					int16(3), // explicit norm_version
					sql.NullString{String: "Работы по бетонированию", Valid: true},
					int64(42),
					sqlmock.AnyArg(),
				).
				WillReturnResult(sqlmock.NewResult(0, 1))
		}),
	)

	// WHEN
	err := service.MatchPosition(ctx, req)

	// THEN
	assert.NoError(t, err)
}

func TestMatchPosition_SetCatalogPositionIDFails(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN SetCatalogPositionID returns error
	req := api_models.MatchPositionRequest{
		PositionItemID:    100,
		CatalogPositionID: 42,
		Hash:              "hash-fail",
		NormVersion:       1,
	}

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectExec("UPDATE position_items").
				WithArgs(sql.NullInt64{Int64: 42, Valid: true}, int64(100)).
				WillReturnError(errors.New("deadlock detected"))
		}),
	)

	// WHEN
	err := service.MatchPosition(ctx, req)

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ошибка обновления position_items")
}

func TestMatchPosition_GetPositionItemByIDFails_ContinuesWithEmptyTitle(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN GetPositionItemByID fails (position lookup for cache is non-critical)
	req := api_models.MatchPositionRequest{
		PositionItemID:    999,
		CatalogPositionID: 42,
		Hash:              "hash-missing-pos",
		NormVersion:       1,
	}

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectExec("UPDATE position_items").
				WithArgs(sql.NullInt64{Int64: 42, Valid: true}, int64(999)).
				WillReturnResult(sqlmock.NewResult(0, 1))

			// GetPositionItemByID returns not found
			mock.ExpectQuery("SELECT .+ FROM position_items WHERE id").
				WithArgs(int64(999)).
				WillReturnError(sql.ErrNoRows)

			// UpsertMatchingCache still called — with empty job_title_text (NullString Valid: false)
			mock.ExpectExec("INSERT INTO matching_cache").
				WithArgs(
					"hash-missing-pos",
					int16(1),
					sql.NullString{String: "", Valid: false}, // empty because position not found
					int64(42),
					sqlmock.AnyArg(),
				).
				WillReturnResult(sqlmock.NewResult(0, 1))
		}),
	)

	// WHEN
	err := service.MatchPosition(ctx, req)

	// THEN — no error; GetPositionItemByID failure is non-critical (logged as warning)
	assert.NoError(t, err)
}

func TestMatchPosition_UpsertMatchingCacheFails(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN UpsertMatchingCache returns error
	req := api_models.MatchPositionRequest{
		PositionItemID:    100,
		CatalogPositionID: 42,
		Hash:              "hash-cache-fail",
		NormVersion:       1,
	}

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectExec("UPDATE position_items").
				WithArgs(sql.NullInt64{Int64: 42, Valid: true}, int64(100)).
				WillReturnResult(sqlmock.NewResult(0, 1))

			mock.ExpectQuery("SELECT .+ FROM position_items WHERE id").
				WithArgs(int64(100)).
				WillReturnRows(sqlmock.NewRows(positionItemColumns).
					AddRow(positionItemRow(100, "Штукатурка стен")...))

			mock.ExpectExec("INSERT INTO matching_cache").
				WithArgs(
					"hash-cache-fail",
					int16(1),
					sql.NullString{String: "Штукатурка стен", Valid: true},
					int64(42),
					sqlmock.AnyArg(),
				).
				WillReturnError(errors.New("unique constraint violation"))
		}),
	)

	// WHEN
	err := service.MatchPosition(ctx, req)

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ошибка обновления matching_cache")
}

func TestMatchPosition_ExecTxFails(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN ExecTx itself fails (e.g., cannot begin transaction)
	req := api_models.MatchPositionRequest{
		PositionItemID:    100,
		CatalogPositionID: 42,
		Hash:              "hash-tx-fail",
		NormVersion:       1,
	}

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).
		Return(errors.New("tx begin failed: too many connections"))

	// WHEN
	err := service.MatchPosition(ctx, req)

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tx begin failed")
}

// =============================================================================
// Table-Driven: GetUnmatchedPositions validation edge cases
// =============================================================================

func TestGetUnmatchedPositions_ValidationErrors_TableDriven(t *testing.T) {
	tests := []struct {
		name  string
		limit int32
	}{
		{name: "zero limit", limit: 0},
		{name: "negative limit -1", limit: -1},
		{name: "negative limit -100", limit: -100},
		{name: "min int32", limit: -2147483648},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, _ := setupTestService(t)
			ctx := context.Background()

			result, err := service.GetUnmatchedPositions(ctx, tt.limit)

			require.Error(t, err)
			assert.Nil(t, result)

			var validationErr *apierrors.ValidationError
			assert.True(t, errors.As(err, &validationErr),
				"limit=%d: expected ValidationError, got: %T (%v)", tt.limit, err, err)
		})
	}
}

// =============================================================================
// Table-Driven: GetUnmatchedPositions context string building
// =============================================================================

func TestGetUnmatchedPositions_ContextStringBuilding_TableDriven(t *testing.T) {
	tests := []struct {
		name           string
		fullParentPath string
		jobTitle       string
		expectedCtx    string
	}{
		{
			name:           "with single breadcrumb",
			fullParentPath: "Кровельные работы",
			jobTitle:       "Устройство кровли",
			expectedCtx:    "Раздел: Кровельные работы | Позиция: Устройство кровли",
		},
		{
			name:           "with nested breadcrumbs",
			fullParentPath: "Общестроительные работы | Фундаменты | Свайные работы",
			jobTitle:       "Забивка свай",
			expectedCtx:    "Раздел: Общестроительные работы | Фундаменты | Свайные работы | Позиция: Забивка свай",
		},
		{
			name:           "root position (no breadcrumbs)",
			fullParentPath: "",
			jobTitle:       "Демонтаж перегородок",
			expectedCtx:    "Позиция: Демонтаж перегородок",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, mockStore := setupTestService(t)
			ctx := context.Background()

			dbRows := []db.GetUnmatchedPositionsRow{
				{
					PositionItemID:     1,
					JobTitleInProposal: tt.jobTitle,
					FullParentPath:     tt.fullParentPath,
					DraftCatalogID:     sql.NullInt64{Valid: false},
					StandardJobTitle:   sql.NullString{Valid: false},
				},
			}

			mockStore.EXPECT().
				GetUnmatchedPositions(ctx, int32(10)).
				Return(dbRows, nil)

			result, err := service.GetUnmatchedPositions(ctx, 10)

			require.NoError(t, err)
			require.Len(t, result, 1)
			assert.Equal(t, tt.expectedCtx, result[0].RichContextString)
		})
	}
}

// =============================================================================
// Table-Driven: MatchPosition norm_version handling
// =============================================================================

func TestMatchPosition_NormVersion_TableDriven(t *testing.T) {
	tests := []struct {
		name            string
		inputVersion    int
		expectedVersion int16
	}{
		{name: "default (0 → 1)", inputVersion: 0, expectedVersion: 1},
		{name: "explicit 1", inputVersion: 1, expectedVersion: 1},
		{name: "explicit 2", inputVersion: 2, expectedVersion: 2},
		{name: "explicit 5", inputVersion: 5, expectedVersion: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, mockStore := setupTestService(t)
			ctx := context.Background()

			req := api_models.MatchPositionRequest{
				PositionItemID:    100,
				CatalogPositionID: 42,
				Hash:              "test-hash",
				NormVersion:       tt.inputVersion,
			}

			mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
				execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
					mock.ExpectExec("UPDATE position_items").
						WithArgs(sql.NullInt64{Int64: 42, Valid: true}, int64(100)).
						WillReturnResult(sqlmock.NewResult(0, 1))

					mock.ExpectQuery("SELECT .+ FROM position_items WHERE id").
						WithArgs(int64(100)).
						WillReturnRows(sqlmock.NewRows(positionItemColumns).
							AddRow(positionItemRow(100, "Тестовая позиция")...))

					mock.ExpectExec("INSERT INTO matching_cache").
						WithArgs(
							"test-hash",
							tt.expectedVersion,
							sql.NullString{String: "Тестовая позиция", Valid: true},
							int64(42),
							sqlmock.AnyArg(),
						).
						WillReturnResult(sqlmock.NewResult(0, 1))
				}),
			)

			err := service.MatchPosition(ctx, req)
			assert.NoError(t, err, "norm_version=%d", tt.inputVersion)
		})
	}
}

// =============================================================================
// Edge Cases
// =============================================================================

func TestGetUnmatchedPositions_LimitJustBelowMax(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN limit = MaxUnmatchedPositionsLimit - 1 (should NOT be capped)
	limit := int32(MaxUnmatchedPositionsLimit - 1)
	mockStore.EXPECT().
		GetUnmatchedPositions(ctx, limit).
		Return([]db.GetUnmatchedPositionsRow{}, nil)

	// WHEN
	result, err := service.GetUnmatchedPositions(ctx, limit)

	// THEN
	require.NoError(t, err)
	assert.Len(t, result, 0)
}

func TestGetUnmatchedPositions_LimitJustAboveMax(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN limit = MaxUnmatchedPositionsLimit + 1 (should be capped)
	mockStore.EXPECT().
		GetUnmatchedPositions(ctx, int32(MaxUnmatchedPositionsLimit)).
		Return([]db.GetUnmatchedPositionsRow{}, nil)

	// WHEN
	result, err := service.GetUnmatchedPositions(ctx, MaxUnmatchedPositionsLimit+1)

	// THEN — capped to MaxUnmatchedPositionsLimit
	require.NoError(t, err)
	assert.Len(t, result, 0)
}

func TestGetUnmatchedPositions_LimitOne(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN limit = 1 (minimum valid value)
	dbRows := []db.GetUnmatchedPositionsRow{
		{
			PositionItemID:     1,
			JobTitleInProposal: "Единственная позиция",
			FullParentPath:     "",
			DraftCatalogID:     sql.NullInt64{Valid: false},
			StandardJobTitle:   sql.NullString{Valid: false},
		},
	}

	mockStore.EXPECT().
		GetUnmatchedPositions(ctx, int32(1)).
		Return(dbRows, nil)

	// WHEN
	result, err := service.GetUnmatchedPositions(ctx, 1)

	// THEN
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "Позиция: Единственная позиция", result[0].RichContextString)
}

func TestMatchPosition_EmptyJobTitle_PositionFound(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN position exists but has empty job_title_in_proposal
	// (edge case: job_title_text should be NullString{Valid: false})
	req := api_models.MatchPositionRequest{
		PositionItemID:    100,
		CatalogPositionID: 42,
		Hash:              "hash-empty-title",
		NormVersion:       1,
	}

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectExec("UPDATE position_items").
				WithArgs(sql.NullInt64{Int64: 42, Valid: true}, int64(100)).
				WillReturnResult(sqlmock.NewResult(0, 1))

			// Position found but with empty job_title
			mock.ExpectQuery("SELECT .+ FROM position_items WHERE id").
				WithArgs(int64(100)).
				WillReturnRows(sqlmock.NewRows(positionItemColumns).
					AddRow(positionItemRow(100, "")...))

			// Empty job_title → NullString{Valid: false}
			mock.ExpectExec("INSERT INTO matching_cache").
				WithArgs(
					"hash-empty-title",
					int16(1),
					sql.NullString{String: "", Valid: false},
					int64(42),
					sqlmock.AnyArg(),
				).
				WillReturnResult(sqlmock.NewResult(0, 1))
		}),
	)

	// WHEN
	err := service.MatchPosition(ctx, req)

	// THEN
	assert.NoError(t, err)
}

func TestMaxUnmatchedPositionsLimit_Constant(t *testing.T) {
	// GIVEN the constant is defined
	// THEN it should be 1000 (documented contract with external consumers)
	assert.Equal(t, int32(1000), int32(MaxUnmatchedPositionsLimit))
}
