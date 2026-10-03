# Security policy

## Reporting a vulnerability

Please report security problems privately through GitHub:
**Security tab → "Report a vulnerability"** on https://github.com/ariaservice/arianet-cli, rather than
opening a public issue. Include the CLI version (`arianet version`), your OS and steps to reproduce.

We aim to acknowledge reports within 3 working days.

Problems with the API service itself (not this client) can be reported the same way or through
https://ariaservice.net.

## Supported versions

Only the latest release receives fixes.

## How releases are protected

- Releases are built by the GitHub Actions workflow in `.github/workflows/release.yml` from a
  version tag; third-party actions are pinned to commit SHAs.
- `checksums.txt` is signed keyless with Sigstore/cosign, bound to that workflow's identity, and
  covers every archive and package. An SBOM is published for each archive.
- The apt repository is signed with a dedicated key (fingerprint in the README) that is used for
  nothing else.
- Packages contain no maintainer scripts. The binary is built with `CGO_ENABLED=0 -trimpath`.
- CI runs `go vet`, the tests (with the race detector) and `govulncheck` on every change.

## What the client protects against

- The API token is stored `0600` (directory `0700`), written atomically, and masked in
  `arianet configure show`.
- The token is only sent over HTTPS (loopback `http://` excepted), and is never forwarded on a
  redirect to another host.
- Server-supplied text is stripped of terminal escape sequences before it is printed.
- Response bodies are size-limited.
