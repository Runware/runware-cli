package serverless

import "github.com/spf13/cobra"

// newAppsCmd returns the "serverless apps" command group for managing
// deployed serverless applications (API: deployments).
func newAppsCmd() *cobra.Command {
	cmd := stubGroup("apps", "Manage deployed serverless applications")
	cmd.Long = "Manage deployed serverless applications (deployments) on the Runware platform."
	cmd.AddCommand(
		newAppsListCmd(),
		newAppsShowCmd(),
		newAppsEndpointsCmd(),
		newAppsVersionsCmd(),
		newAppsLogsCmd(),
		newAppsWorkersCmd(),
		newAppsScaleCmd(),
		newAppsUsageCmd(),
		newAppsSecretCmd(),
		newAppsStopCmd(),
		newAppsResumeCmd(),
		newAppsDeleteCmd(),
	)
	return cmd
}

func newAppsListCmd() *cobra.Command {
	return stubLeaf(
		"list",
		"List serverless applications",
		`  # list all serverless applications
  runware serverless apps list`,
		cobra.NoArgs,
	)
}

func newAppsShowCmd() *cobra.Command {
	return stubLeaf(
		"show <name>",
		"Show details for a serverless application",
		`  # show details for an application
  runware serverless apps show my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsEndpointsCmd() *cobra.Command {
	return stubLeaf(
		"endpoints <name>",
		"List endpoints for a serverless application",
		`  # list endpoints for an application
  runware serverless apps endpoints my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsVersionsCmd() *cobra.Command {
	return stubLeaf(
		"versions <name>",
		"List versions of a serverless application",
		`  # list deployed versions
  runware serverless apps versions my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsLogsCmd() *cobra.Command {
	return stubLeaf(
		"logs <name>",
		"Show logs for a serverless application",
		`  # fetch or stream logs for an application
  runware serverless apps logs my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsWorkersCmd() *cobra.Command {
	return stubLeaf(
		"workers <name>",
		"List workers for a serverless application",
		`  # list workers for an application
  runware serverless apps workers my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsScaleCmd() *cobra.Command {
	return stubLeaf(
		"scale <name>",
		"Scale a serverless application",
		`  # update scaling configuration for an application
  runware serverless apps scale my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsUsageCmd() *cobra.Command {
	return stubLeaf(
		"usage <name>",
		"Show usage for a serverless application",
		`  # show usage events for an application
  runware serverless apps usage my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsStopCmd() *cobra.Command {
	return stubLeaf(
		"stop <name>",
		"Stop a serverless application",
		`  # stop a running application
  runware serverless apps stop my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsResumeCmd() *cobra.Command {
	return stubLeaf(
		"resume <name>",
		"Resume a stopped serverless application",
		`  # resume a stopped application
  runware serverless apps resume my-app`,
		cobra.ExactArgs(1),
	)
}

func newAppsDeleteCmd() *cobra.Command {
	return stubLeaf(
		"delete <name>",
		"Delete a serverless application",
		`  # delete an application
  runware serverless apps delete my-app`,
		cobra.ExactArgs(1),
	)
}
