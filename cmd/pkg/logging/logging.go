package logging

import (
	"fmt"
	"io"
	"os"
	"path"
	"runtime"

	"github.com/sirupsen/logrus"
)

// Logger defines the unified logging interface used across all services.
// This interface supports structured logging via WithField/WithFields/WithError.
// The concrete implementation (*logrusLogger) satisfies this interface.
//
// Any test can provide a no-op mock that implements Logger
// without importing logrus or any other logging library.
type Logger interface {
	WithField(key string, value interface{}) Logger
	WithFields(fields map[string]interface{}) Logger
	WithError(err error) Logger

	Debug(args ...any)
	Debugf(format string, args ...any)
	Info(args ...any)
	Infof(format string, args ...any)
	Warn(args ...any)
	Warnf(format string, args ...any)
	Error(args ...any)
	Errorf(format string, args ...any)
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

// logrusLogger is the concrete logrus-backed implementation of Logger.
type logrusLogger struct {
	entry *logrus.Entry
}

type writerHook struct {
	Writer    []io.Writer
	LogLevels []logrus.Level
}

func (hook *writerHook) Fire(entry *logrus.Entry) error {
	line, err := entry.String()
	if err != nil {
		return err
	}

	for _, w := range hook.Writer {
		w.Write([]byte(line))
	}
	return err
}

func (hook *writerHook) Levels() []logrus.Level {
	return hook.LogLevels
}

var e *logrus.Entry

// GetLogger returns the global Logger instance backed by logrus.
func GetLogger() Logger {
	return &logrusLogger{entry: e}
}

// --- Logger interface implementation for logrusLogger ---

func (l *logrusLogger) WithField(key string, value interface{}) Logger {
	return &logrusLogger{entry: l.entry.WithField(key, value)}
}

func (l *logrusLogger) WithFields(fields map[string]interface{}) Logger {
	return &logrusLogger{entry: l.entry.WithFields(logrus.Fields(fields))}
}

func (l *logrusLogger) WithError(err error) Logger {
	return &logrusLogger{entry: l.entry.WithError(err)}
}

func (l *logrusLogger) Debug(args ...any)                 { l.entry.Debug(args...) }
func (l *logrusLogger) Debugf(format string, args ...any) { l.entry.Debugf(format, args...) }
func (l *logrusLogger) Info(args ...any)                  { l.entry.Info(args...) }
func (l *logrusLogger) Infof(format string, args ...any)  { l.entry.Infof(format, args...) }
func (l *logrusLogger) Warn(args ...any)                  { l.entry.Warn(args...) }
func (l *logrusLogger) Warnf(format string, args ...any)  { l.entry.Warnf(format, args...) }
func (l *logrusLogger) Error(args ...any)                 { l.entry.Error(args...) }
func (l *logrusLogger) Errorf(format string, args ...any) { l.entry.Errorf(format, args...) }
func (l *logrusLogger) Fatal(args ...any)                 { l.entry.Fatal(args...) }
func (l *logrusLogger) Fatalf(format string, args ...any) { l.entry.Fatalf(format, args...) }

func init() {
	l := logrus.New()
	l.SetReportCaller(true)
	l.Formatter = &logrus.TextFormatter{
		CallerPrettyfier: func(frame *runtime.Frame) (function string, file string) {
			filename := path.Base(frame.File)
			return fmt.Sprintf("%s()", frame.Function), fmt.Sprintf("%s:%d", filename, frame.Line)
		},
		DisableColors: true,
		FullTimestamp: true,
	}
	err := os.MkdirAll("logs", 0755)
	if err != nil {
		panic(err)
	}

	allFile, err := os.OpenFile("logs/all.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
	if err != nil {
		panic(err)
	}

	l.SetOutput(io.Discard)

	l.AddHook(&writerHook{
		Writer:    []io.Writer{allFile, os.Stdout},
		LogLevels: logrus.AllLevels,
	})

	l.SetLevel(logrus.TraceLevel)

	e = logrus.NewEntry(l)
}
