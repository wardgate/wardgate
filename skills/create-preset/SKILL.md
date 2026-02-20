---
name: create-preset
description: >
  Create Wardgate preset YAML files for APIs. Use when the user asks to add a new preset,
  create a preset for an API, or add support for a new service to Wardgate.
---

# Create Preset

Create preset YAML files in `presets/` that define API capabilities for Wardgate endpoints.

## Workflow

1. **Research the API** - look up the target API's documentation to understand its endpoint structure, HTTP methods, URL patterns, and authentication method.
2. **Design capabilities** - group endpoints into logical capabilities (read, create, update, delete patterns). Each capability maps to one or more match rules.
3. **Write the preset file** - create `presets/<name>.yaml` following the format below.
4. **Update docs** - add the preset to `docs/presets.md` following the existing pattern.

## Preset File Format

```yaml
name: <preset-name>
description: "<Human-readable service description>"
upstream: <base-url>
docs_url: <api-docs-url>
auth_type: bearer | plain
adapter: http | imap | smtp  # omit for http (default)

capabilities:
  - name: <capability_name>
    description: "<What this capability allows>"
    rules:
      - match: { method: GET, path: "/some/path/*" }
      - match: { method: POST, path: "/other/path" }
```

### Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | No | Defaults to filename without extension |
| `description` | Yes | Short human-readable description of the API |
| `upstream` | Yes | Base URL (omit if instance-dependent, user overrides in config) |
| `docs_url` | No | Link to API documentation |
| `auth_type` | Yes | `bearer` (Authorization: Bearer token) or `plain` (username:password) |
| `adapter` | No | `imap`, `smtp`, or omit for HTTP |
| `capabilities` | No | List of named capability groups |

### Match Rules

Rules use `method` and `path` with `*` as a single-segment wildcard.

| Pattern | Matches |
|---------|---------|
| `{ method: GET }` | All GET requests |
| `{ method: POST, path: "/tasks" }` | POST to exactly /tasks |
| `{ method: DELETE, path: "/tasks/*" }` | DELETE to /tasks/{id} |
| `{ method: PUT, path: "/repos/*/*" }` | PUT to /repos/{owner}/{repo} |
| `{ method: PATCH, path: "/repos/*/*/issues/*" }` | PATCH to /repos/{owner}/{repo}/issues/{id} |

## Design Guidelines

### Capability Naming

- Use snake_case: `read_data`, `create_issues`, `manage_dns`
- Start with a verb: `read_`, `create_`, `update_`, `delete_`, `manage_`, `send_`, `merge_`
- `read_data` is the conventional name for the "all GETs" capability
- Use `manage_` when grouping create + update + delete for one resource
- Separate create/update/delete into distinct capabilities when they have meaningfully different risk levels

### Capability Ordering

1. `read_data` first (always)
2. Create operations
3. Update operations
4. Delete operations
5. Grouped manage operations last

### Auth Type Selection

- `bearer` - most REST APIs (token in `Authorization: Bearer <token>` header)
- `plain` - IMAP/SMTP or APIs using username:password credentials

### When to Omit Upstream

Omit `upstream` when the API is self-hosted and instance-dependent (e.g., Gitea, GitLab, Mattermost). The user sets it in their endpoint config:

```yaml
endpoints:
  my-gitea:
    preset: gitea
    upstream: https://gitea.example.com/api/v1
    auth:
      credential_env: WARDGATE_CRED_GITEA_TOKEN
    capabilities:
      read_data: allow
```

## Updating docs/presets.md

After creating the preset file, add an entry to `docs/presets.md`:

1. Add a row to the "Available Presets" table (alphabetical order)
2. Add a section with capability table + example config
3. Include a link to get API credentials when applicable

### Section Template

```markdown
---

## <preset-name>

**<Description>**

| Capability | Description |
|------------|-------------|
| `capability_name` | Description |

**Example:**
\`\`\`yaml
endpoints:
  <name>:
    preset: <preset-name>
    auth:
      credential_env: WARDGATE_CRED_<SERVICE>_TOKEN
    capabilities:
      read_data: allow
      some_write: ask
\`\`\`

**Get your token:** [Link](https://...)
```

## Example: Simple Preset

```yaml
# presets/plausible.yaml
name: plausible
description: "Plausible Analytics API"
upstream: https://plausible.io/api/v1
docs_url: https://plausible.io/docs
auth_type: bearer

capabilities:
  - name: read_data
    description: "Read analytics data and stats"
    rules:
      - match: { method: GET }

  - name: send_events
    description: "Send custom events"
    rules:
      - match: { method: POST, path: "/event" }
```

## Example: Complex Preset

```yaml
# presets/github.yaml
name: github
description: "GitHub REST API"
upstream: https://api.github.com
docs_url: https://docs.github.com/rest
auth_type: bearer

capabilities:
  - name: read_data
    description: "Read repositories, issues, pull requests, and other data"
    rules:
      - match: { method: GET }

  - name: create_issues
    description: "Create new issues in repositories"
    rules:
      - match: { method: POST, path: "/repos/*/*/issues" }

  - name: create_comments
    description: "Add comments to issues and pull requests"
    rules:
      - match: { method: POST, path: "/repos/*/*/issues/*/comments" }
      - match: { method: POST, path: "/repos/*/*/pulls/*/comments" }

  - name: manage_labels
    description: "Add and remove labels on issues"
    rules:
      - match: { method: POST, path: "/repos/*/*/issues/*/labels" }
      - match: { method: DELETE, path: "/repos/*/*/issues/*/labels/*" }

  - name: create_pull_requests
    description: "Create new pull requests"
    rules:
      - match: { method: POST, path: "/repos/*/*/pulls" }

  - name: merge_pull_requests
    description: "Merge pull requests"
    rules:
      - match: { method: PUT, path: "/repos/*/*/pulls/*/merge" }

  - name: manage_releases
    description: "Create, update, and delete releases"
    rules:
      - match: { method: POST, path: "/repos/*/*/releases" }
      - match: { method: PATCH, path: "/repos/*/*/releases/*" }
      - match: { method: DELETE, path: "/repos/*/*/releases/*" }
```

## Checklist

- [ ] API researched (endpoints, methods, URL patterns, auth)
- [ ] Capabilities designed with appropriate granularity
- [ ] `presets/<name>.yaml` created
- [ ] `docs/presets.md` updated with table row + section
