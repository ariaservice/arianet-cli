#!/usr/bin/env bash
# Builds the metadata of a signed apt repository inside <site-dir>.
# <site-dir>/pool must already contain the .deb files.
#
# Usage: KEYID=<gpg key id> [APT_GPG_PASSPHRASE=...] scripts/build-apt-repo.sh <site-dir>
set -euo pipefail

SITE="${1:?usage: build-apt-repo.sh <site-dir>}"
: "${KEYID:?KEYID must name the GPG signing key}"

cd "$SITE"
for arch in amd64 arm64; do
  mkdir -p "dists/stable/main/binary-$arch"
  apt-ftparchive --arch "$arch" packages pool > "dists/stable/main/binary-$arch/Packages"
  gzip -9kf "dists/stable/main/binary-$arch/Packages"
done

apt-ftparchive \
  -o APT::FTPArchive::Release::Origin=Ariaservice \
  -o APT::FTPArchive::Release::Label=arianet \
  -o APT::FTPArchive::Release::Suite=stable \
  -o APT::FTPArchive::Release::Codename=stable \
  -o APT::FTPArchive::Release::Architectures="amd64 arm64" \
  -o APT::FTPArchive::Release::Components=main \
  release dists/stable > dists/stable/Release

GPG_ARGS=(--batch --yes --pinentry-mode loopback -u "$KEYID")
if [ -n "${APT_GPG_PASSPHRASE:-}" ]; then
  GPG_ARGS+=(--passphrase "$APT_GPG_PASSPHRASE")
fi
gpg "${GPG_ARGS[@]}" --clearsign -o dists/stable/InRelease dists/stable/Release
gpg "${GPG_ARGS[@]}" --armor --detach-sign -o dists/stable/Release.gpg dists/stable/Release

gpg --batch --yes --export "$KEYID" > arianet-archive-keyring.gpg
gpg --batch --yes --armor --export "$KEYID" > arianet-archive-keyring.asc
touch .nojekyll
