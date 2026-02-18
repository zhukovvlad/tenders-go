package testutil

import "github.com/zhukovvlad/tenders-go/cmd/pkg/logging"

// MockLogger implements logging.Logger as a no-op for unit tests.
// Use NewMockLogger() in tests instead of defining local mockLogger types.
type MockLogger struct{}

// NewMockLogger returns a ready-to-use no-op MockLogger.
func NewMockLogger() *MockLogger { return &MockLogger{} }

func (m *MockLogger) WithField(key string, value interface{}) logging.Logger  { return m }
func (m *MockLogger) WithFields(fields map[string]interface{}) logging.Logger { return m }
func (m *MockLogger) WithError(err error) logging.Logger                      { return m }
func (m *MockLogger) Debug(args ...any)                                       {}
func (m *MockLogger) Debugf(format string, args ...any)                       {}
func (m *MockLogger) Info(args ...any)                                        {}
func (m *MockLogger) Infof(format string, args ...any)                        {}
func (m *MockLogger) Warn(args ...any)                                        {}
func (m *MockLogger) Warnf(format string, args ...any)                        {}
func (m *MockLogger) Error(args ...any)                                       {}
func (m *MockLogger) Errorf(format string, args ...any)                       {}
func (m *MockLogger) Fatal(args ...any)                                       {}
func (m *MockLogger) Fatalf(format string, args ...any)                       {}
