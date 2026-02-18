package lot

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/apierrors"
	"github.com/zhukovvlad/tenders-go/cmd/internal/testutil"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

/*
BEHAVIORAL SCENARIOS FOR LOT SERVICE (Unit Tests)

What user problems does this protect us from?
================================================================================
1. Key parameters update — lot key params must be correctly serialized and stored
2. Input validation — invalid lot IDs must be rejected (ValidationError)
3. Resource lookup — missing tenders/lots must return NotFoundError (not 500)
4. Error propagation — DB errors must be properly wrapped and returned
5. Transaction safety — all updates happen inside ExecTx

These unit tests use gomock MockStore + go-sqlmock for *Queries inside ExecTx
to test LotService methods in isolation without a real database.

GIVEN / WHEN / THEN Scenarios:
================================================================================

SCENARIO 1: UpdateLotKeyParameters
- GIVEN valid tenderEtpID, lotKey, and keyParameters with existing tender and lot
  WHEN UpdateLotKeyParameters is called
  THEN lot key parameters are updated successfully

- GIVEN a non-existent tender (sql.ErrNoRows)
  WHEN UpdateLotKeyParameters is called
  THEN NotFoundError is returned

- GIVEN an existing tender but non-existent lot (sql.ErrNoRows)
  WHEN UpdateLotKeyParameters is called
  THEN NotFoundError is returned

- GIVEN a DB error when looking up the tender
  WHEN UpdateLotKeyParameters is called
  THEN wrapped DB error is returned

- GIVEN a DB error when looking up the lot
  WHEN UpdateLotKeyParameters is called
  THEN wrapped DB error is returned

- GIVEN a DB error when updating the lot
  WHEN UpdateLotKeyParameters is called
  THEN wrapped DB error is returned

- GIVEN keyParameters that cannot be serialized to JSON
  WHEN UpdateLotKeyParameters is called
  THEN serialization error is returned without calling ExecTx

SCENARIO 2: UpdateLotKeyParametersDirectly
- GIVEN valid lotIDStr and keyParameters with existing lot
  WHEN UpdateLotKeyParametersDirectly is called
  THEN lot key parameters are updated successfully

- GIVEN an invalid (non-numeric) lotIDStr
  WHEN UpdateLotKeyParametersDirectly is called
  THEN ValidationError is returned without calling ExecTx

- GIVEN a valid lotIDStr but non-existent lot (sql.ErrNoRows)
  WHEN UpdateLotKeyParametersDirectly is called
  THEN NotFoundError is returned

- GIVEN a DB error when looking up the lot
  WHEN UpdateLotKeyParametersDirectly is called
  THEN wrapped DB error is returned

- GIVEN a DB error when updating the lot
  WHEN UpdateLotKeyParametersDirectly is called
  THEN wrapped DB error is returned

- GIVEN keyParameters that cannot be serialized to JSON
  WHEN UpdateLotKeyParametersDirectly is called
  THEN serialization error is returned without calling ExecTx
*/

// setupTestService creates a LotService with mock store for unit testing.
// gomock.NewController registers t.Cleanup(ctrl.Finish) automatically,
// so callers don't need to defer ctrl.Finish().
func setupTestService(t *testing.T) (*LotService, *db.MockStore) {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockStore := db.NewMockStore(ctrl)
	logger := testutil.NewMockLogger()

	service := &LotService{
		store:  mockStore,
		logger: logger,
	}

	return service, mockStore
}

// Helper: column names for SQL result sets
var (
	tenderColumns = []string{"id", "etp_id", "title", "category_id", "object_id", "executor_id", "data_prepared_on_date", "created_at", "updated_at"}
	lotColumns    = []string{"id", "lot_key", "lot_title", "lot_key_parameters", "tender_id", "created_at", "updated_at"}
)

// Helper: create a mock DB + Queries for use inside ExecTx DoAndReturn.
// Returns the sqlmock for setting up expectations and a *db.Queries backed by it.
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

// Helper: execTxDoAndReturn returns a DoAndReturn function that executes
// the ExecTx callback with a sqlmock-backed *Queries.
// The setupFn is called to set up sqlmock expectations before running the callback.
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
// UpdateLotKeyParameters TESTS
// =============================================================================

func TestUpdateLotKeyParameters_Success(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN valid tender, lot, and key parameters
	tenderEtpID := "ETP-123"
	lotKey := "lot-1"
	keyParams := map[string]interface{}{"param1": "value1", "param2": 42.0}

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// GetTenderByEtpID returns tender
			mock.ExpectQuery("SELECT .+ FROM tenders WHERE etp_id").
				WithArgs("ETP-123").
				WillReturnRows(sqlmock.NewRows(tenderColumns).
					AddRow(int64(1), "ETP-123", "Test Tender", nil, int64(1), int64(1), nil, now, now))

			// GetLotByTenderAndKey returns lot
			mock.ExpectQuery("SELECT .+ FROM lots WHERE tender_id").
				WithArgs(int64(1), "lot-1").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(int64(10), "lot-1", "Test Lot", nil, int64(1), now, now))

			// UpdateLotDetails returns updated lot
			mock.ExpectQuery("UPDATE lots").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(int64(10), "lot-1", "Test Lot", []byte(`{"param1":"value1","param2":42}`), int64(1), now, now))
		}),
	)

	// WHEN
	err := service.UpdateLotKeyParameters(ctx, tenderEtpID, lotKey, keyParams)

	// THEN
	assert.NoError(t, err)
}

func TestUpdateLotKeyParameters_TenderNotFound(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN tender does not exist
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("SELECT .+ FROM tenders WHERE etp_id").
				WithArgs("NONEXISTENT").
				WillReturnError(sql.ErrNoRows)
		}),
	)

	// WHEN
	err := service.UpdateLotKeyParameters(ctx, "NONEXISTENT", "lot-1", map[string]interface{}{"k": "v"})

	// THEN
	require.Error(t, err)
	var notFoundErr *apierrors.NotFoundError
	assert.True(t, errors.As(err, &notFoundErr), "expected NotFoundError, got: %T", err)
	assert.Contains(t, notFoundErr.Message, "NONEXISTENT")
}

func TestUpdateLotKeyParameters_LotNotFound(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN tender exists but lot does not
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("SELECT .+ FROM tenders WHERE etp_id").
				WithArgs("ETP-123").
				WillReturnRows(sqlmock.NewRows(tenderColumns).
					AddRow(int64(1), "ETP-123", "Test Tender", nil, int64(1), int64(1), nil, now, now))

			mock.ExpectQuery("SELECT .+ FROM lots WHERE tender_id").
				WithArgs(int64(1), "missing-lot").
				WillReturnError(sql.ErrNoRows)
		}),
	)

	// WHEN
	err := service.UpdateLotKeyParameters(ctx, "ETP-123", "missing-lot", map[string]interface{}{"k": "v"})

	// THEN
	require.Error(t, err)
	var notFoundErr *apierrors.NotFoundError
	assert.True(t, errors.As(err, &notFoundErr), "expected NotFoundError, got: %T", err)
	assert.Contains(t, notFoundErr.Message, "missing-lot")
}

func TestUpdateLotKeyParameters_TenderDBError(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	dbErr := errors.New("connection refused")

	// GIVEN a DB error when looking up the tender
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("SELECT .+ FROM tenders WHERE etp_id").
				WithArgs("ETP-123").
				WillReturnError(dbErr)
		}),
	)

	// WHEN
	err := service.UpdateLotKeyParameters(ctx, "ETP-123", "lot-1", map[string]interface{}{"k": "v"})

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ошибка при поиске тендера")
	// Must NOT be NotFoundError — it's a DB error
	var notFoundErr *apierrors.NotFoundError
	assert.False(t, errors.As(err, &notFoundErr), "DB error should not be NotFoundError")
}

func TestUpdateLotKeyParameters_LotDBError(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()
	dbErr := errors.New("timeout")

	// GIVEN tender exists, but DB error when looking up the lot
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("SELECT .+ FROM tenders WHERE etp_id").
				WithArgs("ETP-123").
				WillReturnRows(sqlmock.NewRows(tenderColumns).
					AddRow(int64(1), "ETP-123", "Test Tender", nil, int64(1), int64(1), nil, now, now))

			mock.ExpectQuery("SELECT .+ FROM lots WHERE tender_id").
				WithArgs(int64(1), "lot-1").
				WillReturnError(dbErr)
		}),
	)

	// WHEN
	err := service.UpdateLotKeyParameters(ctx, "ETP-123", "lot-1", map[string]interface{}{"k": "v"})

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ошибка при поиске лота")
}

func TestUpdateLotKeyParameters_UpdateDBError(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()
	dbErr := errors.New("unique constraint violation")

	// GIVEN tender and lot exist, but DB error when updating
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("SELECT .+ FROM tenders WHERE etp_id").
				WithArgs("ETP-123").
				WillReturnRows(sqlmock.NewRows(tenderColumns).
					AddRow(int64(1), "ETP-123", "Test Tender", nil, int64(1), int64(1), nil, now, now))

			mock.ExpectQuery("SELECT .+ FROM lots WHERE tender_id").
				WithArgs(int64(1), "lot-1").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(int64(10), "lot-1", "Test Lot", nil, int64(1), now, now))

			mock.ExpectQuery("UPDATE lots").
				WillReturnError(dbErr)
		}),
	)

	// WHEN
	err := service.UpdateLotKeyParameters(ctx, "ETP-123", "lot-1", map[string]interface{}{"k": "v"})

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "не удалось обновить ключевые параметры лота")
}

func TestUpdateLotKeyParameters_JSONSerializationError(t *testing.T) {
	service, _ := setupTestService(t)
	ctx := context.Background()

	// GIVEN keyParameters with a value that cannot be serialized to JSON
	badParams := map[string]interface{}{
		"bad_value": make(chan int), // channels cannot be JSON-serialized
	}

	// WHEN — no ExecTx expectation, error happens before the transaction
	err := service.UpdateLotKeyParameters(ctx, "ETP-123", "lot-1", badParams)

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "не удалось сериализовать ключевые параметры")
}

func TestUpdateLotKeyParameters_ExecTxError(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN ExecTx itself returns an error (e.g., cannot begin transaction)
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).Return(errors.New("tx begin failed"))

	// WHEN
	err := service.UpdateLotKeyParameters(ctx, "ETP-123", "lot-1", map[string]interface{}{"k": "v"})

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tx begin failed")
}

func TestUpdateLotKeyParameters_EmptyKeyParameters(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN empty key parameters (valid JSON: {})
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("SELECT .+ FROM tenders WHERE etp_id").
				WithArgs("ETP-123").
				WillReturnRows(sqlmock.NewRows(tenderColumns).
					AddRow(int64(1), "ETP-123", "Test Tender", nil, int64(1), int64(1), nil, now, now))

			mock.ExpectQuery("SELECT .+ FROM lots WHERE tender_id").
				WithArgs(int64(1), "lot-1").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(int64(10), "lot-1", "Test Lot", nil, int64(1), now, now))

			mock.ExpectQuery("UPDATE lots").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(int64(10), "lot-1", "Test Lot", []byte(`{}`), int64(1), now, now))
		}),
	)

	// WHEN
	err := service.UpdateLotKeyParameters(ctx, "ETP-123", "lot-1", map[string]interface{}{})

	// THEN
	assert.NoError(t, err)
}

// =============================================================================
// UpdateLotKeyParametersDirectly TESTS
// =============================================================================

func TestUpdateLotKeyParametersDirectly_Success(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN valid lot ID and key parameters
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			// GetLotByID returns lot
			mock.ExpectQuery("SELECT .+ FROM lots WHERE id").
				WithArgs(int64(42)).
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(int64(42), "lot-key", "Test Lot", nil, int64(1), now, now))

			// UpdateLotDetails returns updated lot
			mock.ExpectQuery("UPDATE lots").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(int64(42), "lot-key", "Test Lot", []byte(`{"param":"value"}`), int64(1), now, now))
		}),
	)

	// WHEN
	err := service.UpdateLotKeyParametersDirectly(ctx, "42", map[string]interface{}{"param": "value"})

	// THEN
	assert.NoError(t, err)
}

func TestUpdateLotKeyParametersDirectly_InvalidLotID_NotANumber(t *testing.T) {
	service, _ := setupTestService(t)
	ctx := context.Background()

	// GIVEN non-numeric lot ID — no ExecTx expectation
	err := service.UpdateLotKeyParametersDirectly(ctx, "abc", map[string]interface{}{"k": "v"})

	// THEN
	require.Error(t, err)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr), "expected ValidationError, got: %T", err)
	assert.Contains(t, validationErr.Message, "abc")
}

func TestUpdateLotKeyParametersDirectly_InvalidLotID_EmptyString(t *testing.T) {
	service, _ := setupTestService(t)
	ctx := context.Background()

	// GIVEN empty lot ID string
	err := service.UpdateLotKeyParametersDirectly(ctx, "", map[string]interface{}{"k": "v"})

	// THEN
	require.Error(t, err)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr), "expected ValidationError, got: %T", err)
}

func TestUpdateLotKeyParametersDirectly_InvalidLotID_Float(t *testing.T) {
	service, _ := setupTestService(t)
	ctx := context.Background()

	// GIVEN float-format lot ID
	err := service.UpdateLotKeyParametersDirectly(ctx, "3.14", map[string]interface{}{"k": "v"})

	// THEN
	require.Error(t, err)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr), "expected ValidationError, got: %T", err)
}

func TestUpdateLotKeyParametersDirectly_LotNotFound(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN lot does not exist
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("SELECT .+ FROM lots WHERE id").
				WithArgs(int64(999)).
				WillReturnError(sql.ErrNoRows)
		}),
	)

	// WHEN
	err := service.UpdateLotKeyParametersDirectly(ctx, "999", map[string]interface{}{"k": "v"})

	// THEN
	require.Error(t, err)
	var notFoundErr *apierrors.NotFoundError
	assert.True(t, errors.As(err, &notFoundErr), "expected NotFoundError, got: %T", err)
	assert.Contains(t, notFoundErr.Message, "999")
}

func TestUpdateLotKeyParametersDirectly_LotDBError(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	dbErr := errors.New("connection refused")

	// GIVEN DB error when looking up the lot
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("SELECT .+ FROM lots WHERE id").
				WithArgs(int64(42)).
				WillReturnError(dbErr)
		}),
	)

	// WHEN
	err := service.UpdateLotKeyParametersDirectly(ctx, "42", map[string]interface{}{"k": "v"})

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ошибка при поиске лота")
	var notFoundErr *apierrors.NotFoundError
	assert.False(t, errors.As(err, &notFoundErr), "DB error should not be NotFoundError")
}

func TestUpdateLotKeyParametersDirectly_UpdateDBError(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()
	dbErr := errors.New("disk full")

	// GIVEN lot exists but DB error when updating
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("SELECT .+ FROM lots WHERE id").
				WithArgs(int64(42)).
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(int64(42), "lot-key", "Test Lot", nil, int64(1), now, now))

			mock.ExpectQuery("UPDATE lots").
				WillReturnError(dbErr)
		}),
	)

	// WHEN
	err := service.UpdateLotKeyParametersDirectly(ctx, "42", map[string]interface{}{"k": "v"})

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "не удалось обновить ключевые параметры лота")
}

func TestUpdateLotKeyParametersDirectly_JSONSerializationError(t *testing.T) {
	service, _ := setupTestService(t)
	ctx := context.Background()

	// GIVEN keyParameters with an unserializable value — no ExecTx expectation
	badParams := map[string]interface{}{
		"fn": func() {}, // functions cannot be JSON-serialized
	}

	// WHEN
	err := service.UpdateLotKeyParametersDirectly(ctx, "42", badParams)

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "не удалось сериализовать ключевые параметры")
}

func TestUpdateLotKeyParametersDirectly_ExecTxError(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()

	// GIVEN ExecTx itself returns an error
	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).Return(errors.New("tx begin failed"))

	// WHEN
	err := service.UpdateLotKeyParametersDirectly(ctx, "42", map[string]interface{}{"k": "v"})

	// THEN
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tx begin failed")
}

func TestUpdateLotKeyParametersDirectly_LargeNumericID(t *testing.T) {
	service, mockStore := setupTestService(t)
	ctx := context.Background()
	now := time.Now()

	// GIVEN a large but valid int64 lot ID
	largeID := "9223372036854775807" // max int64

	mockStore.EXPECT().ExecTx(gomock.Any(), gomock.Any()).DoAndReturn(
		execTxDoAndReturn(t, func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery("SELECT .+ FROM lots WHERE id").
				WithArgs(int64(9223372036854775807)).
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(int64(9223372036854775807), "lot-max", "Max Lot", nil, int64(1), now, now))

			mock.ExpectQuery("UPDATE lots").
				WillReturnRows(sqlmock.NewRows(lotColumns).
					AddRow(int64(9223372036854775807), "lot-max", "Max Lot", []byte(`{"k":"v"}`), int64(1), now, now))
		}),
	)

	// WHEN
	err := service.UpdateLotKeyParametersDirectly(ctx, largeID, map[string]interface{}{"k": "v"})

	// THEN
	assert.NoError(t, err)
}

func TestUpdateLotKeyParametersDirectly_OverflowID(t *testing.T) {
	service, _ := setupTestService(t)
	ctx := context.Background()

	// GIVEN lot ID that overflows int64
	err := service.UpdateLotKeyParametersDirectly(ctx, "9223372036854775808", map[string]interface{}{"k": "v"})

	// THEN — ParseInt fails
	require.Error(t, err)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr), "expected ValidationError, got: %T", err)
}

func TestUpdateLotKeyParametersDirectly_NegativeID(t *testing.T) {
	service, _ := setupTestService(t)
	ctx := context.Background()

	// GIVEN negative lot ID — rejected before DB access
	// No ExecTx expectation: validation fires before transaction

	// WHEN
	err := service.UpdateLotKeyParametersDirectly(ctx, "-1", map[string]interface{}{"k": "v"})

	// THEN
	require.Error(t, err)
	var validationErr *apierrors.ValidationError
	assert.True(t, errors.As(err, &validationErr), "expected ValidationError, got: %T", err)
	assert.Contains(t, err.Error(), "отрицательным")
}

// =============================================================================
// NewLotService TESTS
// =============================================================================

func TestNewLotService_CreatesInstance(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockStore := db.NewMockStore(ctrl)
	logger := testutil.NewMockLogger()

	// WHEN
	service := NewLotService(mockStore, logger)

	// THEN
	require.NotNil(t, service)
	assert.Equal(t, mockStore, service.store)
	assert.Equal(t, logger, service.logger)
}

// =============================================================================
// Logger interface compliance TESTS
// =============================================================================

func TestMockLoggerImplementsInterface(t *testing.T) {
	// Verify that testutil.MockLogger implements the logging.Logger interface
	var _ logging.Logger = (*testutil.MockLogger)(nil)
}
