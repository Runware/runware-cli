package serverless

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"testing"
)

const (
	scopeFlag        = "--scope"
	trustGCPName     = "gcp-prod"
	trustGenericName = "vault"
	trustIssuer      = "https://oidc.runware.example"
	trustEnvUUID     = "0190a6a8-0000-7000-8000-0000000000e1"
	trustPrefix      = "worker:org:0190a6a8-0000-7000-8000-00000000000a:env:" + trustEnvUUID + ":"
	trustPool        = "projects/123456789/locations/global/workloadIdentityPools/runware"
	trustTail        = `"issuer":"` + trustIssuer + `",` + testConnectorTail
)

// attachedWith returns the attached-connector list body for one connector:
// base is the connector JSON without its closing brace.
func attachedWith(base string) string {
	return `{"data":[` + base + `,"subject":"` + testSubject + `","subjectPattern":"` + testSubjectPattern + `"}]}`
}

const (
	trustAWSBase = `{"id":"11111111-1111-1111-1111-111111111111","name":"aws-prod","kind":"aws","audience":"runware:org-1",` +
		`"aws":{"roleArn":"arn:aws:iam::123456789012:role/ci/runware-worker"},` + trustTail
	trustGCPBase = `{"id":"22222222-2222-2222-2222-222222222222","name":"gcp-prod","kind":"gcp","audience":"//iam.googleapis.com/` + testWIP + `",` +
		`"gcp":{"workloadIdentityProvider":"` + testWIP + `","serviceAccountEmail":"` + testServiceAccount + `"},` + trustTail
	trustGCPDirectBase = `{"id":"22222222-2222-2222-2222-222222222222","name":"gcp-prod","kind":"gcp","audience":"//iam.googleapis.com/` + testWIP + `",` +
		`"gcp":{"workloadIdentityProvider":"` + testWIP + `"},` + trustTail
	trustGenericBase = `{"id":"33333333-3333-3333-3333-333333333333","name":"vault","kind":"generic","audience":"https://vault.example.com/it's",` + trustTail
)

// runTrust runs `serverless connectors trust` against a stub holding the
// connector and its attachment, and returns the printed script.
func runTrust(t *testing.T, name, base string, args ...string) serverlessRun {
	t.Helper()
	connectorPath := testConnectorsPath + "/" + name
	api := func(w http.ResponseWriter, r *http.Request) {
		connectorAPI(t, &bodyRecorder{}, map[string]stubResponse{
			"GET " + connectorPath:     {status: http.StatusOK, body: base + "}"},
			"GET " + testAppConnectors: {status: http.StatusOK, body: attachedWith(base)},
		})(w, r)
	}
	all := append([]string{testConnectorsGroup, "trust", name, "--app", testAppID}, args...)
	return runServerlessAgainst(t, api, all...)
}

func TestConnectorsTrustPrintsTheCommandsOfEachProviderAndScope(t *testing.T) {
	cases := []struct {
		name string
		conn string
		base string
		args []string
		want []string
		deny []string
	}{
		{
			name: "aws app",
			conn: "aws-prod",
			base: trustAWSBase,
			want: []string{
				"aws iam create-open-id-connect-provider --url " + trustIssuer + " --client-id-list runware:org-1\n",
				`"StringEquals": {`,
				`"oidc.runware.example:aud": "runware:org-1"`,
				`"oidc.runware.example:sub": "` + testSubject + `"`,
				`"Federated": "arn:aws:iam::123456789012:oidc-provider/oidc.runware.example"`,
				"aws iam create-role --role-name runware-worker --path /ci/ --assume-role-policy-document file://trust-policy.json\n",
			},
			deny: []string{"StringLike", testSubjectPattern},
		},
		{
			name: "aws environment",
			conn: "aws-prod",
			base: trustAWSBase,
			args: []string{scopeFlag, trustScopeEnvironment},
			want: []string{
				`"StringLike": {`,
				`"oidc.runware.example:sub": "` + testSubjectPattern + `"`,
				`"oidc.runware.example:aud": "runware:org-1"`,
			},
			deny: []string{testSubject + `"`},
		},
		{
			name: "gcp app with a service account",
			conn: trustGCPName,
			base: trustGCPBase,
			want: []string{
				"gcloud iam workload-identity-pools create " + trustPool + " --display-name='Runware Serverless'\n",
				"gcloud iam workload-identity-pools providers create-oidc " + testWIP + " \\\n",
				"  --issuer-uri=" + trustIssuer + " \\\n",
				"attribute.environment_id=assertion.environment_id",
				`  --attribute-condition="assertion.sub.startsWith('` + trustPrefix + `')"` + "\n",
				"gcloud iam service-accounts add-iam-policy-binding " + testServiceAccount + " \\\n",
				"  --role=roles/iam.workloadIdentityUser \\\n",
				"  --member=principal://iam.googleapis.com/" + trustPool + "/subject/" + testSubject + "\n",
			},
			deny: []string{"principalSet", "BUCKET"},
		},
		{
			name: "gcp environment with a service account",
			conn: trustGCPName,
			base: trustGCPBase,
			args: []string{scopeFlag, trustScopeEnvironment},
			want: []string{
				"  --member=principalSet://iam.googleapis.com/" + trustPool + "/attribute.environment_id/" + trustEnvUUID + "\n",
				"roles/iam.workloadIdentityUser",
			},
			deny: []string{"principal://", "BUCKET"},
		},
		{
			name: "gcp app without a service account",
			conn: trustGCPName,
			base: trustGCPDirectBase,
			want: []string{
				`: "${BUCKET:?set BUCKET to the name of the bucket the workers may read}"` + "\n",
				`gcloud storage buckets add-iam-policy-binding "gs://${BUCKET}" \` + "\n",
				"  --role=roles/storage.objectViewer \\\n",
				"  --member=principal://iam.googleapis.com/" + trustPool + "/subject/" + testSubject + "\n",
			},
			deny: []string{"workloadIdentityUser", "principalSet"},
		},
		{
			name: "gcp environment without a service account",
			conn: trustGCPName,
			base: trustGCPDirectBase,
			args: []string{scopeFlag, trustScopeEnvironment},
			want: []string{
				"  --member=principalSet://iam.googleapis.com/" + trustPool + "/attribute.environment_id/" + trustEnvUUID + "\n",
				"roles/storage.objectViewer",
			},
			deny: []string{"principal://", "workloadIdentityUser"},
		},
		{
			name: "generic app",
			conn: trustGenericName,
			base: trustGenericBase,
			want: []string{
				"OIDC_ISS=" + trustIssuer + "\n",
				`OIDC_AUD='https://vault.example.com/it'\''s'` + "\n",
				"OIDC_SUB=" + testSubject + "\n",
			},
			deny: []string{"OIDC_SUB_PREFIX", "OIDC_SUB_PATTERN"},
		},
		{
			name: "generic environment",
			conn: trustGenericName,
			base: trustGenericBase,
			args: []string{scopeFlag, trustScopeEnvironment},
			want: []string{
				"OIDC_ISS=" + trustIssuer + "\n",
				"OIDC_SUB_PREFIX=" + trustPrefix + "\n",
				"OIDC_SUB_PATTERN='" + testSubjectPattern + "'\n",
			},
			deny: []string{"OIDC_SUB="},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := runTrust(t, tc.conn, tc.base, tc.args...)
			if run.err != nil {
				t.Fatal(run.err)
			}
			for _, want := range tc.want {
				if !strings.Contains(run.stdout, want) {
					t.Errorf("output missing %q:\n%s", want, run.stdout)
				}
			}
			for _, deny := range tc.deny {
				if strings.Contains(run.stdout, deny) {
					t.Errorf("output has %q:\n%s", deny, run.stdout)
				}
			}
			if sh, err := exec.LookPath("sh"); err == nil {
				check := exec.CommandContext(t.Context(), sh, "-n") //nolint:gosec // sh from LookPath, fixed arguments
				check.Stdin = strings.NewReader(run.stdout)
				if out, err := check.CombinedOutput(); err != nil {
					t.Errorf("output is not valid shell: %v\n%s\n%s", err, out, run.stdout)
				}
			}
		})
	}
}

func TestConnectorsTrustAwsPolicyIsValidJSONWithTheTrustedSubject(t *testing.T) {
	for scope, wantOperator := range map[string]string{trustScopeApp: "StringEquals", trustScopeEnvironment: "StringLike"} {
		run := runTrust(t, testConnectorName, trustAWSBase, scopeFlag, scope)
		if run.err != nil {
			t.Fatal(run.err)
		}
		_, doc, _ := strings.Cut(run.stdout, "<<'EOF'\n")
		doc, _, _ = strings.Cut(doc, "EOF\n")
		var policy awsTrustPolicy
		if err := json.Unmarshal([]byte(doc), &policy); err != nil {
			t.Fatalf("%s: policy is not JSON: %v\n%s", scope, err, doc)
		}
		cond := policy.Statement[0].Condition
		if got := cond["StringEquals"]["oidc.runware.example:aud"]; got != "runware:org-1" {
			t.Errorf("%s: aud = %q", scope, got)
		}
		want := testSubject
		if scope == trustScopeEnvironment {
			want = testSubjectPattern
		}
		if got := cond[wantOperator]["oidc.runware.example:sub"]; got != want {
			t.Errorf("%s: %s sub = %q, want %q", scope, wantOperator, got, want)
		}
	}
}

func TestConnectorsTrustRefusals(t *testing.T) {
	t.Run("connector not attached", func(t *testing.T) {
		api := connectorAPI(t, &bodyRecorder{}, map[string]stubResponse{
			"GET " + testConnectorPath: {status: http.StatusOK, body: trustAWSBase + "}"},
			"GET " + testAppConnectors: {status: http.StatusOK, body: `{"data":[]}`},
		})
		run := runServerlessAgainst(t, api, testConnectorsGroup, "trust", testConnectorName, "--app", testAppID)
		if run.err == nil || !strings.Contains(run.err.Error(), "is not attached") || run.stdout != "" {
			t.Fatalf("err = %v, stdout = %q, want a not attached error and no output", run.err, run.stdout)
		}
	})
	t.Run("unknown scope", func(t *testing.T) {
		run := runTrust(t, testConnectorName, trustAWSBase, scopeFlag, "org")
		if run.err == nil || !strings.Contains(run.err.Error(), "invalid --scope") {
			t.Fatalf("err = %v", run.err)
		}
	})
	t.Run("app is required", func(t *testing.T) {
		run := runServerlessAgainst(t, nil, testConnectorsGroup, "trust", testConnectorName)
		if run.err == nil || !strings.Contains(run.err.Error(), `"app" not set`) {
			t.Fatalf("err = %v", run.err)
		}
	})
	t.Run("connector without an issuer", func(t *testing.T) {
		base := strings.Replace(trustAWSBase, `"issuer":"`+trustIssuer+`",`, "", 1)
		run := runTrust(t, testConnectorName, base)
		if run.err == nil || !strings.Contains(run.err.Error(), "no issuer") {
			t.Fatalf("err = %v", run.err)
		}
	})
	t.Run("a value that could end a shell quote is refused", func(t *testing.T) {
		base := strings.Replace(trustGCPBase, testServiceAccount, "x';touch pwned;'@y.iam.gserviceaccount.com", 1)
		run := runTrust(t, trustGCPName, base)
		if run.err == nil || run.stdout != "" {
			t.Fatalf("err = %v, stdout = %q, want a refusal and no output", run.err, run.stdout)
		}
	})
	t.Run("subject outside the pattern", func(t *testing.T) {
		_, err := renderTrust(trustInput{
			subject:        testStagingSubject,
			subjectPattern: testSubjectPattern,
		})
		if err == nil || !strings.Contains(err.Error(), "outside subjectPattern") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestConnectorsTrustUsesTheEnvironmentOfTheAttachment(t *testing.T) {
	var query map[string][]string
	api := func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oidc-connectors") {
			query = r.URL.Query()
		}
		connectorAPI(t, &bodyRecorder{}, map[string]stubResponse{
			"GET " + testConnectorPath: {status: http.StatusOK, body: trustAWSBase + "}"},
			"GET " + testAppConnectors: {status: http.StatusOK, body: attachedWith(trustAWSBase)},
		})(w, r)
	}
	run := runServerlessAgainst(t, api, testConnectorsGroup, "trust", testConnectorName, "--app", testAppID, "--environment", testEnvironment)
	if run.err != nil {
		t.Fatal(run.err)
	}
	if len(query["environment"]) != 1 || query["environment"][0] != testEnvironment {
		t.Fatalf("environment = %v, want [%s]", query["environment"], testEnvironment)
	}
	if !strings.Contains(run.stdout, "environment "+testEnvironment) {
		t.Errorf("header does not name the environment:\n%s", run.stdout)
	}
}
