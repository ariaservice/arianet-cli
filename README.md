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
- Writes exactly one file of its own: `config.yaml` in your user config directory, mode `0600` inside
  a `0700` directory. The API token is the only secret in it. You can avoid the file entirely by
  using the `ARIANET_TOKEN` environment variable. (`baas db pull` and `baas storage download` write
  the file you ask them for.)
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
arianet sms send --from <line-id> --to 0912... [--to ...] --body "..." | --template <id> [--at <ISO time>] [--reference <ref>]
arianet sms messages list [--status s] [--batch id] [--to n] | get <id>
arianet sms batches list|get | inbound | prices | report [--from day] [--to day]
arianet sms scheduled list|cancel <id>
arianet sms lines list|request [--number n] [--note "..."]
arianet sms templates list|get|create|update|delete
arianet sms blocked list|add|delete
arianet sms webhook get|set --url <url>|rotate-secret
# Lists take --page and --per-page (at most 200).
```

`arianet vps` (without `regional`) keeps managing the original VPS servers of
`/api/v1/servers`; the regional VPS product has its own API and lives under
`arianet vps regional`.

### BaaS (database, auth, storage, edge functions)

`arianet baas` manages Arianet BaaS projects through `<api-url>/baas/v1`. A project is a
PostgreSQL database with a public endpoint (REST, auth, storage) and its keys; projects run in
region `thr-1`. Token scopes: `baas:projects:read` (reads, including `migration list`, `db pull`,
`db diff`, `gen types`, `advisors`), `baas:projects:manage` (create, rename, delete, pause, resume,
plan, `domains`, `transfer`, `api-settings set`), `baas:keys:manage` (reveal and rotate keys,
`env`), `baas:sql:execute` (`sql`, `db push`, `db pull --mark-applied`), `baas:storage:manage`
(storage writes), `baas:auth-users:manage` (auth users create and delete), `baas:functions:manage`
(functions deploy, delete, download and secrets changes).

Run `arianet baas link` once in your app's directory: it writes `arianet/.baas/project.json`
(project ID, ref, name, endpoint, region; no secret) and every command below that takes the
project ID as its first argument uses the linked project when the ID is left out. An explicit
ID still wins; `arianet baas unlink` removes the link.

```bash
# Projects
arianet baas plans
arianet baas projects list | get <id>
arianet baas projects create --name shop --plan free [--region thr-1] [--wait]
arianet baas projects rename <id> --name <name> | pause <id> | resume <id> | plan <id> <plan-code>
arianet baas projects delete <id> --confirm <ref>        # the ref from "projects get"
arianet baas actions list|get|wait

# Link this directory to a project (then <id> can be left out)
arianet baas link [<id> | <ref>]
arianet baas unlink

# Keys (reveals are recorded in your activity log)
arianet baas keys list <id>
arianet baas keys reveal <id> anon|service|publishable|secret|database
arianet baas keys rotate <id>

# SQL: one transaction, the rows of the last statement that returned rows
arianet baas sql <id> "select * from todos limit 10"
arianet baas sql <id> -f report.sql --read-only --json

# Migrations: *.sql files of arianet/migrations in name order, tracked in baas_cli.migrations
arianet baas migration new create_todos                 # arianet/migrations/<UTC timestamp>_create_todos.sql
arianet baas migration list <id>                        # applied | pending | changed | remote only
arianet baas db push <id> [--dir arianet/migrations] [--dry-run]
arianet baas db pull <id> [--dir arianet/migrations | --file -] [--schema public,app] [--mark-applied]
arianet baas db diff <id> [--against schema.sql]        # exit 0: no difference, 2: differences

# TypeScript types (Tables: Row / Insert / Update, Relationships, Views, Functions, Enums)
arianet baas gen types <id> --lang typescript [--schema public] [--file src/database.types.ts]
arianet baas types <id> > src/database.types.ts         # alias of gen types

# Environment for your app
arianet baas env <id> > .env.local          # or --format json

# Storage and end users
arianet baas storage buckets <id> | buckets create <id> --name avatars [--public] | buckets delete <id> <bucket>
arianet baas storage ls <id> <bucket> [prefix]
arianet baas storage upload <id> <bucket> ./me.png [users/42/me.png] [--upsert]
arianet baas storage download <id> <bucket> users/42/me.png [local-file | -]
arianet baas storage rm <id> <bucket> <path>...
arianet baas auth users list|get|create|delete <id> ...

# Edge functions: code in arianet/functions/<name>/index.ts, shared code in arianet/functions/_shared/
arianet baas functions new hello-world
arianet baas functions serve [hello-world] [--env-file arianet/functions/.env] [--port 8000]   # needs Deno
arianet baas functions deploy <id> [name...] [--no-verify-jwt | --verify-jwt]   # no name: every function
arianet baas functions list <id> | get <id> <name> | delete <id> <name>
arianet baas functions download <id> <name> [--version 2] [--force]
arianet baas functions secrets list <id>
arianet baas functions secrets set <id> STRIPE_KEY=sk_live_123 [--env-file .env]
arianet baas functions secrets unset <id> STRIPE_KEY

# Logs and use
arianet baas logs <id> [--source gateway|auth|storage|rest|sql] [--since 1h] [--status 5]
arianet baas usage <id>

# Advisors, custom domains, transfer, API settings
arianet baas advisors <id> [--kind security|performance] [--run] [--details]
arianet baas domains list <id> | add <id> api.example.com | check <id> <domain-id> | remove <id> <domain-id>
arianet baas transfer <id> --to <account-id> --confirm <ref> | transfer status|cancel <id>
arianet baas transfer list [--direction incoming|outgoing] | accept|decline <transfer-id>
arianet baas api-settings get <id> | set <id> [--max-rows 500] [--jwt-expiry 7200]
```

`db push` sends every file to the project, which applies the ones not recorded yet in one
transaction, each followed by its record (name, SHA-256 checksum, time): if one fails nothing is
applied and the failing file is named. A file that was applied and changed since is refused: put
the change in a new file. Files must not contain `BEGIN`/`COMMIT`. `db pull` writes the project's
DDL (extensions, enums, sequences, tables, indexes, views, functions, triggers, row level
security, policies, comments) as `arianet/migrations/<timestamp>_remote_schema.sql`;
`--mark-applied` records it in the project so `db push` skips it. `db diff` compares the files
with the project's history and the project's schema now with the newest snapshot (a unified
diff); it exits 2 when there is a difference, for CI. A directory that still has `./migrations`
and no `./arianet/migrations` keeps using it (with a note). In a region that does not have the
migrations, schema or types endpoints yet, these commands fall back to the SQL endpoint and the
table introspection (one transaction per file; tables only).

`env` prints `ARIANET_BAAS_URL` and `ARIANET_BAAS_ANON_KEY` (revealed), plus the same values under
the public prefixes of common frameworks: `NEXT_PUBLIC_BAAS_URL` / `NEXT_PUBLIC_BAAS_ANON_KEY`
(Next.js), `VITE_BAAS_*` (Vite), `NUXT_PUBLIC_BAAS_*` (Nuxt) and `EXPO_PUBLIC_BAAS_*` (Expo). Keep
the lines your app reads; if your code expects other variable names, map them in your `.env`
(for example `MY_APP_API_URL=$NEXT_PUBLIC_BAAS_URL`) or pass the values to your client library's
constructor. The anon key is meant for browser code (row level security decides what it reads);
never put the service or secret key there.

`functions deploy` zips `arianet/functions/<name>/` (at `<name>/...`) with every folder whose
name starts with `_` (shared code), leaving out dotfiles and `node_modules`, and uploads it as
the function's new active version; the entrypoint is `<name>/index.ts` (or `.js`, `.mjs`,
`.tsx`, `.jsx`). A function answers at `https://<ref>.baas.arianet.app/functions/v1/<name>` and
by default needs a project JWT (`--no-verify-jwt` for webhooks). Functions get
`ARIANET_BAAS_URL`, `ARIANET_BAAS_ANON_KEY`, `ARIANET_BAAS_DB_URL` and the project's secrets as
environment variables. Limits (number of functions, bundle size, invocations a month) come
from the plan and are shown by `functions list`.

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
