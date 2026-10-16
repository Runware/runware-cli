#!/bin/sh
# Install the Runware CLI, or reuse one that is already installed, then create
# an App directory with 'runware serverless init'.
#
#   curl -fsSL https://serverless.runware.ai/install.sh | sh -s -- my-app
#
# The only argument is the App directory (default: my-app). The script never
# runs pip, uv or python: init installs the Serverless library into the App
# directory's own environment (docs/adr/0001).
#
# Environment overrides:
#   RUNWARE_INSTALL_DIR       where the binary goes (default: ~/.local/bin)
#   RUNWARE_RELEASE_BASE_URL  where releases come from (default: the GitHub
#                             releases of Runware/runware-cli). With an
#                             override, the script reads the latest version
#                             from <base>/latest, as JSON with a "tag_name"
#                             field, and the release files from
#                             <base>/download/<tag>/<file>.

# Exit immediately if a command exits with a non-zero status
set -e

REPO="Runware/runware-cli"
CLI_NAME="runware"

if [ "$#" -gt 1 ]; then
  echo "❌ Error: Too many arguments. Usage: sh install.sh [app-directory]"
  exit 1
fi
APP_DIR="${1:-my-app}"

if [ -n "$RUNWARE_RELEASE_BASE_URL" ]; then
  RELEASE_BASE_URL="${RUNWARE_RELEASE_BASE_URL%/}"
  LATEST_URL="$RELEASE_BASE_URL/latest"
else
  RELEASE_BASE_URL="https://github.com/$REPO/releases"
  LATEST_URL="https://api.github.com/repos/$REPO/releases/latest"
fi

INSTALL_DIR="${RUNWARE_INSTALL_DIR:-$HOME/.local/bin}"

# download_install puts the latest release, after checksum verification, into
# INSTALL_DIR and sets RUNWARE to the absolute path of the binary.
download_install() {
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  ARCH=$(uname -m)

  case "$OS" in
    linux|darwin) ;;
    *)
      echo "❌ Error: Unsupported OS: $OS"
      exit 1
      ;;
  esac

  case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *)
      echo "❌ Error: Unsupported architecture: $ARCH"
      exit 1
      ;;
  esac

  echo "==> 🔍 Detected OS: $OS, Architecture: $ARCH"

  echo "==> 🌐 Fetching latest version..."
  LATEST_TAG=$(curl -fsSL "$LATEST_URL" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

  if [ -z "$LATEST_TAG" ]; then
    echo "❌ Error: Could not fetch latest release."
    exit 1
  fi

  echo "==> 📦 Latest version is $LATEST_TAG"

  TARBALL="${CLI_NAME}_${OS}_${ARCH}.tar.gz"
  DOWNLOAD_URL="$RELEASE_BASE_URL/download/$LATEST_TAG/$TARBALL"
  CHECKSUMS_URL="$RELEASE_BASE_URL/download/$LATEST_TAG/checksums.txt"

  TMP_DIR=$(mktemp -d)
  trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

  TARBALL_PATH="$TMP_DIR/$TARBALL"
  CHECKSUMS_PATH="$TMP_DIR/checksums.txt"

  echo "==> ⬇️  Downloading $TARBALL..."

  curl -fsSL "$DOWNLOAD_URL" -o "$TARBALL_PATH"
  curl -fsSL "$CHECKSUMS_URL" -o "$CHECKSUMS_PATH"

  EXPECTED_SHA=$(grep " $TARBALL\$" "$CHECKSUMS_PATH" | awk '{print $1}')
  if [ -z "$EXPECTED_SHA" ]; then
    echo "❌ Error: Could not find checksum entry for $TARBALL"
    exit 1
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL_SHA=$(sha256sum "$TARBALL_PATH" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    ACTUAL_SHA=$(shasum -a 256 "$TARBALL_PATH" | awk '{print $1}')
  else
    echo "❌ Error: sha256sum/shasum not found; cannot verify download."
    exit 1
  fi

  if [ "$EXPECTED_SHA" != "$ACTUAL_SHA" ]; then
    echo "❌ Error: Checksum mismatch for $TARBALL"
    exit 1
  fi

  tar -xzf "$TARBALL_PATH" -C "$TMP_DIR"

  mkdir -p "$INSTALL_DIR"
  # Make the directory absolute, so that RUNWARE and the PATH fix work from
  # anywhere.
  INSTALL_DIR=$(cd "$INSTALL_DIR" && pwd)

  echo "==> 🚀 Installing to $INSTALL_DIR"
  mv "$TMP_DIR/$CLI_NAME" "$INSTALL_DIR/"
  chmod +x "$INSTALL_DIR/$CLI_NAME"

  rm -rf "$TMP_DIR"
  trap - EXIT INT TERM

  RUNWARE="$INSTALL_DIR/$CLI_NAME"
  DOWNLOADED=1
}

# has_init reports whether the CLI at $1 has 'serverless init'. The exit
# status alone cannot tell: a CLI from before init prints the help of the
# serverless group for 'serverless init --help' and exits 0. Only init's own
# help names the init command in its usage line.
has_init() {
  "$1" serverless init --help 2>/dev/null | grep -q 'serverless init'
}

# from_brew reports whether the CLI at $1 is the one Homebrew installed. An
# older CLI elsewhere on PATH can shadow Homebrew's copy, and upgrading the
# cask would leave that one in place.
from_brew() {
  command -v brew >/dev/null 2>&1 && [ "$1" = "$(brew --prefix)/bin/$CLI_NAME" ]
}

# brew_runware sets RUNWARE to the CLI that Homebrew installed.
brew_runware() {
  RUNWARE="$(brew --prefix)/bin/$CLI_NAME"
}

DOWNLOADED=0
EXISTING=$(command -v "$CLI_NAME" 2>/dev/null || true)

if [ -n "$EXISTING" ] && has_init "$EXISTING"; then
  echo "==> ✅ Using the installed CLI at $EXISTING"
  RUNWARE="$EXISTING"
elif [ -n "$EXISTING" ]; then
  echo "==> ⚠️  The CLI at $EXISTING is too old: it has no 'serverless init'. Installing the latest version."
  if from_brew "$EXISTING"; then
    echo "==> 🍺 Upgrading via brew..."
    brew upgrade --cask runware/tap/runware
    brew_runware
  else
    download_install
  fi
elif [ "$(uname -s)" = "Darwin" ] && command -v brew >/dev/null 2>&1; then
  echo "==> 🍺 Homebrew detected — installing via brew (recommended for macOS)..."
  brew install --cask runware/tap/runware
  brew_runware
else
  if [ "$(uname -s)" = "Darwin" ]; then
    echo "==> ⚠️  Homebrew not found. Falling back to direct binary download."
    echo "    To install Homebrew: https://brew.sh"
    echo ""
  fi
  download_install
fi

# A release from before init would print help for 'serverless init <dir>' and
# exit 0, so check before delegating to it.
if ! has_init "$RUNWARE"; then
  echo "❌ Error: The CLI at $RUNWARE has no 'serverless init', even after the install. The latest release may not include init yet."
  exit 1
fi

echo "==> 🏗️  Creating the App directory $APP_DIR..."
# Keep going after a failed init, so that the PATH fix still prints. The
# script exits with init's status at the end.
INIT_STATUS=0
# The "--" ends flag parsing, so that a directory such as --force stays a
# directory.
"$RUNWARE" serverless init -- "$APP_DIR" || INIT_STATUS=$?

if [ "$DOWNLOADED" = 1 ]; then
  CURRENT=$(command -v "$CLI_NAME" 2>/dev/null || true)
  case ":$PATH:" in
    *":$INSTALL_DIR:"*)
      if [ "$CURRENT" != "$RUNWARE" ]; then
        echo "==> ⚠️  Another CLI at $CURRENT comes before $INSTALL_DIR in your PATH."
        echo "    Remove it, so that '$CLI_NAME' runs the version installed now."
      fi
      ;;
    *)
      # Keep $HOME unexpanded for the default directory, so the line works in
      # any shell config file.
      if [ "$INSTALL_DIR" = "$HOME/.local/bin" ]; then
        # shellcheck disable=SC2016
        PATH_ENTRY='$HOME/.local/bin'
      else
        PATH_ENTRY=$INSTALL_DIR
      fi
      echo "==> ✅ Installed to $RUNWARE"
      echo "==> ⚠️  $INSTALL_DIR is not in your PATH."
      echo ""
      echo "To use the CLI from anywhere, add this line to your ~/.bashrc, ~/.zshrc, or ~/.profile:"
      echo ""
      echo "    export PATH=\"$PATH_ENTRY:\$PATH\""
      echo ""
      echo "Then restart your terminal or run: source ~/.bashrc (or your respective config file)."
      if [ -n "$CURRENT" ]; then
        echo "Put it before the directory of the older CLI at $CURRENT."
      fi
      ;;
  esac
fi

exit "$INIT_STATUS"
