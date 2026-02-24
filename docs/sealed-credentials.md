# Sealed Credentials

Sealed credentials let agents carry their own encrypted API keys. Wardgate decrypts them at proxy time and forwards the real values to upstream services. Agents never see the plaintext -- even if they dump the encrypted values, those are useless without the seal key (which lives only on the Wardgate server).

## When to Use

**Static credentials** (`credential_env`) work well when the operator controls all upstream API keys centrally. Sealed credentials are better when:

- Agents need **individual API keys** for the same service (e.g., per-agent GitHub tokens)
- You're running agents in **sandboxed environments** (conclaves) and don't want to manage every upstream key on the server
- You want the agent to **control which headers** reach upstream (any auth scheme: Bearer, API key, Basic, custom headers)

## How It Works

The operator encrypts upstream credentials using a shared seal key. The encrypted values are given to agents (e.g., as environment variables in their sandbox). Agents send them as `X-Wardgate-Sealed-*` prefixed headers. Wardgate strips the prefix, decrypts, and forwards.

```
Agent sends:
  Authorization: Bearer <jwt-for-wardgate-auth>
  X-Wardgate-Sealed-Authorization: <encrypt("Bearer ghp_realtoken")>
  X-Wardgate-Sealed-X-Api-Key:     <encrypt("key_12345")>

Wardgate processes:
  1. JWT auth validates agent identity
  2. Strip "X-Wardgate-Sealed-" prefix  ->  Authorization, X-Api-Key
  3. Decrypt value                      ->  "Bearer ghp_realtoken", "key_12345"
  4. Strip agent's Authorization header (it's for Wardgate, not upstream)

Upstream receives:
  Authorization: Bearer ghp_realtoken
  X-Api-Key: key_12345
```

No mapping config needed. The agent is in full control of what headers reach upstream.

## Setup

### 1. Generate a seal key

```bash
# Generate a 32-byte hex-encoded AES-256 key
export WARDGATE_SEAL_KEY=$(openssl rand -hex 32)

# Add to your .env file
echo "WARDGATE_SEAL_KEY=$WARDGATE_SEAL_KEY" >> .env
```

### 2. Configure the server

```yaml
server:
  listen: :8080
  jwt:
    secret_env: JWT_SECRET
  seal:
    key_env: WARDGATE_SEAL_KEY    # 32-byte hex-encoded AES-256 key

endpoints:
  github:
    upstream: https://api.github.com
    auth:
      sealed: true                # credentials come from agent's sealed headers
    rules:
      - match: { method: GET }
        action: allow
      - match: { method: POST, path: "/repos/*/issues" }
        action: allow
      - match: { method: "*" }
        action: deny
```

When `sealed: true`:
- `type` and `credential_env` are **not required** (the agent provides credentials)
- `credential_env` and `sealed` are **mutually exclusive** -- you cannot set both
- `server.seal` must be configured or validation fails
- All policy evaluation (rules, grants, rate limits) still applies normally

### Header Whitelist (Optional)

By default, only common authentication headers are allowed to be sealed:
- `Authorization`
- `X-Api-Key`
- `X-Auth-Token`
- `Proxy-Authorization`

To allow additional headers, configure `allowed_headers`:

```yaml
server:
  seal:
    key_env: WARDGATE_SEAL_KEY
    allowed_headers:
      - Authorization
      - X-Api-Key
      - X-Custom-Header  # Allow custom headers if needed
```

Headers not in the whitelist will be rejected to prevent agents from sealing sensitive headers like `Host` or `Cookie`.

### 3. Encrypt credentials for agents

```bash
# Encrypt an upstream API token
wardgate seal "Bearer ghp_agent1_github_token"
# Output: c2VhbGVkX1...base64...

# Encrypt an API key
wardgate seal "key_12345"
# Output: YWJjZGVm...base64...
```

The `seal` command reads `WARDGATE_SEAL_KEY` from the environment (or `.env` file).

### 4. Give encrypted values to agents

Inject the encrypted values as environment variables in the agent's sandbox:

```bash
# Agent's environment
GITHUB_SEALED="c2VhbGVkX1...base64..."
```

### 5. Agent makes requests

The agent prefixes each upstream header name with `X-Wardgate-Sealed-`:

```bash
wardgate-cli \
  -H "X-Wardgate-Sealed-Authorization: $GITHUB_SEALED" \
  https://api.github.com/repos/owner/repo/issues
```

## Configuration Reference

### server.seal

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `key_env` | string | (required) | Environment variable holding the 32-byte hex-encoded AES-256 key |
| `cache_size` | int | `1000` | Maximum number of entries in the decryption LRU cache |
| `allowed_headers` | []string | `["Authorization", "X-Api-Key", "X-Auth-Token", "Proxy-Authorization"]` | Whitelist of header names that can be sealed. If empty, defaults to common auth headers. |

### endpoints.auth.sealed

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `sealed` | bool | `false` | When `true`, credentials come from agent's `X-Wardgate-Sealed-*` headers |

## Encryption Details

- **Algorithm**: AES-256-GCM (authenticated encryption from Go standard library)
- **Key**: 32 bytes, stored as hex in an environment variable
- **Sealed format**: `base64(12-byte-nonce || ciphertext || GCM-tag)`
- **No new dependencies**: uses `crypto/aes` and `crypto/cipher` from the Go standard library

Each encryption produces a unique ciphertext (random nonce), so encrypting the same value twice yields different outputs. Decryption is deterministic.

## Decryption Cache

To avoid repeated AES-GCM decryption for the same sealed values across requests, Wardgate caches results in a fixed-size LRU (least recently used) cache.

- **Key**: the sealed ciphertext string (same ciphertext always maps to the same plaintext)
- **Eviction**: when the cache is full, the least recently used entry is evicted
- **Capacity**: configurable via `cache_size` (default: 1000 entries)
- **Thread safety**: mutex-protected for concurrent access

The cache is purely a performance optimization. Evicted entries are re-decrypted on the next request. There is no TTL -- the ciphertext-to-plaintext mapping is deterministic, so cached entries never become stale.

## Mixing Static and Sealed Endpoints

Sealed credentials are configured per-endpoint. Existing static credential endpoints continue to work unchanged:

```yaml
endpoints:
  # Sealed: agent provides encrypted credentials
  github:
    upstream: https://api.github.com
    auth:
      sealed: true
    rules: [...]

  # Static: Wardgate injects credential from vault
  todoist:
    upstream: https://api.todoist.com
    auth:
      type: bearer
      credential_env: TODOIST_TOKEN
    rules: [...]
```

## Multiple Sealed Headers

The agent can send multiple `X-Wardgate-Sealed-*` headers in a single request. Each is independently decrypted and forwarded:

```bash
wardgate-cli \
  -H "X-Wardgate-Sealed-Authorization: $AUTH_SEALED" \
  -H "X-Wardgate-Sealed-X-Api-Key: $KEY_SEALED" \
  -H "X-Wardgate-Sealed-X-Custom-Header: $CUSTOM_SEALED" \
  https://api.example.com/data
```

## Error Handling

| Condition | Response |
|-----------|----------|
| No `X-Wardgate-Sealed-*` headers on a sealed endpoint | `400 Bad Request` |
| Invalid base64 encoding | Header is skipped (logged) |
| Tampered or invalid ciphertext | Header is skipped (logged) |
| Wrong seal key | Decryption fails, header is skipped |

## Security Considerations

- The seal key must be kept secret. It should only exist on the Wardgate server (in the `.env` file or process environment).
- Encrypted values are safe to give to agents -- they cannot be decrypted without the seal key.
- All policy rules, rate limits, grants, and approval workflows still apply to sealed endpoints.
- Sealed headers are stripped before forwarding to upstream -- the upstream never sees `X-Wardgate-Sealed-*` headers.
- Non-sealed headers (e.g., `Content-Type`, `Accept`) are passed through to upstream unchanged.
- Sealed values have no replay protection -- the same encrypted value can be used across multiple requests. This is by design, as sealed credentials represent long-lived API keys that agents reuse. Treat sealed values with the same care as the plaintext credentials they contain.
