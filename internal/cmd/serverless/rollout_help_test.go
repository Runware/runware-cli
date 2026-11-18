package serverless

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	testPendingRollMessage = "change made during the previous rollout: rolling to pick it up"
	testSecretWait         = "secret change accepted: rolls when the rollout in progress finishes"
	testConnectorWait      = "OIDC connector change accepted: rolls when the rollout in progress finishes"
)

// oneLine collapses the line wrapping of help and spec text, so a quoted event
// message still matches when it is wrapped across lines.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func TestSecretsHelpExplainsAChangeDuringARollout(t *testing.T) {
	t.Parallel()

	commands := map[string]*cobra.Command{
		"secrets set":    newSecretsSetCmd(nil),
		"secrets attach": newSecretsAttachCmd(nil),
		"secrets detach": newSecretsDetachCmd(nil),
	}
	for name, cmd := range commands {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			long := oneLine(cmd.Long)
			for _, want := range []string{testSecretWait, testPendingRollMessage, "credential change not applied", "apps events --type"} {
				if !strings.Contains(long, want) {
					t.Errorf("%s help does not contain %q", name, want)
				}
			}
			if strings.Contains(long, "If a rollout is already in progress") {
				t.Errorf("%s help still has the old sentence that a change waits for the next deploy", name)
			}
		})
	}
}

func TestRolloutInProgressHelpQuotesTheWaitMessageItIsGiven(t *testing.T) {
	t.Parallel()

	got := oneLine(rolloutInProgressHelp(testConnectorWait))
	if !strings.Contains(got, `"`+testConnectorWait+`"`) {
		t.Errorf("help does not quote the wait message it was given:\n%s", got)
	}
	if strings.Contains(got, testSecretWait) {
		t.Errorf("help quotes the secret wait message for a connector:\n%s", got)
	}
}

// The help quotes events by their exact text, so the published spec must still
// list them. A spec sync that rewords one of them fails here.
func TestAppEventSpecListsTheMessagesTheHelpQuotes(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../../api/serverless/openapi.yaml")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Description string `yaml:"description"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	description := oneLine(spec.Components.Schemas["AppEvent"].Description)
	if description == "" {
		t.Fatal("AppEvent has no description in the spec")
	}
	for _, want := range []string{testSecretWait, testConnectorWait, testPendingRollMessage} {
		if !strings.Contains(description, want) {
			t.Errorf("AppEvent description does not contain %q", want)
		}
	}
}
