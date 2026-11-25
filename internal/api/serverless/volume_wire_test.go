package serverless

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/runware/runware-cli/internal/volumes/put"
)

// volumeWireSafeKeys are the string members of the Volumes wire types that
// carry no secret, so the debug log may show them. A new string member must be
// added here or to volumeSecretKeys.
var volumeWireSafeKeys = map[string]struct{}{
	"statement":      {},
	"signature":      {},
	"keyPairId":      {},
	"id":             {},
	"volumeId":       {},
	"state":          {},
	"path":           {},
	"sha256":         {},
	"attemptId":      {},
	"failure":        {},
	"crc64nvme":      {},
	"manifestSha256": {},
	"nextCursor":     {},
	"type":           {},
	"title":          {},
	"detail":         {},
	"limit":          {},
	"pointer":        {},
}

// TestVolumeWireStringMembersAreClassified: every string or byte member of a
// request or answer the adapter sends or decodes is either redacted in the
// debug log or known to be safe, so a member the API adds later (an upload
// address under another name) cannot reach the log unnoticed.
func TestVolumeWireStringMembersAreClassified(t *testing.T) {
	roots := []any{
		volumeFilePage{},
		volumeProblemBody{},
		put.KeyPairStatement{},
		put.UploadOpen{},
		put.Upload{},
		put.FileDeclarations{},
		put.DeclarationResult{},
		put.PartTokenRequest{},
		put.UploadTargets{},
		put.FileCompletion{},
		put.UploadFile{},
		put.UploadResult{},
	}
	seen := make(map[reflect.Type]struct{})
	var unclassified []string
	var walk func(reflect.Type)
	walk = func(typ reflect.Type) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array || typ.Kind() == reflect.Map {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct {
			return
		}
		if _, done := seen[typ]; done {
			return
		}
		seen[typ] = struct{}{}
		for field := range typ.Fields() {
			if !field.IsExported() {
				continue
			}
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			if isStringOrBytes(field.Type) && !isSecretKey(name, volumeSecretKeys) {
				if _, safe := volumeWireSafeKeys[name]; !safe {
					unclassified = append(unclassified, typ.String()+"."+name)
				}
			}
			walk(field.Type)
		}
	}
	for _, root := range roots {
		walk(reflect.TypeOf(root))
	}
	if len(unclassified) > 0 {
		slices.Sort(unclassified)
		t.Fatalf("string members neither in volumeSecretKeys nor in volumeWireSafeKeys: %v", unclassified)
	}
}

// isStringOrBytes reports whether typ, through pointers and slices of strings,
// holds text: a string kind (including put.Secret) or a []byte.
func isStringOrBytes(typ reflect.Type) bool {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.String:
		return true
	case reflect.Slice, reflect.Array:
		return isStringOrBytes(typ.Elem()) || typ.Elem().Kind() == reflect.Uint8
	default:
		return false
	}
}
