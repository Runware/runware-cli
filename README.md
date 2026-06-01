# Runware CLI

A command-line tool for interacting with the [Runware](https://runware.ai) inference API. Built in Go, distributed as a single static binary.

## Install

### Homebrew

```bash
brew tap runware/tap
brew install runware
```

### From source

```bash
git clone https://github.com/runware/runware-cli.git
cd runware-cli
make build
```

## Quick start

```bash
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

```bash
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

```bash
runware inference video "a timelapse of a sunset over mountains" --model klingai:5@3
runware inference video "a cat playing piano" --model google:3@2 --duration 5
```

### Audio generation

```bash
runware inference audio "a jazz piano solo with soft drums" --model elevenlabs:1@1 --duration 30
runware inference audio "ocean waves crashing on rocks" --model elevenlabs:1@1 --duration 60
```

### Text generation

```bash
runware inference text "explain how transformers work"
```

### Authentication

```bash
runware auth login              # Authenticate with API key
runware auth login --key <key>  # Non-interactive login
runware auth logout             # Clear stored credentials
runware auth status             # Show current auth state
```

### Account

```bash
runware account credits         # Credit balance and usage stats
```

### Model search

```bash
runware model search "flux"     # Search available models
```

### Presets

Save frequently used configurations:

```bash
runware preset save quick-flux --model runware:100@1 --width 512 --height 512 --steps 4
runware preset list
runware preset show quick-flux
runware preset delete quick-flux
```

### Configuration

```bash
runware config show             # Print current config
runware config set <key> <val>  # Set a config value
runware config reset            # Reset to defaults
runware config path             # Print config file path
```

Config is stored at `~/.runware/config.yaml`.

### Other

```bash
runware ping                    # API connectivity check
runware version                 # Print version info
runware completion bash         # Generate shell completions (bash/zsh/fish)
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

```bash
make build      # Build binary
make test       # Run tests
make lint       # Run golangci-lint
make clean      # Remove binary
make snapshot   # GoReleaser snapshot build
```

## Shell completions

```bash
# Bash
runware completion bash > /etc/bash_completion.d/runware

# Zsh
runware completion zsh > ~/.zfunc/_runware

# Fish
runware completion fish > ~/.config/fish/completions/runware.fish
```
