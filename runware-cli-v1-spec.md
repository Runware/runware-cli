# Runware CLI — v1 Feature Specification

## Overview

A command-line tool for interacting with the Runware inference API. Built in Go using the Cobra CLI framework. Designed initially as an internal developer tool with a clear path to public release.

**Language:** Go
**CLI Framework:** Cobra + Viper (config management)
**Distribution:** Single static binary (cross-compiled for macOS, Linux, Windows)
**Config location:** `~/.runware/config.yaml`

---

## Architecture Principles

- **Single binary, zero dependencies.** No runtimes, no installers. Download and run.
- **CLI commands mirror API task types.** Command names match the Runware API task types directly (e.g. `imageInference`, `upscale`, `promptEnhance`) to eliminate mental translation between CLI and API.
- **Composable by default.** All commands support `--format json` for piping into `jq`, scripts, etc. Human-friendly table output by default.
- **Sensible defaults everywhere.** The most common operation (`runware imageInference "prompt"`) should work with zero flags.
- **Preset-driven workflow.** Frequently used configurations are saved and reused, not retyped.
- **Internal/public separation via config mode.** A `mode: internal` flag in config unlocks developer-only commands. These commands live under `runware dev ...` and are hidden from default `--help` output when in public mode.

---

## API Task Types → CLI Commands

The CLI command names map directly to the Runware API task types. This table shows the full mapping:

| API Task Type              | CLI Command              | v1 Scope      | Description                         |
|----------------------------|--------------------------|---------------|-------------------------------------|
| `imageInference`           | `imageInference`         | Must have     | Text-to-image / image-to-image      |
| `videoInference`           | `videoInference`         | Nice to have  | Video generation                    |
| `audioInference`           | `audioInference`         | Nice to have  | Audio generation                    |
| `textInference`            | `textInference`          | Nice to have  | Text/LLM inference                  |
| `3dInference`              | `3dInference`            | Nice to have  | 3D model generation                 |
| `upscale`                  | `upscale`                | Nice to have  | Image upscaling                     |
| `removeBackground`         | `removeBackground`       | Nice to have  | Background removal                  |
| `imageCaption`             | `imageCaption`           | Nice to have  | Image captioning                    |
| `videoCaption`             | `videoCaption`           | Nice to have  | Video captioning                    |
| `audioTranscription`       | `audioTranscription`     | Nice to have  | Audio-to-text transcription         |
| `promptEnhance`            | `promptEnhance`          | Nice to have  | Prompt improvement/expansion        |
| `imageControlNetPreProcess`| `controlNetPreProcess`   | Nice to have  | ControlNet preprocessing            |
| `imageMasking`             | `imageMasking`           | Nice to have  | Auto mask generation                |
| `photoMaker`               | `photoMaker`             | Nice to have  | PhotoMaker face transfer            |
| `vectorize`                | `vectorize`              | Nice to have  | Image to vector                     |
| `imageUpload`              | `imageUpload`            | Nice to have  | Upload image to Runware storage     |
| `modelSearch`              | `modelSearch`            | Must have     | Search available models             |
| `modelUpload`              | `modelUpload`            | Nice to have  | Upload custom model                 |
| `modelDelete`              | `modelDelete`            | Nice to have  | Delete custom model                 |
| `accountManagement`        | `account`                | Must have     | Account info, credits, usage        |
| `ping`                     | `ping`                   | Must have     | API connectivity check              |

**Note:** Deprecated/backward-compat task types (`imageUpscale`, `imageBackgroundRemoval`) are NOT exposed as CLI commands. The CLI uses only the current canonical names. Internal-only types (`authentication`, `getServerConfig`, `internalTools`, `getResponse`, `mediaStorage`, `caption`) are either handled implicitly (auth) or exposed under `runware dev`.

---

## Command Structure

```
runware
│
│   # ── Inference Commands (mirror API task types) ──────────────
│
├── imageInference             # Text-to-image and image-to-image generation
│   └── (flags for model, dimensions, steps, seed, cfg, scheduler, etc.)
│
├── videoInference             # Video generation
├── audioInference             # Audio generation
├── textInference              # Text/LLM inference
├── 3dInference                # 3D model generation
│
│   # ── Processing Commands (mirror API task types) ─────────────
│
├── upscale                    # Image upscaling
├── removeBackground           # Background removal
├── imageCaption               # Image captioning
├── videoCaption               # Video captioning
├── audioTranscription         # Audio to text
├── promptEnhance              # Prompt improvement/expansion
├── controlNetPreProcess       # ControlNet preprocessing
├── imageMasking               # Auto mask generation
├── photoMaker                 # PhotoMaker face transfer
├── vectorize                  # Image to vector
│
│   # ── Asset Management (mirror API task types) ────────────────
│
├── imageUpload                # Upload image to Runware storage
├── modelSearch                # Search available models
├── modelUpload                # Upload custom model
├── modelDelete                # Delete custom model
│
│   # ── Account & Connectivity ──────────────────────────────────
│
├── account                    # Account info, credits, usage
│   ├── info                   # Account details
│   ├── credits                # Current credit balance
│   └── usage                  # Usage stats (recent period)
│
├── ping                       # API connectivity check
│
│   # ── CLI Management ──────────────────────────────────────────
│
├── auth
│   ├── login                  # Authenticate with API key
│   ├── logout                 # Clear stored credentials
│   ├── status                 # Show current auth state and environment
│   └── switch                 # Switch between environments (prod/staging)
│
├── preset
│   ├── list                   # Show all saved presets
│   ├── show <name>            # Show preset details
│   ├── save <name>            # Save current flags as a named preset
│   ├── delete <name>          # Remove a preset
│   └── import/export          # Share presets as YAML
│
├── config
│   ├── show                   # Print current config
│   ├── set <key> <value>      # Set a config value
│   ├── reset                  # Reset to defaults
│   └── path                   # Print config file path
│
├── dev                        # [INTERNAL ONLY — hidden when mode != internal]
│   ├── queue-stats            # Live queue depth and processing rates
│   ├── server-status          # Infrastructure health overview
│   ├── server-config          # Maps to getServerConfig task type
│   ├── replay <job-id>        # Re-submit a historical job (from prod logs)
│   ├── benchmark              # Run standardised inference benchmarks
│   ├── debug-job <id>         # Extended job metadata, routing info, timings
│   ├── internal-tools         # Maps to internalTools task type
│   └── flush-cache            # Invalidate model/config caches
│
├── completion                 # Generate shell completions (bash/zsh/fish)
└── version                    # Print version, commit, build date
```

---

## Shell Completion — Task Type Awareness

Tab completion is aware of all API task types. Completion triggers:

- **Command names:** `runware <tab>` lists all available commands, grouped logically (inference, processing, asset management, etc.)
- **Task type matching:** For commands with camelCase names, completion matches partial lowercase (e.g. `runware image<tab>` suggests `imageInference`, `imageCaption`, `imageMasking`, `imageUpload`)
- **Model names:** `--model <tab>` fetches from cached model list (via `modelSearch` API, cached locally with TTL)
- **Preset names:** `--preset <tab>` reads from config
- **Flag names:** All flags auto-complete via Cobra
- **Enum values:** Flags with known values complete dynamically (e.g. `--scheduler <tab>` suggests `euler`, `dpm++`, etc.; `--output-format <tab>` suggests `png`, `jpg`, `webp`)

Model list caching: first `--model <tab>` hits the API via `modelSearch`, writes to `~/.runware/cache/models.json` with a 1-hour TTL. Subsequent completions read from cache.

---

## Feature Detail

### 1. Authentication & Configuration

**Config file:** `~/.runware/config.yaml`

```yaml
# Environment
environment: production          # production | staging
api_key: rw_live_xxxxxxxxxxxx   # stored here or in OS keychain

# Mode
mode: internal                   # internal | public (controls dev command visibility)

# Defaults (apply to imageInference unless overridden)
defaults:
  model: flux-dev
  width: 1024
  height: 1024
  steps: 28
  cfg_scale: 3.5
  scheduler: euler
  output_dir: ./outputs
  output_format: png             # png | jpg | webp
  format: table                  # table | json | yaml (output format for CLI)

# Named presets (also manageable via `runware preset` commands)
presets:
  quick-flux:
    model: flux-dev
    width: 1024
    height: 1024
    steps: 4
    cfg_scale: 1.0
    scheduler: euler
  sdxl-portrait:
    model: sdxl-1.0
    width: 768
    height: 1344
    steps: 30
    cfg_scale: 7.0
  flux-schnell:
    model: flux-schnell
    width: 1024
    height: 1024
    steps: 1
```

**Environment handling:**
- `runware auth switch staging` swaps the active environment
- Per-environment API keys stored separately
- `RUNWARE_API_KEY` and `RUNWARE_ENV` environment variables override config (12-factor friendly)
- `runware auth status` shows current environment, key prefix (masked), and key validity

**Keychain integration (nice-to-have for v1):**
- macOS Keychain / Linux libsecret for API key storage instead of plaintext YAML
- Fallback to config file if keychain unavailable

### 2. Core Inference — `runware imageInference`

The primary command. Should be as frictionless as possible.

**Minimal usage:**
```bash
runware imageInference "a chess match in the park"
```
This uses all defaults from config — model, dimensions, steps, output dir. Downloads the result and prints the file path.

**Full usage:**
```bash
runware imageInference "a cat riding a rocket" \
  --model flux-dev \
  --width 1024 \
  --height 768 \
  --steps 28 \
  --cfg 3.5 \
  --scheduler euler \
  --seed 42 \
  --negative "blurry, low quality" \
  --count 4 \
  --output ./my-images/ \
  --output-format webp \
  --no-download \
  --format json
```

**Image-to-image (same command, with source flag):**
```bash
runware imageInference "make it more cinematic" \
  --source ./input.png \
  --strength 0.7 \
  --model flux-dev
```

**Inpainting (same command, with source + mask):**
```bash
runware imageInference "replace with a golden retriever" \
  --source ./photo.png \
  --mask ./mask.png \
  --model sdxl-1.0
```

The `imageInference` command handles text-to-image, image-to-image, and inpainting in a single command, differentiated by which flags are provided. This mirrors how the API task type works — one task type, multiple modes.

**Preset usage:**
```bash
runware imageInference "a cat" --preset quick-flux
runware imageInference "a portrait" --preset sdxl-portrait --seed 12345
```
Presets provide base values; explicit flags override any preset value.

**Behaviour:**
- Progress indicator while waiting (spinner or progress bar)
- On completion: download image(s) to output dir, print file path(s)
- `--no-download` skips download and prints the image URL(s) instead
- `--open` opens the result in the default image viewer (macOS `open`, Linux `xdg-open`)
- `--format json` outputs full API response as JSON (for scripting)
- `--dry-run` prints the API request payload without executing
- `--verbose` / `-v` prints request/response details, timings

**Batch mode (nice-to-have):**
```bash
runware imageInference --batch prompts.txt --preset quick-flux
```
Reads one prompt per line, submits all, downloads results.

### 3. Other Inference Commands

All inference commands follow the same pattern as `imageInference`: positional prompt argument (where applicable), shared flags (`--format`, `--dry-run`, `--verbose`, `--output`, `--preset`), plus command-specific flags.

**Video:**
```bash
runware videoInference "a timelapse of a sunset over mountains" \
  --model <video-model> \
  --duration 5
```

**Audio:**
```bash
runware audioInference "a calm piano melody" \
  --model <audio-model> \
  --duration 10
```

**Text:**
```bash
runware textInference "explain quantum computing" \
  --model <text-model> \
  --max-tokens 500
```

**3D:**
```bash
runware 3dInference "a low-poly treasure chest" \
  --model <3d-model>
```

### 4. Processing Commands

```bash
# Upscaling
runware upscale ./low-res.png --scale 4 --output ./upscaled.png

# Background removal
runware removeBackground ./photo.png --output ./no-bg.png

# Captioning
runware imageCaption ./photo.png
runware videoCaption ./video.mp4

# Audio transcription
runware audioTranscription ./recording.mp3 --format json

# Prompt enhancement
runware promptEnhance "a cat sitting"
# → outputs enhanced prompt to stdout

# ControlNet preprocessing
runware controlNetPreProcess ./photo.png --type canny --output ./canny.png

# Mask generation
runware imageMasking ./photo.png --prompt "the dog" --output ./mask.png

# PhotoMaker
runware photoMaker "a professional headshot" --face ./selfie.png --output ./result.png

# Vectorize
runware vectorize ./logo.png --output ./logo.svg
```

### 5. Asset Management

```bash
# Upload image to Runware storage
runware imageUpload ./image.png
# → returns storage URL

# Search models
runware modelSearch "flux"
runware modelSearch --type checkpoint --format json

# Upload custom model
runware modelUpload ./my-model.safetensors --name "my-custom-model"

# Delete custom model
runware modelDelete my-custom-model
```

### 6. Connectivity — `runware ping`

```bash
runware ping
# → Runware API: OK (43ms) — environment: production
```

Quick connectivity and auth validation. Useful for scripting health checks and verifying setup.

### 7. Account — `runware account`

```bash
runware account info                    # Account ID, plan, etc.
runware account credits                 # Current credit balance
runware account usage                   # Usage breakdown (last 7/30 days)
runware account usage --period 30d --format json
```

### 8. Presets — `runware preset`

```bash
runware preset list
runware preset show quick-flux
runware preset save my-preset --model flux-dev --width 512 --height 512 --steps 8
runware preset delete my-preset
runware preset export > presets.yaml       # Share with team
runware preset import < team-presets.yaml  # Load shared presets
```

Presets are stored in `~/.runware/config.yaml` under the `presets` key. Import merges without overwriting unless `--force` is passed.

**Project-level presets (nice-to-have):**
A `.runware.yaml` in the current directory is loaded and merged on top of global config. Allows per-repo defaults.

### 9. Developer Commands — `runware dev` (Internal Only)

Hidden from `--help` unless `mode: internal` is set in config.

```bash
runware dev queue-stats                 # Live queue depth, processing rates, per-model breakdown
runware dev server-status               # Infrastructure health, GPU utilisation
runware dev server-config               # Fetch server config (maps to getServerConfig)
runware dev replay abc-123              # Re-submit a historical job with same params
runware dev benchmark                   # Standardised perf test suite
runware dev benchmark --model flux-dev --iterations 10
runware dev debug-job abc-123           # Full job trace: routing, server, timings, retries
runware dev internal-tools              # Access to internalTools task type
runware dev flush-cache                 # Invalidate model/config caches
```

These commands may hit internal-only API endpoints or connect to internal infrastructure directly (Redis, monitoring systems).

---

## Cross-Cutting Concerns

### Output Formatting

Every command supports:
- `--format table` (default) — human-readable aligned columns
- `--format json` — machine-readable, pipe to `jq`
- `--format yaml` — for config-oriented output

Global default is configurable via `defaults.format` in config.

### Error Handling

- Clear, actionable error messages. Not raw API responses.
- Auth errors → suggest `runware auth login`
- Rate limit errors → show retry-after, suggest waiting
- Network errors → suggest checking connectivity, suggest `runware ping`
- `--verbose` mode shows full request/response for debugging

### Shell Completions

```bash
runware completion bash > /etc/bash_completion.d/runware
runware completion zsh > ~/.zfunc/_runware
runware completion fish > ~/.config/fish/completions/runware.fish
```

Dynamic completions for: command names, model names (cached), preset names, enum flag values (schedulers, output formats, etc.).

### Logging & Debug

- `--verbose` / `-v` — show request URLs, headers (masked key), response times
- `--debug` — full request/response bodies, internal state
- Logs to stderr so stdout remains clean for piping

### Versioning

- Semantic versioning, starting at `0.1.0`
- `runware version` shows version, git commit, build date, Go version
- `runware version --check` checks for updates (hits GitHub releases or internal endpoint)

---

## API Client Architecture

The API client supports two transport modes behind a shared interface:

```
internal/api/
├── client.go        # Client interface definition
├── rest.go          # REST/HTTP implementation (standard CLI commands)
├── ws.go            # WebSocket implementation (TUI/interactive mode, future)
└── types.go         # Shared request/response types
```

- **REST client** — used for all standard CLI commands. Stateless, one request per invocation.
- **WebSocket client** — used for future TUI/interactive mode. Persistent connection, real-time feedback, job progress streaming.
- Both implement the same `Client` interface so commands don't need to know which transport they're using.

---

## v1 Scope — Must Have vs Nice to Have

### Must Have (v1.0)

- [ ] `auth login/logout/status/switch`
- [ ] `imageInference` with full flag support (text-to-image, img2img, inpainting via flags)
- [ ] `config show/set/reset`
- [ ] Preset system (save/list/show/delete, config-based)
- [ ] Sensible per-model defaults
- [ ] `modelSearch` — search available models
- [ ] `account credits`
- [ ] `ping` — connectivity check
- [ ] `--format json` on all commands
- [ ] `--dry-run` on inference/processing commands
- [ ] `--verbose` mode
- [ ] Shell completions (bash/zsh/fish) with dynamic task type, model, and preset completion
- [ ] `version` command
- [ ] `dev` command group (hidden, internal)
- [ ] Cross-compilation for macOS (arm64/amd64), Linux (amd64)
- [ ] Error handling with actionable messages

### Nice to Have (v1.x)

- [ ] `videoInference`, `audioInference`, `textInference`, `3dInference`
- [ ] `upscale`, `removeBackground`, `imageCaption`, `videoCaption`
- [ ] `audioTranscription`, `promptEnhance`, `controlNetPreProcess`
- [ ] `imageMasking`, `photoMaker`, `vectorize`
- [ ] `imageUpload`, `modelUpload`, `modelDelete`
- [ ] `account info/usage`
- [ ] Batch mode (`--batch`)
- [ ] `--open` flag (open result in viewer)
- [ ] Preset import/export
- [ ] Project-level `.runware.yaml`
- [ ] Keychain integration for API key storage
- [ ] `runware version --check` update checker
- [ ] Interactive mode / TUI (e.g. with bubbletea) using WebSocket API
- [ ] Windows cross-compilation
- [ ] Homebrew tap for distribution

---

## Go Project Structure (Suggested)

```
runware-cli/
├── cmd/                         # Cobra command definitions
│   ├── root.go                  # Root command, global flags, config init
│   ├── auth.go                  # auth subcommands
│   ├── image_inference.go       # imageInference command
│   ├── video_inference.go       # videoInference command
│   ├── audio_inference.go       # audioInference command
│   ├── text_inference.go        # textInference command
│   ├── three_d_inference.go     # 3dInference command
│   ├── upscale.go               # upscale command
│   ├── remove_background.go     # removeBackground command
│   ├── image_caption.go         # imageCaption command
│   ├── video_caption.go         # videoCaption command
│   ├── audio_transcription.go   # audioTranscription command
│   ├── prompt_enhance.go        # promptEnhance command
│   ├── controlnet_preprocess.go # controlNetPreProcess command
│   ├── image_masking.go         # imageMasking command
│   ├── photo_maker.go           # photoMaker command
│   ├── vectorize.go             # vectorize command
│   ├── image_upload.go          # imageUpload command
│   ├── model_search.go          # modelSearch command
│   ├── model_upload.go          # modelUpload command
│   ├── model_delete.go          # modelDelete command
│   ├── account.go               # account subcommands
│   ├── ping.go                  # ping command
│   ├── preset.go                # preset subcommands
│   ├── config.go                # config subcommands
│   ├── dev.go                   # dev subcommands (internal)
│   ├── completion.go            # shell completion
│   └── version.go               # version command
├── internal/
│   ├── api/                     # Runware API client
│   │   ├── client.go            # Interface definition + base client
│   │   ├── rest.go              # REST API implementation (single calls)
│   │   ├── ws.go                # WebSocket implementation (TUI/interactive, future)
│   │   ├── types.go             # Request/response types for all task types
│   │   └── tasks/               # Per-task-type request builders
│   │       ├── image_inference.go
│   │       ├── upscale.go
│   │       ├── model_search.go
│   │       └── ...
│   ├── config/                  # Config loading, merging, validation
│   │   ├── config.go
│   │   └── presets.go
│   ├── output/                  # Output formatting (table, json, yaml)
│   │   └── formatter.go
│   ├── cache/                   # Local caching (model list, etc.)
│   │   └── cache.go
│   └── dev/                     # Internal-only logic
│       └── commands.go
├── main.go                      # Entry point
├── go.mod
├── go.sum
├── Makefile                     # Build, test, lint, release targets
├── .goreleaser.yaml             # GoReleaser config for cross-compilation & releases
└── README.md
```

**Key dependencies:**
- `github.com/spf13/cobra` — CLI framework
- `github.com/spf13/viper` — Config management
- `github.com/olekukonez/tablewriter` — Table output
- `github.com/briandowns/spinner` — Progress spinners
- `github.com/gorilla/websocket` — WebSocket client (for future TUI)
- `github.com/zalando/go-keyring` — OS keychain (nice-to-have)

---

## Notes for Implementation

1. **Start with `imageInference`, `auth`, and `ping`.** Get the core loop working end-to-end first: authenticate, ping, submit a job, get a result, save to disk.
2. **Build the API client as an independent `internal/api` package.** Clean separation means it can be extracted into a Go SDK later if needed. The client interface should accept a task type string and params, making it trivial to add new task types.
3. **Config merging order:** defaults → config file → project `.runware.yaml` → environment variables → CLI flags. Each layer overrides the previous. Viper handles this natively.
4. **Test with `--dry-run` early.** It's useful for development too — validate flag parsing and request construction without hitting the API.
5. **GoReleaser for builds.** Handles cross-compilation, checksums, changelogs, and GitHub releases in one config file.
6. **Task type registry pattern.** Since all task types follow a similar pattern (send params, get result), consider a registry that maps task type strings to their flag definitions and response handlers. This makes adding new task types mechanical rather than requiring new plumbing each time.
