# Wardgate Policy System

This document explains how to write and configure policy rules in Wardgate.

## How Policies Work

When an agent makes a request, Wardgate first checks [dynamic grants](grants.md). If a matching active grant exists, the request is allowed immediately. Otherwise, static rules are evaluated in order until one matches. The first matching rule determines the action. If no rules match, the request is denied by default.

```
Request → Dynamic Grant Check → Static Rule Evaluation → Default Deny
              ↓ (match)                ↓ (first match)
            Allow               allow / deny / ask
```

```
Request: POST /tasks/123/close
         ↓
Rule 1: GET /tasks* → allow         (no match - wrong method)
         ↓
Rule 2: POST /tasks/*/close → allow (match!)
         ↓
Action: allow
```

## Rule Structure

Each rule has three parts:

```yaml
rules:
  - match:        # Conditions that must be true
      method: GET
      path: "/tasks*"
    action: allow # What to do when matched
    message: ""   # Optional message (for deny)
    rate_limit:   # Optional rate limiting
      max: 100
      window: "1m"
    time_range:   # Optional time restrictions
      hours: ["09:00-17:00"]
      days: ["mon", "tue", "wed", "thu", "fri"]
```

## Match Conditions

### Method Matching

Match HTTP methods:

```yaml
# Exact method
- match: { method: GET }

# Any method (wildcard)
- match: { method: "*" }
```

Supported methods: `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`, `*`

### Path Matching

Wardgate supports several path matching patterns:

#### Exact Match

```yaml
- match: { path: "/tasks" }
# Matches: /tasks
# Does not match: /tasks/123, /tasks/
```

#### Trailing Wildcard

```yaml
- match: { path: "/tasks*" }
# Matches: /tasks, /tasks/123, /tasks/123/comments
```

#### Single Segment Wildcard

```yaml
- match: { path: "/tasks/*/close" }
# Matches: /tasks/123/close, /tasks/abc/close
# Does not match: /tasks/close, /tasks/a/b/close
```

#### Multi-Segment Wildcard

```yaml
- match: { path: "/api/**/status" }
# Matches: /api/status, /api/v1/status, /api/v1/tasks/123/status
```

### Combined Conditions

Match conditions are AND-ed together:

```yaml
- match:
    method: POST
    path: "/tasks"
# Both method AND path must match
```

## Actions

### allow

Permits the request to proceed to the upstream service.

```yaml
- match: { method: GET }
  action: allow
```

The agent receives the upstream response as-is.

### deny

Blocks the request and returns an error to the agent.

```yaml
- match: { method: DELETE }
  action: deny
  message: "Deletion is not permitted"
```

Returns HTTP 403 Forbidden with the message.

### ask

Requires human approval before proceeding. The request blocks until approved, denied, or timeout.

```yaml
- match: { method: PUT }
  action: ask
```

When an `ask` rule matches:

1. Request is held pending
2. Notification sent to configured channels (Slack, webhook)
3. Human clicks approve or deny link
4. Request proceeds or returns 403

**Note:** The agent blocks while waiting. Configure `notify.timeout` to limit wait time.

## Rate Limiting

Prevent agents from making too many requests:

```yaml
- match: { method: GET }
  action: allow
  rate_limit:
    max: 100      # Maximum requests allowed
    window: "1m"  # Time window
```

### Window Format

- Seconds: `"30s"`
- Minutes: `"5m"`
- Hours: `"1h"`

### Rate Limit Behavior

- Limits are per-agent (identified by agent ID from config)
- Each rule has independent limits
- When exceeded, returns HTTP 429 Too Many Requests
- `Retry-After` header indicates when to retry

### Examples

```yaml
# 100 requests per minute for reads
- match: { method: GET }
  action: allow
  rate_limit: { max: 100, window: "1m" }

# 10 writes per hour
- match: { method: POST }
  action: allow
  rate_limit: { max: 10, window: "1h" }

# Strict limit on sensitive endpoint
- match: { method: GET, path: "/admin*" }
  action: allow
  rate_limit: { max: 5, window: "1m" }
```

## Time-Based Rules

Restrict when rules apply:

```yaml
- match: { method: POST }
  action: allow
  time_range:
    hours: ["09:00-17:00"]
    days: ["mon", "tue", "wed", "thu", "fri"]
```

### Hours Format

24-hour format ranges:

```yaml
hours:
  - "09:00-17:00"  # 9 AM to 5 PM
  - "00:00-06:00"  # Midnight to 6 AM
```

Multiple ranges can be specified (OR logic).

### Days Format

Three-letter day abbreviations:

```yaml
days: ["mon", "tue", "wed", "thu", "fri"]  # Weekdays
days: ["sat", "sun"]                        # Weekends
days: ["mon", "wed", "fri"]                 # Specific days
```

### Time Range Behavior

When a request arrives outside the specified time range:
- The rule is skipped (not matched)
- Evaluation continues to the next rule
- This is NOT a deny - it just doesn't match

### Examples

```yaml
# Allow writes only during business hours
- match: { method: POST }
  action: allow
  time_range:
    hours: ["09:00-18:00"]
    days: ["mon", "tue", "wed", "thu", "fri"]

# Catch-all deny outside business hours
- match: { method: POST }
  action: deny
  message: "Writes only allowed during business hours"

# Allow reads anytime
- match: { method: GET }
  action: allow
```

## Common Policy Patterns

### Read-Only Access

```yaml
rules:
  - match: { method: GET }
    action: allow
  - match: { method: HEAD }
    action: allow
  - match: { method: "*" }
    action: deny
    message: "Read-only access"
```

### Allow Specific Operations

```yaml
rules:
  # Allow reading tasks
  - match: { method: GET, path: "/tasks*" }
    action: allow
  
  # Allow creating tasks
  - match: { method: POST, path: "/tasks" }
    action: allow
  
  # Allow closing tasks
  - match: { method: POST, path: "/tasks/*/close" }
    action: allow
  
  # Deny everything else
  - match: { method: "*" }
    action: deny
```

### Approval for Sensitive Operations

```yaml
rules:
  # Auto-allow reads
  - match: { method: GET }
    action: allow
  
  # Auto-allow common writes
  - match: { method: POST, path: "/tasks" }
    action: allow
  
  # Require approval for updates
  - match: { method: PUT }
    action: ask
  
  # Require approval for deletes
  - match: { method: DELETE }
    action: ask
```

### Rate-Limited API Access

```yaml
rules:
  # Generous limit for reads
  - match: { method: GET }
    action: allow
    rate_limit: { max: 1000, window: "1h" }
  
  # Strict limit for writes
  - match: { method: POST }
    action: allow
    rate_limit: { max: 100, window: "1h" }
  
  # Very strict limit for deletes
  - match: { method: DELETE }
    action: allow
    rate_limit: { max: 10, window: "1h" }
```

### Business Hours Only

```yaml
rules:
  # Allow during business hours
  - match: { method: "*" }
    action: allow
    time_range:
      hours: ["09:00-18:00"]
      days: ["mon", "tue", "wed", "thu", "fri"]
  
  # Require approval outside business hours
  - match: { method: "*" }
    action: ask
```

### Tiered Access by Sensitivity

```yaml
rules:
  # Public endpoints - allow freely
  - match: { method: GET, path: "/public*" }
    action: allow
  
  # Normal operations - allow with rate limit
  - match: { method: GET }
    action: allow
    rate_limit: { max: 100, window: "1m" }
  
  # Sensitive operations - require approval
  - match: { method: "*", path: "/admin*" }
    action: ask
  
  # Everything else - deny
  - match: { method: "*" }
    action: deny
```

## Conclave Policies (Remote Execution)

Each conclave has its own `rules:` section that evaluates shell commands sent via `wardgate-cli exec`.

### Exec Match Fields

| Field | Type | Description |
|-------|------|-------------|
| `command` | glob | Command name (e.g., `rg`, `python*`, `*`) |
| `args_pattern` | regex | Joined argument string (e.g., `^(status\|log\|diff)`) |
| `cwd_pattern` | glob | Working directory (e.g., `/data/vault/**`) |

All fields are optional and AND-ed together.

### Conclave Policy Examples

```yaml
conclaves:
  code:
    key_env: WARDGATE_CONCLAVE_CODE_KEY
    rules:
      # Read-only tools
      - match: { command: "rg" }
        action: allow
      - match: { command: "cat" }
        action: allow

      # Git: allow reads, ask for writes
      - match: { command: "git", args_pattern: "^(status|log|diff|show)" }
        action: allow
      - match: { command: "git", args_pattern: "^(push|commit|rebase)" }
        action: ask

      # Restrict to a directory
      - match: { command: "git", cwd_pattern: "/home/dev/project/**" }
        action: allow

      # Default deny
      - match: { command: "*" }
        action: deny
```

### Shell Pipelines

When agents run piped commands (`rg TODO | head -20`), `wardgate-cli` parses the pipeline and evaluates each segment individually. All must pass.

Supported: pipes (`|`), chains (`&&`, `||`, `;`), redirections (`>`, `>>`, `<`).

Rejected: command substitution (`$()`, backticks), process substitution (`<()`, `>()`), subshells (`(cmd)`).

See [Conclaves](conclaves.md) for full documentation.

## Command Template Rules

Command templates (`commands:` in conclave config) support per-argument policy rules, evaluated independently from conclave-level exec rules. This lets you set different actions for different argument values - for example, allowing reads in one directory while requiring approval for another.

### Two Layers

1. **Path validation** (`type: path` + `allowed_paths` on args) - hard boundary, rejects immediately
2. **Command rules** (`rules` on the command) - first-match-wins policy (allow/ask/deny)

### Example

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
      # no catch-all -> unmatched paths default deny

  patch:
    template: "python3 /usr/local/lib/wardgate-tools/file_patch.py {file} {old_text} {new_text}"
    args:
      - name: file
        type: path
        allowed_paths: ["notes/**"]
      - name: old_text
      - name: new_text
    action: ask  # no rules, static action for all paths
```

### Rule Evaluation

- Rules are evaluated in order (first match wins)
- Match keys are arg names, values are glob patterns
- Multiple match keys in one rule are AND-ed (all must match)
- If no rule matches, the request is **denied** (consistent with conclave exec rules)
- When no `rules` are present, the `action` field is used directly (default: `allow`)

### Path Validation

Args with `type: path` are validated before rules are evaluated:

- Absolute paths (`/etc/passwd`) are rejected
- Path traversal (`../secret`) is rejected
- The value must match at least one `allowed_paths` glob pattern
- If `allowed_paths` is omitted, no gateway-level restriction is applied (only the script-level CWD check)

## Debugging Policies

### Policy Evaluation Order

Rules are evaluated top-to-bottom. Put more specific rules first:

```yaml
# CORRECT: Specific rules first
rules:
  - match: { method: GET, path: "/tasks/123" }  # Specific
    action: deny
  - match: { method: GET, path: "/tasks*" }      # General
    action: allow

# WRONG: General rule matches first
rules:
  - match: { method: GET, path: "/tasks*" }      # Matches first!
    action: allow
  - match: { method: GET, path: "/tasks/123" }  # Never reached
    action: deny
```

### Default Deny

If no rules match, requests are denied. Always add a catch-all rule at the end:

```yaml
rules:
  - match: { method: GET }
    action: allow
  # ... other rules ...
  
  # Catch-all at the end
  - match: { method: "*" }
    action: deny
    message: "No matching rule"
```

### Audit Logs

Check audit logs to see which rules were evaluated:

```json
{
  "method": "POST",
  "path": "/tasks",
  "decision": "allow",
  "rules_evaluated": ["rule_1", "rule_2"]
}
```

## Migration Guide

### From No Access Control

Start with deny-all, add specific allows:

```yaml
# Step 1: Deny everything
rules:
  - match: { method: "*" }
    action: deny

# Step 2: Add specific allows
rules:
  - match: { method: GET, path: "/tasks" }
    action: allow
  - match: { method: "*" }
    action: deny

# Step 3: Expand as needed
rules:
  - match: { method: GET }
    action: allow
  - match: { method: POST, path: "/tasks" }
    action: allow
  - match: { method: "*" }
    action: deny
```

### From Allow-All

Gradually restrict access:

```yaml
# Step 1: Log everything (still allow)
rules:
  - match: { method: "*" }
    action: allow

# Step 2: Deny dangerous operations
rules:
  - match: { method: DELETE }
    action: deny
  - match: { method: "*" }
    action: allow

# Step 3: Add rate limits
rules:
  - match: { method: DELETE }
    action: deny
  - match: { method: "*" }
    action: allow
    rate_limit: { max: 100, window: "1m" }
```
