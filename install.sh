#!/bin/sh
# Exit immediately if a command exits with a non-zero status
set -e

REPO="Runware/runware-cli"
CLI_NAME="runware"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  i386|i686) ARCH="386" ;;
esac

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

TMP_DIR=$(mktemp -d)
echo "==> ⬇️  Downloading $TARBALL..."

if ! curl -sL "$DOWNLOAD_URL" | tar -xz -C "$TMP_DIR"; then
  echo "❌ Error: Failed to download or extract the CLI."
  rm -rf "$TMP_DIR"
  exit 1
fi

INSTALL_DIR="$HOME/.local/bin"

mkdir -p "$INSTALL_DIR"

echo "==> 🚀 Installing to $INSTALL_DIR"
mv "$TMP_DIR/$CLI_NAME" "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/$CLI_NAME"

rm -rf "$TMP_DIR"

if command -v "$CLI_NAME" >/dev/null 2>&1; then
  echo "==> ✅ Success! Run '$CLI_NAME --help' to get started."
else
  echo "==> ⚠️  Success, but $INSTALL_DIR is not in your PATH."
  echo ""
  echo "To use the CLI from anywhere, add this line to your ~/.bashrc, ~/.zshrc, or ~/.profile:"
  echo ""
  echo "    export PATH=\"\$HOME/.local/bin:\$PATH\""
  echo ""
  echo "Then restart your terminal or run: source ~/.bashrc (or your respective config file)."
fi
