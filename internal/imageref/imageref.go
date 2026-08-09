// Package imageref turns the --image argument into image data for the model.
//
// The argument may be raw base64, a data URL, or a path to an image file.
package imageref

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"psl/internal/llm"
)

// Supported media types, matching what the chat APIs accept.
var supported = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// Load interprets the --image argument.
func Load(arg string) (*llm.Image, error) {
	if arg == "" {
		return nil, nil
	}
	if strings.HasPrefix(arg, "data:") {
		return fromDataURL(arg)
	}
	data, readErr := os.ReadFile(arg)
	if readErr == nil {
		return fromBytes(data, arg)
	}
	// Base64 is tried before the argument is judged to be a path, because the
	// standard alphabet contains '/' and so real base64 data looks path-like.
	decoded, b64Err := decodeBase64(arg)
	if b64Err == nil {
		img, imgErr := fromBytes(decoded, "--image")
		if imgErr == nil {
			return img, nil
		}
		if !looksLikePath(arg) {
			return nil, imgErr
		}
	}
	// Report the read error when the argument was meant as a path, rather than
	// confusing the user with a base64 complaint about their file name.
	if looksLikePath(arg) || !(os.IsNotExist(readErr) || isTooLongName(readErr)) {
		return nil, fmt.Errorf("read image %s: %w", arg, readErr)
	}
	return nil, fmt.Errorf("--image is neither an existing file, a data: URL, nor base64 data: %w", b64Err)
}

// looksLikePath reports whether the argument was clearly written as a file path.
func looksLikePath(arg string) bool {
	if strings.ContainsAny(arg, `/\`) || strings.HasPrefix(arg, "~") {
		return true
	}
	switch strings.ToLower(filepath.Ext(arg)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}

func fromDataURL(arg string) (*llm.Image, error) {
	meta, payload, ok := strings.Cut(strings.TrimPrefix(arg, "data:"), ",")
	if !ok {
		return nil, fmt.Errorf("malformed data URL: missing %q", ",")
	}
	if !strings.Contains(meta, "base64") {
		return nil, fmt.Errorf("data URL must be base64 encoded")
	}
	data, err := decodeBase64(payload)
	if err != nil {
		return nil, fmt.Errorf("malformed data URL: %w", err)
	}
	return fromBytes(data, "data URL")
}

func fromBytes(data []byte, source string) (*llm.Image, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("image %s is empty", source)
	}
	mediaType, _, _ := strings.Cut(http.DetectContentType(data), ";")
	if !supported[mediaType] {
		return nil, fmt.Errorf("image %s has unsupported type %q (want png, jpeg, gif or webp)", source, mediaType)
	}
	return &llm.Image{MediaType: mediaType, Base64: base64.StdEncoding.EncodeToString(data)}, nil
}

// decodeBase64 accepts both standard and URL-safe alphabets, with or without
// padding, and ignores whitespace introduced by shell line wrapping.
func decodeBase64(s string) ([]byte, error) {
	s = strings.Join(strings.Fields(s), "")
	encodings := []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	}
	var err error
	for _, enc := range encodings {
		var data []byte
		if data, err = enc.DecodeString(s); err == nil {
			return data, nil
		}
	}
	return nil, err
}

// isTooLongName reports whether err is the "file name too long" that a long
// base64 blob triggers when it is passed to os.ReadFile.
func isTooLongName(err error) bool {
	return strings.Contains(err.Error(), "file name too long")
}
