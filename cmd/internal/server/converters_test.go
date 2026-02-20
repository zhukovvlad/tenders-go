// Purpose: Protects against regressions in model conversion functions that transform
// database types (SQLC-generated) into API response types. Ensures key_parameters JSON
// parsing handles NULL, empty, invalid, and nested JSON correctly, and that newLotResponse
// correctly maps all fields including time formatting and empty slice initialization.
package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/testutil"
)

// =============================================================================
// parseKeyParameters TESTS
// =============================================================================

/*
BEHAVIORAL SCENARIOS:

Given a valid JSON NullRawMessage
When parseKeyParameters is called
Then it returns the raw JSON as-is

Given a NULL NullRawMessage (Valid=false)
When parseKeyParameters is called
Then it returns an empty JSON object "{}"

Given an empty byte slice NullRawMessage
When parseKeyParameters is called
Then it returns an empty JSON object "{}"

Given a NullRawMessage containing the string "null"
When parseKeyParameters is called
Then it returns an empty JSON object "{}"

Given an invalid JSON NullRawMessage
When parseKeyParameters is called
Then it returns an empty JSON object "{}" and logs a warning
*/

func TestParseKeyParameters_ValidJSON_ReturnsAsIs(t *testing.T) {
	logger := testutil.NewMockLogger()
	input := pqtype.NullRawMessage{
		RawMessage: json.RawMessage(`{"price":100,"currency":"RUB"}`),
		Valid:      true,
	}

	result := parseKeyParameters(input, logger)

	assert.JSONEq(t, `{"price":100,"currency":"RUB"}`, string(result))
	assert.Empty(t, logger.Records(), "no warnings should be logged for valid JSON")
}

func TestParseKeyParameters_NullValue_ReturnsEmptyObject(t *testing.T) {
	logger := testutil.NewMockLogger()
	input := pqtype.NullRawMessage{
		Valid: false,
	}

	result := parseKeyParameters(input, logger)

	assert.JSONEq(t, `{}`, string(result))
	assert.Empty(t, logger.Records())
}

func TestParseKeyParameters_EmptyBytes_ReturnsEmptyObject(t *testing.T) {
	logger := testutil.NewMockLogger()
	input := pqtype.NullRawMessage{
		RawMessage: json.RawMessage(``),
		Valid:      true,
	}

	result := parseKeyParameters(input, logger)

	assert.JSONEq(t, `{}`, string(result))
	assert.Empty(t, logger.Records())
}

func TestParseKeyParameters_NullString_ReturnsEmptyObject(t *testing.T) {
	logger := testutil.NewMockLogger()
	input := pqtype.NullRawMessage{
		RawMessage: json.RawMessage(`null`),
		Valid:      true,
	}

	result := parseKeyParameters(input, logger)

	assert.JSONEq(t, `{}`, string(result))
	assert.Empty(t, logger.Records())
}

func TestParseKeyParameters_InvalidJSON_ReturnsEmptyObjectAndLogsWarning(t *testing.T) {
	logger := testutil.NewMockLogger()
	input := pqtype.NullRawMessage{
		RawMessage: json.RawMessage(`{broken json`),
		Valid:      true,
	}

	result := parseKeyParameters(input, logger)

	assert.JSONEq(t, `{}`, string(result))

	records := logger.Records()
	require.Len(t, records, 1, "should log exactly one warning")
	assert.Equal(t, testutil.LevelWarn, records[0].Level)
	assert.Contains(t, records[0].Message, "невалидный JSON")
}

func TestParseKeyParameters_NestedJSON_PreservesStructure(t *testing.T) {
	logger := testutil.NewMockLogger()
	nested := `{"params":{"min_price":500,"tags":["urgent","construction"]},"version":2}`
	input := pqtype.NullRawMessage{
		RawMessage: json.RawMessage(nested),
		Valid:      true,
	}

	result := parseKeyParameters(input, logger)

	assert.JSONEq(t, nested, string(result))
	assert.Empty(t, logger.Records())
}

func TestParseKeyParameters_ArrayJSON_PreservesArray(t *testing.T) {
	logger := testutil.NewMockLogger()
	arrayJSON := `[1,2,3]`
	input := pqtype.NullRawMessage{
		RawMessage: json.RawMessage(arrayJSON),
		Valid:      true,
	}

	result := parseKeyParameters(input, logger)

	assert.JSONEq(t, arrayJSON, string(result))
	assert.Empty(t, logger.Records())
}

func TestParseKeyParameters_BooleanAndNumberTypes_PreservesTypes(t *testing.T) {
	logger := testutil.NewMockLogger()
	input := pqtype.NullRawMessage{
		RawMessage: json.RawMessage(`{"active":true,"count":42,"rate":3.14,"name":"test"}`),
		Valid:      true,
	}

	result := parseKeyParameters(input, logger)

	assert.JSONEq(t, `{"active":true,"count":42,"rate":3.14,"name":"test"}`, string(result))
	assert.Empty(t, logger.Records())
}

func TestParseKeyParameters_EmptyObject_ReturnsEmptyObject(t *testing.T) {
	logger := testutil.NewMockLogger()
	input := pqtype.NullRawMessage{
		RawMessage: json.RawMessage(`{}`),
		Valid:      true,
	}

	result := parseKeyParameters(input, logger)

	assert.JSONEq(t, `{}`, string(result))
	assert.Empty(t, logger.Records())
}

func TestParseKeyParameters_ValidFalseWithData_ReturnsEmptyObject(t *testing.T) {
	// Even if RawMessage contains data, Valid=false means it's NULL in DB
	logger := testutil.NewMockLogger()
	input := pqtype.NullRawMessage{
		RawMessage: json.RawMessage(`{"should":"be_ignored"}`),
		Valid:      false,
	}

	result := parseKeyParameters(input, logger)

	assert.JSONEq(t, `{}`, string(result))
	assert.Empty(t, logger.Records())
}

func TestParseKeyParameters_WhitespaceOnlyJSON_LogsWarning(t *testing.T) {
	logger := testutil.NewMockLogger()
	input := pqtype.NullRawMessage{
		RawMessage: json.RawMessage(`   `),
		Valid:      true,
	}

	result := parseKeyParameters(input, logger)

	// Whitespace-only is not valid JSON
	assert.JSONEq(t, `{}`, string(result))
	records := logger.Records()
	require.Len(t, records, 1)
	assert.Equal(t, testutil.LevelWarn, records[0].Level)
}

func TestParseKeyParameters_UnicodeJSON_PreservesUnicode(t *testing.T) {
	logger := testutil.NewMockLogger()
	unicodeJSON := `{"описание":"Строительные работы","город":"Москва"}`
	input := pqtype.NullRawMessage{
		RawMessage: json.RawMessage(unicodeJSON),
		Valid:      true,
	}

	result := parseKeyParameters(input, logger)

	assert.JSONEq(t, unicodeJSON, string(result))
	assert.Empty(t, logger.Records())
}

// =============================================================================
// newLotResponse TESTS
// =============================================================================

/*
BEHAVIORAL SCENARIOS:

Given a db.Lot with all fields populated
When newLotResponse is called
Then all fields are correctly mapped, timestamps are RFC3339, Proposals/Winners are empty slices

Given a db.Lot with NULL key_parameters
When newLotResponse is called
Then KeyParameters is "{}", Proposals and Winners are empty slices (not nil)

Given a db.Lot with specific timestamps
When newLotResponse is called
Then CreatedAt and UpdatedAt are formatted as RFC3339 strings
*/

func TestNewLotResponse_AllFieldsPopulated(t *testing.T) {
	logger := testutil.NewMockLogger()
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)

	lot := db.Lot{
		ID:       42,
		LotKey:   "LOT-001",
		LotTitle: "Строительные материалы",
		LotKeyParameters: pqtype.NullRawMessage{
			RawMessage: json.RawMessage(`{"min_price":1000,"max_price":5000}`),
			Valid:      true,
		},
		TenderID:  10,
		CreatedAt: now,
		UpdatedAt: now.Add(24 * time.Hour),
	}

	resp := newLotResponse(lot, logger)

	assert.Equal(t, int64(42), resp.ID)
	assert.Equal(t, "LOT-001", resp.LotKey)
	assert.Equal(t, "Строительные материалы", resp.LotTitle)
	assert.Equal(t, int64(10), resp.TenderID)
	assert.JSONEq(t, `{"min_price":1000,"max_price":5000}`, string(resp.KeyParameters))
	assert.Equal(t, "2025-06-15T10:30:00Z", resp.CreatedAt)
	assert.Equal(t, "2025-06-16T10:30:00Z", resp.UpdatedAt)

	// Proposals and Winners must be empty slices, not nil (for JSON serialization)
	assert.NotNil(t, resp.Proposals, "Proposals must be initialized (not nil)")
	assert.NotNil(t, resp.Winners, "Winners must be initialized (not nil)")
	assert.Empty(t, resp.Proposals)
	assert.Empty(t, resp.Winners)

	assert.Empty(t, logger.Records())
}

func TestNewLotResponse_NullKeyParameters(t *testing.T) {
	logger := testutil.NewMockLogger()
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	lot := db.Lot{
		ID:       1,
		LotKey:   "LOT-X",
		LotTitle: "Lot without params",
		LotKeyParameters: pqtype.NullRawMessage{
			Valid: false,
		},
		TenderID:  5,
		CreatedAt: now,
		UpdatedAt: now,
	}

	resp := newLotResponse(lot, logger)

	assert.JSONEq(t, `{}`, string(resp.KeyParameters))
	assert.Empty(t, logger.Records())
}

func TestNewLotResponse_TimestampFormatting(t *testing.T) {
	tests := []struct {
		name      string
		inputTime time.Time
		expected  string
	}{
		{
			name:      "UTC midnight",
			inputTime: time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
			expected:  "2024-12-31T00:00:00Z",
		},
		{
			name:      "UTC with time",
			inputTime: time.Date(2025, 3, 15, 14, 30, 45, 0, time.UTC),
			expected:  "2025-03-15T14:30:45Z",
		},
		{
			name:      "non-UTC timezone",
			inputTime: time.Date(2025, 7, 1, 12, 0, 0, 0, time.FixedZone("MSK", 3*60*60)),
			expected:  "2025-07-01T12:00:00+03:00",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger := testutil.NewMockLogger()
			lot := db.Lot{
				ID:               1,
				LotKey:           "K",
				LotTitle:         "T",
				LotKeyParameters: pqtype.NullRawMessage{Valid: false},
				TenderID:         1,
				CreatedAt:        tc.inputTime,
				UpdatedAt:        tc.inputTime,
			}

			resp := newLotResponse(lot, logger)

			assert.Equal(t, tc.expected, resp.CreatedAt)
			assert.Equal(t, tc.expected, resp.UpdatedAt)
			assert.Empty(t, logger.Records())
		})
	}
}

func TestNewLotResponse_JSONSerializationProducesArrays(t *testing.T) {
	// Ensures that when the response is serialized to JSON,
	// Proposals and Winners appear as [] instead of null
	logger := testutil.NewMockLogger()
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	lot := db.Lot{
		ID:               1,
		LotKey:           "LOT-1",
		LotTitle:         "Test",
		LotKeyParameters: pqtype.NullRawMessage{Valid: false},
		TenderID:         1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	resp := newLotResponse(lot, logger)

	jsonBytes, err := json.Marshal(resp)
	require.NoError(t, err)

	jsonStr := string(jsonBytes)
	assert.Contains(t, jsonStr, `"proposals":[]`, "proposals must serialize as empty array, not null")
	assert.Contains(t, jsonStr, `"winners":[]`, "winners must serialize as empty array, not null")
}

func TestNewLotResponse_InvalidKeyParameters_FallsBackToEmptyObject(t *testing.T) {
	logger := testutil.NewMockLogger()
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	lot := db.Lot{
		ID:       1,
		LotKey:   "LOT-BAD",
		LotTitle: "Lot with broken JSON",
		LotKeyParameters: pqtype.NullRawMessage{
			RawMessage: json.RawMessage(`{not valid`),
			Valid:      true,
		},
		TenderID:  1,
		CreatedAt: now,
		UpdatedAt: now,
	}

	resp := newLotResponse(lot, logger)

	assert.JSONEq(t, `{}`, string(resp.KeyParameters))
	// Should have logged a warning
	records := logger.Records()
	require.Len(t, records, 1)
	assert.Equal(t, testutil.LevelWarn, records[0].Level)
}

func TestNewLotResponse_FieldMappingIsComplete(t *testing.T) {
	// Verifies that all db.Lot fields are correctly mapped to LotResponse
	logger := testutil.NewMockLogger()
	createdAt := time.Date(2024, 6, 1, 8, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2024, 6, 2, 16, 30, 0, 0, time.UTC)

	lot := db.Lot{
		ID:       999,
		LotKey:   "UNIQUE-KEY-123",
		LotTitle: "Полное тестирование маппинга",
		LotKeyParameters: pqtype.NullRawMessage{
			RawMessage: json.RawMessage(`{"a":1}`),
			Valid:      true,
		},
		TenderID:  777,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}

	resp := newLotResponse(lot, logger)

	// Verify every field
	assert.Equal(t, lot.ID, resp.ID, "ID must match")
	assert.Equal(t, lot.LotKey, resp.LotKey, "LotKey must match")
	assert.Equal(t, lot.LotTitle, resp.LotTitle, "LotTitle must match")
	assert.Equal(t, lot.TenderID, resp.TenderID, "TenderID must match")
	assert.Equal(t, createdAt.Format(time.RFC3339), resp.CreatedAt, "CreatedAt must match RFC3339")
	assert.Equal(t, updatedAt.Format(time.RFC3339), resp.UpdatedAt, "UpdatedAt must match RFC3339")
	assert.JSONEq(t, `{"a":1}`, string(resp.KeyParameters), "KeyParameters must match")

	assert.Empty(t, logger.Records())
}

func TestNewLotResponse_ZeroValueLot(t *testing.T) {
	// Edge case: zero-value db.Lot (all defaults)
	logger := testutil.NewMockLogger()

	var lot db.Lot // zero value

	resp := newLotResponse(lot, logger)

	assert.Equal(t, int64(0), resp.ID)
	assert.Equal(t, "", resp.LotKey)
	assert.Equal(t, "", resp.LotTitle)
	assert.Equal(t, int64(0), resp.TenderID)
	assert.JSONEq(t, `{}`, string(resp.KeyParameters)) // Valid=false → empty object
	assert.Equal(t, time.Time{}.Format(time.RFC3339), resp.CreatedAt, "zero-value CreatedAt must format as RFC3339")
	assert.Equal(t, time.Time{}.Format(time.RFC3339), resp.UpdatedAt, "zero-value UpdatedAt must format as RFC3339")
	assert.NotNil(t, resp.Proposals)
	assert.NotNil(t, resp.Winners)
}
