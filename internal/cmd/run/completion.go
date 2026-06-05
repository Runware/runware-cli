package run

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/runware/runware-cli/internal/api"
	"github.com/spf13/cobra"
)

// schemaArgCompleter provides shell autocompletion for key=value positional arguments.
// When the first arg (the model AIR) is already present, it fetches the model's schema
// and returns dot-notation leaf completions (e.g. "speech.text=", "messages.0.role=")
// for parameters not yet provided in args. Completion is best-effort: failures are
// silently ignored.
func schemaArgCompleter(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	// First argument is the model — let the shell handle free-form text for it.
	if len(args) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	model := args[0]
	schema, err := api.FetchModelSchema(cmd.Context(), model)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var node schemaNode
	if err := json.Unmarshal(schema.RequestSchema, &node); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// Collect the full dot-notation key of every arg the user has already typed,
	// normalised through parseKV so that auto-index sugar (e.g. "messages.role=user")
	// is expanded to its canonical form ("messages.0.role") before being recorded.
	// This ensures nextArrayIdx advances correctly and collectCompletions doesn't
	// re-suggest keys that are already set.
	provided := make(map[string]struct{}, len(args))
	for _, a := range args[1:] {
		if k := normalizeProvidedKey(a, node); k != "" {
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
			if isNumeric(seg) {
				if n := mustAtoi(seg); n > highest {
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

	completions := collectCompletions("", node, provided, prefix, nextArrayIdx)

	// NoSpace so the shell doesn't add a space after the '=', letting the user
	// immediately type the value.
	return completions, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
}

// collectCompletions recursively walks a schema node and emits a dot-notation
// "key=" completion for every leaf property. prefix is the dot-path built so far
// (empty at the top level). Object properties are recursed into; array fields
// use nextArrayIdx to determine the next index to suggest.
func collectCompletions(
	prefix string,
	node schemaNode,
	provided map[string]struct{},
	toCompletePrefix string,
	nextArrayIdx func(string) int,
) []cobra.Completion {
	var out []cobra.Completion

	for name := range node.Properties {
		prop := node.Properties[name]
		if _, skip := autoFields[name]; skip {
			continue
		}

		full := name
		if prefix != "" {
			full = prefix + "." + name
		}

		switch prop.Type {
		case schemaTypeObject:
			if len(prop.Properties) > 0 {
				// Recurse — emit completions for the object's own leaves.
				out = append(out, collectCompletions(full, prop, provided, toCompletePrefix, nextArrayIdx)...)
				continue
			}
			// Object with no known sub-properties — fall through to leaf.

		case schemaTypeArray:
			if prop.Items != nil && prop.Items.Type == schemaTypeObject && len(prop.Items.Properties) > 0 {
				// For object arrays, find the first index slot that still has at least
				// one unfilled leaf field. Only advance past an index once every leaf
				// in the item schema for that index is present in provided.
				maxIdx := nextArrayIdx(full) // highest index touched + 1, or 0
				idx := maxIdx                // default: open a new slot
				for i := range maxIdx {
					if !allLeafsProvided(full+"."+strconv.Itoa(i), *prop.Items, provided) {
						idx = i
						break
					}
				}
				indexedPrefix := full + "." + strconv.Itoa(idx)
				out = append(out, collectCompletions(indexedPrefix, *prop.Items, provided, toCompletePrefix, nextArrayIdx)...)
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
