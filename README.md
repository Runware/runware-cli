# Runware CLI

A command-line tool for interacting with the [Runware](https://runware.ai) inference API. Built in Go, distributed as a single static binary.

## Install

### Homebrew (macOS)

```shell
brew install --cask runware/tap/runware
```

### Scoop (Windows)

```shell
scoop bucket add runware https://github.com/Runware/scoop-bucket.git
scoop install runware
```

### Linux

```shell
curl -fsSL https://cli.runware.ai/install.sh | sh
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
runware run runware:400@1 positivePrompt="a chess match in the park" width=1024 height=1024

# Check your account details
runware account details
```

## Commands

Full command reference is available in the [docs](./docs/runware.md) directory.

### `runware run` — inference

The primary command for all inference tasks. The model is identified by its AIR (AI Resource) identifier; parameters are passed as `key=value` pairs. The model's JSON Schema is fetched automatically to validate inputs and choose the right task type.

```shell
runware run <model> [key=value ...] [flags]
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--preset` | Load parameters from a saved preset (model and params used as defaults; `<model>` may be omitted when the preset provides one) |
| `--task-type` | Override the detected task type. Accepts any API task type, e.g. `imageInference`, `videoInference`, `audioInference`, `textInference`, `3dInference`, or an operation like `removeBackground` |
| `--output-dir` | Directory to save downloaded output files (default `./outputs`) |
| `--no-download` | Skip auto-downloading media files |
| `--delivery-method` | Delivery method (`sync` or `async`) (default `async`) |
| `--poll-interval` | Polling interval for async requests (default `2s`) |
| `--validate` | Validate parameters against the model schema before submitting |

#### Image generation

```shell
# Text-to-image
runware run runware:400@1 positivePrompt="A serene mountain landscape" width=1024 height=1024

# Save to a specific directory
runware run runware:400@1 positivePrompt="Abstract art" --output-dir ./my-images width=1024 height=1024

# Get the URL without downloading
runware run runware:400@1 positivePrompt="Abstract art" --format json --no-download

# Community model — specify task type explicitly
runware run civitai:305149@392545 --task-type imageInference positivePrompt="A portrait" width=1024 height=1024
```

#### Video generation

```shell
runware run google:3@3 positivePrompt="Ocean waves at sunset" width=1280 height=720 duration=8
```

#### Audio generation

```shell
# Music generation
runware run minimax:music@2.6 positivePrompt="Upbeat electronic dance music with driving bass and synth leads" settings.instrumental=true

# Text-to-speech (the voice is designed from the prompt)
runware run alibaba:qwen@3-tts-1.7b-voicedesign positivePrompt="A calm, friendly young woman with a soft tone" speech.text="Hello, this is a text-to-speech example." speech.voice=design
```

#### Text inference (LLM)

```shell
# Single message
runware run google:gemma@4-31b messages.0.role=user messages.0.content="Explain quantum computing"

# Multi-turn conversation
runware run google:gemma@4-31b \
  messages.0.role=user    messages.0.content="What is Go?" \
  messages.1.role=assistant messages.1.content="A compiled language." \
  messages.2.role=user    messages.2.content="How do I install it?"
```

#### 3D inference

```shell
# Text to 3D
runware run tencent:hunyuan-3d@3.1-pro positivePrompt="A red vintage sports car"

# Image to 3D
runware run tencent:hunyuan-3d@3.1-pro inputs.images.0="https://example.com/product.jpg"
```

### Authentication

```shell
runware auth login              # Authenticate with API key (interactive)
runware auth login --key <key>  # Non-interactive login
runware auth logout             # Clear stored credentials
runware auth status             # Show current auth state
```

### Account

```shell
runware account details         # Show account details, team, API keys, and usage stats
```

### Models

```shell
runware model search -q "flux"                          # Search available models
runware model search -q "portrait" --category checkpoint --architecture sdxl
runware model search -q "anime" --wide                  # Include tags and default size columns
runware model show civitai:305149@392545                # Full details for a model
runware model schema google:3@2                         # Show request parameters for a model
runware model schema google:3@2 --response              # Show response schema instead
runware model pricing google:gemini@3.1-pro             # Pricing breakdown for a model
runware model examples google:gemini@3.1-pro            # Example requests for a model
```

`model pricing` and `model examples` read from the public model catalog and accept either the AIR or the model id. `model examples` prints a ready-to-run `runware run` command for each example, so you can copy one, tweak the prompt, and run it. Add `--format json` for the full request and response payloads.

Upload a custom model to your account (WebSocket transport only):

```shell
runware model upload \
  --air "myorg:my-model@1.0" \
  --name "My Custom Model" \
  --category checkpoint \
  --architecture sdxl \
  --download-url "https://example.com/model.safetensors"
```

Run `runware model upload --help` for the full flag set (LoRA trigger words, default scheduler and steps, visibility, and more). Your AIR source is shown by `runware account details`.

### Presets

Save frequently used configurations:

```shell
runware preset save quick-flux runware:100@1 width=512 height=512 steps=4
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

### Upload

```shell
runware upload <file|url>       # Upload an image asset; prints imageUUID (and taskUUID) for use in run params
```

Accepts a local file path, public URL, or data URI. The returned imageUUID can be passed to parameters like `inputs.seedImage=<imageUUID>` on the `run` command. The taskUUID can be used with `runware result`.

### Result

```shell
runware result <taskUUID>       # Resume waiting for an async task by UUID
```

Use when `runware run` was interrupted before a task completed. The taskUUID is printed when the task is first submitted.

### Other

```shell
runware ping                    # API connectivity check
runware version                 # Print version info
runware completion              # Generate shell completions (bash/zsh/fish/powershell, auto-detected)
```

## Global flags

| Flag | Description |
|------|-------------|
| `-F, --format json\|yaml\|table` | Output format (default: table) |
| `-v, --verbose` | Show request/response details |
| `--debug` | Full debug output |
| `--transport ws\|http` | Transport protocol: WebSocket or REST (default: `defaults.transport` from config, or `ws`) |

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
    output_dir: ./outputs
    format: table
    transport: ws
presets:
    quick:
        model: runware:100@1
        params:
            height: "512"
            steps: "10"
            width: "512"
```

## Development

```shell
make build          # Build binary for current platform → ./bin/runware
make build-all      # Build all platforms → ./bin/
make install        # go install for current platform
make run ARGS="..." # Run without building (e.g. make run ARGS="ping")
make test           # Run all tests
make lint           # Run golangci-lint
make docs           # Regenerate ./docs/ command reference
make snapshot       # GoReleaser snapshot build
make clean          # Remove ./bin and ./dist
make go-tidy        # go mod tidy && go mod verify
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
ping        preset      result      run         upload
version

# Complete model AIR identifiers for `run`
$ runware run <Tab>
runware:400@1       -- FLUX.2 [dev] image generation
google:gemma@4-31b  -- Gemma text model
google:3@3          -- Veo 3.1 video generation
...

# Complete parameter names (schema-driven, per model)
$ runware run runware:400@1 <Tab>
positivePrompt=  -- Text prompt describing the image
width=           -- Output width in pixels
height=          -- Output height in pixels
steps=           -- Number of diffusion steps
...

# Array fields use dot-notation with auto-advancing indices
$ runware run google:gemma@4-31b <Tab>
messages.0.role=     -- Role of the message sender
messages.0.content=  -- Content of the message

# After messages.0.* are filled, next Tab suggests index 1
$ runware run google:gemma@4-31b messages.0.role=user messages.0.content="What is Go?" <Tab>
messages.1.role=     -- Role of the message sender
messages.1.content=  -- Content of the message
```
