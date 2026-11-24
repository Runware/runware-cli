package serverless

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
)

const (
	testMountFlag    = "--mount"
	testNoMountsFlag = "--no-mounts"
	testMountWeights = "model-weights:/models"
	testDigestOld    = "1111111111111111111111111111111111111111111111111111111111111111"
	testDigestNew    = "2222222222222222222222222222222222222222222222222222222222222222"
	testListCmd      = "list"
	testShowCmd      = "show"
)

// mountStub is a fake serverless API for the mount commands. It records every
// request and the decoded body of the app write.
type mountStub struct {
	t *testing.T
	// exists makes getApp answer the app instead of 404.
	exists bool
	// refuse, when set, is the problem body the app write answers with 422.
	refuse string
	// appMounts is the mounts array the app read returns; the writes, like the
	// API's, return none.
	appMounts string
	// unpinned makes the app read return a null activeVersionId.
	unpinned bool
	// versions and workers are the list bodies the stub serves.
	versions string
	workers  string

	requests []string
	uploads  int
	write    map[string]json.RawMessage
}

func (s *mountStub) serve() {
	s.t.Helper()
	upload := newSourceUploadStub(s.t)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests = append(s.requests, r.Method+" "+r.URL.Path)
		if upload(w, r) {
			s.uploads++
			return
		}
		appPath := testAppsPath + "/" + testAppID
		switch {
		case r.Method == http.MethodGet && r.URL.Path == appPath:
			if !s.exists {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"type":"about:blank","title":"Not Found","status":404}`)
				return
			}
			body := activeAppBody(newVersionID)
			if s.unpinned {
				body = activeAppBody("")
			}
			if s.appMounts != "" {
				body = strings.TrimSuffix(strings.TrimSpace(body), "}") + `,"mounts":` + s.appMounts + `}`
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/environments/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"name":"production"}`)
		case (r.Method == http.MethodPatch && r.URL.Path == appPath) ||
			(r.Method == http.MethodPost && r.URL.Path == testAppsPath):
			if err := json.NewDecoder(r.Body).Decode(&s.write); err != nil {
				s.t.Errorf("decode app write: %v", err)
			}
			if s.refuse != "" {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = io.WriteString(w, s.refuse)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusCreated)
			}
			_, _ = io.WriteString(w, activeAppBody(newVersionID))
		case r.Method == http.MethodGet && r.URL.Path == appPath+"/versions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, s.versions)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, appPath+"/versions/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, mountedVersionBody(newVersionID))
		case r.Method == http.MethodGet && r.URL.Path == appPath+"/workers":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, s.workers)
		default:
			s.t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	s.t.Cleanup(api.Close)
	s.t.Setenv("RUNWARE_API_KEY", "test-key")
	s.t.Setenv("RUNWARE_SERVERLESS_BASE_URL", api.URL)
}

// mountedVersionBody is a version that mounts model-weights at /models.
func mountedVersionBody(id string) string {
	return `{"id":"` + id + `","appId":"` + testAppID + `","versionNumber":2,"createdAt":"2026-07-30T12:00:00Z",
		"mounts":[{"kind":"volume","name":"model-weights","mountPath":"/models","readOnly":true}]}`
}

// captureStdout runs fn with os.Stdout redirected, since output.Print writes there.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	old := os.Stdout
	os.Stdout = f
	runErr := fn()
	os.Stdout = old
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(b), runErr
}

func runMountDeploy(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newDeployCmd(log.New(io.Discard))
	cmd.SetErr(&bytes.Buffer{})
	cmd.PersistentFlags().String("format", testTableFormat, "")
	cmd.SetArgs(args)
	return captureStdout(t, cmd.Execute)
}

// sourceArgs is a code deploy of a one-file source directory.
func sourceArgs(t *testing.T, extra ...string) []string {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{testModelFile: testPySource})
	return append([]string{testModelFile, testSrcDirFlag, dir, testIDFlag, testAppID}, extra...)
}

func TestDeployCreateSendsMountsAndPrintsThem(t *testing.T) {
	stub := &mountStub{t: t}
	stub.serve()

	out, err := runMountDeploy(t, sourceArgs(t, testGPUTypeFlag, testGPUType,
		testVolumeFlag, testVolumePath, testMountFlag, "model-weights:/models/", testMountFlag, "tokenizer:/tok")...)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	want := `[{"kind":"volume","mountPath":"/models","name":"model-weights"},{"kind":"volume","mountPath":"/tok","name":"tokenizer"}]`
	if got := string(stub.write["mounts"]); got != want {
		t.Fatalf("mounts = %s, want %s", got, want)
	}
	if got := string(stub.write["volumes"]); got != `[{"mountPath":"/weights"}]` {
		t.Fatalf("volumes = %s, want the node-local volume beside the mounts", got)
	}
	if !strings.Contains(out, "model-weights:/models, tokenizer:/tok") {
		t.Fatalf("stdout = %q, want each mount with its Volume and path", out)
	}
}

func TestDeployUpdateWithSourceReplacesMounts(t *testing.T) {
	stub := &mountStub{
		t:      t,
		exists: true,
	}
	stub.serve()

	if _, err := runMountDeploy(t, sourceArgs(t, testMountFlag, testMountWeights)...); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if stub.uploads == 0 {
		t.Fatal("the source was not uploaded")
	}
	if _, ok := stub.write["appSource"]; !ok {
		t.Fatalf("update = %v, want the new source", stub.write)
	}
	if got := string(stub.write["mounts"]); !strings.Contains(got, `"name":"model-weights"`) {
		t.Fatalf("mounts = %s", got)
	}
}

// A deploy without a source changes only the mounts: nothing is packed or
// uploaded, and the update carries no source.
func TestDeployWithoutSourceChangesOnlyTheMounts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		args       []string
		wantMounts string
		wantOut    string
	}{
		{
			name:       "mount",
			args:       []string{testMountFlag, testMountWeights},
			wantMounts: `[{"kind":"volume","mountPath":"/models","name":"model-weights"}]`,
			wantOut:    testMountWeights,
		},
		{
			name:       "no mounts",
			args:       []string{testNoMountsFlag},
			wantMounts: `[]`,
			wantOut:    "none",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &mountStub{
				t:      t,
				exists: true,
			}
			stub.serve()

			out, err := runMountDeploy(t, append([]string{testIDFlag, testAppID}, tc.args...)...)
			if err != nil {
				t.Fatalf("deploy: %v", err)
			}
			if stub.uploads != 0 {
				t.Fatalf("uploads = %d, want none", stub.uploads)
			}
			if _, ok := stub.write["appSource"]; ok {
				t.Fatalf("update = %v, want no source", stub.write)
			}
			if got := string(stub.write["mounts"]); got != tc.wantMounts {
				t.Fatalf("mounts = %s, want %s", got, tc.wantMounts)
			}
			if !strings.Contains(out, tc.wantOut) {
				t.Fatalf("stdout = %q, want %q", out, tc.wantOut)
			}
		})
	}
}

// Leaving --mount and --no-mounts out keeps the mounts: the update sends no
// mounts field.
func TestDeployUpdateWithoutMountFlagsKeepsTheMounts(t *testing.T) {
	stub := &mountStub{
		t:      t,
		exists: true,
	}
	stub.serve()

	out, err := runMountDeploy(t, sourceArgs(t)...)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if _, ok := stub.write["mounts"]; ok {
		t.Fatalf("update = %v, want no mounts field", stub.write)
	}
	if strings.Contains(out, colMounts) {
		t.Fatalf("stdout = %q, want no mounts row", out)
	}
}

// --no-mounts has nothing to remove on a new application, so a create refuses
// it instead of dropping it.
func TestDeployRefusesNoMountsOnACreate(t *testing.T) {
	stub := &mountStub{t: t}
	stub.serve()

	_, err := runMountDeploy(t, sourceArgs(t, testGPUTypeFlag, testGPUType, testNoMountsFlag)...)
	if err == nil || !strings.Contains(err.Error(), "--no-mounts applies to an existing application") {
		t.Fatalf("err = %v, want the --no-mounts refusal", err)
	}
	if stub.write != nil || stub.uploads != 0 {
		t.Fatalf("write = %v, uploads = %d, want neither", stub.write, stub.uploads)
	}
}

// --no-mounts=false removes nothing, so without a source it is a deploy with
// nothing to send and must be refused before any request.
func TestDeployRefusesNoMountsFalseWithoutASource(t *testing.T) {
	stub := &mountStub{t: t, exists: true}
	stub.serve()

	_, err := runMountDeploy(t, testIDFlag, testAppID, testNoMountsFlag+"=false")
	if err == nil || !strings.Contains(err.Error(), deploySourceChoice) {
		t.Fatalf("err = %v, want the source choice refusal", err)
	}
	if stub.write != nil || stub.uploads != 0 {
		t.Fatalf("write = %v, uploads = %d, want neither", stub.write, stub.uploads)
	}
}

func TestDeployWithoutSourceRefusesACreate(t *testing.T) {
	stub := &mountStub{t: t}
	stub.serve()

	_, err := runMountDeploy(t, testIDFlag, testAppID, testMountFlag, testMountWeights)
	if err == nil || !strings.Contains(err.Error(), "does not exist") || !strings.Contains(err.Error(), deploySourceChoice) {
		t.Fatalf("err = %v, want a refusal that says how to create it", err)
	}
	if stub.write != nil || stub.uploads != 0 {
		t.Fatalf("write = %v, uploads = %d, want neither", stub.write, stub.uploads)
	}
}

// Every refusal below happens before the CLI talks to the API at all, so a
// mistake costs no upload and no build.
func TestDeployRefusesMountMistakesBeforeAnyRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "mount inside a node-local volume",
			args: sourceArgs(t, testGPUTypeFlag, testGPUType, testVolumeFlag, "/srv", testMountFlag, "model-weights:/srv/models"),
			want: `--mount "model-weights:/srv/models" overlaps --volume "/srv"; give the mount a path`,
		},
		{
			name: "two mounts at one path",
			args: sourceArgs(t, testGPUTypeFlag, testGPUType, testMountFlag, testMountWeights, testMountFlag, "tokenizer:/models/"),
			want: `uses the same path as --mount "model-weights:/models"`,
		},
		{
			name: "relative path",
			args: sourceArgs(t, testMountFlag, "model-weights:models"),
			want: "must be an absolute path",
		},
		{
			name: "no path",
			args: sourceArgs(t, testMountFlag, "model-weights"),
			want: "want <volume>:<path>",
		},
		{
			name: "invalid Volume name",
			args: sourceArgs(t, testMountFlag, "Model:/models"),
			want: "lowercase letters",
		},
		{
			name: "mount over a reserved path",
			args: sourceArgs(t, testMountFlag, "model-weights:/data"),
			want: "/data/liquid-components is reserved by the platform",
		},
		{
			name: "mount inside a reserved path",
			args: sourceArgs(t, testMountFlag, "model-weights:/var/run/secrets/runware/oidc/x"),
			want: "/var/run/secrets/runware/oidc is reserved by the platform",
		},
		{
			name: "mount and no-mounts",
			args: sourceArgs(t, testMountFlag, testMountWeights, testNoMountsFlag),
			want: "none of the others can be",
		},
		{
			name: "a code-only flag without a source",
			args: []string{testIDFlag, testAppID, testMountFlag, testMountWeights, "--requirement", "torch"},
			want: "--requirement applies to code deploys only",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &mountStub{
				t:      t,
				exists: true,
			}
			stub.serve()

			_, err := runMountDeploy(t, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if len(stub.requests) != 0 {
				t.Fatalf("requests = %v, want none", stub.requests)
			}
		})
	}
}

// A refusal the CLI cannot foresee, such as a missing Volume, exits nonzero
// with the API's reason and the field it points at.
func TestDeployReportsTheAPIRefusalOfAMount(t *testing.T) {
	stub := &mountStub{
		t:      t,
		exists: true,
		refuse: `{"type":"https://docs.runware.ai/serverless/errors#validation-failed","title":"Unprocessable Entity","status":422,
			"detail":"request validation failed","errors":[{"pointer":"/mounts/0/name","detail":"Volume \"model-weights\" not found"}]}`,
	}
	stub.serve()

	_, err := runMountDeploy(t, testIDFlag, testAppID, testMountFlag, testMountWeights)
	if err == nil || !strings.Contains(err.Error(), `/mounts/0/name: Volume "model-weights" not found`) {
		t.Fatalf("err = %v, want the API's reason", err)
	}
}

// apps show prints the mounts the app read returns, also without an active
// version, marks a deleted Volume's, prints "none" when it returns none, and
// reads no version to find them.
func TestAppsShowPrintsTheActiveVersionMounts(t *testing.T) {
	for _, tc := range []struct {
		name, appMounts, want string
		unpinned              bool
	}{
		{"mounted", `[{"kind":"volume","name":"model-weights","mountPath":"/models","readOnly":true}]`, testMountWeights, false},
		{"deleted", `[{"kind":"volume","name":"model-weights","mountPath":"/models","readOnly":true,"deleted":true}]`, testMountWeights + deletedMountSuffix, false},
		{noMounts, "", noMounts, false},
		// After a create whose first rollout failed there is no active
		// version, and the read returns the latest version's mounts.
		{"unpinned", `[{"kind":"volume","name":"model-weights","mountPath":"/models","readOnly":true}]`, testMountWeights, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &mountStub{
				t:         t,
				exists:    true,
				appMounts: tc.appMounts,
				unpinned:  tc.unpinned,
				workers:   `{"data":[],"summary":{"total":0}}`,
			}
			stub.serve()

			out, err := runAppsShow(context.Background(), t, testTableFormat)
			if err != nil {
				t.Fatalf("show: %v", err)
			}
			if !regexp.MustCompile(colMounts + `\s+│\s+` + regexp.QuoteMeta(tc.want) + `\s`).MatchString(out) {
				t.Fatalf("stdout = %q, want a %s row of %q", out, colMounts, tc.want)
			}
			for _, req := range stub.requests {
				if strings.Contains(req, "/versions") {
					t.Errorf("apps show requested %s; the app read carries the mounts", req)
				}
			}
		})
	}
}

func TestAppsVersionsPrintMounts(t *testing.T) {
	stub := &mountStub{
		t:        t,
		exists:   true,
		versions: `{"data":[` + mountedVersionBody(newVersionID) + `],"summary":{"total":1}}`,
	}
	stub.serve()

	for _, args := range [][]string{{testListCmd, testAppID}, {testShowCmd, testAppID, "2"}} {
		cmd := newAppsVersionsCmd(log.New(io.Discard))
		cmd.PersistentFlags().String("format", testTableFormat, "")
		cmd.SetArgs(args)
		out, err := captureStdout(t, cmd.Execute)
		if err != nil {
			t.Fatalf("versions %s: %v", args[0], err)
		}
		if !strings.Contains(out, testMountWeights) {
			t.Fatalf("versions %s stdout = %q, want the mounts", args[0], out)
		}
	}
}

// After an upload, workers started before it show the old digest and workers
// started after it the new one.
func TestAppsWorkersPrintEachMountedDigest(t *testing.T) {
	worker := func(id, digest string) string {
		return `{"id":"` + id + `","appId":"` + testAppID + `","versionId":"` + newVersionID + `","podName":"p","status":"ready",
			"gpuCount":1,"createdAt":"2026-07-30T12:00:00Z","statusOccurredAt":"2026-07-30T12:00:00Z","updatedAt":"2026-07-30T12:00:00Z",
			"mounts":[{"name":"model-weights","mountPath":"/models","manifestSha256":"` + digest + `"}]}`
	}
	stub := &mountStub{
		t:      t,
		exists: true,
		workers: `{"data":[` + worker("44444444-4444-4444-4444-444444444444", testDigestOld) + `,` +
			worker("55555555-5555-5555-5555-555555555555", testDigestNew) + `],"summary":{"total":2}}`,
	}
	stub.serve()

	cmd := newAppsWorkersCmd(log.New(io.Discard))
	cmd.PersistentFlags().String("format", testTableFormat, "")
	cmd.SetArgs([]string{testAppID})
	out, err := captureStdout(t, cmd.Execute)
	if err != nil {
		t.Fatalf("workers: %v", err)
	}
	for _, digest := range []string{testDigestOld, testDigestNew} {
		if !strings.Contains(out, "model-weights:"+digest[:shortDigestLen]) {
			t.Fatalf("stdout = %q, want digest %s", out, digest[:shortDigestLen])
		}
	}
}
