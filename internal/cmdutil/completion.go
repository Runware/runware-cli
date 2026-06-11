package cmdutil

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/schema"
	"github.com/spf13/cobra"
)

// SplitModelArgs splits positional args into the model AIR and key=value
// pairs. Model AIRs never contain "=", so a first argument containing "="
// means the model was omitted (it then comes from a preset).
func SplitModelArgs(args []string) (model string, kvArgs []string) {
	if len(args) == 0 {
		return "", nil
	}
	if strings.Contains(args[0], "=") {
		return "", args
	}
	return args[0], args[1:]
}

// MakeSchemaArgCompleter returns a ValidArgsFunction that provides schema-driven
// shell completion for key=value positional arguments. modelArgIdx is the index
// in args where the model AIR may appear:
//
//	0 → run <model> [key=value ...]
//	1 → preset save <name> <model> [key=value ...]
//
// The model is taken from args when present; when the args at modelArgIdx are
// already key=value pairs (or absent), the command's --preset flag is consulted
// and the preset supplies the model. With no model from either source, no
// completions are offered. Once the model is known, its JSON Schema is fetched
// and dot-notation leaf completions are returned for parameters not already
// typed on the command line. Keys the preset sets are still suggested — they
// can be overridden — with their preset value shown in the description.
// Completion is best-effort: failures are silently ignored.
func MakeSchemaArgCompleter(modelArgIdx int) func(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		// Positional args before the model slot (e.g. the preset name for
		// "preset save") have not been typed yet — let the shell handle them.
		if len(args) < modelArgIdx {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		var rest []string
		if len(args) > modelArgIdx {
			rest = args[modelArgIdx:]
		}
		model, kvArgs := SplitModelArgs(rest)

		// Model not typed positionally — fall back to the --preset flag when the
		// command has one and it names a preset that supplies a model.
		var presetParams map[string]string
		if model == "" {
			if name, err := cmd.Flags().GetString("preset"); err == nil && name != "" {
				config.Init() //nolint:errcheck,gosec
				if p := config.GetPreset(name); p != nil {
					model = p.Model
					presetParams = p.Params
				}
			}
		}
		if model == "" {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
		defer cancel()
		modelSchema, err := api.FetchModelSchema(ctx, model)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		var node schema.Node
		if err := json.Unmarshal(modelSchema.RequestSchema, &node); err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		// Collect the full dot-notation key of every arg the user has already typed,
		// normalised through NormalizeProvidedKey so that auto-index sugar (e.g.
		// "messages.role=user") is expanded to its canonical form ("messages.0.role")
		// before being recorded. This ensures nextArrayIdx advances correctly and
		// CollectCompletions doesn't re-suggest keys that are already set. Preset
		// params are deliberately not counted: they stay suggested so they can be
		// overridden, annotated with the preset's value below.
		provided := make(map[string]struct{}, len(kvArgs))
		for _, a := range kvArgs {
			if k := schema.NormalizeProvidedKey(a, node); k != "" {
				provided[k] = struct{}{}
			}
		}

		// nextArrayIdx returns the next unused index for an array field identified by
		// its dot-notation prefix (e.g. "messages"). It scans already-provided args
		// for the pattern "prefix.N.*" and returns max(N)+1, or 0 if none found.
		nextArrayIdx := func(prefix string) int {
			highest := -1
			needle := prefix + "."
			for k := range provided {
				if !strings.HasPrefix(k, needle) {
					continue
				}
				rest := k[len(needle):]
				seg, _, _ := strings.Cut(rest, ".")
				if schema.IsNumeric(seg) {
					if n := schema.MustAtoi(seg); n > highest {
						highest = n
					}
				}
			}
			return highest + 1
		}

		// If the user has started typing, only return completions that share the prefix.
		prefix, _, hasEq := strings.Cut(toComplete, "=")
		if hasEq {
			// Value side already typed — nothing more to complete.
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		completions := CollectCompletions("", node, provided, prefix, nextArrayIdx)

		if len(presetParams) > 0 {
			normalized := make(map[string]string, len(presetParams))
			for k, v := range presetParams {
				if n := schema.NormalizeProvidedKey(k+"="+v, node); n != "" {
					normalized[n] = v
				}
			}
			completions = annotatePresetCompletions(completions, normalized)
		}

		// NoSpace so the shell doesn't add a space after the '=', letting the user
		// immediately type the value.
		return completions, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
	}
}

// annotatePresetCompletions prefixes the description of completions whose key
// the preset already sets, so users can see the value they would override.
// presetKeys maps canonical dot-notation keys to the preset's values.
func annotatePresetCompletions(completions []cobra.Completion, presetKeys map[string]string) []cobra.Completion {
	out := make([]cobra.Completion, 0, len(completions))
	for _, c := range completions {
		key, desc, _ := strings.Cut(c, "\t")
		v, ok := presetKeys[strings.TrimSuffix(key, "=")]
		if !ok {
			out = append(out, c)
			continue
		}
		out = append(out, cobra.CompletionWithDesc(key, "[preset: "+v+"] "+desc))
	}
	return out
}

// CollectCompletions recursively walks a schema node and emits a dot-notation
// "key=" completion for every leaf property. prefix is the dot-path built so far
// (empty at the top level). Object properties are recursed into; array fields
// use nextArrayIdx to determine the next index to suggest.
func CollectCompletions(
	prefix string,
	node schema.Node,
	provided map[string]struct{},
	toCompletePrefix string,
	nextArrayIdx func(string) int,
) []cobra.Completion {
	var out []cobra.Completion

	// Sort by name to avoid non-deterministic completion ordering.
	names := slices.Sorted(maps.Keys(node.Properties))

	for _, name := range names {
		prop := node.Properties[name]
		if schema.IsAuto(name) {
			continue
		}

		full := name
		if prefix != "" {
			full = prefix + "." + name
		}

		switch prop.Type {
		case schema.TypeObject:
			if len(prop.Properties) > 0 {
				// Recurse — emit completions for the object's own leaves.
				out = append(out, CollectCompletions(full, prop, provided, toCompletePrefix, nextArrayIdx)...)
				continue
			}
			// Object with no known sub-properties — fall through to leaf.

		case schema.TypeArray:
			if prop.Items != nil && prop.Items.Type == schema.TypeObject && len(prop.Items.Properties) > 0 {
				// For object arrays, find the first index slot that still has at least
				// one unfilled leaf field. Only advance past an index once every leaf
				// in the item schema for that index is present in provided.
				maxIdx := nextArrayIdx(full) // highest index touched + 1, or 0
				idx := maxIdx                // default: open a new slot
				for i := range maxIdx {
					if !schema.AllLeafsProvided(full+"."+strconv.Itoa(i), *prop.Items, provided) {
						idx = i
						break
					}
				}
				indexedPrefix := full + "." + strconv.Itoa(idx)
				out = append(out, CollectCompletions(indexedPrefix, *prop.Items, provided, toCompletePrefix, nextArrayIdx)...)
				continue
			}
			// Scalar array or unknown items — next unused index via nextArrayIdx.
			idx := nextArrayIdx(full)
			idxStr := strconv.Itoa(idx)
			indexedPrefix := full + "." + idxStr
			candidate := indexedPrefix + "="
			if _, done := provided[indexedPrefix]; !done && strings.HasPrefix(indexedPrefix, toCompletePrefix) {
				desc := prop.Description
				if desc == "" {
					desc = prop.Type
				}
				out = append(out, cobra.CompletionWithDesc(candidate, desc))
			}
			continue
		}

		// Leaf field (string / integer / number / boolean / object with no sub-props).
		candidate := full + "="
		if _, done := provided[full]; !done && strings.HasPrefix(full, toCompletePrefix) {
			desc := prop.Description
			if desc == "" {
				desc = prop.Type
			}
			out = append(out, cobra.CompletionWithDesc(candidate, desc))
		}
	}

	return out
}
