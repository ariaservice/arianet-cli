# Arianet CLI

The official command-line interface for the [Arianet](https://ariaservice.net) cloud platform. Manage your cloud servers, SSH keys, firewalls, invoices and account balance directly from the terminal.

---

## Installation

### Download binary

Download the latest release from the [releases page](https://github.com/ariaservice/arianet-cli/releases) and place it in your `PATH`:

```bash
# macOS / Linux
chmod +x arianet
sudo mv arianet /usr/local/bin/
```

### Build from source

```bash
git clone https://github.com/ariaservice/arianet-cli.git
cd arianet-cli
go build -o arianet ./cmd/arianet
sudo mv arianet /usr/local/bin/
```

---

## Quick Start

**1. Configure your API token:**

```bash
arianet configure --token <your-api-token>
```

Generate a token at: https://cloud.ariaservice.net/users/api-tokens

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
