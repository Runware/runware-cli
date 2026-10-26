package serverless

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	// defaultEnvironment is the environment the API addresses when a request names none.
	defaultEnvironment   = "production"
	environmentFlag      = "env"
	environmentShorthand = "e"
	environmentFlagHelp  = "Environment the app runs in " + environmentFlagDefault
	// environmentFlagDefault is the --env help's account of a command without
	// the flag.
	environmentFlagDefault = "(default RUNWARE_ENV, else the config's environment, else production)"
	// reportsOrganization marks an --env that only narrows a report of the
	// whole organization, which the default environment does not narrow.
	reportsOrganization = "runware.reportsOrganization"
	// completionTimeout bounds the environments read behind tab completion;
	// the shell waits for it.
	completionTimeout = 3 * time.Second
)

var errEnvironmentIsVariable = errors.New("--env takes an environment name; use --env-var KEY=VALUE for an environment variable")

// environmentValue is the --env flag value.
type environmentValue string

func (v *environmentValue) String() string {
	return string(*v)
}

func (v *environmentValue) Set(name string) error {
	if strings.Contains(name, "=") {
		return errEnvironmentIsVariable
	}
	if !config.EnvironmentNamePattern.MatchString(name) {
		return config.ErrEnvironmentName
	}
	*v = environmentValue(name)
	return nil
}

func (v *environmentValue) Type() string {
	return "name"
}

// addEnvironmentFlag registers --env on one of cmd's flag sets with the given
// help.
func addEnvironmentFlag(cmd *cobra.Command, flags *pflag.FlagSet, logger *log.Logger, help string) {
	flags.VarP(new(environmentValue), environmentFlag, environmentShorthand, help)
	if err := cmd.RegisterFlagCompletionFunc(environmentFlag, environmentCompletion(logger, completionTimeout)); err != nil {
		panic(err)
	}
}

// addReportEnvironmentFlag registers --env on a command that reports the
// whole organization without it, so the default environment does not apply.
func addReportEnvironmentFlag(cmd *cobra.Command, flags *pflag.FlagSet, logger *log.Logger, help string) {
	addEnvironmentFlag(cmd, flags, logger, help)
	if err := flags.SetAnnotation(environmentFlag, reportsOrganization, nil); err != nil {
		panic(err)
	}
}

// environmentCompletion completes --env from the organization's environments,
// giving up after timeout with no names.
func environmentCompletion(logger *log.Logger, timeout time.Duration) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
		defer cancel()
		envs, err := newServerlessClient(cmd, logger).ListEnvironments(ctx)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		names := make([]cobra.Completion, len(envs))
		for i := range envs {
			names[i] = envs[i].Name
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}

// commandEnvironment returns the environment cmd addresses, or "" for
// production. See resolveEnvironment.
func commandEnvironment(cmd *cobra.Command) string {
	name, _ := resolveEnvironment(cmd)
	return name
}

// resolveEnvironment returns the environment cmd addresses and, when --env
// did not name it, where the default came from: --env, else RUNWARE_ENV, else
// the config's environment. A command without --env, or whose --env only
// narrows an organization report, addresses no default.
func resolveEnvironment(cmd *cobra.Command) (name, source string) {
	f := cmd.Flag(environmentFlag)
	if f == nil {
		return "", ""
	}
	v, ok := f.Value.(*environmentValue)
	if !ok {
		return "", ""
	}
	if _, report := f.Annotations[reportsOrganization]; *v != "" || report {
		return string(*v), ""
	}
	return config.GetEnvironment()
}

// newServerlessClient builds the API client for cmd, scoped to the
// environment it addresses. An environment other than production that --env
// did not name is announced, so a default set elsewhere is never silent.
func newServerlessClient(cmd *cobra.Command, logger *log.Logger) *serverlessapi.Client {
	name, source := resolveEnvironment(cmd)
	if source != "" && !isDefaultEnvironment(name) {
		logger.Info(fmt.Sprintf("Using environment %s from %s", name, source))
	}
	client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
	return client.WithEnvironment(name)
}

// deleteSubject is what a delete prompt names: the subject and the environment
// the command addresses.
func deleteSubject(cmd *cobra.Command, subject string) string {
	return subject + " in " + cmp.Or(commandEnvironment(cmd), defaultEnvironment)
}

// environmentArgs is the --env part of a printed command: nothing for
// production, which the command addresses without the flag wherever it runs.
func environmentArgs(environment string) []string {
	if isDefaultEnvironment(environment) {
		return nil
	}
	return appendFlag(nil, "--"+environmentFlag, environment)
}

// isDefaultEnvironment reports whether environment is production, the one the
// API addresses when a request names none.
func isDefaultEnvironment(environment string) bool {
	return environment == "" || environment == defaultEnvironment
}

// serverlessCommand is the runware serverless command line that addresses
// the copy in environment.
func serverlessCommand(environment string, args ...string) string {
	parts := append([]string{"runware", "serverless"}, args...)
	return strings.Join(append(parts, environmentArgs(environment)...), " ")
}

// appCommand is the runware serverless command line that addresses the app's copy.
func appCommand(app *serverlessapi.App, args ...string) string {
	return serverlessCommand(app.Environment, args...)
}

// cursorFlags prefixes a next-page hint's extra flags with the command's --env.
func cursorFlags(cmd *cobra.Command, extra string) string {
	parts := environmentArgs(commandEnvironment(cmd))
	if extra != "" {
		parts = append(parts, extra)
	}
	return strings.Join(parts, " ")
}
