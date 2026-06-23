## runware completion

Generate shell completion scripts

### Synopsis

Generate shell completion scripts for runware.

When called without an argument, runware attempts to detect your current shell
by inspecting environment variables (FISH_VERSION, ZSH_VERSION, BASH_VERSION,
PSModulePath). Pass an explicit shell name to override.

```
runware completion [bash|zsh|fish|powershell] [flags]
```

### Examples

```
  # Auto-detect shell and load completions for the current session
  # Bash/Zsh:
  source <(runware completion)
  # Fish:
  source (runware completion | psub)

  # Bash — Linux system-wide
  runware completion bash | sudo tee /etc/bash_completion.d/runware

  # Bash — macOS (Homebrew bash-completion@2)
  runware completion bash > $(brew --prefix)/etc/bash_completion.d/runware

  # Bash — per-user
  runware completion bash >> ~/.bash_completion

  # Zsh
  mkdir -p ~/.zfunc && runware completion zsh > ~/.zfunc/_runware

  # Fish
  runware completion fish > ~/.config/fish/completions/runware.fish

  # PowerShell — add to $PROFILE
  Invoke-Expression (& runware completion powershell | Out-String)
```

### Options

```
  -h, --help   help for completion
```

### Options inherited from parent commands

```
      --debug              Show full debug output
  -F, --format string      CLI output format: table, json, yaml (default "table")
      --transport string   Transport protocol: ws (WebSocket) or http (REST) (default "ws")
  -v, --verbose            Show request/response details
```

### SEE ALSO

* [runware](runware.md)	 - CLI tool for the Runware API

