package put

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/runware/runware-cli/pkg/volumeformat"
)

type expectedLayout struct {
	Layout PartLayout `json:"layout"`
	Path   string     `json:"path"`
}

// uploadExchange is the shared fixture of the bodies a client sends and the layouts the API answers.
type uploadExchange struct {
	About           string           `json:"about"`
	DeclareFiles    FileDeclarations `json:"declareFiles"`
	ExpectedLayouts []expectedLayout `json:"expectedLayouts"`
	OpenUpload      UploadOpen       `json:"openUpload"`
	OpenedUploadID  string           `json:"openedUploadId"`
}

func decodeStrict(t *testing.T, data []byte, into any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		t.Fatalf("%T does not take %s: %v", into, data, err)
	}
}

func TestWireTypesMatchUploadFixture(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("testdata", "upload-exchange.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ex uploadExchange
	decodeStrict(t, data, &ex)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for key, got := range map[string]any{
		"declareFiles": ex.DeclareFiles,
		"openUpload":   ex.OpenUpload,
	} {
		encoded, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		var want, have any
		if err := json.Unmarshal(raw[key], &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(encoded, &have); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(want, have) {
			t.Errorf("%s encodes as %s, the fixture has %s", key, encoded, raw[key])
		}
	}

	if len(ex.DeclareFiles.Files) != len(ex.ExpectedLayouts) || len(ex.DeclareFiles.Files) == 0 {
		t.Fatalf("%d declarations and %d layouts", len(ex.DeclareFiles.Files), len(ex.ExpectedLayouts))
	}
	for i := range ex.DeclareFiles.Files {
		d, want := ex.DeclareFiles.Files[i], ex.ExpectedLayouts[i]
		if d.Path != want.Path {
			t.Errorf("declaration %d is %s, its layout is for %s", i, d.Path, want.Path)
		}
		if _, err := checkLayout(d.Size, &want.Layout); err != nil {
			t.Errorf("the fixture's layout for %s is not the format's: %v", d.Path, err)
		}
		if len(d.SealedKey) != 80 || len(d.AttemptID) != 32 || len(d.SHA256) != 64 {
			t.Errorf("declaration of %s has a sealed key of %d, an attempt id of %d and a digest of %d characters", d.Path, len(d.SealedKey), len(d.AttemptID), len(d.SHA256))
		}
	}
}

const specDeclareAnswer = `{"files":[
  {"file":{"id":"2e4f6a8c-0b1d-4e3f-9a5c-7b9d1f3e5a77","path":"unet/diffusion_pytorch_model.safetensors","mode":292,"size":10270000000,
           "sha256":"5a1b3c5d7e9f0a2b4c6d8e0f1a3b5c7d9e1f2a4b6c8d0e2f4a6b8c0d2e4f6a8b","state":"uploading","attemptId":"3f9a1c7e5b2d4f6a8c0e1b3d5f7a9c2e",
           "layout":{"segmentBytes":1048576,"segmentPlaintextBytes":1048560,"segmentsPerPart":64,"partSizeBytes":67108880,"partCount":154,"ciphertextBytes":10270159184}},
   "uploadTargets":[{"partNumber":1,"url":"https://upload.example.com/v1/parts","token":"example-part-token-1","expiresAt":"2026-11-09T13:05:00Z"},
                    {"partNumber":2,"url":"https://upload.example.com/v1/parts","token":"example-part-token-2","expiresAt":"2026-11-09T13:05:00Z"}]},
  {"file":{"id":"9c7a5e3b-1d2f-4a6c-8e0b-3f5d7a9c1e22","path":"vae/diffusion_pytorch_model.safetensors","mode":292,"size":334643268,
           "sha256":"0e1d2c3b4a5f6e7d8c9b0a1f2e3d4c5b6a7f8e9d0c1b2a3f4e5d6c7b8a9f0e1d","state":"reused"},
   "blob":{"sha256":"0e1d2c3b4a5f6e7d8c9b0a1f2e3d4c5b6a7f8e9d0c1b2a3f4e5d6c7b8a9f0e1d","size":334643268}}]}`

const specResult = `{"upload":{"id":"4b6d8f0a-2c4e-4f6a-8b0d-2f4a6c8e0b33","volumeId":"6a1f0d6e-5c1b-4f69-9a39-0f4c1c8b2d11","state":"completed",
  "createdAt":"2026-11-09T12:00:00Z","expiresAt":"2026-11-10T12:00:00Z"},"changed":true,"manifestSha256":"9f2c4e1a7b3d5f8e0c6a2b4d1e3f5a7c9b0d2e4f6a8c1b3d5e7f9a0c2e4b6d8f"}`

const specCompletion = `{"crc64nvme":"1f2e3d4c5b6a7980","parts":[{"partNumber":1,"ack":"YWNrLWV4YW1wbGUtcGFydC0x"},{"partNumber":2,"ack":"YWNrLWV4YW1wbGUtcGFydC0y"}]}`

func TestWireTypesTakeSpecExamples(t *testing.T) {
	t.Parallel()
	var answer DeclarationResult
	decodeStrict(t, []byte(specDeclareAnswer), &answer)
	if len(answer.Files) != 2 || answer.Files[0].File.State != stateUploading || len(answer.Files[0].UploadTargets) != 2 {
		t.Fatalf("declaration answer = %+v", answer)
	}
	if answer.Files[1].File.State != stateReused || answer.Files[1].Blob == nil || answer.Files[1].File.Layout != nil {
		t.Errorf("reused answer = %+v", answer.Files[1])
	}
	if _, err := checkLayout(answer.Files[0].File.Size, answer.Files[0].File.Layout); err != nil {
		t.Errorf("the spec's layout is not the format's: %v", err)
	}
	if tok := answer.Files[0].UploadTargets[0].Token; string(tok) != "example-part-token-1" || tok.String() != redactedText {
		t.Errorf("target token reads %q, prints %q", string(tok), tok.String())
	}

	var result UploadResult
	decodeStrict(t, []byte(specResult), &result)
	if !result.Changed || result.Upload.State != "completed" || len(result.ManifestSHA256) != 64 || result.Upload.ExpiresAt.Sub(result.Upload.CreatedAt).Hours() != 24 {
		t.Errorf("upload result = %+v", result)
	}

	var completion FileCompletion
	decodeStrict(t, []byte(specCompletion), &completion)
	out, err := json.Marshal(completion)
	if err != nil {
		t.Fatal(err)
	}
	var want, have any
	if err := json.Unmarshal([]byte(specCompletion), &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &have); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, have) {
		t.Errorf("completion encodes as %s, the spec has %s", out, specCompletion)
	}

	var renew PartTokenRequest
	decodeStrict(t, []byte(`{"partNumbers":[12,13,14]}`), &renew)
	var targets UploadTargets
	decodeStrict(t, []byte(`{"uploadTargets":[{"partNumber":12,"url":"https://upload.example.com/v1/parts","token":"a.b","expiresAt":"2026-11-09T13:05:00Z"}]}`), &targets)
	if len(renew.PartNumbers) != 3 || len(targets.UploadTargets) != 1 {
		t.Errorf("renewal = %+v, %+v", renew, targets)
	}
}

func TestCheckLayout(t *testing.T) {
	t.Parallel()
	size := int64(10270000000)
	plan, err := volumeformat.PlanLayout(size)
	if err != nil {
		t.Fatal(err)
	}
	good := PartLayout{
		SegmentBytes:          volumeformat.SegmentBytes,
		SegmentPlaintextBytes: volumeformat.SegmentPlaintextBytes,
		SegmentsPerPart:       plan.SegmentsPerPart,
		PartSizeBytes:         plan.PartSize,
		PartCount:             plan.PartCount,
		CiphertextBytes:       plan.CiphertextSize,
	}
	if got, err := checkLayout(size, &good); err != nil || got != plan {
		t.Fatalf("checkLayout of the format's layout = %+v, %v", got, err)
	}

	for name, mutate := range map[string]func(*PartLayout){
		"segment size":      func(l *PartLayout) { l.SegmentBytes++ },
		"plaintext segment": func(l *PartLayout) { l.SegmentPlaintextBytes-- },
		"segments per part": func(l *PartLayout) { l.SegmentsPerPart++ },
		"part size":         func(l *PartLayout) { l.PartSizeBytes++ },
		"part count":        func(l *PartLayout) { l.PartCount++ },
		"ciphertext size":   func(l *PartLayout) { l.CiphertextBytes++ },
	} {
		bad := good
		mutate(&bad)
		if _, err := checkLayout(size, &bad); !errors.Is(err, ErrProtocol) {
			t.Errorf("a layout with another %s: error = %v, want a contract violation", name, err)
		}
	}
	if _, err := checkLayout(size, nil); !errors.Is(err, ErrProtocol) {
		t.Errorf("no layout: error = %v, want a contract violation", err)
	}
}

func TestSecretIsRedacted(t *testing.T) {
	t.Parallel()
	const value = "eyJ2IjoxfQ.c2lnbmF0dXJl"
	target := Target{
		PartNumber: 1,
		URL:        "https://upload.example.com/v1/parts",
		Token:      Secret(value),
	}
	ack := PartAck{
		PartNumber: 1,
		Ack:        Secret(value),
	}

	var printed []string
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		printed = append(printed, fmt.Sprintf(verb, Secret(value)), fmt.Sprintf(verb, target), fmt.Sprintf(verb, &ack))
	}
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("sent", "token", Secret(value), "target", target)
	slog.New(slog.NewTextHandler(&logs, nil)).Info("sent", "token", Secret(value), "ack", ack)
	printed = append(printed, logs.String())
	for _, out := range printed {
		if strings.Contains(out, value) {
			t.Error("a printed form holds the secret")
		}
	}

	wire, err := json.Marshal(target)
	if err != nil || !strings.Contains(string(wire), value) {
		t.Errorf("the target encodes as %s (%v), want the token on the wire", wire, err)
	}
}

type pointerCase struct {
	pointer string
	index   int
	ok      bool
}

func TestPointerFile(t *testing.T) {
	t.Parallel()
	cases := []pointerCase{
		{
			pointer: "/files/0/size",
			index:   0,
			ok:      true,
		},
		{
			pointer: "/files/12/path",
			index:   12,
			ok:      true,
		},
		{
			pointer: "/files/7",
			index:   7,
			ok:      true,
		},
		{pointer: "/keyPairId"},
		{pointer: ""},
		{pointer: "/files/x/path"},
		{pointer: "/files/-1/path"},
	}
	for _, tc := range cases {
		if i, ok := pointerFile(tc.pointer); ok != tc.ok || (ok && i != tc.index) {
			t.Errorf("pointerFile(%q) = %d, %v, want %d, %v", tc.pointer, i, ok, tc.index, tc.ok)
		}
	}
}

func TestParseProblem(t *testing.T) {
	t.Parallel()
	body := `{"type":"https://docs.runware.ai/serverless/errors#volume-limit-exceeded","title":"Conflict","status":409,
	  "detail":"maxFileBytes exceeds its limit","limit":"maxFileBytes","cap":268435456000,
	  "errors":[{"detail":"maxFileBytes exceeds its limit","pointer":"/files/2/size"}]}`

	p := parseProblem(http.StatusConflict, []byte(body))

	want := ProblemError{
		Status:  http.StatusConflict,
		Slug:    slugLimitExceeded,
		Detail:  "maxFileBytes exceeds its limit",
		Limit:   limitFileBytes,
		Cap:     268435456000,
		Pointer: "/files/2/size",
	}
	if *p != want {
		t.Errorf("parseProblem = %+v, want %+v", *p, want)
	}
	if bare := parseProblem(http.StatusBadGateway, []byte("<html>")); bare.Status != http.StatusBadGateway || bare.Slug != "" || bare.Detail != "" {
		t.Errorf("a body that is not a problem parses as %+v, want the status only", *bare)
	}
	if titled := parseProblem(http.StatusNotFound, []byte(`{"title":"Not Found","status":404}`)); titled.Detail != "Not Found" {
		t.Errorf("a problem without detail falls back to its title: %+v", *titled)
	}
}

func TestLimitErrorsNameTheLimit(t *testing.T) {
	t.Parallel()
	for limit, want := range map[string]string{
		limitFileBytes:      "file size",
		limitStoredBytes:    "storage quota",
		limitFilesPerVolume: "files",
		limitMountedBytes:   "app version",
		"somethingNew":      "somethingNew",
	} {
		got := (&LimitError{
			Limit: limit,
			Cap:   5,
		}).Error()
		if !strings.Contains(got, want) || !strings.Contains(got, "5") {
			t.Errorf("%s: %q does not mention %q and the cap", limit, got, want)
		}
	}
}
