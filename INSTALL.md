# Installation

Wardgate has three binaries:

| Binary | Purpose |
|--------|---------|
| `wardgate` | The gateway server -- proxies API calls and routes conclave commands |
| `wardgate-cli` | Agent-side tool -- restricted HTTP client and conclave exec; replaces `curl` |
| `wardgate-exec` | Runs inside each conclave -- connects to Wardgate, executes commands |

## Pre-built Binaries

Download from [GitHub Releases](https://github.com/wardgate/wardgate/releases). Each release includes archives for:

- **Platforms:** Linux, macOS, Windows
- **Architectures:** amd64, arm64

```bash
# Example: download wardgate for Linux amd64
curl -LO https://github.com/wardgate/wardgate/releases/latest/download/wardgate_VERSION_linux_amd64.tar.gz
tar xzf wardgate_VERSION_linux_amd64.tar.gz
sudo mv wardgate /usr/local/bin/
```

Replace `VERSION` with the actual release version.

## Go Install

Requires Go 1.22+.

```bash
go install github.com/wardgate/wardgate/cmd/wardgate@latest
go install github.com/wardgate/wardgate/cmd/wardgate-cli@latest
go install github.com/wardgate/wardgate/cmd/wardgate-exec@latest
```

**Note:** `wardgate-cli` installed via `go install` uses the default config path (`/etc/wardgate-cli/config.yaml`). For a custom path, build from source with `-ldflags` (see below).

## Build from Source

Requires Go 1.22+.

```bash
git clone https://github.com/wardgate/wardgate.git
cd wardgate

go build -o wardgate ./cmd/wardgate
go build -o wardgate-cli ./cmd/wardgate-cli
go build -o wardgate-exec ./cmd/wardgate-exec
```

### Custom config path for wardgate-cli

`wardgate-cli` has a fixed config path compiled into the binary so agents cannot override it. Set it at build time:

```bash
# Production (agent container)
go build -ldflags "-X main.configPath=/etc/wardgate-cli/config.yaml" -o wardgate-cli ./cmd/wardgate-cli

# Local development
go build -ldflags "-X main.configPath=$HOME/.wardgate-cli.yaml" -o wardgate-cli ./cmd/wardgate-cli
```

## Docker

### Wardgate Server

Use the included `Dockerfile` or the pre-built image from GoReleaser:

```bash
# Pre-built image
docker pull avoutic/wardgate:latest

# Or build locally
docker build -t wardgate .
```

### Docker Compose

The included `docker-compose.yml` runs Wardgate with Caddy for automatic HTTPS:

```bash
# Create config directory
mkdir -p config
cp config.yaml.example config/config.yaml
cp .env.example config/.env

# Edit config/config.yaml and config/.env with your settings

# Run
docker compose up -d

# Or with a custom domain
DOMAIN=wardgate.example.com docker compose up -d
```

See [Deployment Guide](docs/deployment.md) for more options.

## wardgate-cli Setup

Create a config file at the path compiled into the binary (default: `/etc/wardgate-cli/config.yaml`):

```yaml
server: http://wardgate:8080
key_env: WARDGATE_AGENT_KEY
```

Or with the key directly:

```yaml
server: http://wardgate:8080
key: "your-agent-key"
```

**Custom CA:** If Wardgate uses HTTPS with an internal CA, add `ca_file`:

```yaml
server: https://wardgate.internal:443
key_env: WARDGATE_AGENT_KEY
ca_file: /etc/wardgate-cli/ca.pem
```

**Security notes:**
- Mount the config read-only in the agent container
- Run the agent as a non-root user that cannot write to the config directory
- The config path is fixed at build time -- agents cannot override it

See [wardgate-cli documentation](docs/wardgate-cli.md) for full details. To teach an AI agent how to use `wardgate-cli`, copy the [wardgate-cli AI Skill](skills/wardgate-cli/SKILL.md) into your agent's skill/tool directory.

## wardgate-exec / Conclave Setup

Each conclave runs `wardgate-exec` in an isolated container. Build the conclave image:

```bash
docker build -f Dockerfile.conclave -t wardgate-conclave .
```

Create a config file for the conclave (e.g., `config/conclave-obsidian.yaml`):

```yaml
server: wss://wardgate.example.com/conclaves/ws
key: "conclave-secret-key"
name: obsidian
cwd: /data/vault

# Optional: local allowlist (defense in depth)
allowed_bins:
  - cat
  - rg
  - head
  - ls
  - tee

# Optional: output limits
max_input_bytes: 1048576    # 1MB (default)
max_output_bytes: 10485760  # 10MB (default)
```

Run the conclave container:

```bash
docker run -d \
  -v /path/to/data:/data:ro \
  -v ./config/conclave-obsidian.yaml:/etc/wardgate-exec/config.yaml:ro \
  wardgate-conclave
```

Or add it to `docker-compose.yml`:

```yaml
services:
  conclave-obsidian:
    build:
      context: .
      dockerfile: Dockerfile.conclave
    volumes:
      - /path/to/obsidian/vault:/data:ro
      - ./config/conclave-obsidian.yaml:/etc/wardgate-exec/config.yaml:ro
    networks:
      - wardgate-net
    depends_on:
      - wardgate
```

**Deployment principles:**
- Mount data read-only when the conclave only needs to read (`:ro`)
- Mount config read-only so `wardgate-exec` config cannot be tampered with
- Run as non-root (the `Dockerfile.conclave` creates a dedicated `conclave` user)
- No inbound ports -- `wardgate-exec` connects outbound to Wardgate
- Use `allowed_bins` in the `wardgate-exec` config as a second layer of defense

See [Conclaves documentation](docs/conclaves.md) for full details on policy rules, pipeline support, and security model.

## Quick Start

```bash
cp .env.example .env                 # Add your credentials
cp config.yaml.example config.yaml   # Configure endpoints and conclaves

./wardgate -config config.yaml

# Or specify a different env file
./wardgate -config config.yaml -env /path/to/.env
```

See [Configuration Reference](docs/config.md) for all options.
