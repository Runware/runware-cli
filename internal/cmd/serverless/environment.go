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

// environmentChoice is the environment a command addresses, resolved once so
// that a prompt and the request after it cannot disagree.
type environmentChoice struct {
	// name is the environment the command addresses, or "" when it names
	// none: production for an app command, the whole organization for a report.
	name string
	// source is where a default name came from, or "" when --env named it.
	source string
	// shellDefault is the default environment in this shell, or "" for
	// production. A printed production command needs --env when it is set.
	shellDefault string
}

type environmentChoiceKey struct{}

// environmentFor returns the environment cmd addresses. The first call
// resolves it and stores it on cmd's context; later calls read only that.
func environmentFor(cmd *cobra.Command) environmentChoice {
	ctx := cmp.Or(cmd.Context(), context.Background())
	if choice, ok := ctx.Value(environmentChoiceKey{}).(environmentChoice); ok {
		return choice
	}
	choice := resolveEnvironment(cmd)
	cmd.SetContext(context.WithValue(ctx, environmentChoiceKey{}, choice))
	return choice
}

// commandEnvironment returns the environment cmd addresses, or "" when it
// names none. See environmentFor.
func commandEnvironment(cmd *cobra.Command) string {
	return environmentFor(cmd).name
}

// shellEnvironment returns the default environment in this shell, or "" for
// production. See environmentFor.
func shellEnvironment(cmd *cobra.Command) string {
	return environmentFor(cmd).shellDefault
}

// resolveEnvironment reads the environment cmd addresses: --env, else
// RUNWARE_ENV, else the config's environment. A production default counts as
// none. A command without --env, or whose --env only narrows an organization
// report, addresses no default.
func resolveEnvironment(cmd *cobra.Command) environmentChoice {
	shellDefault, source := config.GetEnvironment()
	if isProductionEnvironment(shellDefault) {
		shellDefault, source = "", ""
	}
	choice := environmentChoice{shellDefault: shellDefault}
	f := cmd.Flag(environmentFlag)
	if f == nil {
		return choice
	}
	v, ok := f.Value.(*environmentValue)
	if !ok {
		return choice
	}
	_, report := f.Annotations[reportsOrganization]
	if *v != "" || report {
		choice.name = string(*v)
		return choice
	}
	choice.name, choice.source = shellDefault, source
	return choice
}

// AnnounceEnvironment resolves the environment cmd addresses, checks a
// default one and logs it, so a default set elsewhere is never silent.
// The root command runs it before every command, ahead of any spinner.
func AnnounceEnvironment(cmd *cobra.Command, logger *log.Logger) error {
	choice := environmentFor(cmd)
	if choice.source == "" {
		return nil
	}
	if !config.EnvironmentNamePattern.MatchString(choice.name) {
		return fmt.Errorf("environment %q from %s: %w", choice.name, choice.source, config.ErrEnvironmentName)
	}
	logger.Info("Using environment", "environment", choice.name, "source", choice.source)
	return nil
}

// newServerlessClient builds the API client for cmd, scoped to the
// environment it addresses.
func newServerlessClient(cmd *cobra.Command, logger *log.Logger) *serverlessapi.Client {
	client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
	return client.WithEnvironment(commandEnvironment(cmd))
}

// deleteSubject is what a delete prompt names: the subject and the environment
// the command addresses.
func deleteSubject(cmd *cobra.Command, subject string) string {
	return subject + " in " + cmp.Or(commandEnvironment(cmd), config.ProductionEnvironment)
}

// environmentArgs is the --env part of a printed command: nothing for a
// command that names no environment, and nothing for production unless
// shellDefault, the default environment in this shell, would send the command
// elsewhere.
func environmentArgs(environment, shellDefault string) []string {
	if isProductionEnvironment(environment) && isProductionEnvironment(shellDefault) {
		return nil
	}
	return appendFlag(nil, "--"+environmentFlag, environment)
}

// isProductionEnvironment reports whether environment names production.
func isProductionEnvironment(environment string) bool {
	return environment == "" || environment == config.ProductionEnvironment
}

// serverlessCommand is the runware serverless command line that addresses
// the copy in environment, under shellDefault.
func serverlessCommand(environment, shellDefault string, args ...string) string {
	parts := append([]string{"runware", "serverless"}, args...)
	return strings.Join(append(parts, environmentArgs(environment, shellDefault)...), " ")
}

// appCommand is the runware serverless command line that addresses the app's
// copy, under shellDefault.
func appCommand(app *serverlessapi.App, shellDefault string, args ...string) string {
	return serverlessCommand(app.Environment, shellDefault, args...)
}

// cursorFlags prefixes a next-page hint's extra flags with --env for the
// environment the command addresses.
func cursorFlags(cmd *cobra.Command, extra string) string {
	choice := environmentFor(cmd)
	parts := environmentArgs(choice.name, choice.shellDefault)
	if extra != "" {
		parts = append(parts, extra)
	}
	return strings.Join(parts, " ")
}
