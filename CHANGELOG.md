

## v0.5.0 - 2026-05-28

### Bug Fixes

- Use obviously-fake key in MaskKey test fixture ([2648925](https://github.com/runware/runware-cli/commit/2648925e950a1c9f0a37fa9c42fe58acbe8d50a2))
- Respect explicit --seed 0 across inference commands ([890ae2e](https://github.com/runware/runware-cli/commit/890ae2e2b0b704a0dd80406750d160c89af83065))
- Return error when video downloads fail ([357a077](https://github.com/runware/runware-cli/commit/357a077d3fc6482c49cd9f21e666e4a50ed25e33))
- Validate HTTP status and add image download timeout (#1) ([5638d5c](https://github.com/runware/runware-cli/commit/5638d5caa4c4a10509d30efafcca08c8fb568365))
- Poll inference status before sleeping (#3) ([6d59df8](https://github.com/runware/runware-cli/commit/6d59df87fc3c123da6ae4eba396ca59e5c7bc07b))
- Error parsing and example google veo duration (#7) ([d4f2116](https://github.com/runware/runware-cli/commit/d4f2116931eb114bb9b72a0e8da084d5cb53abe0))
- Add default duration for audio command (#9) ([72cd1b9](https://github.com/runware/runware-cli/commit/72cd1b9f6b609ed014485a8ed04610c3c7d17c0f))

## v0.4.0 - 2026-03-25

### Features

- Add .golangci.yml config and fix all lint issues ([2758ba2](https://github.com/runware/runware-cli/commit/2758ba2e32a302fd63265fb6c4b1548f91733290))
- Add Windows build support ([f842591](https://github.com/runware/runware-cli/commit/f84259178307ffceb587da8875376e4ad4528b04))

### Bug Fixes

- Require --model for textInference, update examples to use minimax:m2.7@highspeed ([9c2bafc](https://github.com/runware/runware-cli/commit/9c2bafc3d326e4e9a73f184127ab62ce76567d6a))
- Use gh release download in README for private repo ([60d211a](https://github.com/runware/runware-cli/commit/60d211aed4042c155ef0b87c2bbc60cbfcd102a9))

## v0.3.0 - 2026-03-23

### Features

- Add textInference command for LLM text generation ([2f8c2b6](https://github.com/runware/runware-cli/commit/2f8c2b6ba5e6c908c1783f90790b52bc07980415))

## v0.2.0 - 2026-03-22

### Features

- Add dynamic shell completions for flags and arguments ([86dac04](https://github.com/runware/runware-cli/commit/86dac046b962d8e351f43dfbfcbaa14d73061166))
- Add modelSearch command ([825614d](https://github.com/runware/runware-cli/commit/825614ddf38495fb4ba5d195c37bd73fe999e184))
- Add videoInference command with async polling ([24c4ff9](https://github.com/runware/runware-cli/commit/24c4ff9d8eaea13465246c3989611e60049b323e))
- Add audioInference command with async polling ([9bbadec](https://github.com/runware/runware-cli/commit/9bbadec02c17487c6d0e74ba420bf6af727037ee))

### Bug Fixes

- Config set now correctly stores shorthand default keys ([8c17ca6](https://github.com/runware/runware-cli/commit/8c17ca68e24c7725fb91be649aae9deb8279a3f2))
- Don't send model-specific params as defaults in imageInference ([629376f](https://github.com/runware/runware-cli/commit/629376fc863f70c5368e81f70fc0cd05ca70f0cb))
- Update audio test to match numberResults always being sent ([c46fa54](https://github.com/runware/runware-cli/commit/c46fa54c39a4a90a017bc873913478500e4065a8))

## v0.1.0 - 2026-03-22

### Features

- Initial Runware CLI with auth, ping, imageInference, and core infrastructure ([7cf673c](https://github.com/runware/runware-cli/commit/7cf673cacc76236edeccd04317d388a203380600))

### Bug Fixes

- Pin golangci-lint to v2.11.4 for Go 1.26 compatibility ([4976006](https://github.com/runware/runware-cli/commit/49760060c3ba2a298855b8fd7eb6ec6ae36a1ac2))
- Upgrade golangci-lint-action to v7 for v2 linter support ([14a9bd7](https://github.com/runware/runware-cli/commit/14a9bd778829cca8134f214a06ad31cd7db6dfdc))


