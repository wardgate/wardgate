# Wardgate - AI Agent Security Gateway

*Wardgate* is a security proxy that sits between AI agents and external services, providing credential isolation, access control, and audit logging.

Give your AI agents API access — without giving them your credentials.

## The Problem

AI agents are powerful. They can manage your calendar, check your email, update your tasks, and automate your life. But to do that, they need access to your accounts.

They are also like a whizzkid-teenager. They know a lot, but they have a mind of their own. They are gullible (to prompt injections). And just like teens, they don't really think. So thinking about consequences is definitely above their pay-grade!

Containerization is a great way to isolate your agents. But it's not a silver bullet. You still need to be careful about what you put in your containers. They still get your credentials via environment variables or another way. Otherwise they can't help you.

And projects like [OpenClaw](https://github.com/openclaw/openclaw) have all these access control features built in. But unless you gutted capabilities beyond being useful, you have to trust the agent, the application or all the thousands of commits / pull requests that it does not change its own permissions or capabilities and actually does what you want it to do.

Do you really want to give your API keys or e-mail access to that? Give an AI agent direct access to your inbox, 2-factor authentication codes, or other sensitive data? Trust that it won't exfiltrate your data or go rogue, after reading some clever prompt injection?

*The risk is real:*
- Credentials in prompts can leak through model outputs, logs, or attacks
- Prompt injection can make agents do things you didn't intend
- A compromised agent has the same access you gave it — to everything

## The Solution

Wardgate sits between your agents and the outside world. Agents talk to Wardgate. Wardgate talks to APIs. Your credentials never leave Wardgate.


```mermaid
flowchart LR
    Agent[AI Agent - no creds] -->|HTTP(S)| Wardgate[Wardgate - has creds]
    Wardgate -->|API| Service[External Service]
```

*What Wardgate gives you:*

- *Credential Isolation* - Agents never see your API keys, OAuth tokens, or passwords
- *Access Control* - Define what each agent can do: read-only calendar, no email deletion, ask before sending
- *Audit Logging* - Every request logged (metadata only, not content) - know exactly what your agents did
- *Approval Workflows* - Require human approval for sensitive operations (send email, delete data)
- *Anomaly Detection* - Alert on unusual patterns (suddenly fetching 100 emails, or a specific folder, or things from the past?)

## Who Is This For?

You want to use AI agents like [OpenClaw](https://github.com/openclaw/openclaw), [AutoGPT](https://github.com/Significant-Gravitas/AutoGPT), or custom LLM tooling - but you're not comfortable giving them direct access to your life. I'm not sure I'll ever be comfortable with AI agents that have built-in access control.

Wardgate lets you get the benefits of AI automation while keeping a security boundary between the agent and your accounts.

*Use cases:*
- Personal AI assistant with calendar, email, and task access
- Development agents with limited API access
- Multi-agent setups where you want isolation between agents
- Anywhere you'd otherwise paste credentials into an agent's config

## Quick Example

So if you want your agent to be able to read your Todoist tasks, you can configure Wardgate to allow it to do so.

Instead of giving your agent a Todoist API key:

# Agent config (dangerous)
```yaml
todoist_api_key: "abc123..."
```

You configure Wardgate:

# Wardgate config (agent never sees this)
```yaml
server:
  listen: ":8080"

agents:
  - id: agent-name
    key_env: WARDGATE_AGENT_KEY

endpoints:
  todoist-api:
    upstream: https://api.todoist.com/rest/v2
    auth:
      type: bearer
      credential_env: WARDGATE_CRED_TODOIST_API_KEY
    default_rules:
      - match: { method: GET }
        action: allow
      - match: { method: DELETE }
        action: deny
      - match: { method: "*" }
        action: ask  # Human approval required
```

Your agent calls https://wardgate.internal/todoist/tasks — Wardgate injects the real credentials and enforces your rules.

## Quick Start

```bash
# Copy and edit .env file
cp .env.example .env
# Edit .env with your credentials

# Copy and edit config.yaml file
cp config.yaml.example config.yaml
# Edit config.yaml with your configuration

# Run (automatically loads .env)
./wardgate -config config.yaml

# Or specify a different env file
./wardgate -config config.yaml -env /path/to/.env
```

## Usage

Agents make requests to the gateway instead of directly to APIs:

```bash
# Instead of: curl -H "Authorization: Bearer $TODOIST_KEY" https://api.todoist.com/rest/v2/tasks
# Use:
curl -H "Authorization: Bearer $AGENT_KEY" http://localhost:8080/todoist-api/tasks
```

The gateway:
1. Validates the agent's key
2. Evaluates policy rules (allow/deny)
3. Injects the real API credential
4. Forwards the request to the upstream
5. Logs the request/response
6. Returns the response to the agent

## Configuration

See `config.yaml.example` for an example. Key sections:

- `server.listen` — Address to listen on (default `:8080`)
- `agents` — List of agents and their key env vars
- `endpoints` — Map of endpoint name to upstream config and rules

## Policy Rules

Rules are evaluated in order. First match wins.

```yaml
rules:
  - match: { method: GET }
    action: allow
  - match: { method: POST, path: "/tasks" }
    action: allow
  - match: { method: "*" }
    action: deny
    message: "Not permitted"
```

Supported actions: `allow`, `deny`

## Building

```bash
go build -o wardgate ./cmd/wardgate
```

## Testing

```bash
go test ./...
```