package compiler

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"psl/internal/llm"
	"psl/internal/psllog"
	"psl/internal/pslrc"
	"psl/internal/slot"
)

type fakeClient struct {
	reply string
	usage llm.Usage
	err   error
	got   llm.Request
}

func (f *fakeClient) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	f.got = req
	if f.err != nil {
		return nil, f.err
	}
	return &llm.Response{Text: f.reply, StopReason: "end_turn", Usage: f.usage}, nil
}

func testConfig(t *testing.T) *pslrc.Config {
	t.Helper()
	cfg, err := pslrc.Parse(`default_model=claude-opus-5

[claude-opus-5]
base_url=https://api.anthropic.com
api_key=sk-a

[gpt-5.6]
base_url=https://api.openai.com
api_key=sk-o
`, ".pslrc")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// writeSource writes a PSL file named the one way psl accepts: the language
// before the compiler's own extension.
func writeSource(t *testing.T, content string) string {
	t.Helper()
	return writeNamed(t, "main.go.psl", content)
}

func writeNamed(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func compile(t *testing.T, path string, client llm.Client) (*Result, error) {
	t.Helper()
	return Compile(context.Background(), Options{
		Path:      path,
		Config:    testConfig(t),
		NewClient: func(*pslrc.Model) llm.Client { return client },
	})
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCompileResolvesFirstSlot(t *testing.T) {
	path := writeSource(t, "package main\n\nfunc answer() int {\n\t:: return the answer ::\n}\n\n// :: gpt-5.6> document answer ::\n")
	client := &fakeClient{reply: "x := 42\nreturn x"}

	result, err := compile(t, path, client)
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}

	want := "package main\n\nfunc answer() int {\n\tx := 42\n\treturn x\n}\n\n// :: gpt-5.6> document answer ::\n"
	if got := read(t, path); got != want {
		t.Errorf("file =\n%q\nwant\n%q", got, want)
	}
	if result.Model != "claude-opus-5" {
		t.Errorf("Model = %q, want default_model", result.Model)
	}
	if result.Remaining != 1 {
		t.Errorf("Remaining = %d, want 1", result.Remaining)
	}
	if result.Instruction != "return the answer" {
		t.Errorf("Instruction = %q", result.Instruction)
	}
}

// The user message carries the source and nothing else; everything psl says
// about the job — the file's name, the instruction — is in the system prompt.
func TestCompileSendsMaskedFileAsThePrompt(t *testing.T) {
	path := writeSource(t, "package main\n\n:: write main ::\n")
	client := &fakeClient{reply: "func main() {}"}

	if _, err := compile(t, path, client); err != nil {
		t.Fatalf("Compile() error: %v", err)
	}
	if want := "package main\n\n" + slot.Marker + "\n"; client.got.Prompt != want {
		t.Errorf("prompt = %q, want the masked file alone (%q)", client.got.Prompt, want)
	}
	if !strings.Contains(client.got.System, "write main") {
		t.Error("system prompt should include the instruction")
	}
	if !strings.Contains(client.got.System, "main.go") {
		t.Error("system prompt should name the file being compiled")
	}
	if !strings.Contains(client.got.System, slot.Marker) {
		t.Error("system prompt should tell the model what marks the slot")
	}
}

func TestCompileUsesSlotModel(t *testing.T) {
	path := writeSource(t, ":: gpt-5.6> hi ::\n")
	client := &fakeClient{reply: "hello"}

	result, err := compile(t, path, client)
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}
	if result.Model != "gpt-5.6" {
		t.Errorf("Model = %q, want the model named in the slot", result.Model)
	}
	if got := read(t, path); got != "hello\n" {
		t.Errorf("file = %q, want %q", got, "hello\n")
	}
}

func TestCompilePassesImage(t *testing.T) {
	path := writeSource(t, ":: build this screen ::\n")
	client := &fakeClient{reply: "<div/>"}
	image := &llm.Image{MediaType: "image/png", Base64: "aGk="}

	if _, err := Compile(context.Background(), Options{
		Path:      path,
		Config:    testConfig(t),
		Image:     image,
		NewClient: func(*pslrc.Model) llm.Client { return client },
	}); err != nil {
		t.Fatalf("Compile() error: %v", err)
	}
	if client.got.Image != image {
		t.Error("request should carry the image")
	}
	if !strings.Contains(client.got.System, "image is attached") {
		t.Error("system prompt should tell the model an image is attached")
	}
}

func TestCompilePassesPrompt(t *testing.T) {
	path := writeSource(t, "def click_ok():\n    :: move to the OK button ::\n")
	client := &fakeClient{reply: "move(120, 480)"}
	const guidance = "move() takes absolute screen coordinates in pixels"

	if _, err := Compile(context.Background(), Options{
		Path:      path,
		Config:    testConfig(t),
		Prompt:    guidance,
		NewClient: func(*pslrc.Model) llm.Client { return client },
	}); err != nil {
		t.Fatalf("Compile() error: %v", err)
	}
	if !strings.Contains(client.got.System, guidance) {
		t.Errorf("system prompt should carry --prompt:\n%s", client.got.System)
	}
	// --prompt is guidance about the file, not part of it: the user message
	// stays the source alone.
	if strings.Contains(client.got.Prompt, guidance) {
		t.Errorf("user message = %q, want the source alone", client.got.Prompt)
	}
}

func TestCompileNoSlots(t *testing.T) {
	path := writeSource(t, "package main\n\nfunc main() {}\n")
	_, err := compile(t, path, &fakeClient{reply: "unused"})
	if !errors.Is(err, ErrNoSlots) {
		t.Fatalf("Compile() error = %v, want ErrNoSlots", err)
	}
}

func TestCompileLeavesFileUntouchedOnError(t *testing.T) {
	source := ":: mystery-model> hi ::\n"

	t.Run("unconfigured model", func(t *testing.T) {
		path := writeSource(t, source)
		_, err := compile(t, path, &fakeClient{reply: "nope"})
		if err == nil || !strings.Contains(err.Error(), "mystery-model") {
			t.Fatalf("Compile() error = %v, want an unconfigured-model error", err)
		}
		if got := read(t, path); got != source {
			t.Errorf("file = %q, want it unchanged", got)
		}
	})

	t.Run("api failure", func(t *testing.T) {
		path := writeSource(t, ":: hi ::\n")
		_, err := compile(t, path, &fakeClient{err: errors.New("429 rate limited")})
		if err == nil || !strings.Contains(err.Error(), "rate limited") {
			t.Fatalf("Compile() error = %v, want the API error", err)
		}
		if got := read(t, path); got != ":: hi ::\n" {
			t.Errorf("file = %q, want it unchanged", got)
		}
	})

	t.Run("empty model output", func(t *testing.T) {
		path := writeSource(t, ":: hi ::\n")
		_, err := compile(t, path, &fakeClient{reply: "   \n  "})
		if err == nil || !strings.Contains(err.Error(), "no usable text") {
			t.Fatalf("Compile() error = %v, want an empty-output error", err)
		}
		if got := read(t, path); got != ":: hi ::\n" {
			t.Errorf("file = %q, want it unchanged", got)
		}
	})
}

func TestCompileEmptySlot(t *testing.T) {
	path := writeSource(t, "a\n:: ::\n")
	_, err := compile(t, path, &fakeClient{reply: "x"})
	if err == nil || !strings.Contains(err.Error(), "empty slot at line 2") {
		t.Fatalf("Compile() error = %v, want an empty-slot error naming the position", err)
	}
}

func TestCompilePreservesFileMode(t *testing.T) {
	path := writeSource(t, "#!/bin/sh\n:: echo hello ::\n")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := compile(t, path, &fakeClient{reply: "echo hello"}); err != nil {
		t.Fatalf("Compile() error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755 preserved", info.Mode().Perm())
	}
}

func TestCompileMissingFile(t *testing.T) {
	_, err := compile(t, filepath.Join(t.TempDir(), "absent.go.psl"), &fakeClient{})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Compile() error = %v, want a not-exist error", err)
	}
}

// The name is what says which language's syntax psl has to keep out of the way
// of, so a file that does not say is refused before it is even opened.
func TestCompileRequiresTheLanguageInTheName(t *testing.T) {
	for _, name := range []string{"fib.psl", "fib.go", "fib"} {
		t.Run(name, func(t *testing.T) {
			path := writeNamed(t, name, ":: hi ::\n")
			_, err := compile(t, path, &fakeClient{reply: "hello"})
			if err == nil || !strings.Contains(err.Error(), "<name>.<language>.psl") {
				t.Fatalf("Compile() error = %v, want the naming rule enforced", err)
			}
			if got := read(t, path); got != ":: hi ::\n" {
				t.Errorf("file = %q, want it untouched", got)
			}
		})
	}
}

// The second extension picks the rules, and the rules decide what is a slot:
// the same source is one slot in Python and something else entirely without a
// language to read it.
func TestCompileParsesUnderTheLanguageInTheName(t *testing.T) {
	const source = "rev = xs[::-1]\n# :: sum them ::\n"

	path := writeNamed(t, "bot.py.psl", source)
	result, err := compile(t, path, &fakeClient{reply: "total = sum(rev)"})
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}
	if result.Instruction != "sum them" {
		t.Errorf("Instruction = %q, want the slot, not the slice", result.Instruction)
	}
	if result.Language != "Python" {
		t.Errorf("Language = %q, want Python", result.Language)
	}
	if got, want := read(t, path), "rev = xs[::-1]\n# total = sum(rev)\n"; got != want {
		t.Errorf("file = %q, want %q", got, want)
	}

	// The same bytes under an extension no language claims: the slice is
	// read as a delimiter, because nothing said otherwise.
	other := writeNamed(t, "bot.zig.psl", source)
	result, err = compile(t, other, &fakeClient{reply: "x"})
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}
	if result.Language != "generic" {
		t.Errorf("Language = %q, want the generic fallback", result.Language)
	}
	if result.Instruction == "sum them" {
		t.Error("the generic rules should not know about Python's slices")
	}
}

// The model is told what it is writing, rather than left to read it off the
// name.
func TestCompileTellsTheModelTheLanguage(t *testing.T) {
	path := writeNamed(t, "Program.cs.psl", "class P {\n    :: write Main ::\n}\n")
	client := &fakeClient{reply: "static void Main() {}"}
	if _, err := compile(t, path, client); err != nil {
		t.Fatalf("Compile() error: %v", err)
	}
	if !strings.Contains(client.got.System, "written in C#") {
		t.Errorf("system prompt should name the language:\n%s", client.got.System)
	}
}

// readLog returns the entries written to a logger's file.
func readLog(t *testing.T, logger *psllog.Logger) []psllog.Entry {
	t.Helper()
	data, err := os.ReadFile(logger.Path())
	if err != nil {
		t.Fatalf("read %s: %v", logger.Path(), err)
	}
	var entries []psllog.Entry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var e psllog.Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		entries = append(entries, e)
	}
	return entries
}

func compileWithLog(t *testing.T, path string, client llm.Client, image *llm.Image) (*psllog.Logger, error) {
	t.Helper()
	logger, err := psllog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = Compile(context.Background(), Options{
		Path:      path,
		Config:    testConfig(t),
		Image:     image,
		Log:       logger,
		Version:   "9.9.9",
		NewClient: func(*pslrc.Model) llm.Client { return client },
	})
	return logger, err
}

func TestCompileLogsTheRequest(t *testing.T) {
	path := writeSource(t, "package main\n\nfunc answer() int {\n\t:: return the answer ::\n}\n")
	client := &fakeClient{reply: "return 42", usage: llm.Usage{InputTokens: 120, OutputTokens: 34, TotalTokens: 154}}

	logger, err := compileWithLog(t, path, client, &llm.Image{MediaType: "image/png", Base64: "aGVsbG8="})
	if err != nil {
		t.Fatalf("Compile() error: %v", err)
	}

	entries := readLog(t, logger)
	if len(entries) != 1 {
		t.Fatalf("got %d log entries, want 1", len(entries))
	}
	e := entries[0]

	if e.File != path {
		t.Errorf("File = %q, want %q", e.File, path)
	}
	if e.PSLVersion != "9.9.9" {
		t.Errorf("PSLVersion = %q, want the version passed in", e.PSLVersion)
	}
	if e.Time.IsZero() {
		t.Error("Time is zero, want the moment of the request")
	}
	if e.Slot.Line != 4 || e.Slot.Instruction != "return the answer" {
		t.Errorf("Slot = %+v, want line 4 and the instruction", e.Slot)
	}
	if e.Model.Name != "claude-opus-5" {
		t.Errorf("Model = %+v, want the resolved model", e.Model)
	}
	if e.Model.BaseURL != "https://api.anthropic.com" {
		t.Errorf("Model = %+v, want the base URL", e.Model)
	}
	if e.Model.Endpoint != "https://api.anthropic.com/v1/chat/completions" {
		t.Errorf("Endpoint = %q, want the URL the request went to", e.Model.Endpoint)
	}
	if e.Response == nil || e.Response.Text != "return 42" || e.Response.StopReason != "end_turn" {
		t.Errorf("Response = %+v, want the model's reply", e.Response)
	}
	if e.Usage == nil || *e.Usage != (psllog.Usage{InputTokens: 120, OutputTokens: 34, TotalTokens: 154}) {
		t.Errorf("Usage = %+v, want the reported token counts", e.Usage)
	}
	if e.Error != "" {
		t.Errorf("Error = %q, want empty on success", e.Error)
	}
	// The request is logged as the body that went over the wire, so everything
	// psl composed sits in the messages: the system prompt is the first one.
	var body struct {
		Model               string `json:"model"`
		MaxCompletionTokens int    `json:"max_completion_tokens"`
		Messages            []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(e.Request, &body); err != nil {
		t.Fatalf("decode request %s: %v", e.Request, err)
	}
	if body.Model != "claude-opus-5" || body.MaxCompletionTokens == 0 {
		t.Errorf("Request = %s, want the model id and an output limit", e.Request)
	}
	if len(body.Messages) != 2 || body.Messages[0].Role != "system" || body.Messages[1].Role != "user" {
		t.Fatalf("Request = %s, want a system message then the user message", e.Request)
	}
	var system string
	if err := json.Unmarshal(body.Messages[0].Content, &system); err != nil {
		t.Fatalf("decode system content %s: %v", body.Messages[0].Content, err)
	}
	if !strings.Contains(system, "return the answer") || !strings.Contains(system, "main.go") {
		t.Errorf("Request = %s, want the instruction and the file name in the system message", e.Request)
	}
	var parts []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL *struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(body.Messages[1].Content, &parts); err != nil {
		t.Fatalf("decode user content %s: %v", body.Messages[1].Content, err)
	}
	if len(parts) != 2 || parts[0].Type != "image_url" || !strings.Contains(parts[1].Text, "func answer() int {") {
		t.Errorf("Request = %s, want the image then the source", e.Request)
	}
	// The image is recorded by media type and size only — never its bytes.
	if parts[0].ImageURL == nil || parts[0].ImageURL.URL != "data:image/png;base64,…5 bytes elided…" {
		t.Errorf("Request = %s, want the image's media type and decoded size", e.Request)
	}
	if strings.Contains(string(mustReadFile(t, logger.Path())), "aGVsbG8=") {
		t.Error("the log must not contain the image payload")
	}
}

func TestCompileLogsFailures(t *testing.T) {
	path := writeSource(t, ":: hi ::\n")
	logger, err := compileWithLog(t, path, &fakeClient{err: errors.New("429 rate limited")}, nil)
	if err == nil {
		t.Fatal("Compile() succeeded, want the API error")
	}

	entries := readLog(t, logger)
	if len(entries) != 1 {
		t.Fatalf("got %d log entries, want the failed request to be recorded", len(entries))
	}
	if !strings.Contains(entries[0].Error, "rate limited") {
		t.Errorf("Error = %q, want the API error", entries[0].Error)
	}
	if entries[0].Response != nil {
		t.Errorf("Response = %+v, want none when the call failed", entries[0].Response)
	}
	if !strings.Contains(string(entries[0].Request), `"messages"`) {
		t.Errorf("Request = %s, want what was sent recorded even when the call failed", entries[0].Request)
	}
}

func TestCompileLogsEachRunOnItsOwnLine(t *testing.T) {
	path := writeSource(t, "// :: greet ::\n// :: farewell ::\n")
	logger, err := psllog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, reply := range []string{"hello", "goodbye"} {
		if _, err := Compile(context.Background(), Options{
			Path:      path,
			Config:    testConfig(t),
			Log:       logger,
			NewClient: func(*pslrc.Model) llm.Client { return &fakeClient{reply: reply} },
		}); err != nil {
			t.Fatal(err)
		}
	}
	entries := readLog(t, logger)
	if len(entries) != 2 {
		t.Fatalf("got %d log entries, want one per run", len(entries))
	}
	if entries[0].Slot.Instruction != "greet" || entries[1].Slot.Instruction != "farewell" {
		t.Errorf("entries = %q, %q; want them appended in order",
			entries[0].Slot.Instruction, entries[1].Slot.Instruction)
	}
}

func TestCompileWithoutALogger(t *testing.T) {
	path := writeSource(t, ":: hi ::\n")
	if _, err := compile(t, path, &fakeClient{reply: "hello"}); err != nil {
		t.Fatalf("Compile() error: %v, want a nil logger to be fine", err)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRunUntilDone(t *testing.T) {
	path := writeSource(t, "// :: greet ::\n// :: farewell ::\n")
	replies := []string{"hello", "goodbye"}

	for i, reply := range replies {
		result, err := compile(t, path, &fakeClient{reply: reply})
		if err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
		if want := len(replies) - i - 1; result.Remaining != want {
			t.Errorf("run %d: Remaining = %d, want %d", i+1, result.Remaining, want)
		}
	}
	if got, want := read(t, path), "// hello\n// goodbye\n"; got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
	if _, err := compile(t, path, &fakeClient{}); !errors.Is(err, ErrNoSlots) {
		t.Fatalf("final run error = %v, want ErrNoSlots", err)
	}
}
