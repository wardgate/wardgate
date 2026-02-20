# Wardgate Presets Reference

Presets are pre-configured settings for popular APIs. Instead of manually specifying upstream URLs, auth types, and rules, just use a preset name.

Presets are stored as YAML files in the `presets/` directory that ships with Wardgate.

## Quick Start

```yaml
# Point to the presets directory
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

**Important:** You must explicitly specify which capabilities to enable. Any capability not listed is denied by default.

## Available Presets

| Preset | Service | Upstream URL |
|--------|---------|--------------|
| [cloudflare](#cloudflare) | Cloudflare API v4 | `https://api.cloudflare.com/client/v4` |
| [gitea](#gitea) | Gitea Self-Hosted Git Forge | (configure your server) |
| [github](#github) | GitHub REST API | `https://api.github.com` |
| [google-calendar](#google-calendar) | Google Calendar API v3 | `https://www.googleapis.com/calendar/v3` |
| [imap](#imap) | IMAP Email Reading | (configure your server) |
| [pingping](#pingping) | PingPing.io Uptime Monitoring | `https://pingping.io/webapi` |
| [plausible](#plausible) | Plausible Analytics | `https://plausible.io/api/v1` |
| [postmark](#postmark) | Postmark Email API | `https://api.postmarkapp.com` |
| [sentry](#sentry) | Sentry Error Tracking | `https://sentry.io/api/0` |
| [smtp](#smtp) | SMTP Email Sending | (configure your server) |
| [todoist](#todoist) | Todoist Task Management | `https://api.todoist.com/rest/v2` |

You are encouraged to share your own presets with the community by adding them to the `presets/` directory via a Pull Request.

---

## cloudflare

**Cloudflare API v4**

| Capability | Description |
|------------|-------------|
| `read_data` | Read zones, DNS records, and other data |
| `manage_dns` | Create, update, and delete DNS records |
| `purge_cache` | Purge cached content |
| `manage_page_rules` | Create and manage page rules |

**Example:**
```yaml
endpoints:
  cloudflare:
    preset: cloudflare
    auth:
      credential_env: WARDGATE_CRED_CLOUDFLARE_TOKEN
    capabilities:
      read_data: allow
      manage_dns: ask
      purge_cache: allow
```

**Get your token:** [Cloudflare API Tokens](https://dash.cloudflare.com/profile/api-tokens)

---

## gitea

**Gitea Self-Hosted Git Forge API**

Use this preset for Gitea instances. You must set your own `upstream` URL pointing to your instance's API (e.g. `https://gitea.example.com/api/v1`).

| Capability | Description |
|------------|-------------|
| `read_data` | Read repositories, issues, pull requests, users, and other data |
| `create_issues` | Create new issues in repositories |
| `update_issues` | Edit existing issues |
| `create_comments` | Add comments to issues and pull requests |
| `manage_labels` | Add and remove labels on issues |
| `create_pull_requests` | Create new pull requests |
| `update_pull_requests` | Edit existing pull requests |
| `merge_pull_requests` | Merge pull requests |
| `manage_reviews` | Create, submit, and dismiss pull request reviews |
| `manage_releases` | Create, update, and delete releases |
| `manage_files` | Create, update, and delete files in repositories |
| `manage_branches` | Create and delete branches |
| `manage_repos` | Create, edit, and delete repositories |
| `manage_milestones` | Create, update, and delete milestones |
| `manage_webhooks` | Create, update, and delete repository webhooks |
| `manage_organizations` | Create and edit organizations, manage teams and members |

**Example:**
```yaml
endpoints:
  gitea:
    preset: gitea
    upstream: https://gitea.example.com/api/v1
    auth:
      credential_env: WARDGATE_CRED_GITEA_TOKEN
    capabilities:
      read_data: allow
      create_issues: allow
      create_comments: allow
      create_pull_requests: ask
      merge_pull_requests: ask
```

**Get your token:** In your Gitea instance, go to Settings > Applications > Generate New Token

---

## github

**GitHub REST API**

| Capability | Description |
|------------|-------------|
| `read_data` | Read repositories, issues, pull requests, and other data |
| `create_issues` | Create new issues in repositories |
| `update_issues` | Edit existing issues (title, body, state, assignees, milestone) |
| `create_comments` | Add comments to issues and pull requests |
| `manage_labels` | Add and remove labels on issues |
| `create_pull_requests` | Create new pull requests |
| `update_pull_requests` | Edit existing pull requests |
| `merge_pull_requests` | Merge pull requests |
| `manage_releases` | Create, update, and delete releases |
| `manage_files` | Create, update, and delete file contents in repositories |
| `manage_branches` | Create and delete branches and branch protection rules |
| `manage_repos` | Create, update, and delete repositories |
| `manage_gists` | Create, update, and delete gists |
| `manage_reactions` | Add and remove reactions on issues, comments, and PRs |

**Example:**
```yaml
endpoints:
  github:
    preset: github
    auth:
      credential_env: WARDGATE_CRED_GITHUB_TOKEN
    capabilities:
      read_data: allow
      create_issues: allow
      update_issues: allow
      create_comments: allow
      create_pull_requests: ask
      merge_pull_requests: ask
```

**Get your token:** [GitHub Personal Access Tokens](https://github.com/settings/tokens)

---

## google-calendar

**Google Calendar API v3**

| Capability | Description |
|------------|-------------|
| `read_data` | Read calendars and events |
| `create_events` | Create new calendar events |
| `update_events` | Update existing calendar events |
| `delete_events` | Delete calendar events |

**Example:**
```yaml
endpoints:
  google-calendar:
    preset: google-calendar
    auth:
      credential_env: WARDGATE_CRED_GOOGLE_CALENDAR
    capabilities:
      read_data: allow
      create_events: allow
      update_events: ask
      delete_events: deny
```

**Get your token:** Use OAuth2 to obtain an access token from [Google Cloud Console](https://console.cloud.google.com/)

---

## imap

**IMAP Email Reading**

Use this preset for reading emails via IMAP. You must set your own `upstream` URL.

| Capability | Description |
|------------|-------------|
| `list_folders` | List mailbox folders |
| `read_inbox` | Read messages from inbox |
| `read_all_folders` | Read messages from any folder |
| `mark_read` | Mark messages as read |
| `move_messages` | Move messages between folders |

**Example:**
```yaml
endpoints:
  mail:
    preset: imap
    upstream: imaps://imap.gmail.com:993
    auth:
      credential_env: WARDGATE_CRED_IMAP  # format: user:password
    capabilities:
      list_folders: allow
      read_inbox: allow
      mark_read: ask
```

**Credentials:** Format is `username:password` (use app passwords for Gmail)

---

## pingping

**PingPing.io Uptime Monitoring API**

| Capability | Description |
|------------|-------------|
| `read_data` | Read monitors and statistics |
| `create_monitors` | Create new website monitors |
| `update_monitors` | Update existing monitors |
| `delete_monitors` | Delete monitors |
| `manage_checks` | Update, enable, and disable checks |

**Example:**
```yaml
endpoints:
  pingping:
    preset: pingping
    auth:
      credential_env: WARDGATE_CRED_PINGPING_TOKEN
    capabilities:
      read_data: allow
      create_monitors: ask
      update_monitors: ask
      delete_monitors: deny
```

**Get your token:** [PingPing Account → API](https://pingping.io/account/settings)

---

## plausible

**Plausible Analytics API**

| Capability | Description |
|------------|-------------|
| `read_data` | Read analytics data and stats |
| `send_events` | Send custom events |

**Example:**
```yaml
endpoints:
  plausible:
    preset: plausible
    auth:
      credential_env: WARDGATE_CRED_PLAUSIBLE_TOKEN
    capabilities:
      read_data: allow
```

**Get your token:** [Plausible Settings](https://plausible.io/settings)

---

## postmark

**Postmark Email Delivery API**

| Capability | Description |
|------------|-------------|
| `read_data` | Read server info, stats, and message history |
| `send_email` | Send single emails |
| `send_batch` | Send batch emails |
| `send_templates` | Send emails using templates |

**Example:**
```yaml
endpoints:
  postmark:
    preset: postmark
    auth:
      credential_env: WARDGATE_CRED_POSTMARK_TOKEN
    capabilities:
      read_data: allow
      send_email: ask
```

**Get your token:** [Postmark Server Settings](https://account.postmarkapp.com/servers)

---

## sentry

**Sentry Error Tracking API**

| Capability | Description |
|------------|-------------|
| `read_data` | Read projects, issues, and events |
| `resolve_issues` | Resolve and update issue status |
| `manage_projects` | Create and update projects |

**Example:**
```yaml
endpoints:
  sentry:
    preset: sentry
    auth:
      credential_env: WARDGATE_CRED_SENTRY_TOKEN
    capabilities:
      read_data: allow
      resolve_issues: ask
```

**Get your token:** [Sentry Auth Tokens](https://sentry.io/settings/account/api/auth-tokens/)

---

## smtp

**SMTP Email Sending**

Use this preset for sending emails via SMTP. You must set your own `upstream` URL.

| Capability | Description |
|------------|-------------|
| `send_email` | Send emails |

**Example:**
```yaml
endpoints:
  mail-send:
    preset: smtp
    upstream: smtps://smtp.gmail.com:465
    auth:
      credential_env: WARDGATE_CRED_SMTP  # format: user:password
    capabilities:
      send_email: ask
```

**Credentials:** Format is `username:password` (use app passwords for Gmail)

**Note:** For additional SMTP features like recipient allowlists and content filtering, add the `smtp:` configuration block. See the [SMTP configuration](config.md#smtp-endpoints) for details.

---

## todoist

**Todoist Task Management API**

| Capability | Description |
|------------|-------------|
| `read_data` | Read tasks, projects, labels, and other data |
| `create_tasks` | Create new tasks |
| `close_tasks` | Mark tasks as complete |
| `update_tasks` | Update existing tasks |
| `delete_tasks` | Delete tasks permanently |
| `manage_projects` | Create, update, and delete projects |

**Example:**
```yaml
endpoints:
  todoist:
    preset: todoist
    auth:
      credential_env: WARDGATE_CRED_TODOIST_API_KEY
    capabilities:
      read_data: allow
      create_tasks: allow
      close_tasks: allow
      update_tasks: ask
      delete_tasks: deny
```

**Get your API key:** [Todoist Developer Settings](https://todoist.com/app/settings/integrations/developer)

---

## Capability Actions

Each capability must be set to one of three actions:

| Action | Description |
|--------|-------------|
| `allow` | Permit the operation immediately |
| `deny` | Block the operation with an error |
| `ask` | Require human approval before proceeding |

**Note:** Any operation not covered by a configured capability is automatically denied.

---

## Custom Presets

You can also define your own presets for APIs not included in the available presets. See [Custom Presets](config.md#custom-presets-user-defined) in the Configuration Reference.
