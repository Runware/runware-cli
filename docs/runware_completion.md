## runware completion

Generate shell completion scripts

### Synopsis

Generate shell completion scripts for runware.

  bash:  runware completion bash > /etc/bash_completion.d/runware
  zsh:   runware completion zsh > ~/.zfunc/_runware
  fish:  runware completion fish > ~/.config/fish/completions/runware.fish

```
runware completion [bash|zsh|fish] [flags]
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

