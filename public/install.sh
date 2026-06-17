#!/bin/sh
# Exit immediately if a command exits with a non-zero status
set -e

REPO="Runware/runware-cli"
CLI_NAME="runware"

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

if [ "$OS" = "darwin" ]; then
  if command -v brew >/dev/null 2>&1; then
    echo "==> 🍺 Homebrew detected — installing via brew (recommended for macOS)..."
    brew install --cask runware/tap/runware
    exit 0
  else
    echo "==> ⚠️  Homebrew not found. Falling back to direct binary download."
    echo "    To install Homebrew: https://brew.sh"
    echo ""
  fi
fi

echo "==> 🔍 Detected OS: $OS, Architecture: $ARCH"

echo "==> 🌐 Fetching latest version..."
LATEST_TAG=$(curl -s "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$LATEST_TAG" ]; then
  echo "❌ Error: Could not fetch latest release."
  exit 1
fi

echo "==> 📦 Latest version is $LATEST_TAG"

TARBALL="${CLI_NAME}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/$REPO/releases/download/$LATEST_TAG/$TARBALL"
CHECKSUMS_URL="https://github.com/$REPO/releases/download/$LATEST_TAG/checksums.txt"

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

INSTALL_DIR="$HOME/.local/bin"

mkdir -p "$INSTALL_DIR"

echo "==> 🚀 Installing to $INSTALL_DIR"
mv "$TMP_DIR/$CLI_NAME" "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/$CLI_NAME"

rm -rf "$TMP_DIR"

case ":$PATH:" in
  *":$INSTALL_DIR:"*)
    echo "==> ✅ Success! Run '$CLI_NAME --help' to get started."
    ;;
  *)
    echo "==> ✅ Installed to $INSTALL_DIR/$CLI_NAME"
    echo "==> ⚠️  $INSTALL_DIR is not in your PATH."
    echo ""
    echo "To use the CLI from anywhere, add this line to your ~/.bashrc, ~/.zshrc, or ~/.profile:"
    echo ""
    echo "    export PATH=\"\$HOME/.local/bin:\$PATH\""
    echo ""
    echo "Then restart your terminal or run: source ~/.bashrc (or your respective config file)."
    ;;
esac
