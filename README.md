# Runware CLI

A command-line tool for interacting with the [Runware](https://runware.ai) inference API. Built in Go, distributed as a single static binary.

## Install

### Homebrew (MacOS)

```shell
brew tap runware/tap
brew install runware
```

### Scoop (Windows)
```shell
scoop bucket add runware https://github.com/Runware/scoop-bucket.git
scoop install runware
```

### Linux

```shell
curl -fsSL https://install.runware.ai | sh
```

### From source

```shell
git clone https://github.com/runware/runware-cli.git
cd runware-cli
make build
```

## Quick start

```shell
# Authenticate
runware auth login

# Check connectivity
runware ping

# Generate an image
runware inference image "a chess match in the park"

# Check your credits
runware account credits
```

## Commands

Full command reference is available in the [docs](./docs/runware.md) directory.

### Image generation

```shell
# Simple text-to-image
runware inference image "a cat riding a rocket"

# With options
runware inference image "a cyberpunk cityscape" \
  --model runware:100@1 \
  --width 1024 --height 576 \
  --steps 28 --cfg 3.5 \
  --count 4

# Image-to-image
runware inference image "make it more cinematic" \
  --source ./input.png --strength 0.7

# Inpainting
runware inference image "replace with a golden retriever" \
  --source ./photo.png --mask ./mask.png

# Using a preset
runware inference image "a portrait" --preset quick-flux

# Just get the URL, don't download
runware inference image "a sunset" --no-download

# Preview the API request without sending it
runware inference image "a sunset" --dry-run
```

### Video generation

```shell
runware inference video "a timelapse of a sunset over mountains" --model klingai:5@3
runware inference video "a cat playing piano" --model google:3@2 --duration 5
```

### Audio generation

```shell
runware inference audio "a jazz piano solo with soft drums" --model elevenlabs:1@1 --duration 30
runware inference audio "ocean waves crashing on rocks" --model elevenlabs:1@1 --duration 60
```

### Text generation

```shell
runware inference text "explain how transformers work"
```

### Authentication

```shell
runware auth login              # Authenticate with API key
runware auth login --key <key>  # Non-interactive login
runware auth logout             # Clear stored credentials
runware auth status             # Show current auth state
```

### Account

```shell
runware account credits         # Credit balance and usage stats
```

### Model search

```shell
runware model search "flux"     # Search available models
```

### Presets

Save frequently used configurations:

```shell
runware preset save quick-flux --model runware:100@1 --width 512 --height 512 --steps 4
runware preset list
runware preset show quick-flux
runware preset delete quick-flux
```

### Configuration

```shell
runware config show             # Print current config
runware config set <key> <val>  # Set a config value
runware config reset            # Reset to defaults
runware config path             # Print config file path
```

Config is stored at `~/.runware/config.yaml`.

### Other

```shell
runware ping                    # API connectivity check
runware version                 # Print version info
runware completion              # Generate shell completions (bash/zsh/fish/powershell, auto-detected)
```

## Global flags

| Flag | Description |
|------|-------------|
| `--format json\|yaml\|table` | Output format (default: table) |
| `-v, --verbose` | Show request/response details |
| `--debug` | Full debug output |

All commands support `--format json` for piping into `jq` or scripts.

## Configuration

### Environment variables

| Variable | Description |
|----------|-------------|
| `RUNWARE_API_KEY` | API key (overrides config file) |

### Config file

```yaml
api_key: your-api-key

defaults:
  model: runware:100@1
  width: 1024
  height: 1024
  steps: 28
  cfg_scale: 3.5
  scheduler: euler
  output_dir: ./outputs
  output_format: png
  format: table

presets:
  quick:
    model: runware:100@1
    width: 512
    height: 512
    steps: 4
```

## Development

```shell
make build      # Build binary
make test       # Run tests
make lint       # Run golangci-lint
make clean      # Remove binary
make snapshot   # GoReleaser snapshot build
```

## Shell completions

`runware completion` generates a completion script for your shell. Once installed, press `Tab` to complete commands, flags, model AIR identifiers, and — for `runware run` — schema-driven parameter names like `positivePrompt=`, `width=`, or `messages.0.role=`.

When your shell sets a standard version variable (`FISH_VERSION`, `ZSH_VERSION`, `BASH_VERSION`, `PSModulePath`), running `runware completion` without arguments auto-detects your shell. Pass an explicit name to override.

### Bash

**Linux — system-wide:**

```shell
runware completion bash | sudo tee /etc/bash_completion.d/runware > /dev/null
```

**macOS — Homebrew `bash-completion@2`:**

```shell
runware completion bash > $(brew --prefix)/etc/bash_completion.d/runware
```

**Per-user (any platform):**

```shell
runware completion bash >> ~/.bash_completion
source ~/.bash_completion
```

### Zsh

Add `~/.zfunc` to your `fpath` **before** the `compinit` call in `~/.zshrc`:

```zsh
fpath=(~/.zfunc $fpath)
autoload -Uz compinit && compinit
```

Then generate the completion file and reload:

```shell
mkdir -p ~/.zfunc
runware completion zsh > ~/.zfunc/_runware
exec zsh
```

**Oh My Zsh:**

```shell
runware completion zsh > "${ZSH_CUSTOM:-$HOME/.oh-my-zsh/custom}/completions/_runware"
exec zsh
```

### Fish

```shell
runware completion fish > ~/.config/fish/completions/runware.fish
```

Fish picks up completions automatically — no shell restart needed.

### PowerShell

**Inline (recommended) — add to `$PROFILE`:**

```powershell
Invoke-Expression (& runware completion powershell | Out-String)
```

**Or generate a file and dot-source it:**

```powershell
runware completion powershell | Out-File -Encoding utf8 "$HOME\runware.ps1"
# Add this line to $PROFILE:
. "$HOME\runware.ps1"
```

### Demo

Once installed, `Tab` works at every level:

```
# Complete subcommands
$ runware <Tab>
auth        account     completion  config      model
ping        preset      run         version

# Complete model AIR identifiers for `run`
$ runware run <Tab>
runware:101@1  -- FLUX Dev — fast, high-quality image generation
minimax:m3@0   -- MiniMax M3 text model
klingai:5@3    -- Kling AI video generation
...

# Complete parameter names (schema-driven, per model)
$ runware run runware:101@1 <Tab>
positivePrompt=  -- Text prompt describing the image
width=           -- Output width in pixels
height=          -- Output height in pixels
steps=           -- Number of diffusion steps
...

# Array fields use dot-notation with auto-advancing indices
$ runware run minimax:m3@0 <Tab>
messages.0.role=     -- Role of the message sender
messages.0.content=  -- Content of the message

# After messages.0.* are filled, next Tab suggests index 1
$ runware run minimax:m3@0 messages.0.role=user messages.0.content="What is Go?" <Tab>
messages.1.role=     -- Role of the message sender
messages.1.content=  -- Content of the message
```
