# Arianet CLI

The official command-line client for the [Ariaservice / Arianet](https://ariaservice.net) cloud
platform. It talks to the public REST API at `https://api.ariaservice.net` and lets you manage your
own servers, SSH keys, firewalls, invoices and account balance from a terminal or a script.

It is a single static binary, MIT licensed, and the source in this repository is everything that
ships. See [What it does and does not do](#what-it-does-and-does-not-do) before installing.

---

## Installation

### Ubuntu / Debian (apt)

```bash
sudo install -d -m 0755 /etc/apt/keyrings
curl -fsSL https://ariaservice.github.io/arianet-cli/arianet-archive-keyring.gpg \
  | sudo tee /etc/apt/keyrings/arianet.gpg >/dev/null
echo "deb [signed-by=/etc/apt/keyrings/arianet.gpg] https://ariaservice.github.io/arianet-cli stable main" \
  | sudo tee /etc/apt/sources.list.d/arianet.list
sudo apt update && sudo apt install arianet
```

The repository is signed. The key is scoped to this one repository through `signed-by`, so it is not
trusted for anything else on your system. Its fingerprint is
`305A 2F52 0920 153A FC89  8F4A C420 B09C 99E5 515B`; the public key is also committed at
[packaging/apt-signing-key.asc](packaging/apt-signing-key.asc) so you can compare it. The package
contains the binary, shell completions and the licence only, and has **no install or removal
scripts**, so nothing runs as root when you install or remove it.

### macOS and Linux (Homebrew)

```bash
brew tap ariaservice/arianet-cli https://github.com/ariaservice/arianet-cli
brew install arianet
```

### Install script

```bash
curl -fsSL https://raw.githubusercontent.com/ariaservice/arianet-cli/main/scripts/install.sh | sh
```

[Read the script first](scripts/install.sh): it downloads the release archive, verifies its SHA-256
against `checksums.txt` (and the signature too if you have `cosign`), and copies one file into
`~/.local/bin` (or `/usr/local/bin` when writable). Nothing else.

### Manual download and verification

Download the archive for your platform plus `checksums.txt` from the
[releases page](https://github.com/ariaservice/arianet-cli/releases), then:

```bash
sha256sum --check --ignore-missing checksums.txt        # macOS: shasum -a 256 -c --ignore-missing checksums.txt
tar -xzf arianet_<version>_<os>_<arch>.tar.gz
sudo install -m 0755 arianet /usr/local/bin/arianet
```

Optionally verify that `checksums.txt` was produced by this repository's release workflow
(keyless [Sigstore](https://www.sigstore.dev/) signature, no key to trust or rotate):

```bash
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/ariaservice/arianet-cli/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

Each release also ships an SBOM (`*.sbom.json`) for every archive.

### Build from source

```bash
git clone https://github.com/ariaservice/arianet-cli.git
cd arianet-cli
go build -trimpath -o arianet ./cmd/arianet
```

---

## What it does and does not do

- Sends HTTPS requests to **one host**, the API URL you configured (default
  `https://api.ariaservice.net`), with your API token as a bearer token. Nothing else is contacted.
- **No telemetry, no analytics, no update checks, no background processes.**
- Does not execute other programs, does not read your SSH private keys (`ssh add` reads only the
  public key file you point it at), does not modify anything outside its own config file.
- Writes exactly one file: `config.yaml` in your user config directory, mode `0600` inside a `0700`
  directory. The API token is the only secret in it. You can avoid the file entirely by using the
  `ARIANET_TOKEN` environment variable.
- Refuses to send the token over plain `http://` (except to `localhost` for development), refuses
  an API URL that embeds credentials, and does not follow redirects to another host.
- Strips terminal control characters from anything the server returns before printing it.

Tokens are created and revoked at https://cloud.ariaservice.net/users/api-tokens; use a token with
only the scopes you need. See [SECURITY.md](SECURITY.md) to report a vulnerability.

---

## Quick Start

**1. Configure your API token** (typed without echo, stored with `0600` permissions):

```bash
arianet configure
# or, from a script / password manager:
printf '%s' "$ARIANET_TOKEN" | arianet configure --token-stdin
```

Avoid `--token <value>` on shared machines: command-line arguments are visible to other local users
and end up in shell history.

**2. Verify authentication:**

```bash
arianet auth whoami
```

**3. List your servers:**

```bash
arianet server list
```

---

## Configuration

Configuration is stored in `config.yaml` inside your user config directory (`~/.config/arianet/` on Linux, `~/Library/Application Support/arianet/` on macOS). `arianet configure show` prints the exact path.

```bash
arianet configure --token <token>     # Set API token
arianet configure show                # Show current configuration
```

You can also use environment variables:

| Variable          | Description             |
|-------------------|-------------------------|
| `ARIANET_TOKEN`   | API authentication token |
| `ARIANET_API_URL` | Override the API base URL (default `https://api.ariaservice.net`) |

---

## Commands

Every command accepts `--help` with examples. IDs for `--region`, `--plan`,
`--os` and `--ssh-key` come from the matching `list` commands.

### Authentication

```bash
arianet auth whoami               # Show current authenticated user
arianet auth logout               # Revoke current token and log out
arianet auth tokens list          # List all API tokens
arianet auth tokens revoke <id>   # Revoke a token by ID
```

### Catalog

```bash
arianet region list                     # Locations; the ID column is the --region value
arianet plan list --region <id>         # Plans orderable in a region
arianet plan get <id>                   # Plan details and pricing
arianet os list --region <id>           # OS templates available in a region
```

### Servers

```bash
arianet server list [--status active] [--page 2 --limit 20]
arianet server get <id>
arianet server status <id>
arianet server actions <id> [--limit 50]     # History of operations and their outcome

arianet server create                        # Interactive wizard
arianet server create --plan 5 --region 1 --os 3 --hostname web-01
arianet server create --plan 5 --region 1 --os 3 --ssh-key 2     # Log in with an SSH key
arianet server create --plan 5 --region 1 --os 3 --password '...' # Choose the root password
arianet server create --plan 5 --region 1 --os 3 --no-wait --yes --output json

arianet server restart <id>   [--wait] [--timeout 15m]
arianet server power-on <id>  [--wait]
arianet server power-off <id> [--wait]
arianet server reinstall <id> --os <id> [--wait]
arianet server delete <id>    [--wait]
arianet server rename <id> --name <1-30 chars>
arianet server toggle-protection <id> [--enable | --disable]
```

**Creating a server**

- Without `--plan`, `--region` or `--os` an interactive wizard asks for them.
- With neither `--ssh-key` nor `--password`, a strong random root password is
  generated and printed **once** - save it.
- The command waits until the server is active (up to `--timeout`, default 15m).
  `--no-wait` returns as soon as the order is accepted.
- Every order carries an idempotency key, so a retry after a network failure
  cannot buy a second server. If the command is interrupted before it can tell
  you the outcome, re-run it with the `--idempotency-key` it printed; the API
  replays the original result. The server creation rate is limited, and the
  CLI reports how long to wait when the limit is hit.

Restart, power, reinstall and delete return as soon as the platform has
accepted the operation. Add `--wait` to follow it until it finishes; progress
goes to stderr and the exit code is non-zero if it fails or times out.

### SSH Keys

```bash
arianet ssh list [--page 2 --limit 20]
arianet ssh get <id>
arianet ssh add --name laptop --key-file ~/.ssh/id_ed25519.pub --region <id>
arianet ssh add --name laptop --key "ssh-ed25519 AAAA..." --region <id>
arianet ssh update <id> --name <new-name>
arianet ssh delete <id>
```

A key belongs to one region, so `--region` is required when adding.

### Firewalls

```bash
arianet firewall list
arianet firewall get <id>                      # Rules, with the Rule ID column
arianet firewall create --name web-fw --region <id> [--description "..."]
arianet firewall update <id> --name <name> [--description "..."]
arianet firewall delete <id>

arianet firewall rule add <id> --direction ingress --proto tcp --port 443 --remote-ip 0.0.0.0/0
arianet firewall rule add <id> --direction ingress --proto icmp --remote-ip 0.0.0.0/0
arianet firewall rule remove <id> <rule-id>    # Rule ID is a string, from `firewall get`

arianet firewall attach <id> --server 42,43    # Apply to servers
arianet firewall detach <id> --server 42
```

`--remote-ip` is required on purpose: pass `0.0.0.0/0` to allow everyone.
`--port` is required for `tcp` and `udp`.

### Balance & Invoices

```bash
arianet balance                               # Wallet balance
arianet balance transactions [--page 2]
arianet balance invoices [--status paid] [--page 2 --limit 20]
arianet balance invoices get <number>         # Invoice with its line items
```

---

## Global Flags

| Flag           | Description                                          |
|----------------|------------------------------------------------------|
| `-o, --output` | Output format: `table` (default) or `json`           |
| `--token`      | Override API token for this command                  |
| `--api-url`    | Override API base URL for this command               |

---

## Scripting

```bash
arianet server list -o json | jq '.[].id'
```

- With `-o json`, stdout carries only JSON. Progress, prompts and wait output go
  to stderr. Pass `--yes` in scripts so nothing waits for input.
- Errors are written to stderr as `{"success":false,"error":{"code","message","status"}}`.
- Exit code `0` means success, `1` any failure (including a `--wait` that
  failed or timed out).
- Rate limits: when the API answers `RATE_LIMIT_EXCEEDED` the message says how
  long to wait. Reads are retried automatically on transient gateway errors;
  state-changing commands other than `create` are never retried automatically.

---

## Support

- Documentation: https://docs.ariaservice.net/cli
- Issues: https://github.com/ariaservice/arianet-cli/issues
- Support: https://ariaservice.net
