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

## Quick Links

- [GitHub Repository](https://github.com/wardgate/wardgate)
- [README](../README.md)
