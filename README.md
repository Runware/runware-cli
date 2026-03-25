# Runware CLI

A command-line tool for interacting with the [Runware](https://runware.ai) inference API. Built in Go, distributed as a single static binary.

## Install

### From source

```bash
git clone https://github.com/runware/runware-cli.git
cd runware-cli
make build
```

### From release

Requires the [GitHub CLI](https://cli.github.com/) (`gh`) with access to the Runware org.

```bash
# macOS (Apple Silicon)
gh release download --repo Runware/runware-cli --pattern "runware_darwin_arm64.tar.gz"

# macOS (Intel)
gh release download --repo Runware/runware-cli --pattern "runware_darwin_amd64.tar.gz"

# Linux (x86_64)
gh release download --repo Runware/runware-cli --pattern "runware_linux_amd64.tar.gz"

# Linux (ARM64)
gh release download --repo Runware/runware-cli --pattern "runware_linux_arm64.tar.gz"

# Windows (x86_64)
gh release download --repo Runware/runware-cli --pattern "runware_windows_amd64.zip"
```

Extract and move to your PATH:

```bash
# macOS/Linux
tar xzf runware_*.tar.gz
sudo mv runware /usr/local/bin/

# Windows: unzip and add runware.exe to your PATH
```

```bash
runware version
```

## Quick start

```bash
# Authenticate
runware auth login

# Check connectivity
runware ping

# Generate an image
runware imageInference "a chess match in the park"

# Check your credits
runware account credits
```

## Commands

### Image generation

```bash
# Simple text-to-image
runware imageInference "a cat riding a rocket"

# With options
runware imageInference "a cyberpunk cityscape" \
  --model runware:100@1 \
  --width 1024 --height 576 \
  --steps 28 --cfg 3.5 \
  --count 4

# Image-to-image
runware imageInference "make it more cinematic" \
  --source ./input.png --strength 0.7

# Inpainting
runware imageInference "replace with a golden retriever" \
  --source ./photo.png --mask ./mask.png

# Using a preset
runware imageInference "a portrait" --preset quick-flux

# Just get the URL, don't download
runware imageInference "a sunset" --no-download

# Preview the API request without sending it
runware imageInference "a sunset" --dry-run
```

### Authentication

```bash
runware auth login              # Authenticate with API key
runware auth login --key <key>  # Non-interactive login
runware auth logout             # Clear stored credentials
runware auth status             # Show current auth state
runware auth switch staging     # Switch to staging environment
```

### Account

```bash
runware account credits         # Credit balance and usage stats
```

### Model search

```bash
runware modelSearch "flux"      # Search available models (coming soon)
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
| `RUNWARE_ENV` | Environment: `production` or `staging` |

### Config file

```yaml
environment: production
api_key: your-api-key
mode: public              # public | internal

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
