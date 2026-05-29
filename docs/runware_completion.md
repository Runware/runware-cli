## runware completion

Generate shell completion scripts

```
runware completion [bash|zsh|fish] [flags]
```

### Examples

```
  # generate bash completions
  runware completion bash > /etc/bash_completion.d/runware

  # generate zsh completions
  runware completion zsh > ~/.zfunc/_runware

  # generate fish completions
  runware completion fish > ~/.config/fish/completions/runware.fish
```

### Options

```
  -h, --help   help for completion
```

### Options inherited from parent commands

```
      --debug           Show full debug output
  -F, --format string   CLI output format: table, json, yaml
  -v, --verbose         Show request/response details
```

### SEE ALSO

* [runware](runware.md)	 - CLI tool for the Runware inference API

