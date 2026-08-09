package compiler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"psl/internal/llm"
	"psl/internal/pslrc"
	"psl/internal/slot"
)

type fakeClient struct {
	reply string
	err   error
	got   llm.Request
}

func (f *fakeClient) Complete(_ context.Context, req llm.Request) (string, error) {
	f.got = req
	return f.reply, f.err
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

func writeSource(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.go")
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
		NewClient: func(*pslrc.Model) (llm.Client, error) { return client, nil },
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

func TestCompileSendsMaskedFileAsContext(t *testing.T) {
	path := writeSource(t, "package main\n\n:: write main ::\n")
	client := &fakeClient{reply: "func main() {}"}

	if _, err := compile(t, path, client); err != nil {
		t.Fatalf("Compile() error: %v", err)
	}
	if !strings.Contains(client.got.Prompt, slot.Marker) {
		t.Error("prompt should show the file with the slot masked")
	}
	if strings.Contains(client.got.Prompt, ":: write main ::") {
		t.Error("prompt should not contain the raw slot delimiters in the source block")
	}
	if !strings.Contains(client.got.Prompt, "package main") {
		t.Error("prompt should include the surrounding file as context")
	}
	if !strings.Contains(client.got.Prompt, "write main") {
		t.Error("prompt should include the instruction")
	}
	if client.got.System == "" {
		t.Error("request should carry the compiler's system prompt")
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
		NewClient: func(*pslrc.Model) (llm.Client, error) { return client, nil },
	}); err != nil {
		t.Fatalf("Compile() error: %v", err)
	}
	if client.got.Image != image {
		t.Error("request should carry the image")
	}
	if !strings.Contains(client.got.Prompt, "image is attached") {
		t.Error("prompt should tell the model an image is attached")
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
	_, err := compile(t, filepath.Join(t.TempDir(), "absent.psl"), &fakeClient{})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Compile() error = %v, want a not-exist error", err)
	}
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
