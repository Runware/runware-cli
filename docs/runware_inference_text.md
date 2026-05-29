## runware inference text

Generate text using a language model

### Synopsis

Send a message to a language model and get a text response.

```
runware inference text [message] [flags]
```

### Examples

```
  # ask a question
  runware inference text "What is the capital of France?" --model minimax:m2.7@highspeed

  # limit response length
  runware inference text "Explain quantum computing" --model minimax:m2.7@highspeed --max-tokens 500

  # use system prompt and temperature
  runware inference text "Write a haiku about coding" --model minimax:m2.7@highspeed --system "You are a poet" --temperature 0.8

  # request JSON output
  runware inference text "List 3 facts about Mars" --model minimax:m2.7@highspeed --output-format json
```

### Options

```
  -n, --count int              Number of results to generate (1-4) (default 1)
  -X, --dry-run                Print the API request without executing
  -h, --help                   help for text
  -C, --include-cost           Include cost info in response
  -M, --max-tokens int         Maximum tokens in response (1-128000)
  -m, --model string           Model identifier (e.g. runware:qwen3-thinking@1)
  -f, --output-format string   Format of the model response: text, json
  -p, --preset string          Named preset to apply
  -e, --seed int               Random seed for reproducibility
  -Z, --stop strings           Stop sequences (max 5)
  -y, --system string          System prompt
  -q, --temperature float      Sampling temperature (0-2)
  -K, --top-k int              Top-k sampling parameter (1-100)
  -P, --top-p float            Nucleus sampling parameter (0-1)
```

### Options inherited from parent commands

```
      --debug           Show full debug output
  -F, --format string   CLI output format: table, json, yaml
  -v, --verbose         Show request/response details
```

### SEE ALSO

* [runware inference](runware_inference.md)	 - Run inference tasks

