# wardgate-cli

A restricted HTTP client for AI agents that replaces `curl` when talking to Wardgate. It uses curl-like arguments but only allows connections to a single Wardgate server, preventing agents from redirecting to arbitrary APIs.

## Why It Exists

AI agents need to call APIs. The typical approach is to give them `curl` and let them construct requests. But curl is too powerful:

- **Arbitrary URLs** - An agent can connect anywhere
- **Data exfiltration** - Curl can fetch any URL the agent specifies, bypassing Wardgate entirely

Wardgate-cli solves this by **restricting what the agent can do**:

1. **Fixed config path** - The server URL comes from a config file at a path set at build time. The agent cannot pass `-config` or override it via environment variables.
2. **Single server** - Only requests to the configured Wardgate server are allowed. Any other URL is rejected.
3. **Key from config only** - The agent key is the only value that may come from environment variables (via `key_env`). The server URL never comes from env.

## Security Model

| Setting      | Source                    | Agent can override? |
|-------------|---------------------------|---------------------|
| Server URL  | Config file only          | No                  |
| Config path | Fixed at build time       | No                  |
| Agent key   | Config (`key` or `key_env`)| No (key_env reads env, but server is fixed) |

The config file path defaults to `/etc/wardgate-cli/config.yaml` and is compiled into the binary. For production, build with this path and mount the config read-only. The agent runs as a non-root user and cannot write to `/etc/`.

As long as no other tools are allowed that can be used to read arbitrary URLs, this is a secure way to use Wardgate and prevent data leakage in another way.

## Proper Usage

### 1. Download or Build for Your Environment

For the default config path `/etc/wardgate-cli/config.yaml`, you can download the binary from the [releases page](https://github.com/wardgate/wardgate/releases) or build it yourself.

For production (agent container):

```bash
go build -ldflags "-X main.configPath=/etc/wardgate-cli/config.yaml" -o wardgate-cli ./cmd/wardgate-cli
```

For local development:

```bash
go build -ldflags "-X main.configPath=$HOME/.wardgate-cli.yaml" -o wardgate-cli ./cmd/wardgate-cli
```

### 2. Create the Config File

At the path you built in (e.g. `/etc/wardgate-cli/config.yaml`):

```yaml
server: http://wardgate:8080
key_env: WARDGATE_AGENT_KEY
```

Or with the key directly (less flexible):

```yaml
server: http://wardgate:8080
key: "your-agent-key"
```

**Internal Wardgate with custom CA:** If Wardgate uses HTTPS with a custom/internal CA, add `ca_file` so the CLI verifies the server cert:

```yaml
server: https://wardgate.internal:443
key_env: WARDGATE_AGENT_KEY
ca_file: /etc/wardgate-cli/ca.pem
```

Mount the CA cert (PEM) at that path. The CLI adds it to the system trust store for TLS verification. Alternatively, add the CA to the container's system store (e.g. Alpine: put PEM in `/usr/local/share/ca-certificates/` and run `update-ca-certificates`) and omit `ca_file`.

### 3. Provide the Key

If using `key_env`, set that variable in the agent's environment. Options:

- **Container env** - Set `WARDGATE_AGENT_KEY` in the container's environment
- **-env file** - Use `wardgate-cli -env /path/to/.env` (the agent can pass this; it only affects the key, not the server)

### 4. Deploy in the Agent Container

- Mount the config at the built-in path, read-only
- Ensure the agent runs as non-root and cannot write to the config directory
- Give the agent `wardgate-cli` instead of `curl` in its tool set

## Commands

**List endpoints:**

```bash
wardgate-cli endpoints
```

**Make a request (curl-like):**

```bash
wardgate-cli /todoist/tasks
wardgate-cli -X POST -H "Content-Type: application/json" -d '{"content":"Buy milk"}' /todoist/tasks
```

**Execute a command on a conclave:**

```bash
wardgate-cli exec code "git status"
wardgate-cli exec code "rg TODO src/ | head -20"
wardgate-cli exec -C /home/agent/project code "make build"
```

**List conclaves:**

```bash
wardgate-cli conclaves
```

## Options

| Option | Description |
|--------|-------------|
| `-X`, `--request` | HTTP method (default: GET when not specified) |
| `-H`, `--header` | HTTP header (Key: Value) |
| `-d`, `--data` | Request body |
| `-o`, `--output` | Write response to file |
| `-s`, `--silent` | Suppress progress output |
| `-v`, `--verbose` | Verbose output |
| `-L`, `--location` | Follow redirects (same-host only) |
| `-k`, `--insecure` | Skip TLS verification (for self-signed certs) |
| `ca_file` (config) | Path to custom CA cert (PEM) for internal Wardgate with custom CA |
| `-w`, `--write-out` | Write-out format (e.g. `%{http_code}`) |
| `-env` | Path to .env file for key_env (default: .env) |

## Conclave Exec

The `exec` subcommand sends shell commands to a conclave (remote execution environment) through wardgate's policy engine.

### How It Works

1. `wardgate-cli` receives the conclave name and command string
2. Parses the command into individual segments (splitting on `|`, `&&`, `||`, `;`)
3. Sends segments and the raw command to wardgate for policy evaluation and remote execution
4. Wardgate evaluates each segment against the conclave's rules
5. If all pass: forwards the command to the conclave via WebSocket
6. The conclave executes the command and streams stdout/stderr back

### Usage

```bash
wardgate-cli exec [-C <dir>] <conclave> "<command>"
```

### Pipeline Support

Piped and chained commands are parsed and each segment is evaluated individually:

```bash
wardgate-cli exec code "rg TODO src/ | head -20"       # both rg and head checked
wardgate-cli exec code "git add . && git commit -m msg" # both git invocations checked
```

### Rejected Constructs

Command substitution (`$()`), backticks, process substitution (`<()`, `>()`), and subshells are rejected because they introduce hidden command execution that cannot be policy-checked.

### Working Directory

Use `-C` to set the working directory on the conclave. If not specified, the conclave's configured `cwd` is used:

```bash
wardgate-cli exec -C /data/vault obsidian "rg 'meeting notes' ."
```

### Security

- Commands are parsed and each segment is evaluated against per-conclave policy rules
- The agent cannot invoke `/bin/sh` directly (denied by policy)
- `wardgate-cli` controls all parsing — the agent only provides a command string
- Execution happens on the conclave, not the agent host
- Conclaves connect outbound to wardgate — no inbound ports required

See [Conclaves](conclaves.md) for full documentation including policy configuration.

## Agent Integration

Configure your agent to use `wardgate-cli` instead of `curl` when calling Wardgate:

```yaml
# Instead of: curl -H "Authorization: Bearer $KEY" http://wardgate:8080/todoist/tasks
# Use:
wardgate-cli /todoist/tasks

No need to add your bearer token. The standard curl arguments are still supported, but the method is optional and defaults to GET when not specified.
```