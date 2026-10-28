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
	// whole organization, which the default environment does not narrow. Its
	// values name the flags that make the report read one app's copy instead.
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
	if !serverlessapi.EnvironmentNamePattern.MatchString(name) {
		return serverlessapi.ErrEnvironmentName
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
// When one of appFlags names an app, the command reads that app's copy and
// the default applies as it does for an app command.
func addReportEnvironmentFlag(cmd *cobra.Command, flags *pflag.FlagSet, logger *log.Logger, help string, appFlags ...string) {
	addEnvironmentFlag(cmd, flags, logger, help)
	if err := flags.SetAnnotation(environmentFlag, reportsOrganization, appFlags); err != nil {
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

// environmentChoice is the environment a command addresses.
type environmentChoice struct {
	// name is the environment the command addresses, or "" when it names
	// none: production for an app command, the whole organization for a report.
	name string
	// source is where a default name came from, or "" when --env named it.
	source string
	// defaultEnvironment is the default environment, from RUNWARE_ENV or the
	// config, or "" for production. A printed production command needs --env
	// when it is set.
	defaultEnvironment string
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

// defaultEnvironment returns the default environment, from RUNWARE_ENV or the
// config, or "" for production. See environmentFor.
func defaultEnvironment(cmd *cobra.Command) string {
	return environmentFor(cmd).defaultEnvironment
}

// resolveEnvironment reads the environment cmd addresses: --env, else
// RUNWARE_ENV, else the config's environment. A production default counts as
// none. A command that has no --env flag, or whose --env only narrows an
// organization report that names no app, addresses no default.
func resolveEnvironment(cmd *cobra.Command) environmentChoice {
	defaultEnvironment, source := config.GetEnvironment()
	if isProductionEnvironment(defaultEnvironment) {
		defaultEnvironment, source = "", ""
	}
	choice := environmentChoice{defaultEnvironment: defaultEnvironment}
	f := cmd.Flag(environmentFlag)
	if f == nil {
		return choice
	}
	v, ok := f.Value.(*environmentValue)
	if !ok {
		return choice
	}
	appFlags, report := f.Annotations[reportsOrganization]
	orgReport := report && !namesApp(cmd, appFlags)
	if *v != "" || orgReport {
		choice.name = string(*v)
		return choice
	}
	choice.name, choice.source = defaultEnvironment, source
	return choice
}

// namesApp reports whether any of appFlags names an app on cmd.
func namesApp(cmd *cobra.Command, appFlags []string) bool {
	for _, name := range appFlags {
		if f := cmd.Flag(name); f != nil && strings.TrimSpace(f.Value.String()) != "" {
			return true
		}
	}
	return false
}

// AnnounceEnvironment resolves the environment cmd addresses, checks a
// default one and logs it, so a default set elsewhere is never silent.
// The root command runs it before every command, ahead of any spinner.
func AnnounceEnvironment(cmd *cobra.Command, logger *log.Logger) error {
	choice := environmentFor(cmd)
	if choice.source == "" {
		return nil
	}
	if !serverlessapi.EnvironmentNamePattern.MatchString(choice.name) {
		return fmt.Errorf("environment %q from %s: %w", choice.name, choice.source, serverlessapi.ErrEnvironmentName)
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
// defaultEnvironment would send the command elsewhere.
func environmentArgs(environment, defaultEnvironment string) []string {
	if isProductionEnvironment(environment) && isProductionEnvironment(defaultEnvironment) {
		return nil
	}
	return appendFlag(nil, "--"+environmentFlag, environment)
}

// isProductionEnvironment reports whether environment names production.
func isProductionEnvironment(environment string) bool {
	return environment == "" || environment == config.ProductionEnvironment
}

// serverlessCommand is the runware serverless command line that addresses
// the copy in environment, under defaultEnvironment.
func serverlessCommand(environment, defaultEnvironment string, args ...string) string {
	parts := append([]string{"runware", "serverless"}, args...)
	return strings.Join(append(parts, environmentArgs(environment, defaultEnvironment)...), " ")
}

// appCommand is the runware serverless command line that addresses the app's
// copy, under defaultEnvironment.
func appCommand(app *serverlessapi.App, defaultEnvironment string, args ...string) string {
	return serverlessCommand(app.Environment, defaultEnvironment, args...)
}

// cursorFlags prefixes a next-page hint's extra flags with --env for the
// environment the command addresses.
func cursorFlags(cmd *cobra.Command, extra string) string {
	choice := environmentFor(cmd)
	parts := environmentArgs(choice.name, choice.defaultEnvironment)
	if extra != "" {
		parts = append(parts, extra)
	}
	return strings.Join(parts, " ")
}
