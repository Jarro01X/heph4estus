package logger

import (
	"log"
	"os"
)

// Logger is a simple logger interface
type Logger interface {
	Info(format string, v ...interface{})
	Error(format string, v ...interface{})
	Fatal(format string, v ...interface{})
}

// SimpleLogger is a simple implementation of the Logger interface
type SimpleLogger struct {
	infoLogger  *log.Logger
	errorLogger *log.Logger
}

// NewSimpleLogger creates a new simple logger
func NewSimpleLogger() *SimpleLogger {
	return &SimpleLogger{
		infoLogger:  log.New(os.Stdout, "INFO: ", log.LstdFlags),
		errorLogger: log.New(os.Stderr, "ERROR: ", log.LstdFlags),
	}
}

// Info logs an informational message
func (l *SimpleLogger) Info(format string, v ...interface{}) {
	l.infoLogger.Printf(format, v...)
}

// Error logs an error message
func (l *SimpleLogger) Error(format string, v ...interface{}) {
	l.errorLogger.Printf(format, v...)
}

// Fatal logs a fatal error message and exits
func (l *SimpleLogger) Fatal(format string, v ...interface{}) {
	l.errorLogger.Printf(format, v...)
	os.Exit(1)
}
