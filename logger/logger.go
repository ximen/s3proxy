package logger

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// LogLevel представляет уровень логирования
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

// String возвращает строковое представление уровня логирования
func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ParseLogLevel парсит строку в LogLevel
func ParseLogLevel(level string) LogLevel {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return DEBUG
	case "INFO":
		return INFO
	case "WARN", "WARNING":
		return WARN
	case "ERROR":
		return ERROR
	default:
		return INFO // по умолчанию INFO
	}
}

// Logger представляет логгер с уровнями
type Logger struct {
	level  LogLevel
	logger *log.Logger
}

// New создает новый логгер с указанным уровнем
func New(level LogLevel) *Logger {
	return &Logger{
		level:  level,
		logger: log.New(os.Stdout, "", log.LstdFlags),
	}
}

// SetLevel устанавливает уровень логирования
func (l *Logger) SetLevel(level LogLevel) {
	l.level = level
}

// GetLevel возвращает текущий уровень логирования
func (l *Logger) GetLevel() LogLevel {
	return l.level
}

// logf выводит сообщение с указанным уровнем
func (l *Logger) logf(level LogLevel, format string, args ...interface{}) {
	if level >= l.level {
		prefix := fmt.Sprintf("[%s] ", level.String())
		l.logger.Printf(prefix+format, args...)
	}
}

// Debug выводит отладочное сообщение
func (l *Logger) Debug(format string, args ...interface{}) {
	l.logf(DEBUG, format, args...)
}

// Info выводит информационное сообщение
func (l *Logger) Info(format string, args ...interface{}) {
	l.logf(INFO, format, args...)
}

// Warn выводит предупреждение
func (l *Logger) Warn(format string, args ...interface{}) {
	l.logf(WARN, format, args...)
}

// Error выводит сообщение об ошибке
func (l *Logger) Error(format string, args ...interface{}) {
	l.logf(ERROR, format, args...)
}

// Глобальный логгер
var globalLogger = New(INFO)

// SetGlobalLevel устанавливает уровень для глобального логгера
func SetGlobalLevel(level LogLevel) {
	globalLogger.SetLevel(level)
}

// GetGlobalLevel возвращает уровень глобального логгера
func GetGlobalLevel() LogLevel {
	return globalLogger.GetLevel()
}

// Глобальные функции для удобства
func Debug(format string, args ...interface{}) {
	globalLogger.Debug(format, args...)
}

func Info(format string, args ...interface{}) {
	globalLogger.Info(format, args...)
}

func Warn(format string, args ...interface{}) {
	globalLogger.Warn(format, args...)
}

func Error(format string, args ...interface{}) {
	globalLogger.Error(format, args...)
}
