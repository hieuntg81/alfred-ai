package security

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"alfred-ai/internal/domain"
	"alfred-ai/internal/infra/tracer"
)

// RetentionPolicy controls how long audit logs are kept.
type RetentionPolicy struct {
	MaxAge  time.Duration // max age of entries; 0 = no limit
	MaxSize int64         // max file size in bytes; 0 = no limit
}

// FileAuditLogger implements domain.AuditLogger by writing JSONL to a file.
type FileAuditLogger struct {
	mu        sync.Mutex
	file      *os.File
	path      string
	retention *RetentionPolicy
}

// NewFileAuditLogger creates an audit logger that appends to the given path.
// The file is created with 0600 permissions if it does not exist.
func NewFileAuditLogger(path string) (*FileAuditLogger, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	return &FileAuditLogger{file: f, path: path}, nil
}

// SetRetention configures the retention policy for log cleanup.
func (a *FileAuditLogger) SetRetention(policy RetentionPolicy) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.retention = &policy
}

// Log writes an audit event as a single JSON line.
func (a *FileAuditLogger) Log(ctx context.Context, event domain.AuditEvent) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	data, err := json.Marshal(event)
	if err != nil {
		return domain.NewDomainError("FileAuditLogger.Log", domain.ErrAuditWrite, err.Error())
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if _, err := a.file.Write(append(data, '\n')); err != nil {
		return domain.NewDomainError("FileAuditLogger.Log", domain.ErrAuditWrite, err.Error())
	}

	// Also emit as OTel span event if a span is active
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		attrs := make([]attribute.KeyValue, 0, len(event.Detail))
		for k, v := range event.Detail {
			attrs = append(attrs, tracer.StringAttr("audit."+k, v))
		}
		span.AddEvent("audit."+string(event.Type), trace.WithAttributes(attrs...))
	}

	return nil
}

// LogAccess is a convenience method for logging access-type audit events.
func (a *FileAuditLogger) LogAccess(ctx context.Context, actor, resource, action, outcome string) error {
	return a.Log(ctx, domain.AuditEvent{
		Type:     domain.AuditAccessLog,
		Actor:    actor,
		Resource: resource,
		Action:   action,
		Outcome:  outcome,
	})
}

// LogDataEvent is a convenience method for logging data-related audit events.
func (a *FileAuditLogger) LogDataEvent(ctx context.Context, actor, resource, action string, metadata map[string]string) error {
	return a.Log(ctx, domain.AuditEvent{
		Type:     domain.AuditDataEvent,
		Actor:    actor,
		Resource: resource,
		Action:   action,
		Outcome:  "success",
		Detail:   metadata,
	})
}

// Close flushes and closes the audit log file.
func (a *FileAuditLogger) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.file.Close()
}

// EnforceRetention removes old entries based on the configured retention policy.
// It rewrites the log file, keeping only entries that satisfy the policy.
// This is safe to call while the logger is active.
func (a *FileAuditLogger) EnforceRetention(ctx context.Context) (removed int, err error) {
	a.mu.Lock()
	policy := a.retention
	path := a.path
	a.mu.Unlock()

	if policy == nil {
		return 0, nil
	}

	// Check file size first.
	if policy.MaxSize > 0 {
		info, err := os.Stat(path)
		if err != nil {
			return 0, fmt.Errorf("stat audit log: %w", err)
		}
		if info.Size() <= policy.MaxSize && policy.MaxAge == 0 {
			return 0, nil
		}
	}

	cutoff := time.Time{}
	if policy.MaxAge > 0 {
		cutoff = time.Now().Add(-policy.MaxAge)
	}

	// Read all lines, filter, write back.
	a.mu.Lock()
	defer a.mu.Unlock()

	// Close current file handle.
	if err := a.file.Close(); err != nil {
		return 0, fmt.Errorf("close for retention: %w", err)
	}

	// Read existing entries.
	readFile, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open for reading: %w", err)
	}

	var kept [][]byte
	var keptSize int64
	scanner := bufio.NewScanner(readFile)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB max line

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Check age if policy has MaxAge.
		if !cutoff.IsZero() {
			var entry struct {
				Timestamp time.Time `json:"timestamp"`
			}
			if json.Unmarshal(line, &entry) == nil && !entry.Timestamp.IsZero() {
				if entry.Timestamp.Before(cutoff) {
					removed++
					continue
				}
			}
		}

		lineCopy := make([]byte, len(line))
		copy(lineCopy, line)
		kept = append(kept, lineCopy)
		keptSize += int64(len(line)) + 1 // +1 for newline
	}
	readFile.Close()

	if err := scanner.Err(); err != nil {
		// Reopen file handle before returning.
		a.file, _ = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		return 0, fmt.Errorf("scan audit log: %w", err)
	}

	// If MaxSize is set and we're still over, trim oldest entries.
	if policy.MaxSize > 0 && keptSize > policy.MaxSize {
		for len(kept) > 0 && keptSize > policy.MaxSize {
			keptSize -= int64(len(kept[0])) + 1
			kept = kept[1:]
			removed++
		}
	}

	// Write back filtered entries.
	tmpPath := path + ".tmp"
	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		a.file, _ = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		return 0, fmt.Errorf("create temp file: %w", err)
	}

	for _, line := range kept {
		tmpFile.Write(line)
		tmpFile.Write([]byte{'\n'})
	}
	tmpFile.Close()

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		a.file, _ = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		return 0, fmt.Errorf("rename temp file: %w", err)
	}

	// Reopen for appending.
	a.file, err = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return removed, fmt.Errorf("reopen after retention: %w", err)
	}

	return removed, nil
}

// ParseRetentionMaxSize parses a human-readable size string (e.g. "100MB", "1GB").
func ParseRetentionMaxSize(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	s = strings.TrimSpace(s)
	s = strings.ToUpper(s)

	multiplier := int64(1)
	switch {
	case strings.HasSuffix(s, "GB"):
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "MB"):
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "MB")
	case strings.HasSuffix(s, "KB"):
		multiplier = 1024
		s = strings.TrimSuffix(s, "KB")
	case strings.HasSuffix(s, "B"):
		s = strings.TrimSuffix(s, "B")
	}

	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse size %q: %w", s, err)
	}
	return n * multiplier, nil
}
