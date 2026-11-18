package serverless

import "fmt"

// secretWaitMessage and connectorWaitMessage are the apps events messages the
// API records when a secret or OIDC connector change arrives while the
// application already has a rollout in progress.
const (
	secretWaitMessage    = "secret change accepted: rolls when the rollout in progress finishes"
	connectorWaitMessage = "OIDC connector change accepted: rolls when the rollout in progress finishes"
)

// rolloutInProgressHelp is the help paragraph shared by every command that
// changes a secret or an OIDC connector: secrets set, attach and detach, and
// connectors attach, detach and update. Only the first apps events message
// differs between them, so the caller passes it.
func rolloutInProgressHelp(waitMessage string) string {
	return fmt.Sprintf(`If an application already has a rollout in progress, it rolls again on its own
once that rollout finishes. 'runware serverless apps events' shows %q
right away, then "change made during the previous rollout: rolling to pick it
up" when the second rollout starts. If that rollout fails or leaves the
application not live, the application picks the change up on its next rollout.
If the second rollout cannot be deployed, 'runware serverless apps events --type
error' shows "credential change not applied" and the workers keep the previous
configuration until the application is redeployed.`, waitMessage)
}
