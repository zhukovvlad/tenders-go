package testutil

import (
	"fmt"
	"sync"

	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

// LogLevel represents the severity level of a log entry.
type LogLevel string

const (
	LevelDebug LogLevel = "DEBUG"
	LevelInfo  LogLevel = "INFO"
	LevelWarn  LogLevel = "WARN"
	LevelError LogLevel = "ERROR"
	LevelFatal LogLevel = "FATAL"
)

// LogEntry represents a single recorded log call.
type LogEntry struct {
	Level   LogLevel
	Message string                 // formatted message (or fmt.Sprint of args)
	Fields  map[string]interface{} // accumulated structured fields
	Err     error                  // error attached via WithError
}

// MockLogger implements logging.Logger with call-recording.
// All logging methods record entries so tests can inspect what was logged.
//
// Usage:
//
//	logger := testutil.NewMockLogger()
//	// ... run code under test ...
//	entries := logger.Records()
//	assert.Len(t, entries, 1)
//	assert.Equal(t, testutil.LevelError, entries[0].Level)
type MockLogger struct {
	mu      sync.Mutex
	records []LogEntry
	fields  map[string]interface{}
	err     error
}

// NewMockLogger returns a ready-to-use MockLogger with recording enabled.
func NewMockLogger() *MockLogger {
	return &MockLogger{}
}

// Records returns a copy of all recorded log entries (thread-safe).
func (m *MockLogger) Records() []LogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]LogEntry, len(m.records))
	copy(cp, m.records)
	return cp
}

// Reset clears all recorded entries (thread-safe).
func (m *MockLogger) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = nil
}

func (m *MockLogger) record(level LogLevel, msg string) {
	entry := LogEntry{
		Level:   level,
		Message: msg,
		Fields:  copyFields(m.fields),
		Err:     m.err,
	}
	m.mu.Lock()
	m.records = append(m.records, entry)
	m.mu.Unlock()
}

// --- logging.Logger implementation ---

func (m *MockLogger) WithField(key string, value interface{}) logging.Logger {
	return &mockChild{parent: m, fields: map[string]interface{}{key: value}, err: m.err}
}

func (m *MockLogger) WithFields(fields map[string]interface{}) logging.Logger {
	return &mockChild{parent: m, fields: copyFields(fields), err: m.err}
}

func (m *MockLogger) WithError(err error) logging.Logger {
	return &mockChild{parent: m, fields: copyFields(m.fields), err: err}
}

func (m *MockLogger) Debug(args ...any) { m.record(LevelDebug, fmt.Sprint(args...)) }
func (m *MockLogger) Debugf(format string, args ...any) {
	m.record(LevelDebug, fmt.Sprintf(format, args...))
}
func (m *MockLogger) Info(args ...any) { m.record(LevelInfo, fmt.Sprint(args...)) }
func (m *MockLogger) Infof(format string, args ...any) {
	m.record(LevelInfo, fmt.Sprintf(format, args...))
}
func (m *MockLogger) Warn(args ...any) { m.record(LevelWarn, fmt.Sprint(args...)) }
func (m *MockLogger) Warnf(format string, args ...any) {
	m.record(LevelWarn, fmt.Sprintf(format, args...))
}
func (m *MockLogger) Error(args ...any) { m.record(LevelError, fmt.Sprint(args...)) }
func (m *MockLogger) Errorf(format string, args ...any) {
	m.record(LevelError, fmt.Sprintf(format, args...))
}
func (m *MockLogger) Fatal(args ...any) { m.record(LevelFatal, fmt.Sprint(args...)) }
func (m *MockLogger) Fatalf(format string, args ...any) {
	m.record(LevelFatal, fmt.Sprintf(format, args...))
}

// mockChild is a derived logger that shares the parent's record store
// but carries its own fields/error context.
type mockChild struct {
	parent *MockLogger
	fields map[string]interface{}
	err    error
}

func (c *mockChild) WithField(key string, value interface{}) logging.Logger {
	merged := copyFields(c.fields)
	merged[key] = value
	return &mockChild{parent: c.parent, fields: merged, err: c.err}
}

func (c *mockChild) WithFields(fields map[string]interface{}) logging.Logger {
	merged := copyFields(c.fields)
	for k, v := range fields {
		merged[k] = v
	}
	return &mockChild{parent: c.parent, fields: merged, err: c.err}
}

func (c *mockChild) WithError(err error) logging.Logger {
	return &mockChild{parent: c.parent, fields: copyFields(c.fields), err: err}
}

func (c *mockChild) record(level LogLevel, msg string) {
	entry := LogEntry{
		Level:   level,
		Message: msg,
		Fields:  copyFields(c.fields),
		Err:     c.err,
	}
	c.parent.mu.Lock()
	c.parent.records = append(c.parent.records, entry)
	c.parent.mu.Unlock()
}

func (c *mockChild) Debug(args ...any) { c.record(LevelDebug, fmt.Sprint(args...)) }
func (c *mockChild) Debugf(format string, args ...any) {
	c.record(LevelDebug, fmt.Sprintf(format, args...))
}
func (c *mockChild) Info(args ...any) { c.record(LevelInfo, fmt.Sprint(args...)) }
func (c *mockChild) Infof(format string, args ...any) {
	c.record(LevelInfo, fmt.Sprintf(format, args...))
}
func (c *mockChild) Warn(args ...any) { c.record(LevelWarn, fmt.Sprint(args...)) }
func (c *mockChild) Warnf(format string, args ...any) {
	c.record(LevelWarn, fmt.Sprintf(format, args...))
}
func (c *mockChild) Error(args ...any) { c.record(LevelError, fmt.Sprint(args...)) }
func (c *mockChild) Errorf(format string, args ...any) {
	c.record(LevelError, fmt.Sprintf(format, args...))
}
func (c *mockChild) Fatal(args ...any) { c.record(LevelFatal, fmt.Sprint(args...)) }
func (c *mockChild) Fatalf(format string, args ...any) {
	c.record(LevelFatal, fmt.Sprintf(format, args...))
}

// copyFields returns a shallow copy of a fields map (nil-safe).
func copyFields(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	cp := make(map[string]interface{}, len(src))
	for k, v := range src {
		cp[k] = v
	}
	return cp
}
