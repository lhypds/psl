package psllog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenCreatesTheDirectory(t *testing.T) {
	base := t.TempDir()
	logger, err := Open(base)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	if want := filepath.Join(base, Dir, File); logger.Path() != want {
		t.Errorf("Path() = %q, want %q", logger.Path(), want)
	}
	info, err := os.Stat(filepath.Join(base, Dir))
	if err != nil {
		t.Fatalf("stat %s: %v", Dir, err)
	}
	if !info.IsDir() {
		t.Errorf("%s is not a directory", Dir)
	}
	// The file itself only appears once something is logged.
	if _, err := os.Stat(logger.Path()); !os.IsNotExist(err) {
		t.Errorf("stat %s = %v, want the file to be created lazily", logger.Path(), err)
	}
}

func TestLogAppendsOneLinePerEntry(t *testing.T) {
	logger, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	for _, instruction := range []string{"first", "second\nwith a newline"} {
		if err := logger.Log(Entry{Slot: Slot{Instruction: instruction}}); err != nil {
			t.Fatalf("Log() error: %v", err)
		}
	}

	data, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want one per entry even when a value contains a newline", len(lines))
	}
	for i, want := range []string{"first", "second\nwith a newline"} {
		var e Entry
		if err := json.Unmarshal([]byte(lines[i]), &e); err != nil {
			t.Fatalf("decode line %d: %v", i, err)
		}
		if e.Slot.Instruction != want {
			t.Errorf("entry %d instruction = %q, want %q", i, e.Slot.Instruction, want)
		}
	}
}

func TestLogStampsTheTime(t *testing.T) {
	logger, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now().Add(-time.Second)
	if err := logger.Log(Entry{File: "a.psl"}); err != nil {
		t.Fatal(err)
	}

	fixed := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	if err := logger.Log(Entry{File: "b.psl", Time: fixed}); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(logger.Path())
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")

	var auto, given Entry
	json.Unmarshal([]byte(lines[0]), &auto)
	json.Unmarshal([]byte(lines[1]), &given)

	if auto.Time.Before(before) {
		t.Errorf("Time = %v, want it filled in with now", auto.Time)
	}
	if !given.Time.Equal(fixed) {
		t.Errorf("Time = %v, want the supplied timestamp kept", given.Time)
	}
}

func TestNilLoggerIsANoOp(t *testing.T) {
	var logger *Logger
	if err := logger.Log(Entry{File: "a.psl"}); err != nil {
		t.Errorf("Log() on a nil logger = %v, want nil", err)
	}
	if logger.Path() != "" {
		t.Errorf("Path() on a nil logger = %q, want empty", logger.Path())
	}
}

func TestLogReportsAnUnwritableFile(t *testing.T) {
	base := t.TempDir()
	logger, err := Open(base)
	if err != nil {
		t.Fatal(err)
	}
	// A directory where the log file belongs cannot be appended to.
	if err := os.Mkdir(logger.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := logger.Log(Entry{File: "a.psl"}); err == nil {
		t.Fatal("Log() succeeded, want an error the caller can warn about")
	}
}

func TestEntryOmitsEmptyOptionalFields(t *testing.T) {
	line, err := json.Marshal(Entry{File: "a.psl"})
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"response", "usage", "error", "image", "psl_version"} {
		if strings.Contains(string(line), `"`+absent+`"`) {
			t.Errorf("entry = %s, want %q omitted when unset", line, absent)
		}
	}
	for _, present := range []string{"time", "file", "slot", "model", "request", "duration_ms"} {
		if !strings.Contains(string(line), `"`+present+`"`) {
			t.Errorf("entry = %s, want %q always present", line, present)
		}
	}
}
