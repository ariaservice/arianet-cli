# Arianet CLI

The official command-line client for the [Ariaservice / Arianet](https://ariaservice.net) cloud
platform. It talks to the public REST API at `https://api.ariaservice.net` and lets you manage your
own VPS, Arianet Cloud resources, SSH keys, firewalls, invoices and account balance from a
terminal or a script.

It is a single static binary, MIT licensed, and the source in this repository is everything that
ships. See [What it does and does not do](#what-it-does-and-does-not-do) before installing.

---

## Installation

**macOS / Linux** (terminal):

```bash
curl -fsSL https://raw.githubusercontent.com/ariaservice/arianet-cli/main/scripts/install.sh | sh
```

**Windows** (PowerShell, no Administrator needed):

```powershell
irm https://raw.githubusercontent.com/ariaservice/arianet-cli/main/scripts/install.ps1 | iex
```

Then run `arianet configure` and paste an API token from
<https://cloud.ariaservice.net/users/api-tokens>; `arianet vps list` shows your servers.

Both scripts download the release archive from this repository, verify its SHA-256 against
`checksums.txt`, and copy a single binary; read them first if you like
([install.sh](scripts/install.sh), [install.ps1](scripts/install.ps1)).

- `install.sh` installs into `/usr/local/bin` when writable, otherwise `~/.local/bin`, and also
  verifies the Sigstore signature when `cosign` is installed. Environment: `VERSION`, `INSTALL_DIR`.
- `install.ps1` installs `arianet.exe` into `%LOCALAPPDATA%\Programs\arianet` and adds that folder
  to your user `PATH` (open a new terminal window afterwards). It works in Windows PowerShell 5.1
  and PowerShell 7. Environment: `ARIANET_VERSION` (e.g. `0.5.0`), `ARIANET_INSTALL_DIR`. Only a
  64-bit x64 build is published; on Windows 11 on ARM the x64 build runs through emulation.

Other ways to install:

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
brew trust ariaservice/arianet   # recent Homebrew versions require trusting third-party taps
brew install ariaservice/arianet/arianet
```

The formula lives in [ariaservice/homebrew-arianet](https://github.com/ariaservice/homebrew-arianet).
It only downloads the release archive from this repository and checks its SHA-256.

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

### Upgrading

```bash
arianet upgrade            # alias: arianet update
arianet upgrade --check    # only report; exit 0 = up to date, 2 = newer release available, 1 = check failed
arianet upgrade --yes      # no confirmation prompt (needed when not in a terminal)
arianet upgrade --version v0.4.0 --allow-downgrade   # a specific release (standalone/rpm installs)
```

`arianet upgrade` upgrades the way you installed:

| Installed with | What `arianet upgrade` does |
|---|---|
| Homebrew (macOS, Linuxbrew) | runs `brew upgrade ariaservice/arianet/arianet` |
| apt | runs `sudo apt-get update && sudo apt-get install --only-upgrade -y arianet` in a terminal; otherwise prints that command and exits 1 |
| .rpm | downloads the release `.rpm`, verifies it, runs `sudo rpm -U` (prints the command when not in a terminal) |
| install.sh, install.ps1, manual download, Windows `.exe` | downloads the release archive for your OS/architecture, verifies its SHA-256 against `checksums.txt` (and the Sigstore signature when `cosign` is installed), checks that the new binary runs, then swaps it in atomically |

The swap writes the new binary next to the old one and renames it into place, so a failed
upgrade leaves the old binary untouched. On Windows the running `arianet.exe` is moved
aside to `arianet.exe.old` (removed on the next run). If the binary's directory is not
writable, re-run with `sudo arianet upgrade` (Windows: a terminal opened as Administrator).

### Build from source

```bash
git clone https://github.com/ariaservice/arianet-cli.git
cd arianet-cli
go build -trimpath -o arianet ./cmd/arianet
```

---

## What it does and does not do

- Sends HTTPS requests to **one host**, the API URL you configured (default
  `https://api.ariaservice.net`), with your API token as a bearer token (public endpoints such as
  `cloud pricing` get no token at all). Nothing else is contacted.
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

**3. List your VPS:**

```bash
arianet vps list
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
| `ARIANET_NO_UPDATE_NOTIFIER` | Set to any value to turn off the new-release notice |

When a newer release is published, `arianet` prints a short notice suggesting
`arianet upgrade` to stderr after a command. It checks GitHub at most once a day in the background, never delays or fails a
command, and stays quiet with `-o json`, when stderr is not a terminal, when `CI` is set,
or when `ARIANET_NO_UPDATE_NOTIFIER` is set.

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

### VPS

`arianet vps` manages virtual private servers; `server`, `servers` and `srv` still work as
aliases, so older scripts keep running.

```bash
arianet vps list [--status active] [--page 2 --limit 20]
arianet vps get <id>
arianet vps status <id>
arianet vps actions <id> [--limit 50]     # History of operations and their outcome

arianet vps create                        # Interactive wizard
arianet vps create --plan 5 --region 1 --os 3 --hostname web-01
arianet vps create --plan 5 --region 1 --os 3 --ssh-key 2     # Log in with an SSH key
arianet vps create --plan 5 --region 1 --os 3 --password '...' # Choose the root password
arianet vps create --plan 5 --region 1 --os 3 --no-wait --yes --output json

arianet vps restart <id>   [--wait] [--timeout 15m]
arianet vps power-on <id>  [--wait]
arianet vps power-off <id> [--wait]
arianet vps reinstall <id> --os <id> [--wait]
arianet vps delete <id>    [--wait]
arianet vps rename <id> --name <1-30 chars>
arianet vps toggle-protection <id> [--enable | --disable]
```

**Creating a VPS**

- Without `--plan`, `--region` or `--os` an interactive wizard asks for them.
- With neither `--ssh-key` nor `--password`, a strong random root password is
  generated and printed **once** - save it.
- The command waits until the VPS is active (up to `--timeout`, default 15m).
  `--no-wait` returns as soon as the order is accepted.
- Every order carries an idempotency key, so a retry after a network failure
  cannot buy a second VPS. If the command is interrupted before it can tell
  you the outcome, re-run it with the `--idempotency-key` it printed; the API
  replays the original result. The VPS creation rate is limited, and the
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

### Arianet Cloud

`arianet cloud` manages Arianet Cloud resources through the cloud API at
`<api-url>/cloud/v1` (default `https://api.ariaservice.net/cloud/v1`), with the
same token. The token needs the scopes **`cloud:servers:read`** for reads and
**`cloud:servers:write`** for changes.

```bash
arianet cloud datacenters                      # Locations (the --datacenter codes)
arianet cloud plans [--datacenter <code>]      # Server types (the --plan IDs)
arianet cloud images list [--type system|app|snapshot|backup] [--datacenter <code>]
arianet cloud images get <id|image>
arianet cloud pricing [--datacenter <code>]      # Public price list, no token needed
arianet cloud pricing --account                 # Your account's price rows
arianet cloud limits [--datacenter <code>]

arianet cloud servers list [--label-selector env=prod] [--page 1 --per-page 50]
arianet cloud servers get <id>
arianet cloud servers create --name web-1 --datacenter <code> --plan <id> --image ubuntu-24.04 \
    [--ssh-key-id 5] [--labels env=prod] [--firewall 2] [--network 4] [--no-ipv6] \
    [--count 3] [--backups] [--user-data-file cloud-init.yaml] [--project-id 2] [--wait]
arianet cloud servers update <id> [--name n] [--labels k=v | --clear-labels] [--protection-delete=true]
arianet cloud servers delete <id> [--reason price --note "..."] [--keep-backups] [--delete-snapshots] [--yes] [--wait]
arianet cloud servers power-on|power-off|shutdown|reboot|reset <id> [--wait]
arianet cloud servers rebuild <id> --image <image>
arianet cloud servers change-type <id> --plan <id>          # alias: resize
arianet cloud servers enable-rescue|disable-rescue|reset-password <id>
arianet cloud servers change-ipv4 <id> [--confirm-paid]
arianet cloud servers attach-network <id> --network <id> [--ip 10.0.1.5]
arianet cloud servers detach-network <id> --network <id>
arianet cloud servers enable-backup <id> [--window-hour 3] | disable-backup <id>
arianet cloud servers create-backup <id> | restore-backup <id> --backup <id>
arianet cloud servers create-image <id> --name before-upgrade
arianet cloud servers actions <id>
arianet cloud servers costs <id>                # Hourly/monthly cost per billed piece + totals

arianet cloud volumes list|get|create|update|delete
arianet cloud volumes attach <id> --server <id> | detach <id> | resize <id> --size 100

arianet cloud networks list|get|create|update|delete
arianet cloud networks subnets add <id> --ip-range 10.0.2.0/24 | subnets delete <id> <subnet-id>
arianet cloud networks routes set <id> --route 10.100.0.0/16=10.0.1.5 | --clear

arianet cloud firewalls list|get|update|delete
arianet cloud firewalls create --name web --rule direction=in,protocol=tcp,port=443,source=0.0.0.0/0,source=::/0
arianet cloud firewalls rules set <id> --rules-file rules.json | --rule ... | --clear
arianet cloud firewalls apply-to <id> --server 12 --label-selector "env=prod,tier!=db"
arianet cloud firewalls remove-from <id> --label-selector env=prod
arianet cloud firewalls set-servers <id> --server 12,13 | --none

arianet cloud floating-ips list|get|create|update|delete
arianet cloud floating-ips assign <id> --server <id> | unassign <id>
arianet cloud floating-ips dns-ptr <id> --ptr host.example.com | --reset

arianet cloud load-balancers list|get|create|update|delete|types|health
arianet cloud load-balancers create --name web --datacenter <code> --type lb11 --service tcp:443 --target label_selector:role=web
arianet cloud load-balancers add-service|update-service|delete-service|set-services <id> ...
arianet cloud load-balancers add-target <id> --server <id> | --ip <ip> | --label-selector <sel>
arianet cloud load-balancers remove-target <id> --target-id <id>
arianet cloud load-balancers change-type|change-algorithm|attach-network|detach-network <id> ...

arianet cloud ssh-keys list|get|add|update|delete            # same as `arianet ssh`

arianet cloud actions list [--resource-type server --resource-id 12] [--status running]
arianet cloud actions get <id>
arianet cloud actions wait <id> [<id>...] [--timeout 30m]
```

- Every list takes `--label-selector`: comma-separated terms that must all hold,
  each `key=value`, `key!=value`, `key` (has the label) or `!key` (has not), and `--page`/`--per-page` (at most 100); without a page,
  every page is fetched. `--project-id` narrows a list to one project.
- `create` takes `--labels key=value` (repeatable) and `--project-id`.
- Operations run asynchronously and return an action. The command prints its ID;
  `--wait` follows it (progress on stderr) and exits non-zero if it fails or times out.
  With `-o json` the response is printed as the API returned it, plus the final
  `actions` after a wait.
- `cloud pricing` reads the public price list (`/cloud/v1/public/pricing`) and sends
  no token, so it works without `arianet configure`.
- `servers delete --keep-backups` keeps the finished backups as snapshots, billed per
  GB until deleted; `--delete-snapshots` removes the server's unprotected snapshots
  (by default snapshots survive the server).
- A root or rescue password returned by `create`, `rebuild`, `enable-rescue`,
  `reset-password` or `restore-backup` is printed once - save it.
- Reads that hit the rate limit are retried once the `Retry-After` time has passed
  (when that is at most 30 seconds); changes are never retried automatically.

### Hosting, regional VPS, dedicated servers and SMS

The other products are served by the gateway the same way as the cloud, at
`<api-url>/hosting/v1`, `/vps/v1`, `/baremetal/v1` and `/sms/v1`. Every command of
these groups takes `--region <code>` to call one regional install (default: the
product's default region), and `api <METHOD> <path> [--data JSON]` reaches any
endpoint the commands do not cover (see `<prefix>/openapi.json`).

```bash
# Shared hosting (scopes hosting:accounts:read / hosting:accounts:write)
arianet hosting datacenters | plans [--datacenter <code>] [--kind shared|reseller]
arianet hosting accounts list|get|usage|nameservers|traffic|actions <id>
arianet hosting accounts create --datacenter <code> --plan-id <id> --term monthly --domain example.com --password '...'
arianet hosting accounts delete|renew|login <id> | change-plan <id> --plan-id <id> | password <id> --password '...'
arianet hosting dns list|add|update|delete <account-id> ...
arianet hosting email list|add|update|delete <account-id> ...
arianet hosting databases list|create|delete|grant|users|user-create|user-password|user-delete <account-id> ...
arianet hosting ssh-keys list|add|delete <account-id> ...
arianet hosting actions list|get|wait

# Regional VPS, e.g. iran-1 (scopes vps:servers:read / vps:servers:write)
arianet vps regional datacenters | plans | images
arianet vps regional servers list|get|delete|start|stop|restart|kill|console|traffic|actions <id>
arianet vps regional servers create --datacenter <code> --plan-id <id> --image <image> --hostname web-1 --billing-mode hourly [--wait]
arianet vps regional servers reinstall <id> --image <image> | resize <id> --plan-id <id> | reset-password <id>
arianet vps regional actions list|get|wait

# Dedicated servers (scopes baremetal:servers:read / baremetal:servers:write)
arianet baremetal suppliers | ranges | plans | rules | options <catalog> <plan-code>
arianet baremetal quote --catalog eco --plan-code <code> --datacenter <dc> --memory <m> --storage <s> --bandwidth <b>
arianet baremetal orders list|get|cancel <id> | create ... --rules-version <v> --accept-rules
arianet baremetal servers list|get|credentials|renewals|renew|os-templates|reinstall-status <id>
arianet baremetal servers reinstall <id> --template <os>

# SMS (scopes sms:messages:read|write, sms:lines:read, sms:templates:read|write)
arianet sms send --line <id> --to 0912... [--to ...] --text "..."
arianet sms messages list|get | batches list|get | lines list | inbound | scheduled | report
arianet sms templates list|create|update|delete
arianet sms blocked list|add|delete
```

`arianet vps` (without `regional`) keeps managing the original VPS servers of
`/api/v1/servers`; the regional VPS product has its own API and lives under
`arianet vps regional`.

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
arianet vps list -o json | jq '.[].id'
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
