package serverless

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
)

const uploadTestAppID = "my-app"

// TestUploadSource_StagesTheArchiveAndReturnsAReadyUpload walks the three steps
// a deploy now takes before it can create an app, and pins what each one sends:
// the declaration has to describe the archive the transfer then carries, or
// completion refuses it.
func TestUploadSource_StagesTheArchiveAndReturnsAReadyUpload(t *testing.T) {
	archive := []byte("a zip, near enough")
	digest := sha256.Sum256(archive)
	wantSHA := hex.EncodeToString(digest[:])

	var staged []byte
	var declaration serverlessapi.SourceUploadCreate
	var completed bool

	stage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("staging method = %s, want PUT", r.Method)
		}
		// The signed URL carries its own credential; an Authorization header
		// would invalidate it.
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("staging request carried Authorization: %q", auth)
		}
		if got := r.Header.Get("Content-Type"); got != "application/zip" {
			t.Errorf("Content-Type = %q, want the header the instruction named", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read staged body: %v", err)
		}
		staged = body
		w.WriteHeader(http.StatusOK)
	}))
	defer stage.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/apps/"+uploadTestAppID+"/source-uploads":
			if err := json.NewDecoder(r.Body).Decode(&declaration); err != nil {
				t.Fatalf("decode declaration: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{
				"upload": {
					"id": "019c7654-8b21-7abc-9123-abcdef123456",
					"appId": "` + uploadTestAppID + `",
					"declaredByteLength": 18,
					"sha256": "` + wantSHA + `",
					"sourceType": "code",
					"state": "pending",
					"expiresAt": "2026-09-02T12:00:00Z",
					"createdAt": "2026-09-02T11:00:00Z",
					"updatedAt": "2026-09-02T11:00:00Z"
				},
				"transfer": {
					"mode": "singlePut",
					"method": "PUT",
					"url": "` + stage.URL + `/staging-object",
					"headers": {"Content-Type": "application/zip"},
					"expiresAt": "2026-09-02T12:00:00Z"
				}
			}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete"):
			completed = true
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"id": "019c7654-8b21-7abc-9123-abcdef123456",
				"appId": "` + uploadTestAppID + `",
				"declaredByteLength": 18,
				"sha256": "` + wantSHA + `",
				"sourceType": "code",
				"state": "ready",
				"expiresAt": "2026-09-02T12:00:00Z",
				"createdAt": "2026-09-02T11:00:00Z",
				"updatedAt": "2026-09-02T11:00:00Z"
			}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer api.Close()

	client := serverlessapi.NewClient("test-key", api.URL, slog.Default())
	id, err := uploadSource(context.Background(), client, uploadTestAppID, archive, "model.py")
	if err != nil {
		t.Fatalf("uploadSource: %v", err)
	}

	if id.String() != "019c7654-8b21-7abc-9123-abcdef123456" {
		t.Errorf("upload id = %s, want the id completion settled", id)
	}
	if !completed {
		t.Error("the upload was never completed, so no create could consume it")
	}
	if !bytes.Equal(staged, archive) {
		t.Errorf("staged %q, want the archive itself", staged)
	}
	if declaration.Sha256 != wantSHA {
		t.Errorf("declared sha256 = %q, want %q", declaration.Sha256, wantSHA)
	}
	if declaration.DeclaredByteLength != int64(len(archive)) {
		t.Errorf("declared length = %d, want %d", declaration.DeclaredByteLength, len(archive))
	}
	if declaration.ModelFile == nil || *declaration.ModelFile != "model.py" {
		t.Errorf("declared modelFile = %v, want model.py", declaration.ModelFile)
	}
	if declaration.SourceType != serverlessapi.AppSourceTypeCode {
		t.Errorf("declared sourceType = %q, want code", declaration.SourceType)
	}
}

// TestUploadSource_AbortsTheSessionWhenStagingFails keeps a failed transfer
// from leaving a staging object behind to expire on its own.
func TestUploadSource_AbortsTheSessionWhenStagingFails(t *testing.T) {
	stage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer stage.Close()

	aborted := false
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{
				"upload": {
					"id": "019c7654-8b21-7abc-9123-abcdef123456",
					"appId": "` + uploadTestAppID + `",
					"declaredByteLength": 3,
					"sha256": "` + strings.Repeat("a", 64) + `",
					"sourceType": "code",
					"state": "pending",
					"expiresAt": "2026-09-02T12:00:00Z",
					"createdAt": "2026-09-02T11:00:00Z",
					"updatedAt": "2026-09-02T11:00:00Z"
				},
				"transfer": {
					"mode": "singlePut",
					"method": "PUT",
					"url": "` + stage.URL + `/staging-object",
					"headers": {},
					"expiresAt": "2026-09-02T12:00:00Z"
				}
			}`))
		case http.MethodDelete:
			aborted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer api.Close()

	client := serverlessapi.NewClient("test-key", api.URL, slog.Default())
	if _, err := uploadSource(context.Background(), client, uploadTestAppID, []byte("zip"), "model.py"); err == nil {
		t.Fatal("uploadSource succeeded despite a refused transfer")
	}
	if !aborted {
		t.Error("the session was left open after the transfer failed")
	}
}

// TestUploadSource_ReportsARejectedArchive proves a rejection is an error with
// the reason attached, not an upload id a create would then fail on.
func TestUploadSource_ReportsARejectedArchive(t *testing.T) {
	stage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer stage.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/complete") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"id": "019c7654-8b21-7abc-9123-abcdef123456",
				"appId": "` + uploadTestAppID + `",
				"declaredByteLength": 3,
				"sha256": "` + strings.Repeat("a", 64) + `",
				"sourceType": "code",
				"state": "rejected",
				"rejectionReason": "model file not found in archive",
				"expiresAt": "2026-09-02T12:00:00Z",
				"createdAt": "2026-09-02T11:00:00Z",
				"updatedAt": "2026-09-02T11:00:00Z"
			}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"upload": {
				"id": "019c7654-8b21-7abc-9123-abcdef123456",
				"appId": "` + uploadTestAppID + `",
				"declaredByteLength": 3,
				"sha256": "` + strings.Repeat("a", 64) + `",
				"sourceType": "code",
				"state": "pending",
				"expiresAt": "2026-09-02T12:00:00Z",
				"createdAt": "2026-09-02T11:00:00Z",
				"updatedAt": "2026-09-02T11:00:00Z"
			},
			"transfer": {
				"mode": "singlePut",
				"method": "PUT",
				"url": "` + stage.URL + `/staging-object",
				"headers": {},
				"expiresAt": "2026-09-02T12:00:00Z"
			}
		}`))
	}))
	defer api.Close()

	client := serverlessapi.NewClient("test-key", api.URL, slog.Default())
	_, err := uploadSource(context.Background(), client, uploadTestAppID, []byte("zip"), "model.py")
	if err == nil {
		t.Fatal("uploadSource accepted a rejected archive")
	}
	if !strings.Contains(err.Error(), "model file not found in archive") {
		t.Errorf("error = %v, want the rejection reason", err)
	}
}
