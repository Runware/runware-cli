package serverless

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"regexp"
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
	environmentFlagHelp  = "Environment the app runs in (default production)"
	// completionTimeout bounds the environments read behind tab completion;
	// the shell waits for it.
	completionTimeout = 3 * time.Second
)

// environmentNamePattern is the API's EnvironmentName rule.
var environmentNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

var (
	errEnvironmentIsVariable = errors.New("--env takes an environment name; use --env-var KEY=VALUE for an environment variable")
	errEnvironmentName       = errors.New("environment name must be 1-63 lowercase letters, digits, hyphens, underscores or periods, starting with a letter or digit")
)

// environmentValue is the --env flag value.
type environmentValue string

func (v *environmentValue) String() string {
	return string(*v)
}

func (v *environmentValue) Set(name string) error {
	if strings.Contains(name, "=") {
		return errEnvironmentIsVariable
	}
	if !environmentNamePattern.MatchString(name) {
		return errEnvironmentName
	}
	*v = environmentValue(name)
	return nil
}

func (v *environmentValue) Type() string {
	return "name"
}

// addEnvironmentFlag registers --env on one of cmd's flag sets.
func addEnvironmentFlag(cmd *cobra.Command, flags *pflag.FlagSet, logger *log.Logger) {
	flags.VarP(new(environmentValue), environmentFlag, environmentShorthand, environmentFlagHelp)
	_ = cmd.RegisterFlagCompletionFunc(environmentFlag, environmentCompletion(logger, completionTimeout))
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

// commandEnvironment returns the --env value, or "" when the command has no
// such flag or it was not set.
func commandEnvironment(cmd *cobra.Command) string {
	if f := cmd.Flag(environmentFlag); f != nil {
		if v, ok := f.Value.(*environmentValue); ok {
			return string(*v)
		}
	}
	return ""
}

// newServerlessClient builds the API client for cmd, scoped to its --env.
func newServerlessClient(cmd *cobra.Command, logger *log.Logger) *serverlessapi.Client {
	client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
	return client.WithEnvironment(commandEnvironment(cmd))
}

// deleteSubject is what a delete prompt names: the subject and the environment
// the command addresses, production when --env is not set.
func deleteSubject(cmd *cobra.Command, subject string) string {
	return environmentSubject(subject, cmp.Or(commandEnvironment(cmd), defaultEnvironment))
}

// environmentSubject appends " in <environment>" to a confirmation prompt's
// subject; an empty environment adds nothing.
func environmentSubject(subject, environment string) string {
	if environment == "" {
		return subject
	}
	return subject + " in " + environment
}

// environmentArgs is the --env part of a printed command: nothing for the
// default environment, which a command without the flag already addresses.
func environmentArgs(environment string) []string {
	if environment == "" || environment == defaultEnvironment {
		return nil
	}
	return appendFlag(nil, "--"+environmentFlag, environment)
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
