package catalog

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/zhukovvlad/tenders-go/cmd/internal/api_models"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/apierrors"
	"github.com/zhukovvlad/tenders-go/cmd/internal/testutil"
)

/*
BEHAVIORAL SCENARIOS FOR CATALOG SERVICE (Unit Tests)

What user problems does this protect us from?
================================================================================
1. Data integrity — catalog positions must be correctly mapped from DB rows to API responses
2. Input validation — invalid parameters (negative limit/offset) must be rejected
3. Error propagation — DB errors must be properly wrapped and returned
4. Business rules — self-merge prevention, empty ID list handling
5. Context building — correct priority: original description > lemmatized title

These unit tests use gomock MockStore to test CatalogService methods in isolation
without a real database.

GIVEN / WHEN / THEN Scenarios:
================================================================================

SCENARIO 1: ListCatalogPositionsForEmbedding
- GIVEN valid limit and pending_indexing items in DB
  WHEN ListCatalogPositionsForEmbedding is called
  THEN items are returned with correctly built context strings

- GIVEN zero or negative limit
  WHEN ListCatalogPositionsForEmbedding is called
  THEN ValidationError is returned without DB call

- GIVEN a DB error
  WHEN ListCatalogPositionsForEmbedding is called
  THEN wrapped DB error is returned

SCENARIO 2: MarkCatalogItemsAsActive
- GIVEN a list of catalog IDs
  WHEN MarkCatalogItemsAsActive is called
  THEN store.SetCatalogStatusActive is called with those IDs

- GIVEN an empty list of catalog IDs
  WHEN MarkCatalogItemsAsActive is called
  THEN no DB call is made and nil is returned

- GIVEN a DB error
  WHEN MarkCatalogItemsAsActive is called
  THEN wrapped DB error is returned

SCENARIO 3: SuggestMerge
- GIVEN valid merge request with different IDs
  WHEN SuggestMerge is called
  THEN store.UpsertSuggestedMerge is called with correct params

- GIVEN merge request with same main and duplicate IDs (self-merge)
  WHEN SuggestMerge is called
  THEN no DB call is made and nil is returned

- GIVEN a DB error
  WHEN SuggestMerge is called
  THEN wrapped DB error is returned

SCENARIO 4: GetAllActiveCatalogItems
- GIVEN valid limit/offset and active items in DB
  WHEN GetAllActiveCatalogItems is called
  THEN items are returned with correctly built context strings

- GIVEN negative limit or offset
  WHEN GetAllActiveCatalogItems is called
  THEN ValidationError is returned without DB call

- GIVEN a DB error
  WHEN GetAllActiveCatalogItems is called
  THEN wrapped DB error is returned

SCENARIO 5: buildContextString (helper)
- GIVEN a valid description
  WHEN context string is built
  THEN description is used (trimmed)

- GIVEN an empty/null description
  WHEN context string is built
  THEN standardJobTitle is used as fallback

SCENARIO 6: ExecuteMerge (Default Merge, Scenario 1)
- GIVEN empty executedBy
  WHEN ExecuteMerge is called
  THEN ValidationError is returned without DB call

- GIVEN an approved merge and valid positions
  WHEN ExecuteMerge is called
  THEN merge is executed and response includes status+timestamp

- GIVEN merge ID that doesn't exist
  WHEN ExecuteMerge is called
  THEN NotFoundError is returned

- GIVEN merge ID that exists but status != APPROVED
  WHEN ExecuteMerge is called
  THEN ValidationError is returned

- GIVEN a DB error from GetSuggestedMergeByID
  WHEN ExecuteMerge is called
  THEN wrapped DB error is returned (not masked)

- GIVEN duplicate position already merged
  WHEN ExecuteMerge is called
  THEN ValidationError mentions duplicate specifically

- GIVEN master position inactive/merged
  WHEN ExecuteMerge is called
  THEN ValidationError mentions master specifically

- GIVEN a DB error from MergeCatalogPosition
  WHEN ExecuteMerge is called
  THEN wrapped DB error is returned

SCENARIO 7: ExecuteMerge (Merge-to-New, Scenario 2)
- GIVEN an approved merge and newMainTitle is provided
  WHEN ExecuteMerge is called
  THEN C is created, A and B deprecated, response has Scenario=merge_to_new

- GIVEN newMainTitle duplicates existing position (unique constraint)
  WHEN ExecuteMerge is called
  THEN ValidationError about duplicate title is returned

- GIVEN CreateSimpleCatalogPosition fails with DB error
  WHEN ExecuteMerge is called
  THEN wrapped DB error is returned

- GIVEN A is already deprecated when SetPositionMerged runs
  WHEN ExecuteMerge is called with newMainTitle
  THEN ValidationError mentions master position

- GIVEN B is already deprecated when SetPositionMerged runs
  WHEN ExecuteMerge is called with newMainTitle
  THEN ValidationError mentions duplicate position

- GIVEN SetPositionMerged fails with DB error
  WHEN ExecuteMerge is called with newMainTitle
  THEN wrapped DB error is returned

- GIVEN newMainTitle is whitespace-only
  WHEN ExecuteMerge is called
  THEN it trims to empty and falls back to Scenario 1 (Default Merge)

SCENARIO 8: ExecuteBatchMerge (Default Batch, Scenario 1)
- GIVEN valid merge_ids, target_position_id in group
  WHEN ExecuteBatchMerge is called without new_main_title
  THEN target stays active, all others deprecated

- GIVEN valid merge_ids with rename_title
  WHEN ExecuteBatchMerge is called
  THEN target is renamed (pending_indexing), all others deprecated

- GIVEN empty merge_ids
  WHEN ExecuteBatchMerge is called
  THEN ValidationError is returned

- GIVEN duplicate merge_ids in array
  WHEN ExecuteBatchMerge is called
  THEN ValidationError about duplicate merge_id

- GIVEN target_position_id not in group
  WHEN ExecuteBatchMerge is called
  THEN ValidationError about invalid target

- GIVEN some merge_ids have wrong status
  WHEN ExecuteBatchMerge is called
  THEN ValidationError with failed IDs, transaction rolled back

- GIVEN a position already deprecated during batch
  WHEN ExecuteBatchMerge is called
  THEN ValidationError, transaction rolled back

- GIVEN target_position_id is already deprecated
  WHEN ExecuteBatchMerge is called
  THEN ValidationError about invalid/disabled target, transaction rolled back

SCENARIO 9: ExecuteBatchMerge (Batch Merge-to-New, Scenario 2)
- GIVEN valid merge_ids and new_main_title
  WHEN ExecuteBatchMerge is called
  THEN C created, all positions deprecated

- GIVEN duplicate title (pq 23505)
  WHEN ExecuteBatchMerge is called with new_main_title
  THEN ValidationError about existing title

- GIVEN missing target_position_id but no new_main_title
  WHEN ExecuteBatchMerge is called
  THEN ValidationError about required target_position_id
*/

// setupTestService creates a CatalogService with mock store for unit testing.
// gomock.NewController registers t.Cleanup(ctrl.Finish) automatically,
// so callers don't need to defer ctrl.Finish().
func setupTestService(t *testing.T) (*CatalogService, *db.MockStore) {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockStore := db.NewMockStore(ctrl)
	logger := testutil.NewMockLogger()

	service := &CatalogService{
		store:  mockStore,
		logger: logger,
	}

	return service, mockStore
}

// =============================================================================
// buildContextString TESTS (helper function)
// =============================================================================

func TestBuildContextString_WithValidDescription(t *testing.T) {
	// GIVEN a valid, non-empty description
	desc := sql.NullString{String: "Устройство бетонного фундамента", Valid: true}
	title := "устройство бетонный фундамент"

	// WHEN context string is built
	result := buildContextString(desc, title)

	// THEN description is used (not the lemmatized title)
	assert.Equal(t, "Устройство бетонного фундамента", result)
}

func TestBuildContextString_WithWhitespaceDescription(t *testing.T) {
	// GIVEN a description with only whitespace
	desc := sql.NullString{String: "   ", Valid: true}
	title := "устройство бетонный фундамент"

	// WHEN context string is built
	result := buildContextString(desc, title)

	// THEN fallback to standardJobTitle
	assert.Equal(t, "устройство бетонный фундамент", result)
}

func TestBuildContextString_WithNullDescription(t *testing.T) {
	// GIVEN a NULL description
	desc := sql.NullString{Valid: false}
	title := "монтаж металлоконструкция"

	// WHEN context string is built
	result := buildContextString(desc, title)

	// THEN fallback to standardJobTitle
	assert.Equal(t, "монтаж металлоконструкция", result)
}

func TestBuildContextString_TrimsSpaces(t *testing.T) {
	// GIVEN a description with leading/trailing spaces
	desc := sql.NullString{String: "  Штукатурка стен  ", Valid: true}
	title := "штукатурка стена"

	// WHEN context string is built
	result := buildContextString(desc, title)

	// THEN description is trimmed
	assert.Equal(t, "Штукатурка стен", result)
}

func TestBuildContextString_EmptyStringDescription(t *testing.T) {
	// GIVEN an empty string description
	desc := sql.NullString{String: "", Valid: true}
	title := "демонтаж перекрытие"

	// WHEN context string is built
	result := buildContextString(desc, title)

	// THEN fallback to standardJobTitle
	assert.Equal(t, "демонтаж перекрытие", result)
}

// =============================================================================
// ListCatalogPositionsForEmbedding TESTS
// =============================================================================

func TestListCatalogPositionsForEmbedding_Success(t *testing.T) {
	service, mockStore := setupTestService(t)

	// GIVEN items pending indexing in DB
	dbRows := []db.ListCatalogPositionsForEmbeddingRow{
		{
			ID:               1,
			StandardJobTitle: "монтаж трубопровод",
			Description:      sql.NullString{String: "Монтаж трубопровода из ПВХ", Valid: true},
		},
		{
			ID:               2,
			StandardJobTitle: "окраска стена",
			Description:      sql.NullString{Valid: false},
		},
	}
	mockStore.EXPECT().
		ListCatalogPositionsForEmbedding(gomock.Any(), int32(10)).
		Return(dbRows, nil)

	// WHEN
	result, err := service.GetUnindexedCatalogItems(context.Background(), 10)

	// THEN
	require.NoError(t, err)
	require.Len(t, result, 2)

	// First item: uses description (natural language)
	assert.Equal(t, int64(1), result[0].PositionItemID)
	assert.Equal(t, "монтаж трубопровод", result[0].JobTitleInProposal)
	assert.Equal(t, "Монтаж трубопровода из ПВХ", result[0].RichContextString)

	// Second item: falls back to standardJobTitle (no description)
	assert.Equal(t, int64(2), result[1].PositionItemID)
	assert.Equal(t, "окраска стена", result[1].JobTitleInProposal)
	assert.Equal(t, "окраска стена", result[1].RichContextString)
}

func TestListCatalogPositionsForEmbedding_EmptyResult(t *testing.T) {
	service, mockStore := setupTestService(t)

	// GIVEN no pending items
	mockStore.EXPECT().
		ListCatalogPositionsForEmbedding(gomock.Any(), int32(50)).
		Return([]db.ListCatalogPositionsForEmbeddingRow{}, nil)

	// WHEN
	result, err := service.GetUnindexedCatalogItems(context.Background(), 50)

	// THEN
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestListCatalogPositionsForEmbedding_InvalidLimit_Zero(t *testing.T) {
	service, _ := setupTestService(t)

	// GIVEN limit = 0
	// WHEN
	result, err := service.GetUnindexedCatalogItems(context.Background(), 0)

	// THEN ValidationError without DB call
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
}

func TestListCatalogPositionsForEmbedding_InvalidLimit_Negative(t *testing.T) {
	service, _ := setupTestService(t)

	// GIVEN negative limit
	// WHEN
	result, err := service.GetUnindexedCatalogItems(context.Background(), -5)

	// THEN ValidationError without DB call
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
}

func TestListCatalogPositionsForEmbedding_DBError(t *testing.T) {
	service, mockStore := setupTestService(t)

	// GIVEN DB returns an error
	dbErr := errors.New("connection refused")
	mockStore.EXPECT().
		ListCatalogPositionsForEmbedding(gomock.Any(), int32(10)).
		Return(nil, dbErr)

	// WHEN
	result, err := service.GetUnindexedCatalogItems(context.Background(), 10)

	// THEN error is wrapped and returned
	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, dbErr)
	assert.Contains(t, err.Error(), "ошибка БД")
}

// =============================================================================
// MarkCatalogItemsAsActive TESTS
// =============================================================================

func TestMarkCatalogItemsAsActive_Success(t *testing.T) {
	service, mockStore := setupTestService(t)

	// GIVEN valid catalog IDs
	ids := []int64{1, 2, 3}
	mockStore.EXPECT().
		SetCatalogStatusActive(gomock.Any(), ids).
		Return(nil)

	// WHEN
	err := service.MarkCatalogItemsAsActive(context.Background(), ids)

	// THEN success
	require.NoError(t, err)
}

func TestMarkCatalogItemsAsActive_SingleID(t *testing.T) {
	service, mockStore := setupTestService(t)

	// GIVEN single catalog ID
	ids := []int64{42}
	mockStore.EXPECT().
		SetCatalogStatusActive(gomock.Any(), ids).
		Return(nil)

	// WHEN
	err := service.MarkCatalogItemsAsActive(context.Background(), ids)

	// THEN success
	require.NoError(t, err)
}

func TestMarkCatalogItemsAsActive_EmptyList(t *testing.T) {
	service, _ := setupTestService(t)

	// GIVEN empty list — no DB call expected (no EXPECT on mockStore)
	// WHEN
	err := service.MarkCatalogItemsAsActive(context.Background(), []int64{})

	// THEN nil returned without error
	require.NoError(t, err)
}

func TestMarkCatalogItemsAsActive_NilList(t *testing.T) {
	service, _ := setupTestService(t)

	// GIVEN nil list
	// WHEN
	err := service.MarkCatalogItemsAsActive(context.Background(), nil)

	// THEN nil returned without error
	require.NoError(t, err)
}

func TestMarkCatalogItemsAsActive_DBError(t *testing.T) {
	service, mockStore := setupTestService(t)

	// GIVEN DB returns error
	dbErr := errors.New("deadlock detected")
	ids := []int64{1, 2}
	mockStore.EXPECT().
		SetCatalogStatusActive(gomock.Any(), ids).
		Return(dbErr)

	// WHEN
	err := service.MarkCatalogItemsAsActive(context.Background(), ids)

	// THEN wrapped DB error
	require.Error(t, err)
	assert.ErrorIs(t, err, dbErr)
	assert.Contains(t, err.Error(), "ошибка БД")
}

// =============================================================================
// SuggestMerge TESTS
// =============================================================================

func TestSuggestMerge_Success(t *testing.T) {
	service, mockStore := setupTestService(t)

	// GIVEN valid merge request with different IDs
	req := api_models.SuggestMergeRequest{
		MainPositionID:      10,
		DuplicatePositionID: 20,
		SimilarityScore:     0.92,
	}
	mockStore.EXPECT().
		UpsertSuggestedMerge(gomock.Any(), db.UpsertSuggestedMergeParams{
			MainPositionID:      10,
			DuplicatePositionID: 20,
			SimilarityScore:     float32(0.92),
		}).
		Return(nil)

	// WHEN
	err := service.SuggestMerge(context.Background(), req)

	// THEN success
	require.NoError(t, err)
}

func TestSuggestMerge_HighSimilarity(t *testing.T) {
	service, mockStore := setupTestService(t)

	// GIVEN similarity score at boundary (1.0)
	req := api_models.SuggestMergeRequest{
		MainPositionID:      100,
		DuplicatePositionID: 200,
		SimilarityScore:     1.0,
	}
	mockStore.EXPECT().
		UpsertSuggestedMerge(gomock.Any(), db.UpsertSuggestedMergeParams{
			MainPositionID:      100,
			DuplicatePositionID: 200,
			SimilarityScore:     float32(1.0),
		}).
		Return(nil)

	// WHEN
	err := service.SuggestMerge(context.Background(), req)

	// THEN success
	require.NoError(t, err)
}

func TestSuggestMerge_SelfMerge_Skipped(t *testing.T) {
	service, _ := setupTestService(t)

	// GIVEN same main and duplicate ID (self-merge attempt)
	// No EXPECT on mockStore — DB should NOT be called
	req := api_models.SuggestMergeRequest{
		MainPositionID:      42,
		DuplicatePositionID: 42,
		SimilarityScore:     1.0,
	}

	// WHEN
	err := service.SuggestMerge(context.Background(), req)

	// THEN no error, silently skipped
	require.NoError(t, err)
}

func TestSuggestMerge_DBError(t *testing.T) {
	service, mockStore := setupTestService(t)

	// GIVEN DB returns error
	dbErr := errors.New("foreign key violation")
	req := api_models.SuggestMergeRequest{
		MainPositionID:      10,
		DuplicatePositionID: 20,
		SimilarityScore:     0.85,
	}
	mockStore.EXPECT().
		UpsertSuggestedMerge(gomock.Any(), gomock.Any()).
		Return(dbErr)

	// WHEN
	err := service.SuggestMerge(context.Background(), req)

	// THEN wrapped DB error
	require.Error(t, err)
	assert.ErrorIs(t, err, dbErr)
	assert.Contains(t, err.Error(), "ошибка БД")
}

func TestSuggestMerge_LowSimilarity(t *testing.T) {
	service, mockStore := setupTestService(t)

	// GIVEN a very low similarity score — still valid, no threshold in service
	req := api_models.SuggestMergeRequest{
		MainPositionID:      1,
		DuplicatePositionID: 2,
		SimilarityScore:     0.01,
	}
	mockStore.EXPECT().
		UpsertSuggestedMerge(gomock.Any(), db.UpsertSuggestedMergeParams{
			MainPositionID:      1,
			DuplicatePositionID: 2,
			SimilarityScore:     float32(0.01),
		}).
		Return(nil)

	// WHEN
	err := service.SuggestMerge(context.Background(), req)

	// THEN success (service doesn't enforce minimum threshold)
	require.NoError(t, err)
}

// =============================================================================
// GetAllActiveCatalogItems TESTS
// =============================================================================

func TestGetAllActiveCatalogItems_Success(t *testing.T) {
	service, mockStore := setupTestService(t)

	// GIVEN active items in DB
	dbRows := []db.GetActiveCatalogItemsRow{
		{
			CatalogID:        10,
			StandardJobTitle: "кладка кирпич",
			Description:      sql.NullString{String: "Кладка кирпичная несущих стен", Valid: true},
		},
		{
			CatalogID:        20,
			StandardJobTitle: "гидроизоляция фундамент",
			Description:      sql.NullString{Valid: false},
		},
		{
			CatalogID:        30,
			StandardJobTitle: "утепление фасад",
			Description:      sql.NullString{String: "Утепление фасадов минеральной ватой", Valid: true},
		},
	}
	mockStore.EXPECT().
		GetActiveCatalogItems(gomock.Any(), db.GetActiveCatalogItemsParams{
			Limit:  100,
			Offset: 0,
		}).
		Return(dbRows, nil)

	// WHEN
	result, err := service.GetAllActiveCatalogItems(context.Background(), 100, 0)

	// THEN
	require.NoError(t, err)
	require.Len(t, result, 3)

	// Item with description — uses description
	assert.Equal(t, int64(10), result[0].PositionItemID)
	assert.Equal(t, "Кладка кирпичная несущих стен", result[0].RichContextString)

	// Item without description — falls back to title
	assert.Equal(t, int64(20), result[1].PositionItemID)
	assert.Equal(t, "гидроизоляция фундамент", result[1].RichContextString)

	// Item with description
	assert.Equal(t, int64(30), result[2].PositionItemID)
	assert.Equal(t, "Утепление фасадов минеральной ватой", result[2].RichContextString)
}

func TestGetAllActiveCatalogItems_WithOffset(t *testing.T) {
	service, mockStore := setupTestService(t)

	// GIVEN second page of active items
	dbRows := []db.GetActiveCatalogItemsRow{
		{
			CatalogID:        50,
			StandardJobTitle: "установка дверь",
			Description:      sql.NullString{String: "Установка дверных блоков", Valid: true},
		},
	}
	mockStore.EXPECT().
		GetActiveCatalogItems(gomock.Any(), db.GetActiveCatalogItemsParams{
			Limit:  10,
			Offset: 20,
		}).
		Return(dbRows, nil)

	// WHEN
	result, err := service.GetAllActiveCatalogItems(context.Background(), 10, 20)

	// THEN
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, int64(50), result[0].PositionItemID)
}

func TestGetAllActiveCatalogItems_EmptyResult(t *testing.T) {
	service, mockStore := setupTestService(t)

	// GIVEN no active items (e.g. beyond last page)
	mockStore.EXPECT().
		GetActiveCatalogItems(gomock.Any(), db.GetActiveCatalogItemsParams{
			Limit:  10,
			Offset: 1000,
		}).
		Return([]db.GetActiveCatalogItemsRow{}, nil)

	// WHEN
	result, err := service.GetAllActiveCatalogItems(context.Background(), 10, 1000)

	// THEN
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestGetAllActiveCatalogItems_InvalidLimit_Zero(t *testing.T) {
	service, _ := setupTestService(t)

	// GIVEN limit = 0
	// WHEN
	result, err := service.GetAllActiveCatalogItems(context.Background(), 0, 0)

	// THEN ValidationError without DB call
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
}

func TestGetAllActiveCatalogItems_InvalidLimit_Negative(t *testing.T) {
	service, _ := setupTestService(t)

	// GIVEN negative limit
	// WHEN
	result, err := service.GetAllActiveCatalogItems(context.Background(), -1, 0)

	// THEN ValidationError
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
}

func TestGetAllActiveCatalogItems_InvalidOffset_Negative(t *testing.T) {
	service, _ := setupTestService(t)

	// GIVEN negative offset
	// WHEN
	result, err := service.GetAllActiveCatalogItems(context.Background(), 10, -1)

	// THEN ValidationError
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
}

func TestGetAllActiveCatalogItems_DBError(t *testing.T) {
	service, mockStore := setupTestService(t)

	// GIVEN DB returns error
	dbErr := errors.New("timeout exceeded")
	mockStore.EXPECT().
		GetActiveCatalogItems(gomock.Any(), db.GetActiveCatalogItemsParams{
			Limit:  10,
			Offset: 0,
		}).
		Return(nil, dbErr)

	// WHEN
	result, err := service.GetAllActiveCatalogItems(context.Background(), 10, 0)

	// THEN wrapped DB error
	require.Error(t, err)
	assert.Nil(t, result)
	assert.ErrorIs(t, err, dbErr)
	assert.Contains(t, err.Error(), "ошибка БД")
}

// =============================================================================
// CONTEXT STRING CONSISTENCY TEST
// =============================================================================

func TestBuildContextString_DescriptionTakesPriorityOverTitle(t *testing.T) {
	// GIVEN a valid description and a different lemmatized title
	desc := sql.NullString{String: "Монтаж систем вентиляции", Valid: true}
	title := "монтаж система вентиляция"

	// WHEN context string is built
	result := buildContextString(desc, title)

	// THEN description takes priority over lemmatized title
	assert.Equal(t, "Монтаж систем вентиляции", result)
	assert.NotEqual(t, title, result, "description should take priority over lemmatized title")
}

// =============================================================================
// ExecuteMerge HELPERS
// =============================================================================

// Column names for sqlmock result sets
var (
	suggestedMergeColumns = []string{
		"id", "main_position_id", "duplicate_position_id", "similarity_score",
		"status", "created_at", "updated_at", "resolved_at", "resolved_by",
	}
	catalogPositionColumns = []string{
		"id", "standard_job_title", "description", "embedding", "kind", "status",
		"unit_id", "created_at", "updated_at", "fts_vector", "merged_into_id",
	}
)

// newMockQueries creates a sqlmock-backed *db.Queries for ExecTx tests.
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

// =============================================================================
// ExecuteMerge TESTS
// =============================================================================

func TestExecuteMerge_EmptyExecutedBy(t *testing.T) {
	service, _ := setupTestService(t)

	// GIVEN empty executedBy string
	// WHEN ExecuteMerge is called
	result, err := service.ExecuteMerge(context.Background(), 1, "", "")

	// THEN ValidationError is returned without touching DB
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Contains(t, err.Error(), "executedBy")
}

func TestExecuteMerge_Success(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN an approved merge and valid positions
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// ExecuteMerge succeeds
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), int64(42)). // resolved_by, id
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(
						int64(42), int64(100), int64(200), float32(0.95),
						"EXECUTED", now, now, now, "123",
					))

			// MergeCatalogPosition succeeds
			mock.ExpectQuery("UPDATE catalog_positions").
				WithArgs(sqlmock.AnyArg(), int64(200)). // master_id, duplicate_id
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(200), "дубликат работа", sql.NullString{Valid: false}, nil,
						"POSITION", "deprecated", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Int64: 100, Valid: true},
					))
		}),
	)

	// WHEN
	result, err := service.ExecuteMerge(ctx, 42, "123", "")

	// THEN success with enriched response
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, int64(42), result.MergeID)
	assert.Equal(t, int64(100), result.MainPositionID)
	assert.Equal(t, int64(200), result.MergedPositionID)
	assert.Equal(t, int64(100), result.ResultingPositionID) // Scenario 1: A remains
	assert.Equal(t, "active", result.ResultingPositionStatus)
	assert.Equal(t, []int64{int64(200)}, result.DeprecatedPositionIDs)
	assert.Equal(t, "default", result.Scenario)
	assert.Equal(t, "deprecated", result.Status)
	assert.False(t, result.ResolvedAt.IsZero())
}

func TestExecuteMerge_NotFound(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN merge ID that doesn't exist
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// ExecuteMerge returns ErrNoRows
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), int64(999)).
				WillReturnError(sql.ErrNoRows)

			// GetSuggestedMergeByID also returns ErrNoRows → not found
			mock.ExpectQuery("SELECT .+ FROM suggested_merges WHERE id").
				WithArgs(int64(999)).
				WillReturnError(sql.ErrNoRows)
		}),
	)

	// WHEN
	result, err := service.ExecuteMerge(ctx, 999, "123", "")

	// THEN NotFoundError
	require.Error(t, err)
	assert.Nil(t, result)
	var notFoundErr *apierrors.NotFoundError
	assert.True(t, errors.As(err, &notFoundErr))
	assert.Contains(t, err.Error(), "999")
}

func TestExecuteMerge_WrongStatus(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN merge exists but status is REJECTED (not PENDING/APPROVED)
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// ExecuteMerge returns ErrNoRows (status not in PENDING/APPROVED)
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), int64(50)).
				WillReturnError(sql.ErrNoRows)

			// GetSuggestedMergeByID finds the record (exists but wrong status)
			mock.ExpectQuery("SELECT .+ FROM suggested_merges WHERE id").
				WithArgs(int64(50)).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(
						int64(50), int64(10), int64(20), float32(0.90),
						"REJECTED", now, now, now, "admin",
					))
		}),
	)

	// WHEN
	result, err := service.ExecuteMerge(ctx, 50, "123", "")

	// THEN ValidationError about wrong status
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Contains(t, err.Error(), "текущий статус=REJECTED (ожидается PENDING/APPROVED)")
}

func TestExecuteMerge_GetSuggestedMergeByID_DBError(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN ExecuteMerge returns ErrNoRows, then GetSuggestedMergeByID fails with DB error
	dbErr := errors.New("connection refused")
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), int64(7)).
				WillReturnError(sql.ErrNoRows)

			mock.ExpectQuery("SELECT .+ FROM suggested_merges WHERE id").
				WithArgs(int64(7)).
				WillReturnError(dbErr)
		}),
	)

	// WHEN
	result, err := service.ExecuteMerge(ctx, 7, "admin", "")

	// THEN DB error is propagated (not masked as ValidationError)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "GetSuggestedMergeByID")
	// Must NOT be a ValidationError — it's a real DB error
	var validationErr *apierrors.ValidationError
	assert.False(t, errors.As(err, &validationErr))
}

func TestExecuteMerge_DuplicateAlreadyMerged(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN merge is approved, but duplicate position is already merged
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// ExecuteMerge succeeds
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), int64(42)).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(
						int64(42), int64(100), int64(200), float32(0.95),
						"EXECUTED", now, now, now, "123",
					))

			// MergeCatalogPosition fails (ErrNoRows — WHERE clause excluded it)
			mock.ExpectQuery("UPDATE catalog_positions").
				WithArgs(sqlmock.AnyArg(), int64(200)).
				WillReturnError(sql.ErrNoRows)

			// GetCatalogPositionByID for duplicate — already merged
			mock.ExpectQuery("SELECT .+ FROM catalog_positions").
				WithArgs(int64(200)).
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(200), "дубликат", sql.NullString{Valid: false}, nil,
						"POSITION", "deprecated", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Int64: 99, Valid: true},
					))
		}),
	)

	// WHEN
	result, err := service.ExecuteMerge(ctx, 42, "123", "")

	// THEN ValidationError specifically mentions duplicate
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Contains(t, err.Error(), "дубликат")
	assert.Contains(t, err.Error(), "200")
}

func TestExecuteMerge_MasterInactive(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN merge is approved, but master position is deprecated
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// ExecuteMerge succeeds
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), int64(42)).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(
						int64(42), int64(100), int64(200), float32(0.95),
						"EXECUTED", now, now, now, "123",
					))

			// MergeCatalogPosition fails (ErrNoRows — master not active)
			mock.ExpectQuery("UPDATE catalog_positions").
				WithArgs(sqlmock.AnyArg(), int64(200)).
				WillReturnError(sql.ErrNoRows)

			// GetCatalogPositionByID for duplicate — OK (not merged yet)
			mock.ExpectQuery("SELECT .+ FROM catalog_positions").
				WithArgs(int64(200)).
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(200), "дубликат", sql.NullString{Valid: false}, nil,
						"POSITION", "active", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Valid: false},
					))

			// GetCatalogPositionByID for master — deprecated
			mock.ExpectQuery("SELECT .+ FROM catalog_positions").
				WithArgs(int64(100)).
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(100), "мастер", sql.NullString{Valid: false}, nil,
						"POSITION", "deprecated", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Int64: 50, Valid: true},
					))
		}),
	)

	// WHEN
	result, err := service.ExecuteMerge(ctx, 42, "123", "")

	// THEN ValidationError specifically mentions master
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Contains(t, err.Error(), "мастер")
	assert.Contains(t, err.Error(), "100")
}

func TestExecuteMerge_MergeCatalogPosition_DBError(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN ExecuteMerge succeeds, MergeCatalogPosition fails with DB error
	dbErr := errors.New("deadlock detected")
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), int64(42)).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(
						int64(42), int64(100), int64(200), float32(0.95),
						"EXECUTED", now, now, now, "123",
					))

			mock.ExpectQuery("UPDATE catalog_positions").
				WithArgs(sqlmock.AnyArg(), int64(200)).
				WillReturnError(dbErr)
		}),
	)

	// WHEN
	result, err := service.ExecuteMerge(ctx, 42, "123", "")

	// THEN wrapped DB error
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "MergeCatalogPosition")
	// Must NOT be a ValidationError
	var validationErr *apierrors.ValidationError
	assert.False(t, errors.As(err, &validationErr))
}

func TestExecuteMerge_DBError(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN ExecuteMerge fails with a real DB error (not ErrNoRows)
	dbErr := errors.New("connection timeout")
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), int64(42)).
				WillReturnError(dbErr)
		}),
	)

	// WHEN
	result, err := service.ExecuteMerge(ctx, 42, "123", "")

	// THEN wrapped DB error
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "ExecuteMerge")
}

// =============================================================================
// ExecuteMerge SCENARIO 2 (Merge-to-New) TESTS
// =============================================================================

func TestExecuteMerge_MergeToNew_Success(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN an approved merge and newMainTitle provided
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// ExecuteMerge succeeds
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), int64(42)).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(
						int64(42), int64(100), int64(200), float32(0.95),
						"EXECUTED", now, now, now, "admin",
					))

			// CreateSimpleCatalogPosition succeeds — new position C (ID=300)
			mock.ExpectQuery("INSERT INTO catalog_positions").
				WithArgs("Новое название позиции").
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(300), "Новое название позиции", sql.NullString{Valid: false}, nil,
						"POSITION", "pending_indexing", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Valid: false},
					))

			// SetPositionMerged for A (ID=100) → deprecated, merged_into_id=300
			mock.ExpectQuery("UPDATE catalog_positions").
				WithArgs(sqlmock.AnyArg(), int64(100)).
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(100), "мастер работа", sql.NullString{Valid: false}, nil,
						"POSITION", "deprecated", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Int64: 300, Valid: true},
					))

			// SetPositionMerged for B (ID=200) → deprecated, merged_into_id=300
			mock.ExpectQuery("UPDATE catalog_positions").
				WithArgs(sqlmock.AnyArg(), int64(200)).
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(200), "дубликат работа", sql.NullString{Valid: false}, nil,
						"POSITION", "deprecated", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Int64: 300, Valid: true},
					))
		}),
	)

	// WHEN
	result, err := service.ExecuteMerge(ctx, 42, "admin", "Новое название позиции")

	// THEN success with Merge-to-New response
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, int64(42), result.MergeID)
	assert.Equal(t, int64(100), result.MainPositionID)
	assert.Equal(t, int64(200), result.MergedPositionID)
	assert.Equal(t, int64(300), result.ResultingPositionID) // C
	assert.Equal(t, "pending_indexing", result.ResultingPositionStatus)
	assert.Equal(t, []int64{int64(100), int64(200)}, result.DeprecatedPositionIDs)
	assert.Equal(t, "merge_to_new", result.Scenario)
	assert.Equal(t, "deprecated", result.Status)
	assert.False(t, result.ResolvedAt.IsZero())
}

func TestExecuteMerge_MergeToNew_DuplicateTitle(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN merge approved, but title already exists (unique constraint violation)
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// ExecuteMerge succeeds
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), int64(42)).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(
						int64(42), int64(100), int64(200), float32(0.90),
						"EXECUTED", now, now, now, "admin",
					))

			// CreateSimpleCatalogPosition fails with unique constraint (pq 23505)
			mock.ExpectQuery("INSERT INTO catalog_positions").
				WithArgs("Существующее название").
				WillReturnError(&pq.Error{Code: "23505", Message: "duplicate key value"})
		}),
	)

	// WHEN
	result, err := service.ExecuteMerge(ctx, 42, "admin", "Существующее название")

	// THEN ValidationError about duplicate title
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Contains(t, err.Error(), "уже существует")
	assert.Contains(t, err.Error(), "Существующее название")
}

func TestExecuteMerge_MergeToNew_CreatePosition_DBError(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN merge approved, but CreateSimpleCatalogPosition fails with DB error
	dbErr := errors.New("disk full")
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), int64(42)).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(
						int64(42), int64(100), int64(200), float32(0.90),
						"EXECUTED", now, now, now, "admin",
					))

			mock.ExpectQuery("INSERT INTO catalog_positions").
				WithArgs("Новая позиция").
				WillReturnError(dbErr)
		}),
	)

	// WHEN
	result, err := service.ExecuteMerge(ctx, 42, "admin", "Новая позиция")

	// THEN wrapped DB error, not ValidationError
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "CreateSimpleCatalogPosition")
	var validationErr *apierrors.ValidationError
	assert.False(t, errors.As(err, &validationErr))
}

func TestExecuteMerge_MergeToNew_A_AlreadyDeprecated(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN merge approved, C created, but A is already deprecated
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), int64(42)).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(
						int64(42), int64(100), int64(200), float32(0.90),
						"EXECUTED", now, now, now, "admin",
					))

			// CreateSimpleCatalogPosition succeeds
			mock.ExpectQuery("INSERT INTO catalog_positions").
				WithArgs("Объединённая позиция").
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(300), "Объединённая позиция", sql.NullString{Valid: false}, nil,
						"POSITION", "pending_indexing", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Valid: false},
					))

			// SetPositionMerged for A → ErrNoRows (A already deprecated)
			mock.ExpectQuery("UPDATE catalog_positions").
				WithArgs(sqlmock.AnyArg(), int64(100)).
				WillReturnError(sql.ErrNoRows)
		}),
	)

	// WHEN
	result, err := service.ExecuteMerge(ctx, 42, "admin", "Объединённая позиция")

	// THEN ValidationError mentions master position
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Contains(t, err.Error(), "мастер-позиция")
	assert.Contains(t, err.Error(), "100")
}

func TestExecuteMerge_MergeToNew_B_AlreadyDeprecated(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN merge approved, C created, A merged, but B is already deprecated
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), int64(42)).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(
						int64(42), int64(100), int64(200), float32(0.90),
						"EXECUTED", now, now, now, "admin",
					))

			// CreateSimpleCatalogPosition succeeds
			mock.ExpectQuery("INSERT INTO catalog_positions").
				WithArgs("Конечная позиция").
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(300), "Конечная позиция", sql.NullString{Valid: false}, nil,
						"POSITION", "pending_indexing", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Valid: false},
					))

			// SetPositionMerged for A → succeeds
			mock.ExpectQuery("UPDATE catalog_positions").
				WithArgs(sqlmock.AnyArg(), int64(100)).
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(100), "мастер позиция", sql.NullString{Valid: false}, nil,
						"POSITION", "deprecated", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Int64: 300, Valid: true},
					))

			// SetPositionMerged for B → ErrNoRows (B already deprecated)
			mock.ExpectQuery("UPDATE catalog_positions").
				WithArgs(sqlmock.AnyArg(), int64(200)).
				WillReturnError(sql.ErrNoRows)
		}),
	)

	// WHEN
	result, err := service.ExecuteMerge(ctx, 42, "admin", "Конечная позиция")

	// THEN ValidationError mentions duplicate
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Contains(t, err.Error(), "дубликат")
	assert.Contains(t, err.Error(), "200")
}

func TestExecuteMerge_MergeToNew_SetPositionMerged_DBError(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN merge approved, C created, but SetPositionMerged for A fails with DB error
	dbErr := errors.New("serialization failure")
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), int64(42)).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(
						int64(42), int64(100), int64(200), float32(0.90),
						"EXECUTED", now, now, now, "admin",
					))

			mock.ExpectQuery("INSERT INTO catalog_positions").
				WithArgs("Позиция XYZ").
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(300), "Позиция XYZ", sql.NullString{Valid: false}, nil,
						"POSITION", "pending_indexing", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Valid: false},
					))

			// SetPositionMerged for A → DB error (not ErrNoRows)
			mock.ExpectQuery("UPDATE catalog_positions").
				WithArgs(sqlmock.AnyArg(), int64(100)).
				WillReturnError(dbErr)
		}),
	)

	// WHEN
	result, err := service.ExecuteMerge(ctx, 42, "admin", "Позиция XYZ")

	// THEN wrapped DB error, not ValidationError
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "SetPositionMerged")
	var validationErr *apierrors.ValidationError
	assert.False(t, errors.As(err, &validationErr))
}

func TestExecuteMerge_WhitespaceTitle_FallsBackToScenario1(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN newMainTitle is whitespace-only → after trim it's empty → Scenario 1
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// ExecuteMerge succeeds
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), int64(42)).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(
						int64(42), int64(100), int64(200), float32(0.95),
						"EXECUTED", now, now, now, "admin",
					))

			// MergeCatalogPosition succeeds (Scenario 1 path, NOT CreateSimpleCatalogPosition)
			mock.ExpectQuery("UPDATE catalog_positions").
				WithArgs(sqlmock.AnyArg(), int64(200)).
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(200), "дубликат работа", sql.NullString{Valid: false}, nil,
						"POSITION", "deprecated", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Int64: 100, Valid: true},
					))
		}),
	)

	// WHEN — whitespace-only title should trim to empty → Scenario 1
	result, err := service.ExecuteMerge(ctx, 42, "admin", "   \t  ")

	// THEN falls back to Scenario 1 (Default Merge)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "default", result.Scenario)
	assert.Equal(t, int64(100), result.ResultingPositionID) // A remains
	assert.Equal(t, "active", result.ResultingPositionStatus)
	assert.Equal(t, []int64{int64(200)}, result.DeprecatedPositionIDs)
	assert.Equal(t, int64(200), result.MergedPositionID)
	assert.Equal(t, "deprecated", result.Status)
}

// =============================================================================
// ExecuteBatchMerge TESTS
// =============================================================================

func TestExecuteBatchMerge_EmptyExecutedBy(t *testing.T) {
	service, _ := setupTestService(t)

	// GIVEN empty executedBy
	req := api_models.ExecuteBatchMergeRequest{MergeIDs: []int64{1, 2}}

	// WHEN
	result, err := service.ExecuteBatchMerge(context.Background(), req, "")

	// THEN ValidationError
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Contains(t, err.Error(), "executedBy")
}

func TestExecuteBatchMerge_EmptyMergeIDs(t *testing.T) {
	service, _ := setupTestService(t)

	// GIVEN empty merge_ids
	req := api_models.ExecuteBatchMergeRequest{MergeIDs: []int64{}}

	// WHEN
	result, err := service.ExecuteBatchMerge(context.Background(), req, "admin")

	// THEN ValidationError
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Contains(t, err.Error(), "merge_ids")
}

func TestExecuteBatchMerge_DuplicateMergeIDs(t *testing.T) {
	service, _ := setupTestService(t)

	// GIVEN duplicate merge_ids
	req := api_models.ExecuteBatchMergeRequest{
		MergeIDs:         []int64{10, 20, 10},
		TargetPositionID: 1,
	}

	// WHEN
	result, err := service.ExecuteBatchMerge(context.Background(), req, "admin")

	// THEN ValidationError about duplicate
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Contains(t, err.Error(), "дубликат merge_id")
	assert.Contains(t, err.Error(), "10")
}

func TestExecuteBatchMerge_MissingTargetForScenario1(t *testing.T) {
	service, _ := setupTestService(t)

	// GIVEN no new_main_title and no target_position_id
	req := api_models.ExecuteBatchMergeRequest{
		MergeIDs: []int64{10, 20},
	}

	// WHEN
	result, err := service.ExecuteBatchMerge(context.Background(), req, "admin")

	// THEN ValidationError about required target
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Contains(t, err.Error(), "target_position_id")
}

func TestExecuteBatchMerge_Scenario1_Success(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN 3 merge-records: (2,59), (89,2), (2,98) → positions {2, 59, 89, 98}, target=2
	req := api_models.ExecuteBatchMergeRequest{
		MergeIDs:         []int64{101, 102, 103},
		TargetPositionID: 2,
	}

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// ExecuteMergeBatch returns 3 merges
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(int64(101), int64(2), int64(59), float32(0.90), "EXECUTED", now, now, now, "admin").
					AddRow(int64(102), int64(89), int64(2), float32(0.85), "EXECUTED", now, now, now, "admin").
					AddRow(int64(103), int64(2), int64(98), float32(0.80), "EXECUTED", now, now, now, "admin"))

			// GetCatalogPositionByID — target=2 should be active
			mock.ExpectQuery("SELECT .+ FROM catalog_positions").
				WithArgs(int64(2)).
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(2), "позиция-target", sql.NullString{Valid: false}, nil,
						"POSITION", "active", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Valid: false},
					))

			// SetPositionMerged for each position != target (59, 89, 98 → target=2)
			// Note: map iteration order is non-deterministic, so we use AnyArg for position_id
			for i := 0; i < 3; i++ {
				mock.ExpectQuery("UPDATE catalog_positions").
					WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
						AddRow(
							int64(59+i), "позиция", sql.NullString{Valid: false}, nil,
							"POSITION", "deprecated", sql.NullInt64{Valid: false},
							now, now, nil, sql.NullInt64{Int64: 2, Valid: true},
						))
			}
		}),
	)

	// WHEN
	result, err := service.ExecuteBatchMerge(ctx, req, "admin")

	// THEN
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, []int64{101, 102, 103}, result.MergeIDs)
	assert.Equal(t, int64(2), result.ResultingPositionID)
	assert.Equal(t, "active", result.ResultingPositionStatus)
	assert.Len(t, result.DeprecatedPositionIDs, 3)
	assert.Equal(t, "default", result.Scenario)
}

func TestExecuteBatchMerge_Scenario1_WithRename(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN 2 merge-records, target=2, rename_title="Чистое имя"
	req := api_models.ExecuteBatchMergeRequest{
		MergeIDs:         []int64{101, 102},
		TargetPositionID: 2,
		RenameTitle:      "Чистое имя",
	}

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// ExecuteMergeBatch returns 2 merges
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(int64(101), int64(2), int64(59), float32(0.90), "EXECUTED", now, now, now, "admin").
					AddRow(int64(102), int64(2), int64(98), float32(0.85), "EXECUTED", now, now, now, "admin"))

			// GetCatalogPositionByID — target=2 should be active
			mock.ExpectQuery("SELECT .+ FROM catalog_positions").
				WithArgs(int64(2)).
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(2), "позиция-target", sql.NullString{Valid: false}, nil,
						"POSITION", "active", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Valid: false},
					))

			// SetPositionMerged for 59 and 98
			mock.ExpectQuery("UPDATE catalog_positions").
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(59), "позиция 59", sql.NullString{Valid: false}, nil,
						"POSITION", "deprecated", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Int64: 2, Valid: true},
					))
			mock.ExpectQuery("UPDATE catalog_positions").
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(98), "позиция 98", sql.NullString{Valid: false}, nil,
						"POSITION", "deprecated", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Int64: 2, Valid: true},
					))

			// UpdateCatalogPositionDetails for rename
			mock.ExpectQuery("UPDATE catalog_positions").
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), int64(2)).
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(2), "Чистое имя", sql.NullString{Valid: false}, nil,
						"POSITION", "pending_indexing", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Valid: false},
					))
		}),
	)

	// WHEN
	result, err := service.ExecuteBatchMerge(ctx, req, "admin")

	// THEN — renamed target gets pending_indexing status
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, int64(2), result.ResultingPositionID)
	assert.Equal(t, "pending_indexing", result.ResultingPositionStatus)
	assert.Equal(t, "default", result.Scenario)
}

func TestExecuteBatchMerge_Scenario1_TargetNotInGroup(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN target_position_id (999) not in merge positions
	req := api_models.ExecuteBatchMergeRequest{
		MergeIDs:         []int64{101},
		TargetPositionID: 999,
	}

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(int64(101), int64(2), int64(59), float32(0.90), "EXECUTED", now, now, now, "admin"))
		}),
	)

	// WHEN
	result, err := service.ExecuteBatchMerge(ctx, req, "admin")

	// THEN ValidationError
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Contains(t, err.Error(), "target_position_id=999")
}

func TestExecuteBatchMerge_PartialMergesFailed(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN 3 merge_ids requested but only 2 returned (merge 103 has wrong status)
	req := api_models.ExecuteBatchMergeRequest{
		MergeIDs:         []int64{101, 102, 103},
		TargetPositionID: 2,
	}

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// Only 2 of 3 returned
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(int64(101), int64(2), int64(59), float32(0.90), "EXECUTED", now, now, now, "admin").
					AddRow(int64(102), int64(89), int64(2), float32(0.85), "EXECUTED", now, now, now, "admin"))
		}),
	)

	// WHEN
	result, err := service.ExecuteBatchMerge(ctx, req, "admin")

	// THEN ValidationError listing failed IDs
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Contains(t, err.Error(), "103")
}

func TestExecuteBatchMerge_Scenario1_PositionAlreadyDeprecated(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN one position already deprecated when SetPositionMerged runs
	req := api_models.ExecuteBatchMergeRequest{
		MergeIDs:         []int64{101, 102},
		TargetPositionID: 2,
	}

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(int64(101), int64(2), int64(59), float32(0.90), "EXECUTED", now, now, now, "admin").
					AddRow(int64(102), int64(2), int64(98), float32(0.85), "EXECUTED", now, now, now, "admin"))

			// GetCatalogPositionByID — target=2 is active
			mock.ExpectQuery("SELECT .+ FROM catalog_positions").
				WithArgs(int64(2)).
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(2), "позиция-target", sql.NullString{Valid: false}, nil,
						"POSITION", "active", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Valid: false},
					))

			// First SetPositionMerged → ErrNoRows (already deprecated)
			mock.ExpectQuery("UPDATE catalog_positions").
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnError(sql.ErrNoRows)
		}),
	)

	// WHEN
	result, err := service.ExecuteBatchMerge(ctx, req, "admin")

	// THEN ValidationError about deprecated position
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Contains(t, err.Error(), "deprecated")
}

func TestExecuteBatchMerge_TargetAlreadyDeprecated(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN target_position_id=2 is already deprecated in the catalog
	req := api_models.ExecuteBatchMergeRequest{
		MergeIDs:         []int64{101, 102},
		TargetPositionID: 2,
	}

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(int64(101), int64(2), int64(59), float32(0.90), "EXECUTED", now, now, now, "admin").
					AddRow(int64(102), int64(2), int64(98), float32(0.85), "EXECUTED", now, now, now, "admin"))

			// GetCatalogPositionByID — target=2 is deprecated
			mock.ExpectQuery("SELECT .+ FROM catalog_positions").
				WithArgs(int64(2)).
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(2), "позиция-target", sql.NullString{Valid: false}, nil,
						"POSITION", "deprecated", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Int64: 100, Valid: true},
					))
		}),
	)

	// WHEN
	result, err := service.ExecuteBatchMerge(ctx, req, "admin")

	// THEN ValidationError about invalid target status
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Contains(t, err.Error(), "target_position_id=2")
	assert.Contains(t, err.Error(), "deprecated")
}

func TestExecuteBatchMerge_Scenario2_Success(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN 3 merge-records, new_main_title → create C, deprecate all
	req := api_models.ExecuteBatchMergeRequest{
		MergeIDs:     []int64{101, 102, 103},
		NewMainTitle: "Единая позиция",
	}

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// ExecuteMergeBatch
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(int64(101), int64(2), int64(59), float32(0.90), "EXECUTED", now, now, now, "admin").
					AddRow(int64(102), int64(89), int64(2), float32(0.85), "EXECUTED", now, now, now, "admin").
					AddRow(int64(103), int64(2), int64(98), float32(0.80), "EXECUTED", now, now, now, "admin"))

			// CreateSimpleCatalogPosition → C (ID=300)
			mock.ExpectQuery("INSERT INTO catalog_positions").
				WithArgs("Единая позиция").
				WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
					AddRow(
						int64(300), "Единая позиция", sql.NullString{Valid: false}, nil,
						"POSITION", "pending_indexing", sql.NullInt64{Valid: false},
						now, now, nil, sql.NullInt64{Valid: false},
					))

			// SetPositionMerged for each unique position (2, 59, 89, 98)
			for i := 0; i < 4; i++ {
				mock.ExpectQuery("UPDATE catalog_positions").
					WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows(catalogPositionColumns).
						AddRow(
							int64(2+i), "позиция", sql.NullString{Valid: false}, nil,
							"POSITION", "deprecated", sql.NullInt64{Valid: false},
							now, now, nil, sql.NullInt64{Int64: 300, Valid: true},
						))
			}
		}),
	)

	// WHEN
	result, err := service.ExecuteBatchMerge(ctx, req, "admin")

	// THEN
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, []int64{101, 102, 103}, result.MergeIDs)
	assert.Equal(t, int64(300), result.ResultingPositionID)
	assert.Equal(t, "pending_indexing", result.ResultingPositionStatus)
	assert.Len(t, result.DeprecatedPositionIDs, 4)
	assert.Equal(t, "merge_to_new", result.Scenario)
}

func TestExecuteBatchMerge_Scenario2_DuplicateTitle(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN title already exists
	req := api_models.ExecuteBatchMergeRequest{
		MergeIDs:     []int64{101},
		NewMainTitle: "Существующее имя",
	}

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows(suggestedMergeColumns).
					AddRow(int64(101), int64(2), int64(59), float32(0.90), "EXECUTED", now, now, now, "admin"))

			mock.ExpectQuery("INSERT INTO catalog_positions").
				WithArgs("Существующее имя").
				WillReturnError(&pq.Error{Code: "23505", Message: "duplicate key value"})
		}),
	)

	// WHEN
	result, err := service.ExecuteBatchMerge(ctx, req, "admin")

	// THEN ValidationError about duplicate
	require.Error(t, err)
	assert.Nil(t, result)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Contains(t, err.Error(), "уже существует")
}

func TestExecuteBatchMerge_ExecuteMergeBatch_DBError(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN DB error from ExecuteMergeBatch
	req := api_models.ExecuteBatchMergeRequest{
		MergeIDs:     []int64{101},
		NewMainTitle: "Позиция",
	}

	dbErr := errors.New("connection reset")
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("UPDATE suggested_merges").
				WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
				WillReturnError(dbErr)
		}),
	)

	// WHEN
	result, err := service.ExecuteBatchMerge(ctx, req, "admin")

	// THEN wrapped DB error
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "ExecuteMergeBatch")
	var validationErr *apierrors.ValidationError
	assert.False(t, errors.As(err, &validationErr))
}

// ========================================================================
// RejectMerge tests
// ========================================================================

// TestRejectMerge_Success проверяет успешное отклонение PENDING-предложения.
func TestRejectMerge_Success(t *testing.T) {
	// GIVEN
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	mockStore.EXPECT().RejectPendingMerge(gomock.Any(), db.RejectPendingMergeParams{
		ResolvedBy: sql.NullString{String: "42", Valid: true},
		ID:         int64(100),
	}).Return(db.SuggestedMerge{
		ID:                  100,
		MainPositionID:      10,
		DuplicatePositionID: 20,
		SimilarityScore:     0.95,
		Status:              "REJECTED",
		CreatedAt:           now,
		UpdatedAt:           now,
		ResolvedAt:          sql.NullTime{Time: now, Valid: true},
		ResolvedBy:          sql.NullString{String: "42", Valid: true},
	}, nil)

	// WHEN
	err := service.RejectMerge(ctx, 100, "42")

	// THEN
	require.NoError(t, err)
}

// TestRejectMerge_EmptyRejectedBy проверяет ValidationError при пустом rejectedBy.
func TestRejectMerge_EmptyRejectedBy(t *testing.T) {
	// GIVEN
	service, _ := setupTestService(t)
	ctx := context.Background()

	// WHEN
	err := service.RejectMerge(ctx, 100, "")

	// THEN
	require.Error(t, err)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Contains(t, err.Error(), "rejectedBy")
}

// TestRejectMerge_NotFound проверяет NotFoundError когда merge не существует.
func TestRejectMerge_NotFound(t *testing.T) {
	// GIVEN
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// RejectPendingMerge возвращает ErrNoRows (merge не найден или не PENDING)
	mockStore.EXPECT().RejectPendingMerge(gomock.Any(), gomock.Any()).
		Return(db.SuggestedMerge{}, sql.ErrNoRows)

	// Fallback: GetSuggestedMergeByID тоже не находит → NotFoundError
	mockStore.EXPECT().GetSuggestedMergeByID(gomock.Any(), int64(999)).
		Return(db.SuggestedMerge{}, sql.ErrNoRows)

	// WHEN
	err := service.RejectMerge(ctx, 999, "42")

	// THEN
	require.Error(t, err)
	var notFoundErr *apierrors.NotFoundError
	assert.True(t, errors.As(err, &notFoundErr))
	assert.Contains(t, err.Error(), "999")
}

// TestRejectMerge_WrongStatus проверяет ValidationError когда merge не в PENDING.
func TestRejectMerge_WrongStatus(t *testing.T) {
	// GIVEN
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// RejectPendingMerge возвращает ErrNoRows (guard status='PENDING' не прошёл)
	mockStore.EXPECT().RejectPendingMerge(gomock.Any(), gomock.Any()).
		Return(db.SuggestedMerge{}, sql.ErrNoRows)

	// Fallback: GetSuggestedMergeByID находит merge в статусе EXECUTED
	mockStore.EXPECT().GetSuggestedMergeByID(gomock.Any(), int64(50)).
		Return(db.SuggestedMerge{
			ID:                  50,
			MainPositionID:      10,
			DuplicatePositionID: 20,
			SimilarityScore:     0.90,
			Status:              "EXECUTED",
			CreatedAt:           now,
			UpdatedAt:           now,
			ResolvedAt:          sql.NullTime{Time: now, Valid: true},
			ResolvedBy:          sql.NullString{String: "admin", Valid: true},
		}, nil)

	// WHEN
	err := service.RejectMerge(ctx, 50, "42")

	// THEN
	require.Error(t, err)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Contains(t, err.Error(), "EXECUTED")
	assert.Contains(t, err.Error(), "PENDING")
}

// TestRejectMerge_RejectPendingMerge_DBError проверяет проброс ошибки БД от RejectPendingMerge.
func TestRejectMerge_RejectPendingMerge_DBError(t *testing.T) {
	// GIVEN
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	dbErr := errors.New("connection refused")
	mockStore.EXPECT().RejectPendingMerge(gomock.Any(), gomock.Any()).
		Return(db.SuggestedMerge{}, dbErr)

	// WHEN
	err := service.RejectMerge(ctx, 100, "42")

	// THEN wrapped DB error (не ValidationError)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RejectPendingMerge")
	var validationErr *apierrors.ValidationError
	assert.False(t, errors.As(err, &validationErr))
}

// TestRejectMerge_GetSuggestedMergeByID_DBError проверяет проброс ошибки БД от fallback-запроса.
func TestRejectMerge_GetSuggestedMergeByID_DBError(t *testing.T) {
	// GIVEN
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// RejectPendingMerge → ErrNoRows (нужен fallback)
	mockStore.EXPECT().RejectPendingMerge(gomock.Any(), gomock.Any()).
		Return(db.SuggestedMerge{}, sql.ErrNoRows)

	// Fallback GetSuggestedMergeByID → ошибка БД
	dbErr := errors.New("timeout expired")
	mockStore.EXPECT().GetSuggestedMergeByID(gomock.Any(), int64(100)).
		Return(db.SuggestedMerge{}, dbErr)

	// WHEN
	err := service.RejectMerge(ctx, 100, "42")

	// THEN wrapped DB error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "merge 100")
	var validationErr *apierrors.ValidationError
	assert.False(t, errors.As(err, &validationErr))
	var notFoundErr *apierrors.NotFoundError
	assert.False(t, errors.As(err, &notFoundErr))
}
