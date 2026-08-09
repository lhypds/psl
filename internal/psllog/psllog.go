// Package psllog records every AI request psl makes to .psl/psl.log.
//
// Each request is one JSON object on its own line, so the file stays appendable
// and greppable while holding prompts and responses that span many lines:
//
//	jq 'select(.error) | {time, model: .model.name, error}' .psl/psl.log
//
// API keys are never written. An image attached to a slot is recorded by media
// type and size, not by its bytes.
package psllog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// Dir is the directory psl keeps its working files in.
	Dir = ".psl"
	// File is the request log inside Dir.
	File = "psl.log"
)

// Logger appends entries to a log file.
type Logger struct {
	mu   sync.Mutex
	path string
}

// Entry is one AI request and what came back.
type Entry struct {
	Time       time.Time `json:"time"`
	PSLVersion string    `json:"psl_version,omitempty"`
	File       string    `json:"file"`
	Slot       Slot      `json:"slot"`
	Model      Model     `json:"model"`
	Request    Request   `json:"request"`
	Response   *Response `json:"response,omitempty"`
	Usage      *Usage    `json:"usage,omitempty"`
	DurationMS int64     `json:"duration_ms"`
	Error      string    `json:"error,omitempty"`
}

// Slot locates the instruction that was resolved.
type Slot struct {
	Line        int    `json:"line"`
	Column      int    `json:"column"`
	Instruction string `json:"instruction"`
}

// Model is where the request was sent.
type Model struct {
	Name      string `json:"name"`     // section name in .pslrc
	ID        string `json:"id"`       // model id sent to the API
	BaseURL   string `json:"base_url"` // never includes credentials
	API       string `json:"api"`      // wire protocol
	Endpoint  string `json:"endpoint"`
	MaxTokens int    `json:"max_tokens,omitempty"`
}

// Request is what psl sent, minus any credentials.
type Request struct {
	System string `json:"system"`
	Prompt string `json:"prompt"`
	Image  *Image `json:"image,omitempty"`
}

// Image records that visual context was attached, without its bytes.
type Image struct {
	MediaType string `json:"media_type"`
	Bytes     int    `json:"bytes"`
}

// Response is what the model returned.
type Response struct {
	Text       string `json:"text"`
	StopReason string `json:"stop_reason,omitempty"`
}

// Usage is what the model reported spending.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// Open prepares baseDir/.psl/psl.log for appending, creating the directory if
// it is not there yet.
func Open(baseDir string) (*Logger, error) {
	dir := filepath.Join(baseDir, Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	return &Logger{path: filepath.Join(dir, File)}, nil
}

// Path is the log file being written.
func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Log appends one entry. A nil Logger discards it, so callers that could not
// open a log need no special case.
func (l *Logger) Log(e Entry) error {
	if l == nil {
		return nil
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encode log entry: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", l.path, err)
	}
	defer f.Close()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", l.path, err)
	}
	return nil
}
