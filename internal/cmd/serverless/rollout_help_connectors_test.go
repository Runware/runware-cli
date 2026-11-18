package serverless

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestConnectorsHelpExplainsAChangeDuringARollout(t *testing.T) {
	t.Parallel()

	commands := map[string]*cobra.Command{
		"apps connectors attach": newAppsConnectorsAttachCmd(nil),
		"apps connectors detach": newAppsConnectorsDetachCmd(nil),
		"connectors update":      newConnectorsUpdateCmd(nil),
	}
	for name, cmd := range commands {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			long := oneLine(cmd.Long)
			for _, want := range []string{
				testConnectorWait,
				testPendingRollMessage,
				"credential change not applied",
				"If that rollout fails or leaves the application not live",
				"An application that is not live",
			} {
				if !strings.Contains(long, want) {
					t.Errorf("%s help does not contain %q", name, want)
				}
			}
			if strings.Contains(long, "already has a rollout in progress, picks") {
				t.Errorf("%s help still says an application with a rollout in progress waits for its next rollout", name)
			}
		})
	}
}
