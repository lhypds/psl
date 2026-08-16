// Package psllog records every AI request psl makes to ~/.psl/psl.log.
//
// Each request is one JSON object on its own line, so the file stays appendable
// and greppable while holding prompts and responses that span many lines:
//
//	jq 'select(.error) | {time, model: .model.name, error}' ~/.psl/psl.log
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
	// Request is the JSON body the endpoint received, kept raw so each API
	// keeps its own shape: what psl composed is only worth recording as the
	// thing that was actually sent. Its "messages" hold the prompt, and — for
	// APIs that carry it there — the system prompt too.
	//
	// A slot the model searched to resolve took more than one request. This is
	// the first, the one that carried the file; the searches that followed are
	// under Searches, and Usage is what all of them cost together.
	Request json.RawMessage `json:"request"`
	// Searches are the web searches the model made, in the order it asked them.
	// Absent unless the model's section turned search on.
	Searches   []Search  `json:"searches,omitempty"`
	Response   *Response `json:"response,omitempty"`
	Usage      *Usage    `json:"usage,omitempty"`
	DurationMS int64     `json:"duration_ms"`
	Error      string    `json:"error,omitempty"`
}

// Search is one query the model asked the web while resolving the slot, and the
// answer it was given. A search that failed is recorded with the reason: the
// slot was resolved without it, and the log is where that shows.
type Search struct {
	Query   string   `json:"query"`
	Answer  string   `json:"answer,omitempty"`
	Sources []string `json:"sources,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// Slot locates the instruction that was resolved.
type Slot struct {
	Line        int    `json:"line"`
	Column      int    `json:"column"`
	Instruction string `json:"instruction"`
}

// Model is where the request was sent.
type Model struct {
	Name      string `json:"name"`     // section name in .pslrc, and the id sent to the API
	BaseURL   string `json:"base_url"` // never includes credentials
	Endpoint  string `json:"endpoint"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	// WebSearch names the model offered to this one as a web_search tool.
	// Absent when the section left search off.
	WebSearch string `json:"web_search,omitempty"`
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
// it is not there yet. psl passes the user's home directory; tests pass their
// own.
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
