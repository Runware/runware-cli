package cmdutil

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/schema"
	"github.com/spf13/cobra"
)

// MakeSchemaArgCompleter returns a ValidArgsFunction that provides schema-driven
// shell completion for key=value positional arguments. modelArgIdx is the index
// in args that holds the model AIR:
//
//	0 → run <model> [key=value ...]
//	1 → preset save <name> <model> [key=value ...]
//
// When the model has not yet been typed (len(args) <= modelArgIdx), no
// completions are offered. Once the model is present, the model's JSON Schema
// is fetched and dot-notation leaf completions are returned for parameters not
// already provided. Completion is best-effort: failures are silently ignored.
func MakeSchemaArgCompleter(modelArgIdx int) func(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		// Model argument has not been typed yet — let the shell handle free-form text.
		if len(args) <= modelArgIdx {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		model := args[modelArgIdx]
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
		// CollectCompletions doesn't re-suggest keys that are already set.
		kvArgs := args[modelArgIdx+1:]
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

		// NoSpace so the shell doesn't add a space after the '=', letting the user
		// immediately type the value.
		return completions, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
	}
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

	for name := range node.Properties {
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
