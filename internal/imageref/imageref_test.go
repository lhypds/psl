package imageref

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A 1x1 transparent PNG.
const pngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

func TestLoadEmpty(t *testing.T) {
	img, err := Load("")
	if err != nil || img != nil {
		t.Fatalf("Load(\"\") = %v, %v; want nil, nil", img, err)
	}
}

func TestLoadBase64(t *testing.T) {
	img, err := Load(pngBase64)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if img.MediaType != "image/png" {
		t.Errorf("MediaType = %q, want image/png", img.MediaType)
	}
	if img.Base64 != pngBase64 {
		t.Errorf("Base64 = %q, want the input re-encoded unchanged", img.Base64)
	}
}

func TestLoadBase64WithWhitespace(t *testing.T) {
	wrapped := pngBase64[:20] + "\n" + pngBase64[20:]
	img, err := Load(wrapped)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if img.Base64 != pngBase64 {
		t.Errorf("Base64 = %q, want line wrapping to be ignored", img.Base64)
	}
}

// The standard base64 alphabet contains '/', so encoded data can look like a
// path. It must still be recognised as image data.
func TestLoadBase64ContainingSlashes(t *testing.T) {
	gif := base64.StdEncoding.EncodeToString([]byte("GIF89a\xff\xff\xff"))
	if !strings.Contains(gif, "/") {
		t.Fatalf("test fixture %q should contain a slash", gif)
	}
	img, err := Load(gif)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if img.MediaType != "image/gif" {
		t.Errorf("MediaType = %q, want image/gif", img.MediaType)
	}
}

func TestLoadFile(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString(pngBase64)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	img, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if img.MediaType != "image/png" || img.Base64 != pngBase64 {
		t.Errorf("Load(path) = %+v, want the encoded png", img)
	}
}

func TestLoadDataURL(t *testing.T) {
	img, err := Load("data:image/png;base64," + pngBase64)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if img.MediaType != "image/png" || img.Base64 != pngBase64 {
		t.Errorf("Load(dataURL) = %+v, want the encoded png", img)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want string
	}{
		{"not base64", "this-is-not-an-image!!", "neither an existing file"},
		{"missing file", "/no/such/screenshot.png", "read image /no/such/screenshot.png"},
		{"missing file in the working directory", "screenshot.png", "read image screenshot.png"},
		{"unsupported type", base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n%not an image")), "unsupported type"},
		{"empty payload", "data:image/png;base64,", "empty"},
		{"data url without base64", "data:image/png,abc", "must be base64"},
		{"malformed data url", "data:image/png;base64" + pngBase64, "malformed data URL"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(tc.arg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load() error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
