package serverless

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

const (
	flagApp   = "app"
	flagScope = "scope"

	// trustScopeApp trusts the tokens of one application.
	trustScopeApp = "app"
	// trustScopeEnvironment trusts the tokens of every application of the
	// organization in one environment.
	trustScopeEnvironment = "environment"

	// shellSafe is what a value may contain to be printed unquoted inside a
	// shell command or a heredoc. The identifiers a trust rule is built from
	// (uuids, app ids, resource names, ARNs) all fit; anything else is refused
	// rather than escaped, so no value can end a quote or start an expansion.
	shellSafe = `^[A-Za-z0-9:_./@+,-][A-Za-z0-9:_./@=+,-]*$`
)

var (
	shellSafePattern = regexp.MustCompile(shellSafe)
	// subjectPrefixPattern is the part of a subject up to the app segment:
	// worker:org:<organization uuid>:env:<environment uuid>:
	subjectPrefixPattern = regexp.MustCompile(`^worker:org:([A-Za-z0-9-]+):env:([A-Za-z0-9-]+):$`)
	// gcpProviderPattern is a workload identity provider resource name.
	gcpProviderPattern = regexp.MustCompile(`^(projects/[0-9]+/locations/global/workloadIdentityPools/[a-z0-9-]+)/providers/[a-z0-9-]+$`)
	// awsRolePattern is an IAM role ARN: partition, account, optional path, name.
	awsRolePattern = regexp.MustCompile(`^arn:(aws[a-z-]*):iam::([0-9]{12}):role/(.+)$`)
)

// trustScopes are the --scope values.
var trustScopes = []string{trustScopeApp, trustScopeEnvironment}

// trustInput is what the trust commands are built from.
type trustInput struct {
	// connector is the attachment, which carries the subject and subjectPattern
	// the API reports for the application's tokens.
	connector serverlessapi.AppOidcConnector
	// app, environment and scope describe the request, for the header comment.
	app         string
	environment string
	scope       string
}

// subjectPrefix returns the environment's subject prefix, which the attachment's
// subject and subjectPattern both start with.
func (in trustInput) subjectPrefix() (prefix, envID string, err error) {
	prefix, ok := strings.CutSuffix(in.connector.SubjectPattern, "*")
	if !ok {
		return "", "", fmt.Errorf("subjectPattern %q does not end in *", in.connector.SubjectPattern)
	}
	m := subjectPrefixPattern.FindStringSubmatch(prefix)
	if m == nil {
		return "", "", fmt.Errorf("subjectPattern %q is not worker:org:<organization>:env:<environment>:*", in.connector.SubjectPattern)
	}
	if !strings.HasPrefix(in.connector.Subject, prefix) {
		return "", "", fmt.Errorf("subject %q is outside subjectPattern %q", in.connector.Subject, in.connector.SubjectPattern)
	}
	return prefix, m[2], nil
}

// renderTrust returns the shell commands that make the cloud account of a
// connector trust the application's tokens. The output runs in a POSIX shell
// as printed; the one value it cannot know, the resource the workers may
// reach, is read from a variable and refused when unset.
func renderTrust(in trustInput) (string, error) {
	prefix, envID, err := in.subjectPrefix()
	if err != nil {
		return "", err
	}
	if in.connector.Issuer == nil || *in.connector.Issuer == "" {
		return "", errors.New("the connector has no issuer: this environment has no OIDC issuer")
	}
	issuer := *in.connector.Issuer
	// The subject pattern is already held to subjectPrefixPattern plus one *.
	// Everything else printed unquoted must be shell safe, and no value may
	// carry a * (a glob).
	checked := []string{in.connector.Subject, issuer}
	if in.connector.Kind == serverlessapi.OidcConnectorKindAws {
		// Only an aws audience is printed unquoted; a generic one is quoted.
		checked = append(checked, in.connector.Audience)
	}
	for _, v := range checked {
		if !shellSafePattern.MatchString(v) {
			return "", fmt.Errorf("value %q has characters that cannot be printed safely in a shell command", v)
		}
	}

	var b strings.Builder
	trusted := in.connector.Subject
	if in.scope == trustScopeEnvironment {
		trusted = in.connector.SubjectPattern
	}
	fmt.Fprintf(&b, "# Trust for connector %s (%s) in environment %s.\n", in.connector.Name, in.connector.Kind, in.environment)
	if in.scope == trustScopeEnvironment {
		fmt.Fprintf(&b, "# Scope environment: tokens of EVERY application of the organization in this environment (sub %s).\n", trusted)
	} else {
		fmt.Fprintf(&b, "# Scope app: tokens of application %s only (sub %s).\n", in.app, trusted)
	}

	switch in.connector.Kind {
	case serverlessapi.OidcConnectorKindAws:
		err = renderAWS(&b, in, issuer)
	case serverlessapi.OidcConnectorKindGcp:
		err = renderGCP(&b, in, issuer, prefix, envID)
	case serverlessapi.OidcConnectorKindGeneric:
		renderGeneric(&b, in, issuer, prefix)
	default:
		err = fmt.Errorf("unknown connector kind %q", in.connector.Kind)
	}
	if err != nil {
		return "", err
	}
	return b.String(), nil
}

// awsTrustPolicy is the trust policy document of the IAM role.
type awsTrustPolicy struct {
	Version   string              `json:"Version"`
	Statement []awsTrustStatement `json:"Statement"`
}

type awsTrustStatement struct {
	Effect    string                       `json:"Effect"`
	Principal awsTrustPrincipal            `json:"Principal"`
	Action    string                       `json:"Action"`
	Condition map[string]map[string]string `json:"Condition"`
}

type awsTrustPrincipal struct {
	Federated string `json:"Federated"`
}

func renderAWS(b *strings.Builder, in trustInput, issuer string) error {
	if in.connector.Aws == nil {
		return errors.New("the aws connector has no roleArn")
	}
	m := awsRolePattern.FindStringSubmatch(in.connector.Aws.RoleArn)
	if m == nil || !shellSafePattern.MatchString(in.connector.Aws.RoleArn) {
		return fmt.Errorf("roleArn %q is not an IAM role ARN", in.connector.Aws.RoleArn)
	}
	// The audience is a --client-id-list item: a comma would split it in two and a
	// leading dash would read as a flag.
	if strings.Contains(in.connector.Audience, ",") || strings.HasPrefix(in.connector.Audience, "-") {
		return fmt.Errorf("audience %q cannot be printed as an --client-id-list value", in.connector.Audience)
	}
	partition, account, rolePath := m[1], m[2], m[3]
	roleName := rolePath
	path := "/"
	if i := strings.LastIndex(rolePath, "/"); i >= 0 {
		roleName = rolePath[i+1:]
		path = "/" + rolePath[:i+1]
	}
	// The condition keys and the provider ARN name the issuer without its scheme.
	host, ok := strings.CutPrefix(issuer, "https://")
	if !ok {
		return fmt.Errorf("issuer %q does not start with https://", issuer)
	}

	subjectOperator, trusted := "StringEquals", in.connector.Subject
	if in.scope == trustScopeEnvironment {
		subjectOperator, trusted = "StringLike", in.connector.SubjectPattern
	}
	// Two operators cannot share one key, so aud and sub sit in separate blocks.
	condition := map[string]map[string]string{
		"StringEquals": {
			host + ":aud": in.connector.Audience,
		},
	}
	if subjectOperator == "StringEquals" {
		condition["StringEquals"][host+":sub"] = trusted
	} else {
		condition[subjectOperator] = map[string]string{
			host + ":sub": trusted,
		}
	}
	policy := awsTrustPolicy{
		Version: "2012-10-17",
		Statement: []awsTrustStatement{
			{
				Effect: "Allow",
				Principal: awsTrustPrincipal{
					Federated: fmt.Sprintf("arn:%s:iam::%s:oidc-provider/%s", partition, account, host),
				},
				Action:    "sts:AssumeRoleWithWebIdentity",
				Condition: condition,
			},
		},
	}
	var doc bytes.Buffer
	enc := json.NewEncoder(&doc)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(policy); err != nil {
		return err
	}

	b.WriteString("\n# 1. Register the Runware issuer once per AWS account. It fails with EntityAlreadyExists when\n")
	b.WriteString("#    the account already trusts the issuer; then add the audience to the existing provider instead:\n")
	fmt.Fprintf(b, "#    aws iam add-client-id-to-open-id-connect-provider --open-id-connect-provider-arn %s --client-id %s\n",
		policy.Statement[0].Principal.Federated, in.connector.Audience)
	fmt.Fprintf(b, "aws iam create-open-id-connect-provider --url %s --client-id-list %s\n", issuer, in.connector.Audience)

	b.WriteString("\n# 2. Create the role the connector names, trusting only the tokens above.\n")
	b.WriteString("#    Grant it permissions afterwards, for example with aws iam attach-role-policy.\n")
	b.WriteString("cat > trust-policy.json <<'EOF'\n")
	b.WriteString(doc.String())
	b.WriteString("EOF\n")
	fmt.Fprintf(b, "aws iam create-role --role-name %s", roleName)
	if path != "/" {
		fmt.Fprintf(b, " --path %s", path)
	}
	b.WriteString(" --assume-role-policy-document file://trust-policy.json\n")
	b.WriteString("#    If the role already exists, create-role fails. This command sets the role's trust policy to\n")
	b.WriteString("#    trust-policy.json: it REPLACES the existing trust policy and does not merge. If other principals\n")
	b.WriteString("#    rely on the role, add the statement above to its existing trust policy document instead.\n")
	fmt.Fprintf(b, "#    aws iam update-assume-role-policy --role-name %s --policy-document file://trust-policy.json\n", roleName)
	return nil
}

func renderGCP(b *strings.Builder, in trustInput, issuer, prefix, envID string) error {
	if in.connector.Gcp == nil {
		return errors.New("the gcp connector has no workloadIdentityProvider")
	}
	provider := in.connector.Gcp.WorkloadIdentityProvider
	m := gcpProviderPattern.FindStringSubmatch(provider)
	if m == nil {
		return fmt.Errorf("workloadIdentityProvider %q is not projects/<number>/locations/global/workloadIdentityPools/<pool>/providers/<provider>", provider)
	}
	pool := m[1]

	var member string
	if in.scope == trustScopeEnvironment {
		member = "principalSet://iam.googleapis.com/" + pool + "/attribute.environment_id/" + envID
	} else {
		member = "principal://iam.googleapis.com/" + pool + "/subject/" + in.connector.Subject
	}

	b.WriteString("\n# 1. Create the workload identity pool and the provider the connector names.\n")
	b.WriteString("#    The provider admits the tokens of this organization and environment only; the grant in step 2\n")
	b.WriteString("#    narrows it. If the provider already exists (ALREADY_EXISTS), for example from another environment,\n")
	b.WriteString("#    it still rejects this environment until its attribute condition admits it too. This command cannot\n")
	b.WriteString("#    read the existing condition; show it with:\n")
	fmt.Fprintf(b, "#      gcloud iam workload-identity-pools providers describe %s --format='value(attributeCondition)'\n", provider)
	b.WriteString("#    then set it to that condition OR-ed with this one:\n")
	fmt.Fprintf(b, "#      gcloud iam workload-identity-pools providers update-oidc %s --attribute-condition=\"(<existing condition>) || assertion.sub.startsWith('%s')\"\n", provider, prefix)
	fmt.Fprintf(b, "gcloud iam workload-identity-pools create %s --display-name='Runware Serverless'\n", pool)
	fmt.Fprintf(b, "gcloud iam workload-identity-pools providers create-oidc %s \\\n", provider)
	fmt.Fprintf(b, "  --issuer-uri=%s \\\n", issuer)
	b.WriteString("  --attribute-mapping='google.subject=assertion.sub,attribute.org_id=assertion.org_id,attribute.environment_id=assertion.environment_id,attribute.app_id=assertion.app_id' \\\n")
	fmt.Fprintf(b, "  --attribute-condition=\"assertion.sub.startsWith('%s')\"\n", prefix)

	if in.connector.Gcp.ServiceAccountEmail != nil && *in.connector.Gcp.ServiceAccountEmail != "" {
		sa := *in.connector.Gcp.ServiceAccountEmail
		if !shellSafePattern.MatchString(sa) {
			return fmt.Errorf("serviceAccountEmail %q cannot be printed safely in a shell command", sa)
		}
		b.WriteString("\n# 2. Let the workers impersonate the connector's service account.\n")
		b.WriteString("#    Grant the service account the access it needs on your resources separately.\n")
		fmt.Fprintf(b, "gcloud iam service-accounts add-iam-policy-binding %s \\\n", sa)
		b.WriteString("  --role=roles/iam.workloadIdentityUser \\\n")
		fmt.Fprintf(b, "  --member=%s\n", member)
		return nil
	}

	b.WriteString("\n# 2. Grant the workers' identity access to your resource directly (the connector has no service account).\n")
	b.WriteString("#    Set BUCKET to the bucket they may read first, for example: BUCKET=my-inputs\n")
	b.WriteString("#    To grant another role or resource, change this command.\n")
	b.WriteString(": \"${BUCKET:?set BUCKET to the name of the bucket the workers may read}\"\n")
	b.WriteString("gcloud storage buckets add-iam-policy-binding \"gs://${BUCKET}\" \\\n")
	b.WriteString("  --role=roles/storage.objectViewer \\\n")
	fmt.Fprintf(b, "  --member=%s\n", member)
	return nil
}

func renderGeneric(b *strings.Builder, in trustInput, issuer, prefix string) {
	b.WriteString("\n# What a verifier of these tokens checks. Also check the signature (RS256, keys from the issuer's\n")
	b.WriteString("# discovery document), exp and nbf.\n")
	fmt.Fprintf(b, "OIDC_ISS=%s\n", issuer)
	fmt.Fprintf(b, "OIDC_AUD=%s\n", shellQuote(in.connector.Audience))
	if in.scope == trustScopeEnvironment {
		b.WriteString("# sub must start with this prefix; it matches every application of the organization in this environment.\n")
		fmt.Fprintf(b, "OIDC_SUB_PREFIX=%s\n", prefix)
		fmt.Fprintf(b, "OIDC_SUB_PATTERN=%s\n", shellQuote(in.connector.SubjectPattern))
		return
	}
	b.WriteString("# sub must equal this value; it matches this application in this environment only.\n")
	fmt.Fprintf(b, "OIDC_SUB=%s\n", in.connector.Subject)
}

// shellQuote single-quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func newConnectorsTrustCmd(logger *log.Logger) *cobra.Command {
	var app, scope string
	cmd := &cobra.Command{
		Use:   "trust <name>",
		Short: "Print the cloud commands that make a provider trust an application's tokens",
		Long: `Print the commands that set up the cloud side of an attached OIDC connector, with
every value filled in from the connector and its attachment:

  aws      aws iam commands for the OIDC identity provider and the role, with the
           trust policy JSON
  gcp      gcloud commands for the workload identity pool and provider, with its
           attribute condition, and the IAM binding for the application's identity
  generic  the iss, aud and sub a verifier of your own checks

The output is shell and runs as printed. The one value it cannot know is the
bucket a gcp connector without a service account grants access to: set the
BUCKET variable first. Nothing is run or changed by this command.

The connector must be attached to the application in the environment (see
'apps connectors attach'), because the subject exists only then.

--scope app (the default) trusts the tokens of this application only.
--scope environment trusts every application of the organization in the
environment, so use it only when all of them may reach the resource. Tokens of
another environment are never trusted by either.`,
		Example: `  runware serverless connectors trust aws-prod --app my-app

  # every application of the staging environment
  runware serverless connectors trust aws-prod --app my-app --env staging --scope environment

  # run the printed commands
  BUCKET=my-inputs sh -c "$(runware serverless connectors trust gcp-prod --app my-app)"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !isTrustScope(scope) {
				return fmt.Errorf("invalid --%s %q: must be one of: %s", flagScope, scope, strings.Join(trustScopes, ", "))
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Reading the connectors attached to %s...", app))
			spin.Start()
			attached, err := newServerlessClient(cmd, logger).ListAppOidcConnectors(cmd.Context(), app)
			spin.Stop()
			if err != nil {
				return err
			}

			// The attachment carries the connector's kind, audience, provider block and
			// issuer as well as the subject, so the connector itself is not read.
			i := slices.IndexFunc(attached, func(c serverlessapi.AppOidcConnector) bool { return c.Name == name })
			if i < 0 {
				attach := serverlessCommand(commandEnvironment(cmd), defaultEnvironment(cmd), "apps", "connectors", "attach", app, name)
				return fmt.Errorf("connector %s is not attached to %s (or does not exist): attach it first with '%s'",
					name, deleteSubject(cmd, app), attach)
			}

			script, err := renderTrust(trustInput{
				connector:   attached[i],
				app:         app,
				environment: cmp.Or(commandEnvironment(cmd), serverlessapi.ProductionEnvironment),
				scope:       scope,
			})
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), script)
			return err
		},
	}
	cmd.Flags().StringVar(&app, flagApp, "", "Application whose tokens to trust")
	cmd.Flags().StringVar(&scope, flagScope, trustScopeApp, "Whose tokens to trust: app (this application) or environment (every application of the organization in it)")
	if err := cmd.MarkFlagRequired(flagApp); err != nil {
		panic(err)
	}
	addEnvironmentFlag(cmd, cmd.Flags(), logger, "Environment the application runs in "+environmentFlagDefault)
	return cmd
}

func isTrustScope(s string) bool {
	return slices.Contains(trustScopes, s)
}
