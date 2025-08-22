/*
 * Copyright 2025 Cong Wang
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/amtp-protocol/agentry/internal/config"
)

// LogLevel represents the severity level of a log entry
type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
	LevelFatal LogLevel = "fatal"
)

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp  time.Time              `json:"timestamp"`
	Level      LogLevel               `json:"level"`
	Message    string                 `json:"message"`
	Component  string                 `json:"component,omitempty"`
	RequestID  string                 `json:"request_id,omitempty"`
	MessageID  string                 `json:"message_id,omitempty"`
	UserID     string                 `json:"user_id,omitempty"`
	Operation  string                 `json:"operation,omitempty"`
	Duration   *time.Duration         `json:"duration_ms,omitempty"`
	Error      string                 `json:"error,omitempty"`
	StatusCode *int                   `json:"status_code,omitempty"`
	Method     string                 `json:"method,omitempty"`
	Path       string                 `json:"path,omitempty"`
	RemoteAddr string                 `json:"remote_addr,omitempty"`
	UserAgent  string                 `json:"user_agent,omitempty"`
	Fields     map[string]interface{} `json:"fields,omitempty"`
	Caller     string                 `json:"caller,omitempty"`
}

// Logger provides structured logging functionality
type Logger struct {
	writer    io.Writer
	level     LogLevel
	component string
	fields    map[string]interface{}
}

// contextKey is used for context keys to avoid collisions
type contextKey string

const (
	requestIDKey contextKey = "request_id"
	messageIDKey contextKey = "message_id"
	userIDKey    contextKey = "user_id"
)

// NewLogger creates a new logger instance
func NewLogger(config config.LoggingConfig) *Logger {
	var writer io.Writer = os.Stdout

	// In production, you might want to write to files or external systems
	// For now, always use stdout

	return &Logger{
		writer: writer,
		level:  LogLevel(strings.ToLower(config.Level)),
		fields: make(map[string]interface{}),
	}
}

// WithComponent creates a new logger with a component name
func (l *Logger) WithComponent(component string) *Logger {
	return &Logger{
		writer:    l.writer,
		level:     l.level,
		component: component,
		fields:    copyFields(l.fields),
	}
}

// WithFields creates a new logger with additional fields
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	newFields := copyFields(l.fields)
	for k, v := range fields {
		newFields[k] = v
	}

	return &Logger{
		writer:    l.writer,
		level:     l.level,
		component: l.component,
		fields:    newFields,
	}
}

// WithField creates a new logger with an additional field
func (l *Logger) WithField(key string, value interface{}) *Logger {
	fields := copyFields(l.fields)
	fields[key] = value

	return &Logger{
		writer:    l.writer,
		level:     l.level,
		component: l.component,
		fields:    fields,
	}
}

// WithContext creates a new logger with context values
func (l *Logger) WithContext(ctx context.Context) *Logger {
	logger := &Logger{
		writer:    l.writer,
		level:     l.level,
		component: l.component,
		fields:    copyFields(l.fields),
	}

	// Extract context values
	if requestID, ok := ctx.Value(requestIDKey).(string); ok {
		logger.fields["request_id"] = requestID
	}
	if messageID, ok := ctx.Value(messageIDKey).(string); ok {
		logger.fields["message_id"] = messageID
	}
	if userID, ok := ctx.Value(userIDKey).(string); ok {
		logger.fields["user_id"] = userID
	}

	return logger
}

// Debug logs a debug message
func (l *Logger) Debug(message string) {
	l.log(LevelDebug, message, nil)
}

// Debugf logs a formatted debug message
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.log(LevelDebug, fmt.Sprintf(format, args...), nil)
}

// Info logs an info message
func (l *Logger) Info(message string) {
	l.log(LevelInfo, message, nil)
}

// Infof logs a formatted info message
func (l *Logger) Infof(format string, args ...interface{}) {
	l.log(LevelInfo, fmt.Sprintf(format, args...), nil)
}

// Warn logs a warning message
func (l *Logger) Warn(message string) {
	l.log(LevelWarn, message, nil)
}

// Warnf logs a formatted warning message
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.log(LevelWarn, fmt.Sprintf(format, args...), nil)
}

// Error logs an error message
func (l *Logger) Error(message string, err error) {
	l.log(LevelError, message, err)
}

// Errorf logs a formatted error message
func (l *Logger) Errorf(err error, format string, args ...interface{}) {
	l.log(LevelError, fmt.Sprintf(format, args...), err)
}

// Fatal logs a fatal message and exits
func (l *Logger) Fatal(message string, err error) {
	l.log(LevelFatal, message, err)
	os.Exit(1)
}

// Fatalf logs a formatted fatal message and exits
func (l *Logger) Fatalf(err error, format string, args ...interface{}) {
	l.log(LevelFatal, fmt.Sprintf(format, args...), err)
	os.Exit(1)
}

// LogRequest logs an HTTP request
func (l *Logger) LogRequest(method, path, remoteAddr, userAgent string, statusCode int, duration time.Duration) {
	entry := l.createEntry(LevelInfo, "HTTP request", nil)
	entry.Method = method
	entry.Path = path
	entry.RemoteAddr = remoteAddr
	entry.UserAgent = userAgent
	entry.StatusCode = &statusCode
	entry.Duration = &duration
	entry.Operation = "http_request"

	l.writeEntry(entry)
}

// LogMessageProcessing logs message processing events
func (l *Logger) LogMessageProcessing(messageID, operation, status string, duration *time.Duration, err error) {
	level := LevelInfo
	message := fmt.Sprintf("Message %s: %s", operation, status)

	if err != nil {
		level = LevelError
		message = fmt.Sprintf("Message %s failed: %s", operation, status)
	}

	entry := l.createEntry(level, message, err)
	entry.MessageID = messageID
	entry.Operation = operation
	entry.Duration = duration

	if status != "" {
		entry.Fields = copyFields(entry.Fields)
		if entry.Fields == nil {
			entry.Fields = make(map[string]interface{})
		}
		entry.Fields["status"] = status
	}

	l.writeEntry(entry)
}

// LogDelivery logs message delivery events
func (l *Logger) LogDelivery(messageID, recipient, status string, attempts int, duration *time.Duration, err error) {
	level := LevelInfo
	message := fmt.Sprintf("Message delivery to %s: %s", recipient, status)

	if err != nil {
		level = LevelError
		message = fmt.Sprintf("Message delivery to %s failed: %s", recipient, status)
	}

	entry := l.createEntry(level, message, err)
	entry.MessageID = messageID
	entry.Operation = "delivery"
	entry.Duration = duration

	if entry.Fields == nil {
		entry.Fields = make(map[string]interface{})
	}
	entry.Fields["recipient"] = recipient
	entry.Fields["status"] = status
	entry.Fields["attempts"] = attempts

	l.writeEntry(entry)
}

// log is the internal logging method
func (l *Logger) log(level LogLevel, message string, err error) {
	if !l.shouldLog(level) {
		return
	}

	entry := l.createEntry(level, message, err)
	l.writeEntry(entry)
}

// createEntry creates a log entry
func (l *Logger) createEntry(level LogLevel, message string, err error) *LogEntry {
	entry := &LogEntry{
		Timestamp: time.Now().UTC(),
		Level:     level,
		Message:   message,
		Component: l.component,
		Fields:    copyFields(l.fields),
	}

	// Add error information
	if err != nil {
		entry.Error = err.Error()
	}

	// Add caller information for errors and above
	if level == LevelError || level == LevelFatal {
		if pc, file, line, ok := runtime.Caller(3); ok {
			if fn := runtime.FuncForPC(pc); fn != nil {
				entry.Caller = fmt.Sprintf("%s:%d %s", file, line, fn.Name())
			} else {
				entry.Caller = fmt.Sprintf("%s:%d", file, line)
			}
		}
	}

	// Extract context fields
	if entry.Fields != nil {
		if requestID, ok := entry.Fields["request_id"].(string); ok {
			entry.RequestID = requestID
			delete(entry.Fields, "request_id")
		}
		if messageID, ok := entry.Fields["message_id"].(string); ok {
			entry.MessageID = messageID
			delete(entry.Fields, "message_id")
		}
		if userID, ok := entry.Fields["user_id"].(string); ok {
			entry.UserID = userID
			delete(entry.Fields, "user_id")
		}
	}

	return entry
}

// writeEntry writes a log entry to the output
func (l *Logger) writeEntry(entry *LogEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		// Fallback to simple text output if JSON marshaling fails
		fmt.Fprintf(l.writer, "[%s] %s %s: %s\n",
			entry.Timestamp.Format(time.RFC3339),
			strings.ToUpper(string(entry.Level)),
			entry.Component,
			entry.Message)
		return
	}

	fmt.Fprintln(l.writer, string(data))
}

// shouldLog determines if a message should be logged based on level
func (l *Logger) shouldLog(level LogLevel) bool {
	levelOrder := map[LogLevel]int{
		LevelDebug: 0,
		LevelInfo:  1,
		LevelWarn:  2,
		LevelError: 3,
		LevelFatal: 4,
	}

	return levelOrder[level] >= levelOrder[l.level]
}

// copyFields creates a copy of a fields map
func copyFields(fields map[string]interface{}) map[string]interface{} {
	if fields == nil {
		return nil
	}

	copy := make(map[string]interface{})
	for k, v := range fields {
		copy[k] = v
	}
	return copy
}

// Context helper functions

// WithRequestID adds a request ID to the context
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// WithMessageID adds a message ID to the context
func WithMessageID(ctx context.Context, messageID string) context.Context {
	return context.WithValue(ctx, messageIDKey, messageID)
}

// WithUserID adds a user ID to the context
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// GetRequestID extracts the request ID from context
func GetRequestID(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDKey).(string); ok {
		return requestID
	}
	return ""
}

// GetMessageID extracts the message ID from context
func GetMessageID(ctx context.Context) string {
	if messageID, ok := ctx.Value(messageIDKey).(string); ok {
		return messageID
	}
	return ""
}

// GetUserID extracts the user ID from context
func GetUserID(ctx context.Context) string {
	if userID, ok := ctx.Value(userIDKey).(string); ok {
		return userID
	}
	return ""
}
