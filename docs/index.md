<p align="center">
  <img src="assets/wardgate-banner.png" alt="Wardgate Banner" width="800">
</p>

# Wardgate Documentation

Wardgate is a security proxy that sits between AI agents and external services, providing credential isolation, access control, and audit logging.

## Quick Start with Presets

The easiest way to configure Wardgate is using **presets** - pre-configured settings for popular APIs:

```yaml
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

**Included presets:** `todoist`, `github`, `cloudflare`, `google-calendar`, `postmark`, `sentry`, `plausible`

See the **[Presets Reference](presets.md)** for all capabilities and examples.

## Documentation

- [Presets Reference](presets.md) - Included presets and capabilities and how to make your own
- [Configuration Reference](config.md) - All configuration options
- [Security Architecture](architecture.md) - How Wardgate protects your credentials
- [Policy System](policies.md) - Writing and configuring rules
- [Deployment Guide](deployment.md) - Docker, Caddy, and production setup
- [wardgate-cli](wardgate-cli.md) - Restricted HTTP client for agents (replaces curl)

## Sensitive Data Filtering

Wardgate automatically blocks OTP codes, verification links, and API keys in responses by default. This prevents prompt injection attacks from extracting 2FA codes or credentials. Configure per-endpoint in your config. See [Configuration Reference](config.md#sensitive-data-filtering) for details.

## Endpoint Discovery API

Agents can query `GET /endpoints` to discover available endpoints. See the [README](../README.md#endpoint-discovery-api) for details.

## Admin UI & CLI

Wardgate includes a web dashboard (`/ui/`) and CLI for managing approval requests. Configure `admin_key_env` in your server settings to enable. See the [README](../README.md#admin-ui--cli) for details.

The dashboard includes:
- **Pending** - Requests awaiting approval
- **History** - Past approval decisions
- **Logs** - Recent request activity with filtering

## Quick Links

- [GitHub Repository](https://github.com/wardgate/wardgate)
- [README](../README.md)
