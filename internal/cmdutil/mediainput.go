package cmdutil

import (
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// imageExtMIME maps the file extensions accepted by the imageUpload task to their
// MIME types. Used as a fallback when magic-byte detection is unreliable (e.g. BMP).
var imageExtMIME = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".gif":  "image/gif",
}

// allowedImageMIME is the set of MIME types accepted by the imageUpload task.
var allowedImageMIME = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
	"image/bmp":  {},
	"image/gif":  {},
}

// BuildImageInput converts a CLI argument into an image value for the imageUpload
// task, which accepts images only. Remote URLs and data URIs are returned unchanged;
// local file paths are read, validated to be a supported image, and encoded as a
// data URI. Use BuildMediaInput for mediaStorage, which accepts any media type.
func BuildImageInput(arg string) (string, error) {
	if isRemoteOrDataURI(arg) {
		return arg, nil
	}

	data, err := readLocalFile(arg)
	if err != nil {
		return "", err
	}

	ct, err := detectImageMIME(data, arg)
	if err != nil {
		return "", err
	}

	return dataURI(ct, data), nil
}

// BuildMediaInput converts a CLI argument into a media value for the mediaStorage
// task, which accepts any media type (image, video, audio, 3D, and more). Remote
// URLs and data URIs are returned unchanged; local file paths are read and encoded
// as a data URI with a best-effort content type, without restricting the type.
func BuildMediaInput(arg string) (string, error) {
	if isRemoteOrDataURI(arg) {
		return arg, nil
	}

	data, err := readLocalFile(arg)
	if err != nil {
		return "", err
	}

	return dataURI(detectMediaMIME(data, arg), data), nil
}

// readLocalFile reads a local file path, rejecting directories and missing files.
func readLocalFile(arg string) ([]byte, error) {
	info, err := os.Stat(arg)
	if err != nil {
		return nil, fmt.Errorf("cannot stat file %q: %w", arg, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%q is a directory, not a file", arg)
	}

	data, err := os.ReadFile(arg) //nolint:gosec // user-supplied path is expected for a CLI upload
	if err != nil {
		return nil, fmt.Errorf("cannot read file %q: %w", arg, err)
	}
	return data, nil
}

// dataURI base64-encodes data as a data URI with the given content type.
func dataURI(contentType string, data []byte) string {
	return fmt.Sprintf("data:%s;base64,%s", contentType, base64.StdEncoding.EncodeToString(data))
}

// detectImageMIME returns the MIME type for image data, preferring magic-byte
// detection and falling back to the file extension. It rejects non-image content.
func detectImageMIME(data []byte, path string) (string, error) {
	ct := http.DetectContentType(data)
	if _, ok := allowedImageMIME[ct]; ok {
		return ct, nil
	}

	// Fall back to the extension only when sniffing is indeterminate (e.g. some
	// BMPs), never when it detected a clearly non-image type.
	if ct == "application/octet-stream" {
		if extMIME, ok := imageExtMIME[strings.ToLower(filepath.Ext(path))]; ok {
			return extMIME, nil
		}
	}

	return "", fmt.Errorf(
		"unsupported file type (detected %q): supported types are JPEG, JPG, PNG, WEBP, BMP, GIF",
		ct,
	)
}

// detectMediaMIME returns a best-effort content type for any file: magic-byte
// detection first, then the file extension, defaulting to application/octet-stream.
// It never rejects a file, since mediaStorage accepts any media type.
func detectMediaMIME(data []byte, path string) string {
	ct := http.DetectContentType(data)
	if ct == "application/octet-stream" {
		if byExt := mime.TypeByExtension(filepath.Ext(path)); byExt != "" {
			return byExt
		}
	}
	return ct
}

// isRemoteOrDataURI reports whether arg should be forwarded to the API verbatim
// rather than read from disk.
func isRemoteOrDataURI(arg string) bool {
	lower := strings.ToLower(arg)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "data:")
}
