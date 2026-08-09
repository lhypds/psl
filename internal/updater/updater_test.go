package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRelease serves a GitHub releases endpoint plus the assets it advertises.
type fakeRelease struct {
	t       *testing.T
	tag     string
	assets  map[string][]byte // name -> contents
	server  *httptest.Server
	omitted map[string]bool // advertised but not actually served
	hits    map[string]int
}

func newFakeRelease(t *testing.T, tag string) *fakeRelease {
	t.Helper()
	f := &fakeRelease{
		t:       t,
		tag:     tag,
		assets:  map[string][]byte{},
		omitted: map[string]bool{},
		hits:    map[string]int{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		f.hits["release"]++
		var entries []string
		for name := range f.assets {
			entries = append(entries, fmt.Sprintf(`{"name":%q,"browser_download_url":"%s/download/%s"}`,
				name, f.server.URL, name))
		}
		fmt.Fprintf(w, `{"tag_name":%q,"html_url":"https://example.test/release","assets":[%s]}`,
			f.tag, strings.Join(entries, ","))
	})
	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/download/")
		f.hits[name]++
		body, ok := f.assets[name]
		if !ok || f.omitted[name] {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

// publish adds an asset and refreshes the checksums file, the way release.sh does.
func (f *fakeRelease) publish(name string, body []byte) {
	f.assets[name] = body
	var sums strings.Builder
	for asset, data := range f.assets {
		if asset == ChecksumsAsset {
			continue
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
	}
	f.assets[ChecksumsAsset] = []byte(sums.String())
}

func (f *fakeRelease) options(t *testing.T, current, exe string) Options {
	t.Helper()
	return Options{
		Repo:    "owner/psl",
		Current: current,
		GOOS:    "linux",
		GOARCH:  "amd64",
		APIBase: f.server.URL,
		ExePath: exe,
		HTTP:    f.server.Client(),
	}
}

func tarGz(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipped(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// installedExe writes a stand-in for the running executable.
func installedExe(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "psl")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestUpdateReplacesTheExecutable(t *testing.T) {
	f := newFakeRelease(t, "v0.2.0")
	f.publish("psl-0.2.0-linux-amd64.tar.gz", tarGz(t, map[string]string{
		"psl-0.2.0-linux-amd64/README.md": "docs",
		"psl-0.2.0-linux-amd64/psl":       "NEW BINARY",
	}))
	exe := installedExe(t, "OLD BINARY")

	var log bytes.Buffer
	opts := f.options(t, "0.1.0", exe)
	opts.Log = &log

	result, err := Update(context.Background(), opts)
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if !result.Updated {
		t.Error("Updated = false, want true")
	}
	if result.Previous != "0.1.0" || result.Latest != "0.2.0" {
		t.Errorf("Result = %+v, want 0.1.0 -> 0.2.0", result)
	}
	if got := readFile(t, exe); got != "NEW BINARY" {
		t.Errorf("executable = %q, want the downloaded binary", got)
	}
	if info, err := os.Stat(exe); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755 preserved", info.Mode().Perm())
	}
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Error("the .old backup should be cleaned up after a successful swap")
	}
	if !strings.Contains(log.String(), "psl-0.2.0-linux-amd64.tar.gz") {
		t.Errorf("log = %q, want it to name the asset being downloaded", log.String())
	}
}

func TestUpdateFromZip(t *testing.T) {
	f := newFakeRelease(t, "v0.2.0")
	f.publish("psl-0.2.0-windows-amd64.zip", zipped(t, map[string]string{
		"psl-0.2.0-windows-amd64/psl.exe": "NEW WINDOWS BINARY",
	}))
	exe := installedExe(t, "OLD")

	opts := f.options(t, "0.1.0", exe)
	opts.GOOS = "windows"

	if _, err := Update(context.Background(), opts); err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if got := readFile(t, exe); got != "NEW WINDOWS BINARY" {
		t.Errorf("executable = %q, want the binary from the zip", got)
	}
}

func TestUpdateAlreadyLatest(t *testing.T) {
	f := newFakeRelease(t, "v0.1.0")
	f.publish("psl-0.1.0-linux-amd64.tar.gz", tarGz(t, map[string]string{
		"psl-0.1.0-linux-amd64/psl": "NEW BINARY",
	}))
	exe := installedExe(t, "CURRENT BINARY")

	result, err := Update(context.Background(), f.options(t, "0.1.0", exe))
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if result.Updated {
		t.Error("Updated = true, want false when the newest release is installed")
	}
	if got := readFile(t, exe); got != "CURRENT BINARY" {
		t.Errorf("executable = %q, want it untouched", got)
	}
	if f.hits["psl-0.1.0-linux-amd64.tar.gz"] != 0 {
		t.Error("nothing should be downloaded when already up to date")
	}
}

func TestUpdateLeavesExecutableOnFailure(t *testing.T) {
	tests := []struct {
		name    string
		arrange func(f *fakeRelease)
		want    string
	}{
		{
			name: "no asset for this platform",
			arrange: func(f *fakeRelease) {
				f.publish("psl-0.2.0-darwin-arm64.tar.gz", tarGz(t, nil))
			},
			want: "no asset named psl-0.2.0-linux-amd64.tar.gz",
		},
		{
			name: "checksum mismatch",
			arrange: func(f *fakeRelease) {
				f.publish("psl-0.2.0-linux-amd64.tar.gz", []byte("archive"))
				f.assets[ChecksumsAsset] = []byte(
					"0000000000000000000000000000000000000000000000000000000000000000  psl-0.2.0-linux-amd64.tar.gz\n")
			},
			want: "checksum mismatch",
		},
		{
			name: "no checksums published",
			arrange: func(f *fakeRelease) {
				f.publish("psl-0.2.0-linux-amd64.tar.gz", []byte("archive"))
				delete(f.assets, ChecksumsAsset)
			},
			want: "will not install a release it cannot verify",
		},
		{
			name: "archive without the binary",
			arrange: func(f *fakeRelease) {
				f.publish("psl-0.2.0-linux-amd64.tar.gz", tarGz(t, map[string]string{
					"psl-0.2.0-linux-amd64/README.md": "docs only",
				}))
			},
			want: "contains no psl",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeRelease(t, "v0.2.0")
			tc.arrange(f)
			exe := installedExe(t, "OLD BINARY")

			_, err := Update(context.Background(), f.options(t, "0.1.0", exe))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Update() error = %v, want it to contain %q", err, tc.want)
			}
			if got := readFile(t, exe); got != "OLD BINARY" {
				t.Errorf("executable = %q, want it left in place after a failed update", got)
			}
			if entries, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".psl-update-*")); len(entries) != 0 {
				t.Errorf("staged files left behind: %v", entries)
			}
		})
	}
}

func TestUpdateReportsMissingRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	exe := installedExe(t, "OLD")
	_, err := Update(context.Background(), Options{
		Repo: "owner/psl", Current: "0.1.0", APIBase: server.URL, ExePath: exe, HTTP: server.Client(),
	})
	if err == nil || !strings.Contains(err.Error(), "no published release") {
		t.Fatalf("Update() error = %v, want a missing-release error", err)
	}
}

func TestArchiveName(t *testing.T) {
	tests := []struct {
		goos, goarch, want string
	}{
		{"darwin", "arm64", "psl-1.2.3-darwin-arm64.tar.gz"},
		{"linux", "amd64", "psl-1.2.3-linux-amd64.tar.gz"},
		{"windows", "amd64", "psl-1.2.3-windows-amd64.zip"},
	}
	for _, tc := range tests {
		if got := archiveName("1.2.3", tc.goos, tc.goarch); got != tc.want {
			t.Errorf("archiveName(%s/%s) = %q, want %q", tc.goos, tc.goarch, got, tc.want)
		}
	}
}

func TestExpectedSum(t *testing.T) {
	listing := "aaaa  psl-1.0.0-linux-amd64.tar.gz\nBBBB  *psl-1.0.0-windows-amd64.zip\n"

	if got, err := expectedSum(listing, "psl-1.0.0-linux-amd64.tar.gz"); err != nil || got != "aaaa" {
		t.Errorf("expectedSum() = %q, %v; want aaaa", got, err)
	}
	// shasum's binary-mode marker must not defeat the lookup.
	if got, err := expectedSum(listing, "psl-1.0.0-windows-amd64.zip"); err != nil || got != "bbbb" {
		t.Errorf("expectedSum() = %q, %v; want bbbb", got, err)
	}
	if _, err := expectedSum(listing, "psl-1.0.0-darwin-arm64.tar.gz"); err == nil {
		t.Error("expectedSum() succeeded for an unlisted asset, want an error")
	}
}
