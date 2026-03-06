---
name: wardgate-proxy
description: >
  Use wardgate-proxy to call APIs through Wardgate. Use when the agent runs behind a local
  wardgate-proxy and needs to make HTTP requests to external services. The agent uses any
  HTTP client (curl, requests, fetch) pointing at the proxy address -- no special binary needed.
compatibility: Requires wardgate-proxy running on a known local address (default 127.0.0.1:18080).
---

# wardgate-proxy

A local reverse proxy that transparently injects the agent key into every request to a Wardgate server. The agent makes plain HTTP requests to the proxy using any HTTP client -- no special binary, no shell access, no restarts required.

## How it works

```
Your HTTP request → http://127.0.0.1:18080/todoist/tasks
  → wardgate-proxy injects agent key
    → https://wardgate.example.com/todoist/tasks (with Bearer <agent-key>)
      → response streamed back to you
```

The proxy handles authentication automatically. You never see or manage the agent key.

## Key constraints

- **Fixed server** -- all requests go to the Wardgate server configured in the proxy. You cannot change the destination.
- **Auth is automatic** -- do not pass `Authorization` headers. Any you send will be overwritten by the proxy.
- **Access control is server-side** -- Wardgate's policy engine decides what's allowed. If a request is denied, the response will tell you why.

## Discovery -- always start here

Before making requests, discover what endpoints are available:

```bash
curl http://127.0.0.1:18080/endpoints
```

Returns JSON with `name`, `description`, `upstream`, and `docs_url` for each endpoint. Use the endpoint name as the first path segment in all requests.

## Making requests

Use any HTTP client. The proxy address is `http://127.0.0.1:18080` by default.

### curl

```bash
# GET
curl http://127.0.0.1:18080/todoist/tasks

# POST with JSON body
curl -X POST -H "Content-Type: application/json" \
  -d '{"content":"Buy milk"}' \
  http://127.0.0.1:18080/todoist/tasks

# PUT
curl -X PUT -H "Content-Type: application/json" \
  -d '{"content":"Buy oat milk"}' \
  http://127.0.0.1:18080/todoist/tasks/123

# DELETE
curl -X DELETE http://127.0.0.1:18080/todoist/tasks/123
```

### Python

```python
import requests

BASE = "http://127.0.0.1:18080"

# GET
tasks = requests.get(f"{BASE}/todoist/tasks").json()

# POST
requests.post(f"{BASE}/todoist/tasks", json={"content": "Buy milk"})
```

### Node.js

```javascript
const BASE = "http://127.0.0.1:18080";

// GET
const resp = await fetch(`${BASE}/todoist/tasks`);
const tasks = await resp.json();

// POST
await fetch(`${BASE}/todoist/tasks`, {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ content: "Buy milk" }),
});
```

## Custom headers

All headers you send (except `Authorization` and `X-Forwarded-For`) are forwarded to the Wardgate server and then to the upstream API. Use this for `Content-Type`, `Accept`, or any other headers the upstream expects.

```bash
curl -H "Accept: application/json" \
  -H "X-Request-Id: req-123" \
  http://127.0.0.1:18080/my-endpoint/resource
```

### Sealed credentials

If the endpoint is configured for sealed credentials, you can send encrypted headers via `X-Wardgate-Sealed-*` prefixed headers. The Wardgate server decrypts them before forwarding to the upstream.

```bash
# Encrypt a credential with the endpoint's public seal key, then send it:
curl -H "X-Wardgate-Sealed-Authorization: <base64-encrypted-value>" \
  http://127.0.0.1:18080/my-endpoint/resource
```

Allowed sealed headers: `Authorization`, `X-Api-Key`, `X-Auth-Token`, `Proxy-Authorization`. The Wardgate server strips the `X-Wardgate-Sealed-` prefix after decryption and sets the real header.

## Streaming and SSE

The proxy supports Server-Sent Events and chunked transfer encoding. Responses are flushed immediately -- no buffering.

```bash
curl -N http://127.0.0.1:18080/my-endpoint/events
```

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| Connection refused on `:18080` | Proxy not running | Check that wardgate-proxy is started |
| 502 Bad Gateway with "key error" | Proxy cannot read agent key | Check proxy config file |
| 502 Bad Gateway with "proxy error" | Wardgate server unreachable | Check proxy's `server` config |
| 403 Forbidden | Wardgate policy denied the request | Read the response body; use only allowed methods/paths |
| 401 Unauthorized | Agent key invalid or expired | Key rotation needed in proxy config |

## Important rules

1. **Always call `/endpoints` first** to discover what's available before guessing paths.
2. **Never fabricate endpoint paths** -- use only what discovery returns.
3. **Do not pass Authorization headers** -- auth is handled by the proxy.
4. **Respect denials** -- if a request is denied by policy, do not attempt workarounds.
5. **Use the proxy address for all external service requests** -- do not call upstream APIs directly.
