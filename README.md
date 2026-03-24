# Arianet CLI

The official command-line interface for the [Arianet](https://ariaservice.net) cloud platform. Manage your cloud servers, SSH keys, firewalls, and account balance directly from the terminal.

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
go build -o arianet ./main.go
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

Configuration is stored at `~/.arianet/config.yaml`.

```bash
arianet configure --token <token>     # Set API token
arianet configure show                # Show current configuration
```

You can also use environment variables:

| Variable          | Description             |
|-------------------|-------------------------|
| `ARIANET_TOKEN`   | API authentication token |
| `ARIANET_API_URL` | Override the API base URL |

---

## Commands

### Authentication

```bash
arianet auth whoami               # Show current authenticated user
arianet auth logout               # Revoke current token and log out
arianet auth tokens list          # List all API tokens
arianet auth tokens revoke <id>   # Revoke a token by ID
```

### Servers

```bash
arianet server list                        # List all your cloud servers
arianet server get <id>                    # Get details of a server
arianet server create                      # Create a new server (interactive wizard)
arianet server create \
  --datacenter <id> \
  --plan <id> \
  --os <id> \
  --hostname myserver                      # Create with flags (non-interactive)
arianet server delete <id>                 # Delete a server
arianet server status <id>                 # Check server status
arianet server restart <id>                # Restart a server
arianet server power-on <id>               # Power on a server
arianet server power-off <id>              # Power off a server
arianet server rename <id> --hostname <h>  # Rename a server
arianet server reinstall <id> --os <id>    # Reinstall OS on a server
arianet server toggle-protection <id>      # Toggle deletion protection
```

**Creating a server** — if you omit flags, an interactive wizard guides you through selecting a region, plan, OS, and hostname. After creation, the CLI automatically polls the server status every 30 seconds until it becomes active.

### SSH Keys

```bash
arianet ssh list              # List your SSH keys
arianet ssh get <id>          # Get details of an SSH key
arianet ssh add \
  --name <name> \
  --key <public-key>          # Add a new SSH key
arianet ssh update <id> \
  --name <new-name>           # Update an SSH key
arianet ssh delete <id>       # Delete an SSH key
```

### Firewalls

```bash
arianet firewall list                              # List your firewalls
arianet firewall get <id>                          # Get firewall details and rules
arianet firewall create --name <name>              # Create a new firewall
arianet firewall delete <id>                       # Delete a firewall
arianet firewall rule add <firewall-id> \
  --port <port> \
  --protocol <tcp|udp> \
  --source <cidr>                                  # Add a rule to a firewall
arianet firewall rule remove <firewall-id> <index> # Remove a rule by index
```

### Balance & Transactions

```bash
arianet balance                          # Show account balance
arianet balance transactions             # View transaction history
arianet balance transactions --page 2    # Paginate results
```

### Catalog

```bash
arianet region list                     # List available regions/datacenters
arianet plan list                       # List all available plans
arianet plan get <id>                   # Get plan details and pricing
arianet os list                         # List available OS templates
```

---

## Global Flags

| Flag          | Description                                      |
|---------------|--------------------------------------------------|
| `-o, --output` | Output format: `table` (default) or `json`      |
| `--token`      | Override API token for this command              |
| `--api-url`    | Override API base URL for this command           |

---

## Output Formats

```bash
arianet server list -o table   # Default — AWS CLI-style table
arianet server list -o json    # JSON output for scripting
```

---

## Support

- Documentation: https://docs.arianet.ir/cli
- Issues: https://github.com/ariaservice/arianet-cli/issues
- Support: https://ariaservice.net
