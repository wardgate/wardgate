---
name: wardgate-cli
description: >
  Use wardgate-cli to call APIs and execute commands on remote conclaves through Wardgate.
  Use when the user asks to interact with external services (APIs, tools), run shell commands
  on remote environments, or when wardgate-cli is available in the toolset instead of curl.
compatibility: Requires wardgate-cli binary with a baked-in config path pointing to a valid config.
allowed-tools: Bash(wardgate-cli:*)
---

# wardgate-cli

A restricted HTTP client and remote execution tool. It replaces `curl` for API calls and provides `exec` for running commands on isolated remote environments (conclaves) - all routed through a Wardgate security proxy.

## Key constraints

- **Fixed server** - the Wardgate server URL is compiled into the binary; you cannot change it.
- **No arbitrary URLs** - only paths on the configured Wardgate server are allowed.
- **Auth is automatic** - the agent key comes from config; do not pass `Authorization` headers.

## Discovery - always start here

Before making requests, discover what's available:

```bash
# List API endpoints
wardgate-cli endpoints

# List remote execution environments
wardgate-cli conclaves
```

`endpoints` returns JSON with `name`, `description`, `upstream`, and `docs_url` for each endpoint.
`conclaves` returns JSON with the available remote environments and their descriptions.

Use the endpoint name as the first path segment in requests (e.g. `/todoist/tasks`).

## Making API requests

Syntax mirrors curl but only the path is needed (server and auth are automatic):

```bash
# GET (default method)
wardgate-cli /todoist/tasks

# POST with JSON body
wardgate-cli -X POST -H "Content-Type: application/json" -d '{"content":"Buy milk"}' /todoist/tasks

# PUT
wardgate-cli -X PUT -H "Content-Type: application/json" -d '{"content":"Buy oat milk"}' /todoist/tasks/123

# DELETE
wardgate-cli -X DELETE /todoist/tasks/123

# Save response to file
wardgate-cli -o response.json /github/repos/owner/repo/issues

# Check status code
wardgate-cli -s -w '%{http_code}' /todoist/tasks
```

### Options

| Flag | Description |
|------|-------------|
| `-X METHOD` | HTTP method (default: GET) |
| `-H "Key: Value"` | Add header |
| `-d 'body'` | Request body (sets method to POST if -X not given by convention, but wardgate-cli defaults to GET - always pass `-X POST` explicitly) |
| `-o file` | Write response body to file |
| `-s` | Silent - suppress status line on stderr |
| `-v` | Verbose output |
| `-L` | Follow redirects (same-host only) |
| `-w format` | Write-out format (`%{http_code}`, `%{http_status}`) |

## Executing commands on conclaves

Conclaves are isolated remote environments (e.g. a code repo, a notes vault). Commands are policy-checked before execution.

```bash
wardgate-cli exec <conclave> "<command>"
wardgate-cli exec -C /path <conclave> "<command>"
```

### Examples

```bash
# Search in a notes vault
wardgate-cli exec obsidian "rg 'meeting notes' ."

# Git operations in a code repo
wardgate-cli exec code "git status"
wardgate-cli exec code "git diff HEAD~1"

# Piped commands (each segment is policy-checked)
wardgate-cli exec code "rg TODO src/ | head -20"

# Chained commands
wardgate-cli exec code "git add . && git commit -m 'fix typo'"

# Set working directory
wardgate-cli exec -C /data/archive obsidian "ls -la"
```

### What's rejected

Command substitution (`$()`), backticks, process substitution (`<()`, `>()`), subshells (`(...)`), and shell redirections (`>`, `>>`, `<`, etc.) are **always rejected** - they allow hidden command execution or uncontrolled file writes that can't be policy-checked.

### Policy actions

Commands may be `allow`ed, `deny`ed, or require human approval (`ask`). If a command is denied, the error message will tell you why. Do not retry denied commands with tricks to bypass policy - this will also be denied.

## Running custom command templates

Conclaves can define pre-made command templates that agents invoke by name. Templates have shell-escaped argument substitution built in, so you only pass the values.

```bash
wardgate-cli run <conclave> <command> [args...]
wardgate-cli run -C /path <conclave> <command> [args...]
```

### Discovery

The `conclaves` output includes available commands and their arguments for each conclave. Always check before guessing command names.

### Examples

```bash
# Search notes by filename pattern
wardgate-cli run obsidian search "*.md"

# Search note contents
wardgate-cli run obsidian grep "TODO"

# Command with no arguments
wardgate-cli run obsidian status

# With working directory
wardgate-cli run -C /data/archive obsidian search "*.txt"
```

### How it works

1. The server looks up the named command template for that conclave.
2. Your positional args are shell-escaped and substituted into `{placeholder}` positions in the template.
3. The expanded command is executed on the conclave (same policy actions as `exec` - `allow`, `deny`, or `ask`).

If the number of arguments doesn't match what the template expects, you'll get an error.

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| "server not configured" | Config file missing or empty `server` | Check config exists at the baked-in path |
| "Error: key not found" | No `key` or `key_env` in config | Ensure the agent key is configured |
| Non-2xx response | Upstream API error or policy denial | Read the response body for details |
| "Command not in allowlist" | Conclave policy denied the command | Use only allowed commands; run `wardgate-cli conclaves` to check |
| "not supported" on exec | Used `$()`, backticks, or subshells | Rewrite without shell substitution |
| "redirections are not allowed" | Used `>`, `>>`, `<`, etc. | Use `tee` to write files instead of redirections |

## Important rules

1. **Always run `wardgate-cli endpoints` or `wardgate-cli conclaves` first** to discover what's available before guessing paths.
2. **Never fabricate endpoint paths** - use only what discovery returns.
3. **Do not pass Authorization/Bearer headers** - auth is handled automatically.
4. **Do not use curl** - use `wardgate-cli` for all HTTP requests to external services.
5. **Respect denials** - if a command or request is denied by policy, do not attempt workarounds.
