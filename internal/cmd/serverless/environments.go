package serverless

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/oapi-codegen/nullable"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The flags of the environments commands. A *Field flag is a setting, also
// named as the --clear value that clears it.
const (
	maxWorkersField    = "max-workers"
	maxGPUsField       = "max-gpus"
	budgetField        = "budget"
	alertPercentsField = "alert-percents"
	pauseAtBudgetFlag  = "pause-at-budget"
	clearFlag          = "clear"
)

// moneyAmountPattern is the API's MoneyAmount amount rule.
var moneyAmountPattern = regexp.MustCompile(`^\d+\.\d+$`)

// wholeAmountPattern is a budget written without decimals, which the CLI
// sends with two.
var wholeAmountPattern = regexp.MustCompile(`^\d+$`)

var (
	errBudgetAmount           = errors.New("--budget takes a USD amount such as 50 or 49.99")
	errNothingToUpdate        = errors.New("nothing to update; pass a setting flag or --clear <field>")
	errNothingToSetOnDefaults = errors.New("nothing to set; pass --max-workers, --max-gpus or --clear <field>")
)

// updateClearFields are the fields environments update clears.
var updateClearFields = []string{
	maxWorkersField,
	maxGPUsField,
	budgetField,
	alertPercentsField,
}

// defaultsFields are the fields environments defaults set changes or clears.
var defaultsFields = []string{
	maxWorkersField,
	maxGPUsField,
}

// environmentSettingFlags are the setting flags of environments create and
// update.
var environmentSettingFlags = []string{
	maxWorkersField,
	maxGPUsField,
	budgetField,
	alertPercentsField,
	pauseAtBudgetFlag,
}

// environmentSettings are the setting flags of environments create and update.
type environmentSettings struct {
	maxWorkers    int32
	maxGPUs       int32
	budget        string
	alertPercents []int
	pauseAtBudget bool
}

// defaultsSettings are the setting flags of environments defaults set.
type defaultsSettings struct {
	maxWorkers int32
	maxGPUs    int32
}

// newEnvironmentsCmd returns the "serverless environments" command group for
// the organization's environments and the defaults they inherit.
func newEnvironmentsCmd(logger *log.Logger) *cobra.Command {
	cmd := stubGroup("environments", "Manage the organization's environments")
	cmd.Long = `Manage the organization's environments and the defaults they inherit.

An environment holds one copy of each app deployed to it, and its own secrets.
production is the default environment: a command without --env addresses it.

An environment can cap the workers and GPUs it holds, set a monthly budget with
alert thresholds, and pause its workers once the budget is reached. A cap it
does not set is inherited from the organization defaults (see
'environments defaults').`
	cmd.AddCommand(
		newEnvironmentsListCmd(logger),
		newEnvironmentsShowCmd(logger),
		newEnvironmentsCreateCmd(logger),
		newEnvironmentsUpdateCmd(logger),
		newEnvironmentsDeleteCmd(logger),
		newEnvironmentsDefaultsCmd(logger),
	)
	return cmd
}

func newEnvironmentsListCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List environments",
		Long: `List the organization's environments, production first.

A cap the environment does not set shows the organization default it inherits,
marked (org default); - means no cap.`,
		Example: `  runware serverless environments list`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			spin := cmdutil.NewSpinner("Fetching environments...")
			spin.Start()

			envs, err := newServerlessClient(cmd, logger).ListEnvironments(cmd.Context())
			spin.Stop()
			if err != nil {
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), environmentsResult(envs))
		},
	}
}

func newEnvironmentsShowCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show an environment",
		Long: `Show one environment's settings.

A cap the environment does not set shows the organization default it inherits,
marked (org default); - means no cap.`,
		Example:           `  runware serverless environments show staging`,
		Args:              environmentNameArg,
		ValidArgsFunction: environmentArgCompletion(logger),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching environment %s...", name))
			spin.Start()

			env, err := newServerlessClient(cmd, logger).GetEnvironment(cmd.Context(), name)
			spin.Stop()
			if err != nil {
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), environmentResult(*env))
		},
	}
}

func newEnvironmentsCreateCmd(logger *log.Logger) *cobra.Command {
	var settings environmentSettings

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create an environment",
		Long: `Create an environment in the organization.

A cap left unset inherits the organization default. A budget left unset means
no budget. Budget alerts default to 50, 80 and 100 percent of the budget, and
--pause-at-budget defaults to off.

Deploy to the new environment with --env <name> on deploy and app commands.`,
		Example: `  # create an environment that inherits every default
  runware serverless environments create staging

  # cap it at 4 workers and a 50 USD monthly budget, pausing at the budget
  runware serverless environments create staging --max-workers 4 --budget 50 --pause-at-budget

  # alert at 25 and 75 percent of the budget
  runware serverless environments create staging --budget 100.00 --alert-percents 25,75`,
		Args: environmentNameArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := settings.createBody(args[0], cmd.Flags())
			if err != nil {
				return err
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Creating environment %s...", body.Name))
			spin.Start()

			env, err := newServerlessClient(cmd, logger).CreateEnvironment(cmd.Context(), body)
			spin.Stop()
			if err != nil {
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), environmentResult(*env))
		},
	}

	addEnvironmentSettingFlags(cmd.Flags(), &settings)
	return cmd
}

func newEnvironmentsUpdateCmd(logger *log.Logger) *cobra.Command {
	var (
		settings environmentSettings
		clears   []string
	)

	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update an environment's settings",
		Long: `Change the settings the flags name and leave the rest as they are.

--clear <field> clears a setting, and takes one of max-workers, max-gpus,
budget and alert-percents; repeat it or separate fields with commas. A cleared
cap inherits the organization default again, a cleared budget means no budget,
and cleared alert percents send no budget alert.`,
		Example: `  # raise the worker cap
  runware serverless environments update staging --max-workers 8

  # stop pausing at the budget
  runware serverless environments update staging --pause-at-budget=false

  # inherit the organization's worker cap again and remove the budget
  runware serverless environments update staging --clear max-workers,budget`,
		Args:              environmentNameArg,
		ValidArgsFunction: environmentArgCompletion(logger),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			body, err := settings.updateBody(cmd.Flags(), clears)
			if err != nil {
				return err
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Updating environment %s...", name))
			spin.Start()

			env, err := newServerlessClient(cmd, logger).UpdateEnvironment(cmd.Context(), name, body)
			spin.Stop()
			if err != nil {
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), environmentResult(*env))
		},
	}

	addEnvironmentSettingFlags(cmd.Flags(), &settings)
	addClearFlag(cmd, &clears, updateClearFields)
	return cmd
}

func newEnvironmentsDeleteCmd(logger *log.Logger) *cobra.Command {
	var (
		yes   bool
		force bool
	)

	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an environment",
		Long: `Delete an environment that has no apps and no secrets left.

The API refuses an environment that still holds an app or a secret; delete
those first with 'apps delete' and 'secrets remove' under the same --env.
production cannot be deleted. The name becomes free for a new environment.

Confirmation is required unless --yes or --force is passed.`,
		Example: `  # delete an environment (prompts for confirmation)
  runware serverless environments delete staging

  # skip the confirmation prompt
  runware serverless environments delete staging --yes`,
		Args:              environmentNameArg,
		ValidArgsFunction: environmentArgCompletion(logger),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := confirmDelete("environment "+name, yes || force, cmd.InOrStdin(), cmd.ErrOrStderr(), stdinIsTerminal(cmd.InOrStdin()), config.GetAPIKey()); err != nil {
				return err
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Deleting environment %s...", name))
			spin.Start()

			err := newServerlessClient(cmd, logger).DeleteEnvironment(cmd.Context(), name)
			spin.Stop()
			if err != nil {
				return withEnvironmentContentsHint(err, name)
			}

			return output.Print(cmdutil.FormatFor(cmd), environmentDeletedResult{
				Name: name,
			})
		},
	}

	addDeleteConfirmFlags(cmd, &yes, &force)
	return cmd
}

func newEnvironmentsDefaultsCmd(logger *log.Logger) *cobra.Command {
	cmd := stubGroup("defaults", "Manage the organization defaults environments inherit")
	cmd.Long = `Manage the organization defaults: the worker and GPU caps an environment
inherits when it does not set its own.`
	cmd.AddCommand(
		newEnvironmentsDefaultsShowCmd(logger),
		newEnvironmentsDefaultsSetCmd(logger),
	)
	return cmd
}

func newEnvironmentsDefaultsShowCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the organization defaults",
		Long: `Show the worker and GPU caps an environment inherits when it does not set
its own; - means no cap.`,
		Example: `  runware serverless environments defaults show`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			spin := cmdutil.NewSpinner("Fetching organization defaults...")
			spin.Start()

			defaults, err := newServerlessClient(cmd, logger).GetEnvironmentDefaults(cmd.Context())
			spin.Stop()
			if err != nil {
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), environmentDefaultsResult(*defaults))
		},
	}
}

func newEnvironmentsDefaultsSetCmd(logger *log.Logger) *cobra.Command {
	var (
		settings defaultsSettings
		clears   []string
	)

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Change the organization defaults",
		Long: `Change the organization defaults the flags name and keep the rest as they are.

--clear <field> removes a default cap, and takes max-workers or max-gpus;
repeat it or separate fields with commas. The change reaches every environment
that inherits the cap at once; it does not restart running apps.

The API replaces the defaults as a whole, so this reads them and writes them
back with the change. Do not run it while anyone else changes the defaults, in
the CLI or the console: a change made between the read and the write is lost.`,
		Example: `  # cap every environment that sets no cap of its own at 10 workers
  runware serverless environments defaults set --max-workers 10

  # remove the default GPU cap
  runware serverless environments defaults set --clear max-gpus`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cleared, err := clearedFields(cmd.Flags(), clears, defaultsFields)
			if err != nil {
				return err
			}
			if len(cleared) == 0 && !slices.ContainsFunc(defaultsFields, cmd.Flags().Changed) {
				return errNothingToSetOnDefaults
			}

			spin := cmdutil.NewSpinner("Saving organization defaults...")
			spin.Start()

			client := newServerlessClient(cmd, logger)
			current, err := client.GetEnvironmentDefaults(cmd.Context())
			if err != nil {
				spin.Stop()
				return err
			}
			body := settings.applyTo(*current, cmd.Flags(), cleared)
			saved, err := client.PutEnvironmentDefaults(cmd.Context(), body)
			spin.Stop()
			if err != nil {
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), environmentDefaultsResult(*saved))
		},
	}

	cmd.Flags().Int32Var(&settings.maxWorkers, maxWorkersField, 0, "Worker cap of an environment that sets none of its own")
	cmd.Flags().Int32Var(&settings.maxGPUs, maxGPUsField, 0, "GPU cap of an environment that sets none of its own")
	addClearFlag(cmd, &clears, defaultsFields)
	return cmd
}

func addEnvironmentSettingFlags(flags *pflag.FlagSet, s *environmentSettings) {
	flags.Int32Var(&s.maxWorkers, maxWorkersField, 0, "Cap on the workers the environment holds")
	flags.Int32Var(&s.maxGPUs, maxGPUsField, 0, "Cap on the GPUs the environment holds")
	flags.StringVar(&s.budget, budgetField, "", "Monthly budget in USD, e.g. 50 or 49.99")
	flags.IntSliceVar(&s.alertPercents, alertPercentsField, nil, "Percentages of the budget that send an alert email, e.g. 50,80,100")
	flags.BoolVar(&s.pauseAtBudget, pauseAtBudgetFlag, false, "Scale the environment's workers to zero once the budget is reached")
}

// addClearFlag registers --clear, completing the fields it takes.
func addClearFlag(cmd *cobra.Command, clears *[]string, fields []string) {
	cmd.Flags().StringSliceVar(clears, clearFlag, nil, "Clear a setting: "+strings.Join(fields, ", ")+" (repeatable)")
	if err := cmd.RegisterFlagCompletionFunc(clearFlag, cobra.FixedCompletions(fields, cobra.ShellCompDirectiveNoFileComp)); err != nil {
		panic(err)
	}
}

// environmentNameArg takes one argument, an environment name the API accepts.
func environmentNameArg(cmd *cobra.Command, args []string) error {
	if err := cobra.ExactArgs(1)(cmd, args); err != nil {
		return err
	}
	if !environmentNamePattern.MatchString(args[0]) {
		return errEnvironmentName
	}
	return nil
}

// environmentArgCompletion completes a command's one environment name
// argument from the organization's environments.
func environmentArgCompletion(logger *log.Logger) cobra.CompletionFunc {
	complete := environmentCompletion(logger, completionTimeout)
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return complete(cmd, args, toComplete)
	}
}

// clearedFields returns the --clear fields, refusing one the command does not
// take and one whose own flag is also set.
func clearedFields(flags *pflag.FlagSet, values, valid []string) (map[string]struct{}, error) {
	cleared := make(map[string]struct{}, len(values))
	for _, v := range values {
		field := strings.TrimSpace(v)
		if !slices.Contains(valid, field) {
			return nil, fmt.Errorf("--clear takes %s; got %q", strings.Join(valid, ", "), field)
		}
		if flags.Changed(field) {
			return nil, fmt.Errorf("--%s and --clear %s both change %s; pass one", field, field, field)
		}
		cleared[field] = struct{}{}
	}
	return cleared, nil
}

// budgetAmount is the USD MoneyAmount a --budget value sends.
func budgetAmount(value string) (serverlessapi.MoneyAmount, error) {
	amount := strings.TrimSpace(value)
	if wholeAmountPattern.MatchString(amount) {
		amount += ".00"
	}
	if !moneyAmountPattern.MatchString(amount) {
		return serverlessapi.MoneyAmount{}, errBudgetAmount
	}
	return serverlessapi.MoneyAmount{
		Amount:   amount,
		Currency: serverlessapi.CurrencyUSD,
	}, nil
}

// createBody is the create request for the flags set on the command.
func (s environmentSettings) createBody(name string, flags *pflag.FlagSet) (serverlessapi.EnvironmentCreate, error) {
	body := serverlessapi.EnvironmentCreate{
		Name: name,
	}
	if flags.Changed(maxWorkersField) {
		body.MaxConcurrentWorkers = &s.maxWorkers
	}
	if flags.Changed(maxGPUsField) {
		body.MaxConcurrentGpus = &s.maxGPUs
	}
	if flags.Changed(budgetField) {
		budget, err := budgetAmount(s.budget)
		if err != nil {
			return serverlessapi.EnvironmentCreate{}, err
		}
		body.MonthlyBudget = &budget
	}
	if flags.Changed(alertPercentsField) {
		body.BudgetAlertPercents = &s.alertPercents
	}
	if flags.Changed(pauseAtBudgetFlag) {
		body.PauseAtBudget = &s.pauseAtBudget
	}
	return body, nil
}

// updateBody is the patch for the flags set on the command: a flag not set
// leaves its field out, and a cleared field is sent as null, or as no alert
// percents.
func (s environmentSettings) updateBody(flags *pflag.FlagSet, clears []string) (serverlessapi.EnvironmentUpdate, error) {
	cleared, err := clearedFields(flags, clears, updateClearFields)
	if err != nil {
		return serverlessapi.EnvironmentUpdate{}, err
	}
	if len(cleared) == 0 && !slices.ContainsFunc(environmentSettingFlags, flags.Changed) {
		return serverlessapi.EnvironmentUpdate{}, errNothingToUpdate
	}

	var body serverlessapi.EnvironmentUpdate
	if flags.Changed(maxWorkersField) {
		body.MaxConcurrentWorkers = nullable.NewNullableWithValue(s.maxWorkers)
	}
	if flags.Changed(maxGPUsField) {
		body.MaxConcurrentGpus = nullable.NewNullableWithValue(s.maxGPUs)
	}
	if flags.Changed(budgetField) {
		budget, err := budgetAmount(s.budget)
		if err != nil {
			return serverlessapi.EnvironmentUpdate{}, err
		}
		body.MonthlyBudget = nullable.NewNullableWithValue(budget)
	}
	if flags.Changed(alertPercentsField) {
		body.BudgetAlertPercents = &s.alertPercents
	}
	if flags.Changed(pauseAtBudgetFlag) {
		body.PauseAtBudget = &s.pauseAtBudget
	}

	if _, ok := cleared[maxWorkersField]; ok {
		body.MaxConcurrentWorkers = nullable.NewNullNullable[int32]()
	}
	if _, ok := cleared[maxGPUsField]; ok {
		body.MaxConcurrentGpus = nullable.NewNullNullable[int32]()
	}
	if _, ok := cleared[budgetField]; ok {
		body.MonthlyBudget = nullable.NewNullNullable[serverlessapi.MoneyAmount]()
	}
	if _, ok := cleared[alertPercentsField]; ok {
		body.BudgetAlertPercents = &[]int{}
	}
	return body, nil
}

// applyTo is current with the caps the flags set or clear changed; every
// other field, unauthenticatedAccess included, is sent back as it is because
// the API replaces the defaults as a whole.
func (s defaultsSettings) applyTo(current serverlessapi.EnvironmentDefaults, flags *pflag.FlagSet, cleared map[string]struct{}) serverlessapi.EnvironmentDefaults {
	body := current
	if flags.Changed(maxWorkersField) {
		body.MaxConcurrentWorkers = &s.maxWorkers
	}
	if flags.Changed(maxGPUsField) {
		body.MaxConcurrentGpus = &s.maxGPUs
	}
	if _, ok := cleared[maxWorkersField]; ok {
		body.MaxConcurrentWorkers = nil
	}
	if _, ok := cleared[maxGPUsField]; ok {
		body.MaxConcurrentGpus = nil
	}
	return body
}

// withEnvironmentContentsHint follows the API's refusal to delete an
// environment that still holds apps or secrets with the commands that list
// them. production is refused for being production, so it gets no hint.
func withEnvironmentContentsHint(err error, name string) error {
	re, ok := errors.AsType[*transport.RunwareError](err)
	if !ok || re.StatusCode != http.StatusConflict || isDefaultEnvironment(name) {
		return err
	}
	hinted := *re
	hinted.Message = re.Message + "\nList what it still holds with '" + serverlessCommand(name, "apps", "list") +
		"' and '" + serverlessCommand(name, "secrets", "list") +
		"', remove each with 'apps delete' or 'secrets remove' under the same --env, and retry once no app shows as deleting."
	return &hinted
}
