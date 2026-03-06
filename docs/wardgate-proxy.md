# wardgate-proxy

A local reverse proxy for AI agents that transparently injects the agent key into every outgoing request. It sits between the agent and a Wardgate server, reading an agent key from config and adding it as an `Authorization: Bearer` header.

## Why It Exists

`wardgate-cli` replaces `curl` entirely and locks the agent to a single server. But some agents use their own HTTP clients (Python `requests`, Node `fetch`, etc.) and can't be forced to use a custom binary.

`wardgate-proxy` solves this differently: it runs as a local HTTP proxy on `127.0.0.1`. The agent makes plain HTTP requests to the proxy, and the proxy forwards them to Wardgate with the agent key injected. The agent never sees the key.

| Approach | How it works | Best for |
|----------|-------------|----------|
| `wardgate-cli` | Replaces curl; fixed config path compiled in | Sandboxed containers where you control the toolset |
| `wardgate-proxy` | Local proxy; any HTTP client works | Agents with their own HTTP clients, or multi-tool setups |

## How It Works

```
Agent (any HTTP client)
  → http://127.0.0.1:18080/todoist/tasks
    → wardgate-proxy reads agent key from config
      → https://wardgate.example.com/todoist/tasks (with Bearer <agent-key>)
```

1. Agent sends a request to `wardgate-proxy` (no auth required)
2. Proxy reads the agent key from config (cached by mtime, auto-rotated)
3. Proxy forwards the request to the Wardgate server with `Authorization: Bearer <agent-key>`
4. Response is streamed back to the agent (SSE and chunked transfer supported)

## Installation

### Pre-built Binaries

Download from [GitHub Releases](https://github.com/wardgate/wardgate/releases).

### Go Install

```bash
go install github.com/wardgate/wardgate/cmd/wardgate-proxy@latest
```

### Build from Source

```bash
go build -o wardgate-proxy ./cmd/wardgate-proxy
```

## Configuration

### Config File

Create `wardgate-proxy.yaml` (default path, or specify with `-config`):

```yaml
server: https://wardgate.example.com
key: "your-agent-key"
listen: 127.0.0.1:18080
```

Or use an environment variable for the key:

```yaml
server: https://wardgate.example.com
key_env: WARDGATE_AGENT_KEY
listen: 127.0.0.1:18080
```

**Custom CA:** If Wardgate uses HTTPS with an internal CA, add `ca_file`:

```yaml
server: https://wardgate.internal:443
key_env: WARDGATE_AGENT_KEY
ca_file: /etc/wardgate-proxy/ca.pem
```

### Config Fields

| Field | Description | Default |
|-------|-------------|---------|
| `server` | Wardgate server URL (required) | -- |
| `key` | Agent key (takes priority over `key_env`) | -- |
| `key_env` | Environment variable containing the agent key | -- |
| `listen` | Local address to listen on | `127.0.0.1:18080` |
| `ca_file` | Path to custom CA certificate (PEM) | -- |

One of `key` or `key_env` is required.

### CLI Flags

Flags override config file values:

```bash
wardgate-proxy \
  -config wardgate-proxy.yaml \
  -server https://wardgate.example.com \
  -key-env WARDGATE_AGENT_KEY \
  -listen 127.0.0.1:9090
```

| Flag | Description |
|------|-------------|
| `-config` | Path to config file (default: `wardgate-proxy.yaml`) |
| `-server` | Wardgate server URL |
| `-key-env` | Environment variable containing agent key |
| `-listen` | Listen address |
| `-version` | Show version and exit |

## Key Resolution

Priority: `key` > `key_env`. This matches `wardgate-cli` conventions.

- If `key` is set in the config file, it is used directly. The proxy watches the config file's mtime and re-reads the `key` field when it changes, supporting key rotation without restart.
- If `key_env` is set, the named environment variable is read at startup. The key is fixed for the process lifetime (restart to rotate).
- If neither is set, the proxy refuses to start.

## Usage

### Start the Proxy

```bash
# With config file
wardgate-proxy

# With flags
wardgate-proxy -server https://wardgate.example.com -key-env WARDGATE_AGENT_KEY
```

### Agent Requests

The agent makes requests to the proxy as if it were the Wardgate server:

```bash
# From the agent's perspective -- no auth needed
curl http://127.0.0.1:18080/todoist/tasks
curl -X POST -H "Content-Type: application/json" \
  -d '{"content":"Buy milk"}' \
  http://127.0.0.1:18080/todoist/tasks
```

The proxy injects the agent key and forwards to the real Wardgate server. Any `Authorization` header the agent sends is overwritten.

### Key Rotation

When using `key` in the config file, write the new key to the config and the proxy picks it up on the next request (detected by mtime change). No restart required.

If the config file is partially written (YAML parse error or empty key), the proxy falls back to the last known good key and logs a warning.

When using `key_env`, restart the proxy to rotate the key.

## Security Model

| Aspect | Behavior |
|--------|----------|
| Agent key storage | In config file or environment variable, never exposed to agent |
| Key injection | `Authorization: Bearer` header set on every request |
| Agent auth | Overwritten -- agent cannot send its own key upstream |
| `X-Forwarded-For` | Stripped to prevent IP leakage |
| Listen address | `127.0.0.1` by default (localhost only) |

**Compared to `wardgate-cli`:** The proxy does not restrict which paths the agent requests -- it forwards everything to the configured Wardgate server. Access control is enforced by Wardgate's policy engine, not the proxy.

## Docker Example

```yaml
services:
  wardgate-proxy:
    build:
      context: .
      dockerfile: Dockerfile
      target: wardgate-proxy  # if using multi-stage
    volumes:
      - ./wardgate-proxy.yaml:/etc/wardgate-proxy/config.yaml:ro
    command: ["-config", "/etc/wardgate-proxy/config.yaml"]
    network_mode: "service:agent"  # share network namespace with agent
```

By sharing the network namespace, the agent reaches the proxy at `127.0.0.1:18080` without exposing it to other containers.
