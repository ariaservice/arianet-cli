#!/usr/bin/env sh
# Arianet CLI installer (Linux and macOS).
#
#   curl -fsSL https://raw.githubusercontent.com/ariaservice/arianet-cli/main/scripts/install.sh | sh
#
# What it does, and nothing else:
#   1. downloads the release archive and checksums.txt from GitHub Releases,
#   2. verifies the archive's SHA-256 against checksums.txt,
#   3. (if `cosign` is installed) verifies checksums.txt's Sigstore signature,
#   4. copies the single `arianet` binary into INSTALL_DIR.
# It does not edit shell profiles, run the binary, or install anything else.
#
# Environment: VERSION (e.g. 0.1.0, default: latest), INSTALL_DIR
# (default: ~/.local/bin when not root and /usr/local/bin is not writable).

set -eu

REPO="ariaservice/arianet-cli"
BINARY="arianet"

die() { echo "error: $*" >&2; exit 1; }

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  linux|darwin) ;;
  *) die "unsupported OS: $OS (see https://github.com/${REPO}/releases for Windows)" ;;
esac

case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac

if [ -z "${INSTALL_DIR:-}" ]; then
  if [ -w /usr/local/bin ]; then
    INSTALL_DIR=/usr/local/bin
  else
    INSTALL_DIR="$HOME/.local/bin"
  fi
fi

if [ -z "${VERSION:-}" ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | sed -n 's/.*"tag_name": *"v\{0,1\}\([^"]*\)".*/\1/p' | head -n 1)
fi
VERSION="${VERSION#v}"
[ -n "$VERSION" ] || die "could not determine the latest version"
case "$VERSION" in
  *[!0-9A-Za-z._-]*) die "invalid version: $VERSION" ;;
esac

ARCHIVE="${BINARY}_${VERSION}_${OS}_${ARCH}.tar.gz"
BASE="https://github.com/${REPO}/releases/download/v${VERSION}"

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

echo "Installing arianet ${VERSION} (${OS}/${ARCH})"
curl -fsSL "${BASE}/${ARCHIVE}" -o "${TMP_DIR}/${ARCHIVE}"
curl -fsSL "${BASE}/checksums.txt" -o "${TMP_DIR}/checksums.txt"

# Verify the archive against the published checksums.
EXPECTED=$(awk -v f="$ARCHIVE" '$2 == f {print $1}' "${TMP_DIR}/checksums.txt")
[ -n "$EXPECTED" ] || die "${ARCHIVE} is not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL=$(sha256sum "${TMP_DIR}/${ARCHIVE}" | awk '{print $1}')
else
  ACTUAL=$(shasum -a 256 "${TMP_DIR}/${ARCHIVE}" | awk '{print $1}')
fi
[ "$EXPECTED" = "$ACTUAL" ] || die "checksum mismatch for ${ARCHIVE}; refusing to install"
echo "  checksum OK"

# Optional: verify that checksums.txt was produced by this repository's release workflow.
if command -v cosign >/dev/null 2>&1; then
  curl -fsSL "${BASE}/checksums.txt.sigstore.json" -o "${TMP_DIR}/checksums.txt.sigstore.json"
  cosign verify-blob \
    --bundle "${TMP_DIR}/checksums.txt.sigstore.json" \
    --certificate-identity-regexp "^https://github.com/${REPO}/\.github/workflows/release\.yml@refs/tags/v" \
    --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
    "${TMP_DIR}/checksums.txt" >/dev/null 2>&1 \
    || die "signature verification of checksums.txt failed; refusing to install"
  echo "  signature OK"
fi

tar -xzf "${TMP_DIR}/${ARCHIVE}" -C "$TMP_DIR" "$BINARY"
mkdir -p "$INSTALL_DIR"
if [ -w "$INSTALL_DIR" ]; then
  install -m 0755 "${TMP_DIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
else
  echo "  ${INSTALL_DIR} is not writable, using sudo for the copy"
  sudo install -m 0755 "${TMP_DIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
fi

echo "Installed ${INSTALL_DIR}/${BINARY}"
case ":$PATH:" in
  *":${INSTALL_DIR}:"*) ;;
  *) echo "Note: ${INSTALL_DIR} is not in your PATH." ;;
esac
echo ""
echo "Next:  arianet configure     (paste an API token from https://cloud.ariaservice.net/users/api-tokens)"
