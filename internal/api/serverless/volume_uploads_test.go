package serverless

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/volumes/put"
)

const (
	testUploadID  = "4b6d8f0a-2c4e-4f6a-8b0d-2f4a6c8e0b33"
	testFileID    = "9c1e5a3f-9b2d-4e6f-8a0c-1d3e5f7a9b11"
	testFilesPath = "/v1/volumes/weights/files"
	testEnvQuery  = "environment=staging"
	// testFilesQuery is the query of the first listVolumeFiles page in staging.
	testFilesQuery = "environment=staging&limit=1000"
	testDigest     = "9f2c4e1a7b3d5f8e0c6a2b4d1e3f5a7c9b0d2e4f6a8c1b3d5e7f9a0c2e4b6d8f"
)

// volumeExchange is one request the adapter must send and the answer to it.
type volumeExchange struct {
	method string
	path   string
	query  string
	body   string
	status int
	answer string
}

// volumeServer answers the exchanges in order and fails on anything else.
func volumeServer(t *testing.T, exchanges ...volumeExchange) *VolumeUploads {
	t.Helper()
	next := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if next >= len(exchanges) {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusTeapot)
			return
		}
		x := exchanges[next]
		next++
		if r.Method != x.method || r.URL.EscapedPath() != x.path || r.URL.RawQuery != x.query {
			t.Errorf("request %d = %s %s?%s, want %s %s?%s", next, r.Method, r.URL.EscapedPath(), r.URL.RawQuery, x.method, x.path, x.query)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		if x.body != "" && !jsonEqual(t, raw, []byte(x.body)) {
			t.Errorf("request %d body = %s, want %s", next, raw, x.body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(x.status)
		_, _ = io.WriteString(w, x.answer)
	}))
	t.Cleanup(func() {
		srv.Close()
		if next != len(exchanges) {
			t.Errorf("%d of %d requests sent", next, len(exchanges))
		}
	})
	return newClient("test-key", srv.URL, nil, srv.Client()).VolumeUploads()
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ja, _ := json.Marshal(x)
	jb, _ := json.Marshal(y)
	return bytes.Equal(ja, jb)
}

var (
	testVolumeRef = put.VolumeRef{
		Name:        "weights",
		Environment: "staging",
	}
	testUploadRef = put.UploadRef{
		Volume: testVolumeRef,
		ID:     testUploadID,
	}
)

func TestVolumeUploadsLatestFilesReadsEveryPage(t *testing.T) {
	v := volumeServer(t,
		volumeExchange{
			method: http.MethodGet,
			path:   testFilesPath,
			query:  testFilesQuery,
			status: http.StatusOK,
			answer: `{"manifestSha256":"` + testDigest + `","data":[{"path":"a","mode":292,"size":1,"sha256":"aa"}],"nextCursor":"c1"}`,
		},
		volumeExchange{
			method: http.MethodGet,
			path:   testFilesPath,
			query:  "cursor=c1&" + testFilesQuery,
			status: http.StatusOK,
			answer: `{"manifestSha256":"` + testDigest + `","data":[{"path":"b","mode":420,"size":2,"sha256":"bb"}],"nextCursor":null}`,
		},
	)
	list, err := v.LatestFiles(context.Background(), testVolumeRef)
	if err != nil {
		t.Fatal(err)
	}
	if list.ManifestSHA256 != testDigest || len(list.Files) != 2 || list.Files[1].Path != "b" || list.Files[1].Mode != 420 {
		t.Fatalf("list = %+v", list)
	}
}

func TestVolumeUploadsLatestFilesStartsAgainWhenTheListChanges(t *testing.T) {
	first := volumeExchange{
		method: http.MethodGet,
		path:   testFilesPath,
		query:  testFilesQuery,
		status: http.StatusOK,
		answer: `{"manifestSha256":"` + testDigest + `","data":[{"path":"a","mode":292,"size":1,"sha256":"aa"}],"nextCursor":"c1"}`,
	}
	v := volumeServer(t,
		first,
		volumeExchange{
			method: http.MethodGet,
			path:   testFilesPath,
			query:  "cursor=c1&" + testFilesQuery,
			status: http.StatusOK,
			answer: `{"manifestSha256":"` + strings.Repeat("1", 64) + `","data":[],"nextCursor":null}`,
		},
		first,
		volumeExchange{
			method: http.MethodGet,
			path:   testFilesPath,
			query:  "cursor=c1&" + testFilesQuery,
			status: http.StatusBadRequest,
			answer: `{"type":"https://docs.runware.ai/serverless/errors#bad-request","title":"Bad Request","status":400}`,
		},
		volumeExchange{
			method: http.MethodGet,
			path:   testFilesPath,
			query:  testFilesQuery,
			status: http.StatusOK,
			answer: `{"data":[]}`,
		},
	)
	list, err := v.LatestFiles(context.Background(), testVolumeRef)
	if err != nil {
		t.Fatal(err)
	}
	if list.ManifestSHA256 != "" || len(list.Files) != 0 {
		t.Fatalf("list = %+v, want the empty list read last", list)
	}
}

func TestVolumeUploadsCallsEveryRoute(t *testing.T) {
	const (
		uploadPrefix = "/v1/volumes/weights/uploads/" + testUploadID
		target       = `{"partNumber":1,"url":"https://upload.example/p/1","token":"tok-1","expiresAt":"2026-11-10T12:00:00Z"}`
		upload       = `{"id":"` + testUploadID + `","volumeId":"6a1f0d6e-5c1b-4f69-9a39-0f4c1c8b2d11","state":"open","createdAt":"2026-11-09T12:00:00Z","expiresAt":"2026-11-10T12:00:00Z"}`
	)
	v := volumeServer(t,
		volumeExchange{
			method: http.MethodGet,
			path:   "/v1/volumes-key-pair",
			status: http.StatusOK,
			answer: `{"statement":"c3Q","signature":"c2ln"}`,
		},
		volumeExchange{
			method: http.MethodPost,
			path:   "/v1/volumes/weights/uploads",
			query:  testEnvQuery,
			body:   `{"keyPairId":"kp","encapsulation":"enc"}`,
			status: http.StatusCreated,
			answer: upload,
		},
		volumeExchange{
			method: http.MethodPost,
			path:   uploadPrefix + "/files",
			query:  testEnvQuery,
			body:   `{"files":[{"path":"a/b.bin","mode":292,"size":5,"sha256":"ab","sealedKey":"sk","attemptId":"at"}]}`,
			status: http.StatusOK,
			answer: `{"files":[{"file":{"id":"` + testFileID + `","path":"a/b.bin","mode":292,"size":5,"sha256":"ab","state":"uploading","layout":{"segmentBytes":1,"segmentPlaintextBytes":1,"segmentsPerPart":1,"partSizeBytes":1,"partCount":1,"ciphertextBytes":1}},"uploadTargets":[` + target + `]}]}`,
		},
		volumeExchange{
			method: http.MethodPost,
			path:   uploadPrefix + "/files/" + testFileID + "/parts",
			query:  testEnvQuery,
			body:   `{"partNumbers":[1]}`,
			status: http.StatusOK,
			answer: `{"uploadTargets":[` + target + `]}`,
		},
		volumeExchange{
			method: http.MethodPost,
			path:   uploadPrefix + "/files/" + testFileID + "/complete",
			query:  testEnvQuery,
			body:   `{"crc64nvme":"00000000000000ff","parts":[{"partNumber":1,"ack":"ack-1"}]}`,
			status: http.StatusOK,
			answer: `{"id":"` + testFileID + `","path":"a/b.bin","mode":292,"size":5,"sha256":"ab","state":"verified"}`,
		},
		volumeExchange{
			method: http.MethodPost,
			path:   uploadPrefix + "/complete",
			query:  testEnvQuery,
			status: http.StatusOK,
			answer: `{"upload":` + upload + `,"changed":true,"manifestSha256":"` + testDigest + `"}`,
		},
		volumeExchange{
			method: http.MethodDelete,
			path:   uploadPrefix,
			query:  testEnvQuery,
			status: http.StatusNoContent,
		},
	)
	ctx := context.Background()

	st, err := v.KeyPairStatement(ctx)
	if err != nil || st.Statement != "c3Q" || st.Signature != "c2ln" {
		t.Fatalf("KeyPairStatement = %+v, %v", st, err)
	}
	up, err := v.OpenUpload(ctx, testVolumeRef, put.UploadOpen{
		KeyPairID:     "kp",
		Encapsulation: "enc",
	})
	if err != nil || up.ID != testUploadID || !up.ExpiresAt.Equal(time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("OpenUpload = %+v, %v", up, err)
	}
	res, err := v.DeclareFiles(ctx, testUploadRef, put.FileDeclarations{Files: []put.FileDeclaration{
		{
			Path:      "a/b.bin",
			Mode:      292,
			Size:      5,
			SHA256:    "ab",
			SealedKey: "sk",
			AttemptID: "at",
		},
	}})
	if err != nil || len(res.Files) != 1 || res.Files[0].File.Layout == nil || len(res.Files[0].UploadTargets) != 1 || res.Files[0].UploadTargets[0].Token != "tok-1" {
		t.Fatalf("DeclareFiles = %+v, %v", res, err)
	}
	targets, err := v.RenewParts(ctx, testUploadRef, testFileID, []int{1})
	if err != nil || len(targets) != 1 || targets[0].URL != "https://upload.example/p/1" {
		t.Fatalf("RenewParts = %+v, %v", targets, err)
	}
	f, err := v.CompleteFile(ctx, testUploadRef, testFileID, put.FileCompletion{
		CRC64NVME: "00000000000000ff",
		Parts: []put.PartAck{
			{
				PartNumber: 1,
				Ack:        "ack-1",
			},
		},
	})
	if err != nil || f.State != "verified" {
		t.Fatalf("CompleteFile = %+v, %v", f, err)
	}
	result, err := v.CompleteUpload(ctx, testUploadRef)
	if err != nil || !result.Changed || result.ManifestSHA256 != testDigest {
		t.Fatalf("CompleteUpload = %+v, %v", result, err)
	}
	if err := v.AbortUpload(ctx, testUploadRef); err != nil {
		t.Fatalf("AbortUpload = %v", err)
	}
}

func TestVolumeUploadsProductionSendsNoEnvironment(t *testing.T) {
	v := volumeServer(t, volumeExchange{
		method: http.MethodDelete,
		path:   "/v1/volumes/a%20b/uploads/" + testUploadID,
		status: http.StatusNoContent,
	})
	err := v.AbortUpload(context.Background(), put.UploadRef{
		Volume: put.VolumeRef{Name: "a b"},
		ID:     testUploadID,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestVolumeUploadsProblemsReachTheEngine(t *testing.T) {
	cases := []struct {
		name   string
		status int
		answer string
		want   put.ProblemError
	}{
		{
			name:   "limit with a file pointer",
			status: http.StatusConflict,
			answer: `{"type":"https://docs.runware.ai/serverless/errors#volume-limit-exceeded","title":"Conflict","status":409,"detail":"maxFileBytes exceeds its limit","limit":"maxFileBytes","cap":1000,"errors":[{"detail":"too big","pointer":"/files/3/size"},{"detail":"x","pointer":"/files/4/size"}]}`,
			want: put.ProblemError{
				Status:  409,
				Slug:    "volume-limit-exceeded",
				Detail:  "maxFileBytes exceeds its limit",
				Limit:   "maxFileBytes",
				Cap:     1000,
				Pointer: "/files/3/size",
			},
		},
		{
			name:   "session not open",
			status: http.StatusConflict,
			answer: `{"type":"https://docs.runware.ai/serverless/errors#volume-upload-not-open","title":"Conflict","status":409,"detail":"the upload session is not open"}`,
			want: put.ProblemError{
				Status: 409,
				Slug:   "volume-upload-not-open",
				Detail: "the upload session is not open",
			},
		},
		{
			name:   "title without detail",
			status: http.StatusNotFound,
			answer: `{"type":"https://docs.runware.ai/serverless/errors#not-found","title":"Not Found","status":404}`,
			want: put.ProblemError{
				Status: 404,
				Slug:   "not-found",
				Detail: "Not Found",
			},
		},
		{
			name:   "not a problem body",
			status: http.StatusBadGateway,
			answer: `<html>bad gateway</html>`,
			want: put.ProblemError{
				Status: 502,
				Detail: "Bad Gateway",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := volumeServer(t, volumeExchange{
				method: http.MethodPost,
				path:   "/v1/volumes/weights/uploads/" + testUploadID + "/files",
				query:  testEnvQuery,
				status: tc.status,
				answer: tc.answer,
			})
			_, err := v.DeclareFiles(context.Background(), testUploadRef, put.FileDeclarations{})
			p, ok := errors.AsType[*put.ProblemError](err)
			if !ok {
				t.Fatalf("err = %v, want a *put.ProblemError", err)
			}
			if *p != tc.want {
				t.Fatalf("problem = %+v, want %+v", *p, tc.want)
			}
		})
	}
}

func TestVolumeUploadsKeyPairNotReadyIsTheSentinel(t *testing.T) {
	v := volumeServer(t, volumeExchange{
		method: http.MethodGet,
		path:   "/v1/volumes-key-pair",
		status: http.StatusConflict,
		answer: `{"type":"https://docs.runware.ai/serverless/errors#volume-key-pair-not-ready","title":"Conflict","status":409}`,
	})
	if _, err := v.KeyPairStatement(context.Background()); !errors.Is(err, put.ErrKeyPairNotReady) {
		t.Fatalf("err = %v, want ErrKeyPairNotReady", err)
	}
}

func TestVolumeUploadsWithoutAKeySendsNothing(t *testing.T) {
	v := newClient("", "http://127.0.0.1:1", nil, http.DefaultClient).VolumeUploads()
	if _, err := v.KeyPairStatement(context.Background()); !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("err = %v, want ErrNoAPIKey", err)
	}
}

func TestVolumeUploadsDebugLogRedactsSecrets(t *testing.T) {
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"uploadTargets":[{"partNumber":1,"url":"https://upload.example/SECRETURL","token":"SECRETTOKEN","expiresAt":"2026-11-10T12:00:00Z"}],"ack":"SECRETACK","sealedKey":"SECRETKEY"}`)
	}))
	defer srv.Close()
	v := newClient("test-key", srv.URL, logger, srv.Client()).VolumeUploads()
	if _, err := v.RenewParts(context.Background(), testUploadRef, testFileID, []int{1}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logged.String(), "partNumber") {
		t.Fatalf("nothing logged: %s", logged.String())
	}
	for _, secret := range []string{"SECRETURL", "SECRETTOKEN", "SECRETACK", "SECRETKEY"} {
		if strings.Contains(logged.String(), secret) {
			t.Errorf("debug log shows %s: %s", secret, logged.String())
		}
	}
}

// filesPage is a listVolumeFiles exchange for the first page (cursor "") or a later one.
func filesPage(cursor string, status int, answer string) volumeExchange {
	query := testFilesQuery
	if cursor != "" {
		query = "cursor=" + cursor + "&" + query
	}
	return volumeExchange{
		method: http.MethodGet,
		path:   testFilesPath,
		query:  query,
		status: status,
		answer: answer,
	}
}

func TestVolumeUploadsLatestFilesGivesUpWhenTheListKeepsChanging(t *testing.T) {
	var exchanges []volumeExchange
	for i := range volumeFileListAttempts {
		exchanges = append(exchanges,
			filesPage("", http.StatusOK, `{"manifestSha256":"`+testDigest+`","data":[],"nextCursor":"c1"}`),
			filesPage("c1", http.StatusOK, `{"manifestSha256":"`+strings.Repeat(strconv.Itoa(i), 64)+`","data":[],"nextCursor":null}`),
		)
	}
	v := volumeServer(t, exchanges...)
	if _, err := v.LatestFiles(context.Background(), testVolumeRef); !errors.Is(err, errVolumeFilesKeptChanging) {
		t.Fatalf("err = %v, want errVolumeFilesKeptChanging", err)
	}
}

// TestVolumeUploadsLatestFilesKeepsALaterPage400: a later page's 400 that keeps
// coming back is reported with the API's problem, not only as a changing list.
func TestVolumeUploadsLatestFilesKeepsALaterPage400(t *testing.T) {
	bad := `{"type":"https://docs.runware.ai/serverless/errors#bad-request","title":"Bad Request","status":400,"detail":"limit is out of range"}`
	var ex []volumeExchange
	for range volumeFileListAttempts {
		ex = append(ex,
			filesPage("", http.StatusOK, `{"manifestSha256":"`+testDigest+`","data":[],"nextCursor":"c1"}`),
			filesPage("c1", http.StatusBadRequest, bad),
		)
	}
	v := volumeServer(t, ex...)
	_, err := v.LatestFiles(context.Background(), testVolumeRef)
	if p, ok := errors.AsType[*put.ProblemError](err); !ok || p.Detail != "limit is out of range" {
		t.Fatalf("err = %v, want the 400 problem", err)
	}
}

func TestVolumeUploadsLatestFilesStopsOnARepeatedCursor(t *testing.T) {
	page := `{"manifestSha256":"` + testDigest + `","data":[],"nextCursor":"c1"}`
	v := volumeServer(t,
		filesPage("", http.StatusOK, page),
		filesPage("c1", http.StatusOK, page),
	)
	if _, err := v.LatestFiles(context.Background(), testVolumeRef); !errors.Is(err, errRepeatedVolumeCursor) {
		t.Fatalf("err = %v, want errRepeatedVolumeCursor", err)
	}
}

// TestVolumeUploadsLatestFilesPassesAFirstPage400On: only a later page's 400 means the list
// changed; a 400 on the first page is the server's answer.
func TestVolumeUploadsLatestFilesPassesAFirstPage400On(t *testing.T) {
	v := volumeServer(t,
		filesPage("", http.StatusBadRequest, `{"type":"https://docs.runware.ai/serverless/errors#bad-request","title":"Bad Request","status":400,"detail":"limit is out of range"}`),
	)
	_, err := v.LatestFiles(context.Background(), testVolumeRef)
	p, ok := errors.AsType[*put.ProblemError](err)
	if !ok || p.Status != http.StatusBadRequest || p.Detail != "limit is out of range" {
		t.Fatalf("err = %v, want the 400 problem", err)
	}
}

func TestVolumeUploadsMalformedAnswerIsAProtocolError(t *testing.T) {
	v := volumeServer(t,
		filesPage("", http.StatusOK, `{"data":`),
	)
	if _, err := v.LatestFiles(context.Background(), testVolumeRef); !errors.Is(err, put.ErrProtocol) {
		t.Fatalf("err = %v, want put.ErrProtocol", err)
	}
}

func TestVolumeUploadsDotNamesStayOneSegment(t *testing.T) {
	for name, want := range map[string]string{
		".":   "/v1/volumes/%2E/files",
		"..":  "/v1/volumes/%2E%2E/files",
		"a/b": "/v1/volumes/a%2Fb/files",
	} {
		if got := "/" + volumePath(name, "files"); got != want {
			t.Errorf("volumePath(%q) = %s, want %s", name, got, want)
		}
	}
}

// TestVolumeUploadsDebugLogRedactsEverySecretMember: every secret-bearing member of the
// upload answers is replaced, at any depth, in a success and in a problem answer.
func TestVolumeUploadsDebugLogRedactsEverySecretMember(t *testing.T) {
	var answer strings.Builder
	answer.WriteString(`{"nested":{`)
	keys := []string{"value", "token", "ack", "url", "sealedKey", "dataKey", "encapsulation"}
	// encoding/json matches member names without regard to case, so every
	// case variant is a secret too.
	keys = append(keys, "Token", "URL", "SealedKey", "DATAKEY")
	for i, key := range keys {
		if i > 0 {
			answer.WriteString(",")
		}
		fmt.Fprintf(&answer, `"%s":"SECRET-%s"`, key, key)
	}
	answer.WriteString(`},"kept":"visible"}`)
	for _, status := range []int{http.StatusOK, http.StatusConflict} {
		var logged bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, answer.String())
		}))
		v := newClient("test-key", srv.URL, logger, srv.Client()).VolumeUploads()
		_, _ = v.RenewParts(context.Background(), testUploadRef, testFileID, []int{1})
		srv.Close()
		if !strings.Contains(logged.String(), "visible") {
			t.Fatalf("status %d: nothing logged: %s", status, logged.String())
		}
		if strings.Contains(logged.String(), "SECRET-") {
			t.Errorf("status %d: debug log shows a secret: %s", status, logged.String())
		}
	}
}

// TestVolumeUploadsDebugLogOmitsABodyThatIsNotJSON: an answer cut at
// maxVolumeAnswerBytes, or one that is malformed, cannot be checked for
// secrets, so the debug log leaves the body out.
func TestVolumeUploadsDebugLogOmitsABodyThatIsNotJSON(t *testing.T) {
	for name, answer := range map[string]string{
		"truncated": `{"token":"SECRET-TOKEN","pad":"` + strings.Repeat("x", maxVolumeAnswerBytes) + `"}`,
		"malformed": `{"token":"SECRET-TOKEN",`,
	} {
		var logged bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, answer)
		}))
		v := newClient("test-key", srv.URL, logger, srv.Client()).VolumeUploads()
		_, err := v.RenewParts(context.Background(), testUploadRef, testFileID, []int{1})
		srv.Close()
		if !errors.Is(err, put.ErrProtocol) {
			t.Errorf("%s: err = %v, want put.ErrProtocol", name, err)
		}
		if strings.Contains(logged.String(), "SECRET-") {
			t.Errorf("%s: debug log shows a secret", name)
		}
		if !strings.Contains(logged.String(), volumeBodyOmitted) {
			t.Errorf("%s: debug log does not say the body was omitted: %.200s", name, logged.String())
		}
	}
}
