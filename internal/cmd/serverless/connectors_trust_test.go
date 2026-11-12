package serverless

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
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

// trustCase is one provider and scope of the generated commands.
type trustCase struct {
	name string
	conn string
	base string
	args []string
	want []string
	deny []string
}

// runTrust runs `serverless connectors trust` against a stub that lists the
// connector as attached, and returns the run; the script is its cobraOutput.
func runTrust(t *testing.T, name, base string, args ...string) serverlessRun {
	t.Helper()
	api := connectorAPI(t, &bodyRecorder{}, map[string]stubResponse{
		"GET " + testAppConnectors: {
			status: http.StatusOK,
			body:   attachedWith(base),
		},
	})
	all := append([]string{testConnectorsGroup, "trust", name, "--app", testAppID}, args...)
	return runServerlessAgainst(t, api, all...)
}

func TestConnectorsTrustPrintsTheCommandsOfEachProviderAndScope(t *testing.T) {
	cases := []trustCase{
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
				"#    aws iam update-assume-role-policy --role-name runware-worker --policy-document file://trust-policy.json\n",
				"REPLACES the existing trust policy and does not merge",
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
				"#      gcloud iam workload-identity-pools providers describe " + testWIP + " --format='value(attributeCondition)'\n",
				"#      gcloud iam workload-identity-pools providers update-oidc " + testWIP + ` --attribute-condition="(<existing condition>) || assertion.sub.startsWith('` + trustPrefix + `')"` + "\n",
				"  --issuer-uri=" + trustIssuer + " \\\n",
				"attribute.environment_id=assertion.environment_id",
				`  --attribute-condition="assertion.sub.startsWith('` + trustPrefix + `')"` + "\n",
				"gcloud iam service-accounts add-iam-policy-binding " + testServiceAccount + " \\\n",
				"  --role=roles/iam.workloadIdentityUser \\\n",
				"  --member=principal://iam.googleapis.com/" + trustPool + "/subject/" + testSubject + "\n",
			},
			deny: []string{"principalSet", "BUCKET", "harmless"},
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
				if !strings.Contains(run.cobraOutput, want) {
					t.Errorf("output missing %q:\n%s", want, run.cobraOutput)
				}
			}
			for _, deny := range tc.deny {
				if strings.Contains(run.cobraOutput, deny) {
					t.Errorf("output has %q:\n%s", deny, run.cobraOutput)
				}
			}
			if sh, err := exec.LookPath("sh"); err == nil {
				check := exec.CommandContext(t.Context(), sh, "-n") //nolint:gosec // sh from LookPath, fixed arguments
				check.Stdin = strings.NewReader(run.cobraOutput)
				if out, err := check.CombinedOutput(); err != nil {
					t.Errorf("output is not valid shell: %v\n%s\n%s", err, out, run.cobraOutput)
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
		_, doc, _ := strings.Cut(run.cobraOutput, "<<'EOF'\n")
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

func TestConnectorsTrustRefusesWhatItCannotPrintSafely(t *testing.T) {
	const (
		unsafe    = "cannot be printed safely"
		notARole  = "is not an IAM role ARN"
		listValue = "--client-id-list value"
	)
	var hostile = []string{"x'y", "x;y", "x$y", "x`y", "x y", "x\"y"}
	cases := []trustRefusal{}
	add := func(name, wantErr string, edit func(*serverlessapi.AppOidcConnector, string), values []string, base func() serverlessapi.AppOidcConnector) {
		for _, v := range values {
			cases = append(cases, trustRefusal{
				name:    name + " " + v,
				wantErr: wantErr,
				in:      base(),
				edit:    func(c *serverlessapi.AppOidcConnector) { edit(c, v) },
			})
		}
	}
	setIssuer := func(c *serverlessapi.AppOidcConnector, v string) { c.Issuer = &v }
	setSubject := func(c *serverlessapi.AppOidcConnector, v string) { c.Subject = trustPrefix + "app:" + v }
	setAudience := func(c *serverlessapi.AppOidcConnector, v string) { c.Audience = v }
	setRole := func(c *serverlessapi.AppOidcConnector, v string) {
		c.Aws.RoleArn = "arn:aws:iam::123456789012:role/" + v
	}
	setSA := func(c *serverlessapi.AppOidcConnector, v string) { c.Gcp.ServiceAccountEmail = &v }
	for _, base := range []struct {
		kind string
		fn   func() serverlessapi.AppOidcConnector
	}{
		{kind: kindAws, fn: trustAttachmentAWS},
		{kind: kindGcp, fn: trustAttachmentGCP},
		{kind: kindGeneric, fn: trustAttachmentGeneric},
	} {
		add(base.kind+" issuer", unsafe, setIssuer, append([]string{"https://x'y", "https://x;y", "https://x$y"}, "https://x*y"), base.fn)
		add(base.kind+" subject app segment", unsafe, setSubject, append(hostile, "*"), base.fn)
	}
	add("aws audience", unsafe, setAudience, append(hostile, "aud*", "=aud"), trustAttachmentAWS)
	add("aws audience", listValue, setAudience, []string{"-aud", "a,b"}, trustAttachmentAWS)
	add("aws roleArn", notARole, setRole, hostile, trustAttachmentAWS)
	add("gcp serviceAccountEmail", unsafe, setSA, hostile, trustAttachmentGCP)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.edit(&tc.in)
			for _, scope := range trustScopes {
				out, err := renderTrust(trustInput{
					connector:   tc.in,
					app:         testAppID,
					environment: testProductionEnvironment,
					scope:       scope,
				})
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || out != "" {
					t.Errorf("%s scope: err = %v, out = %q, want an error containing %q and no output", scope, err, out, tc.wantErr)
				}
			}
		})
	}
}

// trustRefusal is an attachment with one hostile value and the error it must draw.
type trustRefusal struct {
	name    string
	wantErr string
	in      serverlessapi.AppOidcConnector
	edit    func(*serverlessapi.AppOidcConnector)
}

func trustAttachment(kind serverlessapi.OidcConnectorKind, audience string) serverlessapi.AppOidcConnector {
	issuer := trustIssuer
	return serverlessapi.AppOidcConnector{
		Name:           "c",
		Kind:           kind,
		Audience:       audience,
		Issuer:         &issuer,
		Subject:        testSubject,
		SubjectPattern: testSubjectPattern,
	}
}

func trustAttachmentAWS() serverlessapi.AppOidcConnector {
	c := trustAttachment(serverlessapi.OidcConnectorKindAws, testAudience)
	c.Aws = &serverlessapi.OidcConnectorAws{
		RoleArn: testRoleARN,
	}
	return c
}

func trustAttachmentGCP() serverlessapi.AppOidcConnector {
	c := trustAttachment(serverlessapi.OidcConnectorKindGcp, gcpAudiencePrefix+testWIP)
	sa := testServiceAccount
	c.Gcp = &serverlessapi.OidcConnectorGcp{
		WorkloadIdentityProvider: testWIP,
		ServiceAccountEmail:      &sa,
	}
	return c
}

func trustAttachmentGeneric() serverlessapi.AppOidcConnector {
	return trustAttachment(serverlessapi.OidcConnectorKindGeneric, "https://vault.example.com")
}

func TestConnectorsTrustRefusesInconsistentAttachments(t *testing.T) {
	noIssuer := func(c *serverlessapi.AppOidcConnector) { c.Issuer = nil }
	cases := []struct {
		name    string
		base    func() serverlessapi.AppOidcConnector
		edit    func(*serverlessapi.AppOidcConnector)
		wantErr string
	}{
		{
			name:    "no issuer",
			base:    trustAttachmentAWS,
			edit:    noIssuer,
			wantErr: "no issuer",
		},
		{
			name:    "subject outside the pattern",
			base:    trustAttachmentGeneric,
			edit:    func(c *serverlessapi.AppOidcConnector) { c.Subject = testStagingSubject },
			wantErr: "outside subjectPattern",
		},
		{
			name:    "pattern without the wildcard",
			base:    trustAttachmentGeneric,
			edit:    func(c *serverlessapi.AppOidcConnector) { c.SubjectPattern = trustPrefix },
			wantErr: "does not end in *",
		},
		{
			name:    "pattern that is not an environment prefix",
			base:    trustAttachmentGeneric,
			edit:    func(c *serverlessapi.AppOidcConnector) { c.SubjectPattern = "worker:org:*" },
			wantErr: "is not worker:org",
		},
		{
			name:    "aws issuer without https",
			base:    trustAttachmentAWS,
			edit:    func(c *serverlessapi.AppOidcConnector) { i := "http://oidc.runware.example"; c.Issuer = &i },
			wantErr: "does not start with https://",
		},
		{
			name:    "aws without its block",
			base:    trustAttachmentAWS,
			edit:    func(c *serverlessapi.AppOidcConnector) { c.Aws = nil },
			wantErr: "no roleArn",
		},
		{
			name:    "gcp without its block",
			base:    trustAttachmentGCP,
			edit:    func(c *serverlessapi.AppOidcConnector) { c.Gcp = nil },
			wantErr: "no workloadIdentityProvider",
		},
		{
			name:    "gcp provider that is not a resource name",
			base:    trustAttachmentGCP,
			edit:    func(c *serverlessapi.AppOidcConnector) { c.Gcp.WorkloadIdentityProvider = "providers/x';y" },
			wantErr: "is not projects/",
		},
		{
			name:    "unknown kind",
			base:    trustAttachmentGeneric,
			edit:    func(c *serverlessapi.AppOidcConnector) { c.Kind = "azure" },
			wantErr: "unknown connector kind",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.base()
			tc.edit(&c)
			out, err := renderTrust(trustInput{
				connector: c,
				scope:     trustScopeApp,
			})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) || out != "" {
				t.Fatalf("err = %v, out = %q, want an error containing %q and no output", err, out, tc.wantErr)
			}
		})
	}
}

func TestConnectorsTrustCommandRefusals(t *testing.T) {
	t.Run("connector not attached names the environment in its hint", func(t *testing.T) {
		api := connectorAPI(t, &bodyRecorder{}, map[string]stubResponse{
			"GET " + testAppConnectors: {
				status: http.StatusOK,
				body:   `{"data":[]}`,
			},
		})
		run := runServerlessAgainst(t, api, testConnectorsGroup, "trust", testConnectorName, "--app", testAppID, "--env", testEnvironment)
		want := "runware serverless apps connectors attach " + testAppID + " " + testConnectorName + " --env " + testEnvironment
		if run.err == nil || !strings.Contains(run.err.Error(), want) || !strings.Contains(run.err.Error(), testAppID+" in "+testEnvironment) {
			t.Fatalf("err = %v, want it to contain %q", run.err, want)
		}
		if strings.Contains(run.cobraOutput, "aws iam") {
			t.Fatalf("a script was printed:\n%s", run.cobraOutput)
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
	t.Run("a hostile value in the attachment prints nothing", func(t *testing.T) {
		base := strings.Replace(trustGCPBase, testServiceAccount, "x';touch pwned;'@y.iam.gserviceaccount.com", 1)
		run := runTrust(t, trustGCPName, base)
		if run.err == nil || strings.Contains(run.cobraOutput, "gcloud") {
			t.Fatalf("err = %v, output = %q, want a refusal and no script", run.err, run.cobraOutput)
		}
	})
}

func TestConnectorsTrustUsesTheEnvironmentOfTheAttachment(t *testing.T) {
	var query map[string][]string
	api := func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		connectorAPI(t, &bodyRecorder{}, map[string]stubResponse{
			"GET " + testAppConnectors: {
				status: http.StatusOK,
				body:   attachedWith(trustAWSBase),
			},
		})(w, r)
	}
	run := runServerlessAgainst(t, api, testConnectorsGroup, "trust", testConnectorName, "--app", testAppID, "--environment", testEnvironment)
	if run.err != nil {
		t.Fatal(run.err)
	}
	if len(query["environment"]) != 1 || query["environment"][0] != testEnvironment {
		t.Fatalf("environment = %v, want [%s]", query["environment"], testEnvironment)
	}
	if !strings.Contains(run.cobraOutput, "environment "+testEnvironment) {
		t.Errorf("header does not name the environment:\n%s", run.cobraOutput)
	}
}
