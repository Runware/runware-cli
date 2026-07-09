package cmdutil

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// imageExtMIME maps the file extensions accepted by the media API to their MIME
// types. Used as a fallback when magic-byte detection is unreliable (e.g. BMP).
var imageExtMIME = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".gif":  "image/gif",
}

// allowedImageMIME is the set of MIME types accepted by the media API.
var allowedImageMIME = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
	"image/bmp":  {},
	"image/gif":  {},
}

// BuildImageInput converts a CLI argument into the value accepted by the media
// "image"/"media" fields. Remote URLs and data URIs are returned unchanged; local
// file paths are read, validated by content type, and encoded as a data URI.
func BuildImageInput(arg string) (string, error) {
	if isRemoteOrDataURI(arg) {
		return arg, nil
	}

	info, err := os.Stat(arg)
	if err != nil {
		return "", fmt.Errorf("cannot stat file %q: %w", arg, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%q is a directory, not a file", arg)
	}

	data, err := os.ReadFile(arg) //nolint:gosec // user-supplied path is expected for a CLI upload
	if err != nil {
		return "", fmt.Errorf("cannot read file %q: %w", arg, err)
	}

	mime, err := detectImageMIME(data, arg)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data)), nil
}

// detectImageMIME returns the MIME type for image data, preferring magic-byte
// detection and falling back to the file extension when detection is unreliable.
func detectImageMIME(data []byte, path string) (string, error) {
	mime := http.DetectContentType(data)
	if _, ok := allowedImageMIME[mime]; ok {
		return mime, nil
	}

	if extMIME, ok := imageExtMIME[strings.ToLower(filepath.Ext(path))]; ok {
		if _, allowed := allowedImageMIME[extMIME]; allowed {
			return extMIME, nil
		}
	}

	return "", fmt.Errorf(
		"unsupported file type (detected %q): supported types are JPEG, JPG, PNG, WEBP, BMP, GIF",
		mime,
	)
}

// isRemoteOrDataURI reports whether arg should be forwarded to the API verbatim
// rather than read from disk.
func isRemoteOrDataURI(arg string) bool {
	lower := strings.ToLower(arg)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "data:")
}
