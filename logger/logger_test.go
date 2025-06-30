package logger

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestLogLevels(t *testing.T) {
	// Создаем буфер для захвата вывода
	var buf bytes.Buffer
	
	// Создаем логгер с уровнем DEBUG и нашим буфером
	logger := &Logger{
		level:  DEBUG,
		logger: log.New(&buf, "", log.LstdFlags),
	}

	// Тестируем все уровни
	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	output := buf.String()

	// Проверяем, что все сообщения присутствуют
	if !strings.Contains(output, "[DEBUG] debug message") {
		t.Error("DEBUG message not found")
	}
	if !strings.Contains(output, "[INFO] info message") {
		t.Error("INFO message not found")
	}
	if !strings.Contains(output, "[WARN] warn message") {
		t.Error("WARN message not found")
	}
	if !strings.Contains(output, "[ERROR] error message") {
		t.Error("ERROR message not found")
	}
}

func TestLogLevelFiltering(t *testing.T) {
	// Создаем буфер для захвата вывода
	var buf bytes.Buffer
	
	// Создаем логгер с уровнем ERROR и нашим буфером
	logger := &Logger{
		level:  ERROR,
		logger: log.New(&buf, "", log.LstdFlags),
	}

	// Тестируем все уровни
	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	output := buf.String()

	// Проверяем, что только ERROR сообщения присутствуют
	if strings.Contains(output, "[DEBUG]") {
		t.Error("DEBUG message should be filtered out")
	}
	if strings.Contains(output, "[INFO]") {
		t.Error("INFO message should be filtered out")
	}
	if strings.Contains(output, "[WARN]") {
		t.Error("WARN message should be filtered out")
	}
	if !strings.Contains(output, "[ERROR] error message") {
		t.Error("ERROR message not found")
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected LogLevel
	}{
		{"debug", DEBUG},
		{"DEBUG", DEBUG},
		{"info", INFO},
		{"INFO", INFO},
		{"warn", WARN},
		{"WARN", WARN},
		{"warning", WARN},
		{"WARNING", WARN},
		{"error", ERROR},
		{"ERROR", ERROR},
		{"invalid", INFO}, // по умолчанию INFO
		{"", INFO},        // по умолчанию INFO
	}

	for _, test := range tests {
		result := ParseLogLevel(test.input)
		if result != test.expected {
			t.Errorf("ParseLogLevel(%q) = %v, expected %v", test.input, result, test.expected)
		}
	}
}

func TestGlobalLogger(t *testing.T) {
	// Сохраняем оригинальный уровень и логгер
	originalLevel := GetGlobalLevel()
	originalLogger := globalLogger
	defer func() {
		SetGlobalLevel(originalLevel)
		globalLogger = originalLogger
	}()

	// Создаем буфер для захвата вывода
	var buf bytes.Buffer
	
	// Заменяем глобальный логгер на наш тестовый
	globalLogger = &Logger{
		level:  WARN,
		logger: log.New(&buf, "", log.LstdFlags),
	}

	// Тестируем глобальные функции
	Debug("debug message")
	Info("info message")
	Warn("warn message")
	Error("error message")

	output := buf.String()

	// Проверяем фильтрацию
	if strings.Contains(output, "[DEBUG]") {
		t.Error("DEBUG message should be filtered out")
	}
	if strings.Contains(output, "[INFO]") {
		t.Error("INFO message should be filtered out")
	}
	if !strings.Contains(output, "[WARN] warn message") {
		t.Error("WARN message not found")
	}
	if !strings.Contains(output, "[ERROR] error message") {
		t.Error("ERROR message not found")
	}
}

func TestLogLevelString(t *testing.T) {
	tests := []struct {
		level    LogLevel
		expected string
	}{
		{DEBUG, "DEBUG"},
		{INFO, "INFO"},
		{WARN, "WARN"},
		{ERROR, "ERROR"},
		{LogLevel(999), "UNKNOWN"},
	}

	for _, test := range tests {
		result := test.level.String()
		if result != test.expected {
			t.Errorf("LogLevel(%d).String() = %q, expected %q", test.level, result, test.expected)
		}
	}
}
