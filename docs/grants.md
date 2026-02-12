# Dynamic Grants

Grants are runtime-added policy rules that override static rules in `config.yaml`. They can be **permanent** or **time-limited**, and they solve two common needs:

1. **"Allow this command for the next 10 minutes"** - time-limited grant
2. **"Always allow this command"** - permanent grant

## How It Works

Grants are checked **before** the static policy engine. If a matching grant exists and hasn't expired, the request is allowed immediately without consulting static rules.

```
Request → Agent Scope Check → Grant Check → Static Policy → Allow/Deny/Ask
                                  ↓
                          (if match) → Allow
```

Grants are stored in `grants.json` (separate from `config.yaml` to avoid modifying the human-edited config at runtime).

## Creating Grants

### Via Approval UI

When approving a pending request in the Web UI, you can choose:

- **Approve** - one-time approval (existing behavior)
- **+10m** - approve and allow the same pattern for 10 minutes
- **+1h** - approve and allow for 1 hour
- **Always** - approve and permanently allow

### Via CLI

```bash
# Add a grant for exec command
wardgate grants add tessa conclave:obsidian command:rg

# Add a grant for HTTP endpoint
wardgate grants add tessa endpoint:todoist method:DELETE,path:/tasks/*

# Add a time-limited grant
wardgate grants add tessa conclave:obsidian command:rm --duration 10m

# List all active grants
wardgate grants list

# Revoke a grant
wardgate grants revoke grant_a1b2c3d4
```

### Via Admin API

```bash
# List grants
curl -H "Authorization: Bearer $ADMIN_KEY" http://localhost:8080/ui/api/grants

# Add a grant
curl -X POST -H "Authorization: Bearer $ADMIN_KEY" \
  -d '{"agent_id":"tessa","scope":"conclave:obsidian","match":{"command":"rg"},"action":"allow"}' \
  http://localhost:8080/ui/api/grants

# Revoke a grant
curl -X DELETE -H "Authorization: Bearer $ADMIN_KEY" \
  http://localhost:8080/ui/api/grants/grant_a1b2c3d4
```

## Grant Fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Auto-generated unique ID (e.g., `grant_a1b2c3d4`) |
| `created_at` | timestamp | When the grant was created |
| `expires_at` | timestamp | When it expires (`null` = permanent) |
| `agent_id` | string | Agent this grant applies to (`*` = all agents) |
| `scope` | string | Resource scope (`endpoint:name` or `conclave:name`) |
| `match` | object | What to match (see below) |
| `action` | string | `allow` |
| `reason` | string | Human-readable reason |

### Match Fields

For **exec** grants (conclaves):

| Field | Description |
|-------|-------------|
| `command` | Command name (e.g., `rg`, `rm`) |
| `args_pattern` | Regex for arguments (future) |
| `cwd_pattern` | Glob for working directory (future) |

For **HTTP** grants (endpoints):

| Field | Description |
|-------|-------------|
| `method` | HTTP method (e.g., `DELETE`) |
| `path` | Path pattern with wildcard (e.g., `/tasks/*`) |

## Configuration

```yaml
server:
  grants_file: grants.json  # Default; change to customize location
```

## Persistence

- Grants persist in `grants.json` using atomic writes (write to temp file, rename)
- Expired grants are automatically pruned on load and periodically
- The file is machine-managed - don't edit it by hand (use CLI or API instead)

## Examples

### Allow rg on obsidian for all agents, permanently

```bash
wardgate grants add '*' conclave:obsidian command:rg
```

### Allow DELETE on todoist for tessa, for 1 hour

```bash
wardgate grants add tessa endpoint:todoist method:DELETE,path:/tasks/* --duration 1h
```
