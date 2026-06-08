package run

import (
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

// schemaArgCompleter provides shell autocompletion for the run command's
// key=value positional arguments. args[0] is the model AIR; completion
// begins once the model has been typed. See cmdutil.MakeSchemaArgCompleter
// for the full implementation.
var schemaArgCompleter = cmdutil.MakeSchemaArgCompleter(0)

// collectCompletions is a package-level alias exposed for unit tests.
var collectCompletions = cmdutil.CollectCompletions

// Ensure the variable type satisfies the cobra signature at compile time.
var _ func(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) = schemaArgCompleter
