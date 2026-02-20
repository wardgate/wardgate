# Wardgate Configuration Reference

This document describes all configuration options for Wardgate.

## Configuration Files

Wardgate uses two configuration files:

1. **config.yaml** - Main configuration (endpoints, rules, notifications)
2. **.env** - Credentials and secrets (never commit to version control)

## Quick Start

```bash
# Create config files from examples
cp config.yaml.example config.yaml
cp .env.example .env

# Edit with your settings
vim config.yaml
vim .env

# Run Wardgate
./wardgate -config config.yaml
```

## Presets (Easy Configuration)

Presets let you configure popular APIs with minimal setup. Instead of manually specifying upstream URLs, auth types, and rules, just use a preset name.

Presets are stored as YAML files in the `presets/` directory. You must set `presets_dir` to use them:

```yaml
presets_dir: ./presets

endpoints:
  todoist:
    preset: todoist
    auth:
      credential_env: WARDGATE_CRED_TODOIST_API_KEY
    capabilities:
      read_data: allow
      create_tasks: allow
      delete_tasks: deny
```

**Included presets:** `todoist`, `github`, `cloudflare`, `google-calendar`, `postmark`, `sentry`, `plausible`

**Capability actions:** `allow`, `deny`, `ask` (require human approval)

See the **[Presets Reference](presets.md)** for all presets, capabilities, and examples.

### Overriding Preset Defaults

You can override any preset value:

```yaml
endpoints:
  custom-todoist:
    preset: todoist
    upstream: https://my-proxy.example.com/todoist  # Custom upstream
    auth:
      credential_env: MY_TODOIST_KEY
    rules:  # Custom rules replace capabilities entirely when no capabilities are set
      - match: { method: GET }
        action: allow
      - match: { method: "*" }
        action: deny
```

### Combining Capabilities with Custom Rules

When both `capabilities` and `rules` are specified, your custom rules are evaluated **first** (first-match-wins), followed by the capability-expanded rules. This lets you use capabilities for broad defaults and add surgical overrides via rules:

```yaml
endpoints:
  mail:
    preset: imap
    upstream: imap://protonmail-bridge:143
    auth:
      type: plain
      credential_env: WARDGATE_CRED_IMAP
    capabilities:
      list_folders: allow
    rules:
      # Allow reading only one specific folder (evaluated before capabilities)
      - match: { method: GET, path: "/folders/Folders/Agent John*" }
        action: allow
```

Evaluation order:

1. User-defined `rules` (highest priority, first-match-wins)
2. Rules expanded from `capabilities`
3. Catch-all deny (automatically appended)

## Custom Presets (User-Defined)

You can define your own presets for APIs not included with the source code. You are encouraged to share them with the community by adding them to the `presets/` directory via a Pull Request.

### Option 1: External Preset Files

Create YAML files in a `presets/` directory:

```yaml
# presets/helpscout.yaml
name: helpscout
description: "Help Scout customer support API"
upstream: https://api.helpscout.net/v2
auth_type: bearer

capabilities:
  - name: read_conversations
    description: "Read conversations and threads"
    rules:
      - match: { method: GET, path: "/conversations*" }

  - name: reply_to_conversations
    description: "Reply to customer conversations"
    rules:
      - match: { method: POST, path: "/conversations/*/reply" }

  - name: manage_customers
    description: "Create and update customer profiles"
    rules:
      - match: { method: POST, path: "/customers" }
      - match: { method: PUT, path: "/customers/*" }

default_rules:
  - match: { method: GET }
    action: allow
  - match: { method: "*" }
    action: deny
```

Then reference in your config:

```yaml
presets_dir: ./presets

endpoints:
  helpscout:
    preset: helpscout
    auth:
      credential_env: HELPSCOUT_TOKEN
    capabilities:
      read_conversations: allow
      reply_to_conversations: ask
      manage_customers: deny
```

### Option 2: Inline Custom Presets

Define presets directly in your config file:

```yaml
custom_presets:
  my-internal-api:
    description: "Company Internal API"
    upstream: https://api.internal.company.com/v1
    auth_type: bearer
    capabilities:
      - name: read_data
        description: "Read resources"
        rules:
          - match: { method: GET }
      - name: write_data
        description: "Create/update resources"
        rules:
          - match: { method: POST }
          - match: { method: PUT }
    default_rules:
      - match: { method: GET }
        action: allow
      - match: { method: "*" }
        action: deny

endpoints:
  internal:
    preset: my-internal-api
    auth:
      credential_env: INTERNAL_API_KEY
    capabilities:
      read_data: allow
      write_data: ask
```

### Custom Preset File Format

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | No | Preset name (defaults to filename) |
| `description` | string | Yes | Human-readable description |
| `upstream` | string | Yes | Base URL of the API |
| `docs_url` | string | No | Link to API documentation (exposed in discovery) |
| `auth_type` | string | Yes | Auth type (`bearer` or `plain`) |
| `capabilities` | array | No | List of named capabilities |
| `default_rules` | array | No | Default rules when no capabilities specified |

### Capability Definition

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Capability identifier |
| `description` | string | Yes | Human-readable description |
| `rules` | array | Yes | Rules to apply when capability is enabled |

### Preset Priority

When multiple presets exist with the same name:

1. **Inline custom presets** (highest priority) - `custom_presets` in config.yaml
2. **External preset files** - YAML files in `presets_dir`

## Full Configuration Example

```yaml
# config.yaml
server:
  listen: ":8080"
  base_url: "https://wardgate.example.com"

agents:
  - id: my-agent
    key_env: WARDGATE_AGENT_KEY

notify:
  timeout: "5m"
  slack:
    webhook_url: "https://hooks.slack.com/services/..."

endpoints:
  todoist-api:
    upstream: https://api.todoist.com/rest/v2
    auth:
      type: bearer
      credential_env: WARDGATE_CRED_TODOIST_API_KEY
    rules:
      - match: { method: GET }
        action: allow
        rate_limit: { max: 100, window: "1m" }
      - match: { method: POST, path: "/tasks" }
        action: allow
      - match: { method: DELETE }
        action: ask
      - match: { method: "*" }
        action: deny
```

```bash
# .env
WARDGATE_AGENT_KEY=agent-secret-key-here
WARDGATE_CRED_TODOIST_API_KEY=your-todoist-api-key
```

## Configuration Sections

### server

Server configuration.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `listen` | string | `:8080` | Address and port to listen on |
| `base_url` | string | | Base URL for links in notifications |
| `admin_key_env` | string | | Env var for admin key (enables Web UI at `/ui/` and CLI) |
| `logging.max_entries` | int | `1000` | Max log entries to keep in memory for dashboard |
| `logging.store_bodies` | bool | `false` | Store request bodies in logs (privacy consideration) |

```yaml
server:
  listen: ":8080"                            # Listen on all interfaces, port 8080
  base_url: "https://wardgate.example.com"   # For links in notifications
  admin_key_env: WARDGATE_ADMIN_KEY          # Enables admin Web UI and CLI
  grants_file: grants.json                   # Path to dynamic grants file (default: grants.json)
  logging:
    max_entries: 1000                        # Keep last 1000 requests in memory
    store_bodies: false                      # Don't store request bodies by default
```

#### Dynamic Grants

Wardgate supports dynamic grants -- runtime-added policy rules that override static rules. Grants can be permanent or time-limited. See [Grants Documentation](grants.md) for full details.

#### Admin UI and CLI

When `admin_key_env` is set and the corresponding environment variable contains a key, Wardgate enables:

- **Web UI** at `/ui/` - Dashboard for viewing and managing pending approvals
- **CLI commands** - `wardgate approvals list|approve|deny|view|history|monitor`

The admin key authenticates both the Web UI (via localStorage + Authorization header) and CLI commands.

#### Listen Address Examples

```yaml
listen: ":8080"           # All interfaces, port 8080
listen: "127.0.0.1:8080"  # Localhost only
listen: "0.0.0.0:443"     # All interfaces, HTTPS port
```

### agents

List of agents allowed to use the gateway.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Unique identifier for the agent |
| `key_env` | string | Yes | Environment variable containing the agent's API key |

```yaml
agents:
  - id: my-agent
    key_env: WARDGATE_AGENT_KEY
  
  - id: another-agent
    key_env: WARDGATE_AGENT_2_KEY
```

Agents authenticate using the `Authorization: Bearer <key>` header.

#### Managing Agents via CLI

Instead of manually generating keys and editing files, use the CLI:

```bash
# Add a new agent (generates key, updates config.yaml and .env)
wardgate agent add my-agent

# Remove an agent
wardgate agent remove my-agent
```

`agent add` generates a random 32-byte key, appends the env var to `.env`, adds the agent to `config.yaml`, and prints the key for configuring `wardgate-cli`.

### JWT Agent Authentication

Instead of managing static keys per agent, you can configure JWT-based authentication. This is useful when you have an orchestrator that spins up ephemeral sandboxed agents -- just sign a short-lived JWT with the agent ID and inject it into the sandbox.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `secret` | string | No* | HMAC signing secret (inline, for dev) |
| `secret_env` | string | No* | Env var holding the HMAC signing secret |
| `issuer` | string | No | Expected `iss` claim (rejected if mismatch) |
| `audience` | string | No | Expected `aud` claim (rejected if mismatch) |

\* One of `secret` or `secret_env` is required.

```yaml
server:
  listen: ":8080"
  jwt:
    secret_env: WARDGATE_JWT_SECRET
    issuer: "my-orchestrator"   # optional
    audience: "wardgate"        # optional
```

The JWT `sub` (subject) claim is used as the **agent ID**. Standard `exp` handles expiry. Supported signing algorithms: HS256, HS384, HS512.

Example JWT payload:

```json
{
  "sub": "agent-sandbox-42",
  "exp": 1739400000,
  "iss": "my-orchestrator"
}
```

**How it works:**
- Static keys are checked first (fast map lookup)
- If the token doesn't match any static key and JWT is configured, it's validated as a JWT
- The agent ID from the `sub` claim is used for all downstream features (policy, rate limiting, audit, scoping)
- `wardgate-cli` sends `Authorization: Bearer <key>` where the key can be any string including a JWT -- no client changes needed

**No token revocation:** JWT is stateless by design. Use short-lived tokens with `exp` for ephemeral agents. Both static keys and JWT can be used simultaneously.

#### Managing Conclaves via CLI

```bash
# Add a new conclave (generates key, updates config.yaml and .env)
wardgate conclave add obsidian "Personal notes vault"

# Remove a conclave
wardgate conclave remove obsidian
```

`conclave add` generates a random key, appends the env var to `.env`, adds the conclave to `config.yaml` with a default deny-all rule, and prints a starter `wardgate-exec` config.

### endpoints

Map of endpoint names to their configuration.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `adapter` | string | No | Adapter type: `http` (default), `imap`, or `smtp` |
| `agents` | array | No | Agent IDs allowed to access this endpoint (empty = all agents) |
| `upstream` | string | Yes | URL of the upstream service |
| `docs_url` | string | No | Link to API documentation (exposed in discovery, overrides preset) |
| `auth` | object | Yes | Authentication configuration |
| `rules` | array | No | Policy rules (default: deny all) |
| `imap` | object | No | IMAP-specific settings (for `adapter: imap`) |
| `smtp` | object | No | SMTP-specific settings (for `adapter: smtp`) |

```yaml
endpoints:
  todoist-api:
    agents: [tessa]  # Only agent "tessa" can access this endpoint
    upstream: https://api.todoist.com/rest/v2
    auth:
      type: bearer
      credential_env: WARDGATE_CRED_TODOIST_API_KEY
    rules:
      - match: { method: GET }
        action: allow
```

When `agents` is omitted or empty, all authenticated agents can access the endpoint. When specified, only the listed agents are permitted; other agents receive a `403 Forbidden` response, and the endpoint is hidden from their `GET /endpoints` discovery response.

Endpoints are accessed as: `http://wardgate:8080/{endpoint-name}/{path}`

### endpoints.auth

Authentication configuration for the upstream service.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Authentication type (`bearer`) |
| `credential_env` | string | Yes | Environment variable containing the credential |

```yaml
auth:
  type: bearer
  credential_env: WARDGATE_CRED_TODOIST_API_KEY
```

Currently supported types:
- `bearer` - Adds `Authorization: Bearer <credential>` header
- `plain` - For IMAP: credential format is `username:password`

### endpoints.rules

Array of policy rules. See [Policy Documentation](policies.md) for details.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `match` | object | Yes | Conditions to match |
| `match.method` | string | No | HTTP method to match (`GET`, `POST`, `*`, etc.) |
| `match.path` | string | No | Path pattern to match |
| `match.command` | string | No | Exec: glob match on command path (e.g., `/usr/bin/python*`) |
| `match.args_pattern` | string | No | Exec: regex match on argument string |
| `match.cwd_pattern` | string | No | Exec: glob match on working directory |
| `action` | string | Yes | Action to take (`allow`, `deny`, `ask`) |
| `message` | string | No | Message to return (for `deny`) |
| `rate_limit` | object | No | Rate limiting configuration |
| `time_range` | object | No | Time-based restrictions |

```yaml
rules:
  - match:
      method: GET
      path: "/tasks*"
    action: allow
    rate_limit:
      max: 100
      window: "1m"
    time_range:
      hours: ["09:00-17:00"]
      days: ["mon", "tue", "wed", "thu", "fri"]
```

### endpoints.rules.rate_limit

Rate limiting configuration.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max` | integer | | Maximum requests allowed in window |
| `window` | string | `1m` | Time window (`30s`, `5m`, `1h`) |

```yaml
rate_limit:
  max: 100
  window: "1m"
```

### endpoints.rules.time_range

Time-based restrictions.

| Field | Type | Description |
|-------|------|-------------|
| `hours` | array | Time ranges in 24h format (`["09:00-17:00"]`) |
| `days` | array | Day abbreviations (`["mon", "tue", "wed", "thu", "fri"]`) |

```yaml
time_range:
  hours: ["09:00-17:00", "20:00-22:00"]
  days: ["mon", "tue", "wed", "thu", "fri"]
```

### notify

Notification configuration for the `ask` action.

| Field | Type | Description |
|-------|------|-------------|
| `timeout` | string | How long to wait for approval (`5m`, `1h`) |
| `webhook` | object | Generic webhook configuration |
| `slack` | object | Slack webhook configuration |

```yaml
notify:
  timeout: "5m"
  slack:
    webhook_url: "https://hooks.slack.com/services/..."
```

### notify.webhook

Generic webhook notification.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `url` | string | Yes | Webhook URL |
| `headers` | map | No | Additional headers to send |

```yaml
webhook:
  url: "https://your-service.example.com/notify"
  headers:
    Authorization: "Bearer your-token"
    X-Custom-Header: "value"
```

Webhook payload:

```json
{
  "title": "Approval Required",
  "body": "Agent my-agent wants to DELETE /tasks/123",
  "request_id": "abc123",
  "endpoint": "todoist-api",
  "method": "DELETE",
  "path": "/tasks/123",
  "agent_id": "my-agent",
  "dashboard_url": "https://wardgate.example.com/ui/"
}
```

Note: Webhooks are notification-only. Approvals must be done through the Web UI (requires admin key authentication) or CLI.

### notify.slack

Slack webhook notification.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `webhook_url` | string | Yes | Slack incoming webhook URL |

```yaml
slack:
  webhook_url: "https://hooks.slack.com/services/T00/B00/XXX"
```

To get a webhook URL:
1. Go to [Slack App Directory](https://api.slack.com/apps)
2. Create or select an app
3. Enable Incoming Webhooks
4. Create a webhook for your channel

## Environment Variables

Credentials are stored in environment variables for security.

### Naming Convention

```bash
# Agent keys
WARDGATE_AGENT_<NAME>_KEY=<agent-secret>

# Upstream credentials
WARDGATE_CRED_<NAME>=<credential>
```

### Example .env File

```bash
# Admin key (for Web UI and CLI)
WARDGATE_ADMIN_KEY=your-secret-admin-key

# Agent authentication keys
WARDGATE_AGENT_GEORGE_KEY=sk-agent-xyz789

# Upstream API credentials
WARDGATE_CRED_TODOIST_API_KEY=0123456789abcdef
WARDGATE_CRED_GOOGLE_CALENDAR_KEY=AIzaSy...
WARDGATE_CRED_GITHUB_TOKEN=ghp_xxxxxxxxxxxx
```

### Loading Environment Variables

```bash
# Automatic loading from .env
./wardgate -config config.yaml

# Specify different env file
./wardgate -config config.yaml -env /path/to/.env

# Or export manually
export WARDGATE_AGENT_KEY=sk-agent-abc123
./wardgate -config config.yaml
```

## Command Line Options

```bash
./wardgate [options]

Options:
  -config string
        Path to config file (default "config.yaml")
  -env string
        Path to .env file (default ".env")
  -version
        Show version and exit
```

## CLI Commands for Approvals

Wardgate includes CLI commands for managing approval requests:

```bash
# Set environment variables
export WARDGATE_URL=http://localhost:8080
export WARDGATE_ADMIN_KEY=your-secret-admin-key

# List pending approvals
wardgate approvals list

# View details of an approval (including full email content)
wardgate approvals view <id>

# Approve or deny a request
wardgate approvals approve <id>
wardgate approvals deny <id>

# View history of recent decisions
wardgate approvals history

# Monitor mode - live updates with interactive approve/deny
wardgate approvals monitor
```

### Monitor Mode

The `monitor` command provides a live view of pending approvals with interactive commands:

- `a <id>` or `approve <id>` - Approve a request
- `d <id>` or `deny <id>` - Deny a request
- `v <id>` or `view <id>` - View full request details
- `r` or `refresh` - Refresh the list
- `q` or `quit` - Exit monitor mode

The list auto-refreshes every 3 seconds.

## Multiple Endpoints Example

```yaml
server:
  listen: ":8080"

agents:
  - id: assistant
    key_env: WARDGATE_AGENT_KEY

endpoints:
  # Todoist - task management
  todoist-api:
    upstream: https://api.todoist.com/rest/v2
    auth:
      type: bearer
      credential_env: WARDGATE_CRED_TODOIST
    rules:
      - match: { method: GET }
        action: allow
      - match: { method: POST, path: "/tasks" }
        action: allow
      - match: { method: "*" }
        action: deny

  # Google Calendar - read only
  google-calendar:
    upstream: https://www.googleapis.com/calendar/v3
    auth:
      type: bearer
      credential_env: WARDGATE_CRED_GOOGLE
    rules:
      - match: { method: GET }
        action: allow
      - match: { method: "*" }
        action: deny

  # GitHub - limited write
  github-api:
    upstream: https://api.github.com
    auth:
      type: bearer
      credential_env: WARDGATE_CRED_GITHUB
    rules:
      - match: { method: GET }
        action: allow
      - match: { method: POST, path: "/repos/*/issues" }
        action: allow
        rate_limit: { max: 10, window: "1h" }
      - match: { method: "*" }
        action: ask
```

## Validation

Wardgate validates configuration on startup:

- All endpoints must have `upstream` and `auth`
- All `credential_env` and `key_env` must exist in environment
- All `action` values must be valid (`allow`, `deny`, `ask`)
- Rate limit `window` must be valid duration

Invalid configuration causes startup failure with descriptive error.

## Conclaves (Remote Execution Environments)

The top-level `conclaves:` section defines isolated execution environments and their per-conclave policy rules.

```yaml
conclaves:
  obsidian:
    description: "Obsidian vault (personal notes)"
    key_env: WARDGATE_CONCLAVE_OBSIDIAN_KEY
    agents: [tessa]  # Only agent "tessa" can execute on this conclave
    cwd: /data/vault
    rules:
      - match: { command: "cat" }
        action: allow
      - match: { command: "rg" }
        action: allow
      - match: { command: "tee" }
        action: ask
      - match: { command: "*" }
        action: deny
```

### Conclave Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `description` | string | No | Human-readable description of the conclave |
| `key_env` | string | Yes | Environment variable holding the conclave's pre-shared key |
| `agents` | array | No | Agent IDs allowed to access this conclave (empty = all agents) |
| `cwd` | string | No | Default working directory for commands |
| `commands` | map | No | Named command templates (see below) |
| `rules` | array | No | Policy rules using exec match fields |
| `filter` | object | No | Output filter for sensitive data (see [Output Filtering](#conclave-output-filtering)) |

### Conclave Output Filtering

Conclave output (stdout/stderr) can be scanned for sensitive data before returning to the agent. This reuses the same filter engine as endpoint filtering.

```yaml
conclaves:
  obsidian:
    key_env: WARDGATE_CONCLAVE_OBSIDIAN_KEY
    cwd: /data/vault
    filter:
      enabled: true
      patterns: [api_keys, passwords, private_keys]
      action: block
    commands:
      search:
        template: "find . -iname {query}"
        args: [{ name: query }]
        filter:
          enabled: false  # filenames only, skip filtering
      read:
        template: "python3 /usr/local/lib/wardgate-tools/file_read.py {file}"
        args: [{ name: file }]
        # inherits conclave filter
```

Two levels of configuration:

- **Per-conclave** `filter:` - default for all commands and raw exec
- **Per-command** `filter:` on a command definition - overrides the conclave default

The filter fields are the same as endpoint filtering (`enabled`, `patterns`, `custom_patterns`, `action`, `replacement`). Supported actions for conclave output: `block`, `redact`, `log` (not `ask` - the command has already executed).

When `action` is `block` and sensitive data is found, the response returns 403 with a description of what was detected. When `action` is `redact`, matches are replaced in-place. When `action` is `log`, matches are logged but output is returned unchanged.

### Command Templates

Define pre-made commands that agents invoke by name, supplying only arguments:

```yaml
conclaves:
  obsidian:
    key_env: WARDGATE_CONCLAVE_OBSIDIAN_KEY
    cwd: /data/vault
    commands:
      search:
        description: "Search notes by filename"
        template: "find . -iname {query}"
        args:
          - name: query
            description: "Filename pattern"
      grep:
        description: "Search note contents"
        template: "rg {pattern} | grep -v SECRET1 | grep -v SECRET2"
        args:
          - name: pattern
            description: "Text pattern"
        action: ask
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `description` | string | No | Human-readable description |
| `template` | string | Yes | Command with `{argname}` placeholders |
| `args` | array | No | Ordered argument definitions |
| `args[].name` | string | Yes | Argument name (matches placeholder in template) |
| `args[].description` | string | No | Human-readable description |
| `args[].type` | string | No | `path` enables path validation (rejects absolute paths and traversal) |
| `args[].allowed_paths` | array | No | Glob patterns restricting valid paths (requires `type: path`) |
| `action` | string | No | `allow` (default), `ask`, or `deny`. Fallback when no `rules` match |
| `rules` | array | No | Per-arg policy rules (first match wins, default deny when present) |
| `rules[].match` | map | Yes | Arg name to glob pattern (all must match, AND logic) |
| `rules[].action` | string | Yes | `allow`, `ask`, or `deny` |
| `rules[].message` | string | No | Message to return (for `deny`) |

#### Path Validation

Args with `type: path` and `allowed_paths` are validated before template expansion:

- Absolute paths are rejected
- Path traversal (`../`) is rejected
- The value must match at least one `allowed_paths` glob pattern

This is a hard boundary - rejected paths return 403 immediately.

#### Command Rules

When `rules` is present on a command, they are evaluated in order (first match wins). If no rule matches, the request is denied (consistent with conclave-level rules). When `rules` is absent, the `action` field is used directly.

```yaml
commands:
  read:
    template: "python3 /usr/local/lib/wardgate-tools/file_read.py {file}"
    args:
      - name: file
        type: path
        allowed_paths: ["notes/**", "config/**"]
    rules:
      - match: { file: "notes/**" }
        action: allow
      - match: { file: "config/**" }
        action: ask
      # unmatched paths -> default deny
```

Agents run commands via `wardgate-cli run <conclave> <command> [args...]`. Arguments are shell-escaped before substitution. See [Conclaves](conclaves.md) for details.

When `agents` is omitted or empty, all authenticated agents can execute commands on the conclave. When specified, other agents receive `403 Forbidden` and the conclave is hidden from their `GET /conclaves` discovery response.

### Exec Match Fields

| Field | Type | Description |
|-------|------|-------------|
| `command` | string | Glob match on command name (e.g., `rg`, `python*`, `*`) |
| `args_pattern` | string | Regex match on the joined argument string |
| `cwd_pattern` | string | Glob match on the working directory |

All match fields are optional and AND-ed together. Commands are resolved to absolute paths on the conclave by `wardgate-exec`.

See [Conclaves](conclaves.md) for full documentation including pipeline support, deployment, and limitations.

## IMAP Endpoints

For IMAP endpoints, Wardgate exposes a REST API that wraps the IMAP protocol:

```yaml
endpoints:
  imap-personal:
    adapter: imap
    upstream: imaps://imap.gmail.com:993
    auth:
      type: plain
      credential_env: IMAP_CREDS  # format: username:password
    imap:
      tls: true  # Use TLS (default for imaps://) for ProtonBridge use false for StartTLS
      insecure_skip_verify: true  # Skip TLS cert verification for ProtonBridge
    rules:
      - match: { path: "/inbox*" }
        action: allow
      - match: { path: "/folders" }
        action: allow
      - match: { path: "/*" }
        action: deny
```

### IMAP REST API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/folders` | GET | List all mailbox folders |
| `/folders/{folder}` | GET | Fetch messages from folder |
| `/folders/{folder}?limit=N` | GET | Limit number of messages |
| `/folders/{folder}?since=YYYY-MM-DD` | GET | Messages since date |
| `/folders/{folder}?before=YYYY-MM-DD` | GET | Messages before date |
| `/folders/{folder}/messages/{uid}` | GET | Get full message by UID |
| `/folders/{folder}/messages/{uid}/mark-read` | POST | Mark message as read |
| `/folders/{folder}/messages/{uid}/move?to=X` | POST | Move message to folder |

Message operations are scoped to folders, so policy rules like `/folders/inbox*` will apply to both listing and reading messages from that folder.

### Folder Names with Slashes

IMAP folder names can contain slashes (e.g., `Folder/Orders`). URL-encode them in requests:

| Folder Name | URL Request |
|-------------|-------------|
| `INBOX` | `/folders/INBOX` |
| `Folder/Orders` | `/folders/Folder%2FOrders` |
| `Work/Projects/Active` | `/folders/Work%2FProjects%2FActive` |

The `%2F` is the URL-encoded form of `/`.

**Important:** Policy rules use the *decoded* path, not the encoded form:

```yaml
# Correct - use decoded folder name
- match:
    path: "/folders/Folder/Orders*"
  action: allow

# Wrong - don't use URL encoding in rules
- match:
    path: "/folders/Folder%2FOrders*"
  action: allow
```

### IMAP Upstream URL

| Scheme | Port | TLS |
|--------|------|-----|
| `imaps://` | 993 | Yes |
| `imap://` | 143 | No |

### endpoints.imap

IMAP-specific configuration.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `tls` | bool | true | Use TLS connection |
| `max_conns` | int | 5 | Max connections per endpoint |
| `idle_timeout_secs` | int | 300 | Idle connection timeout |

## SMTP Endpoints

For SMTP endpoints, Wardgate exposes a REST API for sending emails:

```yaml
endpoints:
  smtp-personal:
    adapter: smtp
    upstream: smtps://smtp.gmail.com:465  # Or smtp://smtp.gmail.com:587 for STARTTLS
    auth:
      type: plain
      credential_env: SMTP_CREDS  # format: username:password
    smtp:
      tls: true
      from: "your-email@gmail.com"
      known_recipients:
        - "@company.com"
      ask_new_recipients: true
      blocked_keywords:
        - "password"
        - "secret"
    rules:
      - match: { path: "/send" }
        action: allow
```

### SMTP REST API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/send` | POST | Send an email |

### Send Request Body

```json
{
  "to": ["recipient@example.com"],
  "cc": ["cc@example.com"],
  "bcc": ["bcc@example.com"],
  "reply_to": "reply@example.com",
  "subject": "Email subject",
  "body": "Plain text body",
  "html_body": "<html>...</html>"
}
```

### SMTP Upstream URL

| Scheme | Port | TLS |
|--------|------|-----|
| `smtps://` | 465 | Implicit TLS |
| `smtp://` | 587 | STARTTLS |

### endpoints.smtp

SMTP-specific configuration.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `tls` | bool | false | Use implicit TLS (port 465) |
| `starttls` | bool | true | Use STARTTLS upgrade (port 587) |
| `from` | string | | Default from address |
| `allowed_recipients` | array | | Allowlist of recipients (block all others) |
| `known_recipients` | array | | Recipients that don't need approval |
| `ask_new_recipients` | bool | false | Ask before sending to unknown recipients |
| `blocked_keywords` | array | | Keywords to block in subject/body |

### Recipient Patterns

Allowlist and known recipients support two patterns:

| Pattern | Example | Matches |
|---------|---------|---------|
| Domain | `@company.com` | Any email ending in `@company.com` |
| Exact | `specific@example.com` | Only that exact address |

```yaml
smtp:
  allowed_recipients:
    - "@company.com"  # Allow any @company.com address
    - "partner@external.com"  # Allow this specific address
  known_recipients:
    - "@internal.com"  # No approval needed for internal
```

### Content Filtering

Block emails containing specific keywords in subject or body:

```yaml
smtp:
  blocked_keywords:
    - "password"
    - "secret"
    - "confidential"
```

Keywords are case-insensitive. Any match will reject the email with HTTP 403.

## Sensitive Data Filtering

Wardgate can automatically detect and filter sensitive data in API responses and email messages. This prevents agents from seeing OTP codes, verification links, API keys, and other security-sensitive information.

### Configuration

Filtering is **enabled by default** for all endpoints. You can configure it per endpoint:

```yaml
endpoints:
  my-api:
    upstream: https://api.example.com
    auth:
      credential_env: API_KEY
    filter:
      enabled: true           # Enable/disable filtering (default: true)
      patterns:               # Built-in patterns to detect
        - otp_codes
        - verification_links
        - api_keys
      action: block           # Action: block, redact, ask, log (default: block)
      replacement: "[REDACTED]"  # Replacement text for redact action
```

To disable filtering for a specific endpoint (e.g., an OTP inbox for account creation):

```yaml
endpoints:
  otp-inbox:
    preset: imap
    auth:
      credential_env: IMAP_CREDS
    filter:
      enabled: false  # Allow agent to see OTP codes in this mailbox
```

### Global Defaults

Set default filter settings for all endpoints:

```yaml
filter_defaults:
  enabled: true
  patterns:
    - otp_codes
    - verification_links
    - api_keys
  action: block
  replacement: "[SENSITIVE DATA REDACTED]"
```

### endpoints.filter

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `true` | Enable sensitive data filtering |
| `patterns` | array | `[otp_codes, verification_links, api_keys]` | Built-in patterns to detect |
| `custom_patterns` | array | | User-defined regex patterns |
| `action` | string | `block` | Action when detected: `block`, `redact`, `ask`, `log` |
| `replacement` | string | `[SENSITIVE DATA REDACTED]` | Replacement text for `redact` action |

### Built-in Patterns

| Pattern | Description | Examples |
|---------|-------------|----------|
| `otp_codes` | One-time passwords and verification codes | "Code: 123456", "Your OTP is 847291" |
| `verification_links` | Email verification and password reset URLs | `https://example.com/verify/abc`, `?token=xyz` |
| `api_keys` | Common API key formats | `sk-...`, `ghp_...`, `AKIA...` |
| `ssn` | Social Security Numbers (US SSN) and Dutch BSN | `SSN: 123-45-6789`, `BSN: 123456789` |
| `passport` | Passport numbers (US, NL, and other formats) | `Passport: 123456789`, `paspoort: AB1234567` |
| `credit_cards` | Credit card numbers | `4111-1111-1111-1111` |
| `passwords` | Passwords in common formats | `password: secret123` |
| `private_keys` | Private key headers | `-----BEGIN PRIVATE KEY-----` |

### Actions

| Action | Description | Use Case |
|--------|-------------|----------|
| `block` | Return 403 error, don't return content | Default - highest security |
| `redact` | Replace sensitive data with placeholder | When agent needs partial content |
| `ask` | Require human approval | For SMTP: verify before sending |
| `log` | Log detection, allow passthrough | Monitoring only |

### Custom Patterns

Define your own patterns using regex:

```yaml
endpoints:
  my-api:
    filter:
      custom_patterns:
        - name: internal_id
          pattern: "INTERNAL-[A-Z0-9]{8}"
          description: "Internal tracking IDs"
        - name: ssn
          pattern: "\\d{3}-\\d{2}-\\d{4}"
          description: "Social Security Numbers"
```

### Per-Adapter Behavior

| Adapter | Filter Applies To | Default Action |
|---------|------------------|----------------|
| HTTP | Response bodies (JSON, text, XML) | `block` |
| IMAP | Message subject and body | `block` |
| SMTP | Outgoing email subject/body (triggers `ask`) | `ask` |
| Conclave | Command stdout/stderr | `block` |

For SMTP, detecting sensitive data triggers the approval workflow rather than blocking, so humans can review before sending.

For conclaves, `ask` is not supported (the command has already executed). Use `block`, `redact`, or `log` instead. Per-command `filter:` on a command definition overrides the conclave default.

### Example: Secure Email Access

```yaml
endpoints:
  # Personal email - block OTPs and verification links
  imap-personal:
    preset: imap
    auth:
      credential_env: IMAP_PERSONAL
    filter:
      enabled: true
      patterns:
        - otp_codes
        - verification_links
      action: block

  # OTP inbox for automated account creation - allow OTPs
  imap-otp:
    preset: imap
    auth:
      credential_env: IMAP_OTP
    filter:
      enabled: false  # Agent needs to read OTPs here
```

## Security Recommendations

1. **Never commit .env files** - Add to `.gitignore`
2. **Use strong agent keys** - At least 32 random characters
3. **Separate credentials by endpoint** - Don't reuse across services
4. **Restrict file permissions** - `chmod 600 .env`
5. **Rotate credentials regularly** - Especially after suspected exposure
