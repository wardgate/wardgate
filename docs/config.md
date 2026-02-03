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

## Full Configuration Example

```yaml
# config.yaml
server:
  listen: ":8080"
  approval_url: "https://wardgate.example.com"

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
| `approval_url` | string | | Base URL for approval links in notifications |

```yaml
server:
  listen: ":8080"                              # Listen on all interfaces, port 8080
  approval_url: "https://wardgate.example.com" # For approval links
```

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

### endpoints

Map of endpoint names to their configuration.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `adapter` | string | No | Adapter type: `http` (default), `imap`, or `smtp` |
| `upstream` | string | Yes | URL of the upstream service |
| `auth` | object | Yes | Authentication configuration |
| `rules` | array | No | Policy rules (default: deny all) |
| `imap` | object | No | IMAP-specific settings (for `adapter: imap`) |
| `smtp` | object | No | SMTP-specific settings (for `adapter: smtp`) |

```yaml
endpoints:
  todoist-api:
    upstream: https://api.todoist.com/rest/v2
    auth:
      type: bearer
      credential_env: WARDGATE_CRED_TODOIST_API_KEY
    rules:
      - match: { method: GET }
        action: allow
```

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
  "approve_url": "https://wardgate.example.com/approve/abc123?token=xyz",
  "deny_url": "https://wardgate.example.com/deny/abc123?token=xyz"
}
```

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
```

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

## Security Recommendations

1. **Never commit .env files** - Add to `.gitignore`
2. **Use strong agent keys** - At least 32 random characters
3. **Separate credentials by endpoint** - Don't reuse across services
4. **Restrict file permissions** - `chmod 600 .env`
5. **Rotate credentials regularly** - Especially after suspected exposure
