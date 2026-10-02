#!/usr/bin/env sh
# Arianet CLI installer
# Usage: curl -sSL https://get.arianet.ir/cli | sh

set -e

REPO="arianet/arianet-cli"
BINARY="arianet"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Detect OS and arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

case "$OS" in
  linux|darwin) ;;
  msys*|mingw*|cygwin*) OS="windows" ;;
  *)
    echo "Unsupported OS: $OS"
    exit 1
    ;;
esac

# Get latest release version
VERSION=$(curl -sSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' | sed -E 's/.*"v?([^"]+)".*/\1/')

if [ -z "$VERSION" ]; then
  echo "Failed to determine latest version."
  exit 1
fi

echo "Installing arianet CLI v${VERSION} (${OS}/${ARCH})..."

FILENAME="${BINARY}_${VERSION}_${OS}_${ARCH}"
if [ "$OS" = "windows" ]; then
  FILENAME="${FILENAME}.zip"
else
  FILENAME="${FILENAME}.tar.gz"
fi

URL="https://github.com/${REPO}/releases/download/v${VERSION}/${FILENAME}"
TMP_DIR=$(mktemp -d)

# Download
curl -sSL "$URL" -o "${TMP_DIR}/${FILENAME}"

# Extract
cd "$TMP_DIR"
if [ "$OS" = "windows" ]; then
  unzip -q "$FILENAME"
else
  tar -xzf "$FILENAME"
fi

# Install
if [ -w "$INSTALL_DIR" ]; then
  mv "${BINARY}" "${INSTALL_DIR}/${BINARY}"
else
  sudo mv "${BINARY}" "${INSTALL_DIR}/${BINARY}"
fi

chmod +x "${INSTALL_DIR}/${BINARY}"
rm -rf "$TMP_DIR"

echo ""
echo "arianet CLI installed successfully!"
echo ""
echo "Get started:"
echo "  arianet configure --token <your-api-token>"
echo "  arianet server list"
echo ""
echo "Generate your API token at: https://cloud.ariaservice.net/users/api-tokens"
