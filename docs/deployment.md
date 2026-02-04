# Deployment Guide

Wardgate can be deployed as a standalone binary or using Docker with an optional Caddy reverse proxy for automatic HTTPS.

## Quick Start with Docker Compose

```bash
# 1. Create config directory
mkdir -p config

# 2. Copy and edit configuration files
cp config.yaml.example config/config.yaml
cp .env.example config/.env

# Edit config/config.yaml and config/.env with your settings

# 3. Start with Docker Compose
docker compose up -d

# View logs
docker compose logs -f wardgate
```

For production with HTTPS:

```bash
DOMAIN=wardgate.example.com docker compose up -d
```

## Configuration

### Directory Structure

```
wardgate/
├── config/
│   ├── config.yaml    # Main configuration
│   └── .env           # Credentials (never commit!)
├── docker compose.yml
├── Caddyfile
└── Dockerfile
```

### Environment Variables

The `config/.env` file contains sensitive credentials:

```bash
# Agent authentication keys
WARDGATE_AGENT_KEY=your-secret-agent-key

# API credentials (referenced in config.yaml)
WARDGATE_CRED_TODOIST_API_KEY=your-todoist-key
WARDGATE_CRED_IMAP=user@gmail.com:app-password
WARDGATE_CRED_SMTP=user@gmail.com:app-password
```

### Base URL Configuration

For notifications to link correctly to the dashboard, set `base_url` in your config to the internal or public URL where Wardgate is accessible for you:

```yaml
server:
  listen: ":8080"
  base_url: "https://wardgate.example.com"
```

## Deployment Options

### Option 1: Docker Compose (Recommended)

Includes Caddy for automatic HTTPS and reverse proxy.

```bash
# Development (localhost, no HTTPS)
docker compose up -d

# Production (automatic HTTPS via Let's Encrypt)
DOMAIN=wardgate.example.com docker compose up -d
```

### Option 2: Docker Only

Run Wardgate without Caddy (bring your own reverse proxy):

```bash
docker build -t wardgate .

docker run -d \
  --name wardgate \
  -p 8080:8080 \
  -v $(pwd)/config:/app/config:ro \
  --env-file ./config/.env \
  wardgate
```

### Option 3: Standalone Binary

```bash
# Build
go build -o wardgate ./cmd/wardgate

# Run
./wardgate -config config.yaml -env .env
```

## Production Checklist

### Security

- [ ] Run Wardgate on a separate VPS from your agents
- [ ] Use WireGuard or private network between agents and gateway
- [ ] Configure firewall to only accept connections from trusted networks
- [ ] Use strong, unique agent keys
- [ ] Enable HTTPS via Caddy or your own reverse proxy

### Configuration

- [ ] Set `base_url` to your internal or public URL where Wardgate is accessible for you
- [ ] Configure notification webhooks for approval workflows
- [ ] Review and tune rate limits
- [ ] Set appropriate timeouts

### Monitoring

- [ ] Configure log aggregation (logs are JSON-formatted)
- [ ] Set up alerts for denied requests
- [ ] Monitor for approval request timeouts

## Network Architecture

Recommended production setup using WireGuard:

```
┌─────────────────────────────────────────┐
│          WireGuard Network              │
│             10.0.0.0/24                 │
│                                         │
│  ┌─────────────────┐  ┌──────────────┐  │
│  │  Agent VPS      │  │ Gateway VPS  │  │
│  │  10.0.0.3       │  │ 10.0.0.5     │  │
│  │                 │  │              │  │
│  │  AI agents run  │──│ Wardgate +   │──│──▶ Internet
│  │  here           │  │ Caddy run    │  │
│  │                 │  │ here         │  │
│  └─────────────────┘  └──────────────┘  │
└─────────────────────────────────────────┘
```

Key principles:
- Gateway VPS has no agent code
- Credentials never leave the gateway
- Agents authenticate to gateway with their own API keys
- Gateway authenticates to upstream services with real credentials

## Updating

```bash
# Pull latest changes
git pull

# Rebuild and restart
docker compose build
docker compose up -d
```

## Troubleshooting

### Wardgate won't start

Check logs:
```bash
docker compose logs wardgate
```

Common issues:
- Missing or invalid config file
- Missing credentials in .env
- Invalid YAML syntax

### Can't connect to IMAP/SMTP

- Verify credentials format: `username:password`
- Check TLS settings match your provider
- For Gmail, use App Passwords (not regular password)

### Dashboard links not working

- Ensure `base_url` is set to your internal or public URL where Wardgate is accessible for you
- Verify Caddy is running and accessible
- Check firewall rules

### Rate limiting errors

Increase limits in config:
```yaml
rules:
  - match: { method: GET }
    action: allow
    rate_limit:
      max: 100
      window: "1m"
```
