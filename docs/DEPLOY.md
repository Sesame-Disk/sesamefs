# Deploying SesameFS to Production

This guide covers deploying SesameFS on a single VPS using Docker Compose, Nginx, and Let's Encrypt SSL.

---

## Architecture

```
Internet
   │
   ├── 443 (HTTPS) ──► Nginx (Docker) ──► sesamefs:8080  (API + Frontend)
   │                                  └──► onlyoffice:80  (Document editor)
   │
   └── 80  (HTTP)  ──► Nginx (Docker) ──► 301 redirect to HTTPS

Cassandra (Docker) ← sesamefs (internal Docker network, not exposed)
AWS S3             ← sesamefs (outbound HTTPS)
accounts.sesamedisk.com ← sesamefs (OIDC, outbound HTTPS)
```

**Files involved:**

| File | Purpose |
|---|---|
| `docker-compose.prod.yml` | Production stack (no MinIO, no dev tools) |
| `config.prod.yaml` | Structural config — mounted over the baked image config |
| `nginx/nginx.conf.template` | Nginx config — `${DOMAIN}` substituted at container start |
| `.env.example` | Template for the single `.env` file you create on the server |

---

## Server Requirements

| Resource | Minimum | Recommended |
|---|---|---|
| CPU | 2 vCPU | 4 vCPU |
| RAM | 6 GB | 8–16 GB |
| Disk | 40 GB | 100 GB+ |
| OS | Ubuntu 22.04 / Debian 12 | same |

> Cassandra needs ~2 GB RAM heap. OnlyOffice needs ~1 GB. sesamefs itself is lightweight.

---

## Step 0 — Before you touch the server

### 0.1 Create an S3 bucket

1. Create a bucket in AWS S3 (or an S3-compatible service like Cloudflare R2)
2. Create an IAM user with `s3:*` permission on that bucket
3. Save the `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`

### 0.2 Register an OIDC client

In `accounts.sesamedisk.com`, create a new application/client:

- **Grant type**: Authorization Code
- **PKCE**: Required (optional but recommended)
- **Redirect URIs** (register **both** — web login and desktop client SSO):
  - `https://<your-domain>/sso/`
  - `https://<your-domain>/oauth/callback/`
- **Scopes**: `openid profile email`

Save the `client_id` and `client_secret`.

> **Why two redirect URIs?**
> - `/sso/` — web login flow (React frontend maneja el callback)
> - `/oauth/callback/` — desktop client SSO (el servidor canjea el código y redirige a `seafile://client-login/?token=xxx`)

### 0.3 Generate secrets

Run these locally and save the output — you'll paste them into `.env`:

```bash
openssl rand -hex 32   # → OIDC_JWT_SIGNING_KEY
openssl rand -hex 32   # → ONLYOFFICE_JWT_SECRET
```

### 0.4 Set up DNS

Point two DNS A records to your server's public IP:

```
files.yourdomain.com   A   <server-ip>
office.yourdomain.com  A   <server-ip>
```

Wait for DNS to propagate before running certbot.

---

## Step 1 — Install dependencies on the server

```bash
# Docker
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER
newgrp docker

# Certbot (for SSL)
sudo apt install -y certbot

# Verify
docker compose version
certbot --version
```

---

## Step 2 — Get SSL certificates

Certbot needs port 80 free. Run this **before** starting Docker:

```bash
sudo certbot certonly --standalone \
  -d files.yourdomain.com \
  -d office.yourdomain.com
```

This creates a single certificate covering both domains, stored at:
`/etc/letsencrypt/live/files.yourdomain.com/`

Both nginx server blocks reference this same cert path.

**Auto-renewal** (certbot installs a systemd timer automatically — verify it):
```bash
systemctl status certbot.timer
```

---

## Step 3 — Clone the repo

```bash
git clone <your-repo-url> /opt/sesamefs
cd /opt/sesamefs
```

---

## Step 4 — Create `.env`

```bash
cp .env.prod.example .env
nano .env
```

Fill in these values (everything else can stay as the example default):

```bash
# Domains
DOMAIN=files.yourdomain.com
OFFICE_DOMAIN=office.yourdomain.com

# S3
AWS_ACCESS_KEY_ID=<from step 0.1>
AWS_SECRET_ACCESS_KEY=<from step 0.1>
S3_BUCKET=<your-bucket-name>
S3_REGION=us-east-1
S3_ENDPOINT=          # leave empty for real AWS S3

# Cassandra
CASSANDRA_CLUSTER_NAME=sesamefs-prod
CASSANDRA_MAX_HEAP_SIZE=2G
CASSANDRA_HEAP_NEWSIZE=400M

# OIDC
OIDC_ENABLED=true
AUTH_DEV_MODE=false
AUTH_ALLOW_ANONYMOUS=false
OIDC_CLIENT_ID=<from step 0.2>
OIDC_CLIENT_SECRET=<from step 0.2>
OIDC_JWT_SIGNING_KEY=<from step 0.3 — first openssl output>

# OnlyOffice
ONLYOFFICE_JWT_SECRET=<from step 0.3 — second openssl output>
```

> **Note:** `docker-compose.prod.yml` automatically computes `SERVER_URL`,
> `OIDC_REDIRECT_URIS`, and `ONLYOFFICE_API_JS_URL` from `${DOMAIN}` and
> `${OFFICE_DOMAIN}`. You don't need to set those manually.

---

## Step 5 — Configure firewall

```bash
sudo ufw allow 22/tcp    # SSH
sudo ufw allow 80/tcp    # HTTP (nginx redirect to HTTPS)
sudo ufw allow 443/tcp   # HTTPS
sudo ufw enable
sudo ufw status
```

Cassandra (9042), OnlyOffice (8088), and sesamefs (8080) are bound to
`127.0.0.1` only — they are not reachable from the internet.

---

## Step 6 — Deploy

```bash
cd /opt/sesamefs

# First deploy: builds the Docker image (frontend + Go binary).
# Takes ~5–10 minutes the first time.
docker compose -f docker-compose.prod.yml up -d --build

# Watch logs during startup
docker compose -f docker-compose.prod.yml logs -f
```

Cassandra takes ~60–90 seconds to become healthy on first boot.
sesamefs waits for Cassandra before starting (health check dependency).

---

## Step 7 — First admin user

On first startup, SesameFS seeds the database automatically. **Set `FIRST_ADMIN_EMAIL`
in `.env` to your real email before the first deploy** — that's it, no DB work needed.

```bash
# In .env (already in .env.prod.example):
FIRST_ADMIN_EMAIL=you@yourdomain.com
```

The seed creates an admin account with that email. When you log in via OIDC with
that address, SesameFS matches you to the pre-seeded admin account and you enter
as `admin` automatically.

> **Note:** The seed only runs once (idempotent). Changing `FIRST_ADMIN_EMAIL`
> after the first deploy has no effect.

### If you forgot FIRST_ADMIN_EMAIL, or need to change roles later

```bash
# Connect to Cassandra
docker compose -f docker-compose.prod.yml exec cassandra cqlsh

# Find your user (after logging in once via OIDC):
SELECT user_id, email, role, org_id FROM sesamefs.users;

# Promote to admin in the default org:
UPDATE sesamefs.users SET role = 'admin'
WHERE org_id = '00000000-0000-0000-0000-000000000001'
AND user_id = '<your-user-id>';
```

### Auto-assign roles via OIDC claim (for teams with multiple admins)

In `accounts.sesamedisk.com`, add a `roles` claim with value `admin` or `superadmin`
to the relevant users. Then add to `.env`:

```bash
OIDC_ROLES_CLAIM=roles
```

Roles are synced from the OIDC token on every login.

---

## Step 8 — Verify

```bash
# Basic health (should return: pong)
curl https://files.yourdomain.com/ping

# Readiness — checks Cassandra and S3 connectivity
curl https://files.yourdomain.com/ready
# Expected: {"database":"ok","storage":"ok"}

# OIDC is configured
curl https://files.yourdomain.com/api/v2.1/auth/oidc/config
# Expected: {"issuer":"https://accounts.sesamedisk.com", ...}

# Auth is enforced (unauthenticated request must return 401)
curl -s -o /dev/null -w "%{http_code}" https://files.yourdomain.com/api2/repos/
# Expected: 401

# OnlyOffice is up
curl https://office.yourdomain.com/healthcheck
# Expected: {"status":"ok"}
```

---

## Operations

### View logs

```bash
# All services
docker compose -f docker-compose.prod.yml logs -f

# Single service
docker compose -f docker-compose.prod.yml logs -f sesamefs
docker compose -f docker-compose.prod.yml logs -f cassandra
docker compose -f docker-compose.prod.yml logs -f onlyoffice
```

### Deploy an update

```bash
cd /opt/sesamefs
git pull

# Rebuild only the sesamefs image (Cassandra/OnlyOffice don't need rebuilding)
docker compose -f docker-compose.prod.yml up -d --build sesamefs
```

### Restart a service

```bash
docker compose -f docker-compose.prod.yml restart sesamefs
```

### Stop everything

```bash
docker compose -f docker-compose.prod.yml down
# Add --volumes to also wipe Cassandra data (destructive!)
```

### Check resource usage

```bash
docker stats
```

---

## Configuration reference

### `config.prod.yaml`

Contains structural settings with no secrets. Mounted over the config
baked into the Docker image. Edit and restart sesamefs to apply changes.

Settings that **cannot** be set via env vars and must be in this file:
- `onlyoffice.server_url` — internal Docker URL for OnlyOffice → sesamefs
- `onlyoffice.internal_url` — internal Docker URL for sesamefs → OnlyOffice
- `onlyoffice.view_extensions` / `edit_extensions`
- `cors.allowed_origins` — set to `["https://files.yourdomain.com"]` for strict CORS

### All env var overrides

| Env var | Config field | Notes |
|---|---|---|
| `AUTH_DEV_MODE` | `auth.dev_mode` | Set `false` in prod |
| `AUTH_ALLOW_ANONYMOUS` | `auth.allow_anonymous` | Set `false` in prod |
| `OIDC_ENABLED` | `auth.oidc.enabled` | Set `true` in prod |
| `OIDC_ISSUER` | `auth.oidc.issuer` | Default in config.prod.yaml |
| `OIDC_CLIENT_ID` | `auth.oidc.client_id` | Secret |
| `OIDC_CLIENT_SECRET` | `auth.oidc.client_secret` | Secret |
| `OIDC_REDIRECT_URIS` | `auth.oidc.redirect_uris` | Computed by compose |
| `OIDC_JWT_SIGNING_KEY` | `auth.oidc.jwt_signing_key` | Secret |
| `OIDC_DEFAULT_ROLE` | `auth.oidc.default_role` | |
| `OIDC_AUTO_PROVISION` | `auth.oidc.auto_provision` | |
| `OIDC_SESSION_TTL` | `auth.oidc.session_ttl` | |
| `CASSANDRA_HOSTS` | `database.hosts` | Fixed to `cassandra:9042` in compose |
| `CASSANDRA_KEYSPACE` | `database.keyspace` | |
| `CASSANDRA_LOCAL_DC` | `database.local_dc` | |
| `CASSANDRA_USERNAME` | `database.username` | Optional |
| `CASSANDRA_PASSWORD` | `database.password` | Optional |
| `S3_BUCKET` | `storage.backends.hot.bucket` | |
| `S3_REGION` | `storage.backends.hot.region` | |
| `S3_ENDPOINT` | `storage.backends.hot.endpoint` | Empty = real AWS |
| `AWS_ACCESS_KEY_ID` | (AWS SDK) | Auto-picked by SDK |
| `AWS_SECRET_ACCESS_KEY` | (AWS SDK) | Auto-picked by SDK |
| `ONLYOFFICE_ENABLED` | `onlyoffice.enabled` | |
| `ONLYOFFICE_JWT_SECRET` | `onlyoffice.jwt_secret` | Secret |
| `ONLYOFFICE_API_JS_URL` | `onlyoffice.api_js_url` | Computed by compose |
| `METRICS_ENABLED` | `monitoring.metrics_enabled` | |
| `DESKTOP_CUSTOM_BRAND` | — (server-info response) | Brand name shown in desktop client (default: `Sesame Disk`) |
| `DESKTOP_CUSTOM_LOGO` | — (server-info response) | Full URL to logo image shown in desktop client (optional) |

---

## Troubleshooting

### sesamefs exits immediately on startup

Check the logs:
```bash
docker compose -f docker-compose.prod.yml logs sesamefs
```

Common causes:
- **Cassandra not ready yet** — wait 90s and retry, or check `docker compose ps`
- **S3 connection failed** — verify bucket name, region, and credentials in `.env`
- **Config parse error** — check `config.prod.yaml` for YAML syntax errors

### `/ready` returns storage error

sesamefs can't reach S3. Check:
1. `S3_BUCKET`, `S3_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` in `.env`
2. The bucket exists in the specified region
3. The IAM user has `s3:HeadBucket` and `s3:*` on the bucket
4. `S3_ENDPOINT` is empty for real AWS (not set to a MinIO URL)

### OIDC login fails

1. Verify **both** redirect URIs are registered in accounts.sesamedisk.com:
   - `https://files.yourdomain.com/sso/`
   - `https://files.yourdomain.com/oauth/callback/`
2. Check `OIDC_CLIENT_ID` and `OIDC_CLIENT_SECRET` in `.env`
3. Check sesamefs logs for OIDC errors

### OnlyOffice not loading in documents

1. Verify `https://office.yourdomain.com/healthcheck` returns `{"status":"ok"}`
2. OnlyOffice takes ~2 minutes to start — check logs:
   ```bash
   docker compose -f docker-compose.prod.yml logs -f onlyoffice
   ```
3. Verify `ONLYOFFICE_JWT_SECRET` in `.env` matches what sesamefs expects

### Cassandra won't start

Memory issue — reduce heap size in `.env`:
```bash
CASSANDRA_MAX_HEAP_SIZE=1G
CASSANDRA_HEAP_NEWSIZE=256M
```

Then restart:
```bash
docker compose -f docker-compose.prod.yml up -d cassandra
```

### SSL cert not found (nginx fails to start)

Run certbot before starting Docker:
```bash
sudo certbot certonly --standalone \
  -d files.yourdomain.com \
  -d office.yourdomain.com
```

Verify the cert exists:
```bash
ls /etc/letsencrypt/live/files.yourdomain.com/
```

---

## Known limitations

### Seafile desktop client SSO ✅ Works via browser-based OAuth

The Seafile desktop client (v9+) uses browser-based SSO (`client-sso-via-local-browser`).
When the user clicks "Single Sign On" in the client:

1. The system browser opens `https://your-domain/oauth/login/`
2. User authenticates at the OIDC provider
3. Server redirects to `seafile://client-login/?token=xxx`
4. Desktop client captures the token and is logged in

**Requirements**: `https://your-domain/sso/` y `https://your-domain/oauth/callback/`
deben estar registradas como redirect URIs en el proveedor OIDC (ver paso 0.2).

---

### ⚠️ Seafile CLI (`seaf-cli`) does not work in OIDC-only mode

`POST /api2/auth-token/` (the endpoint `seaf-cli` uses to get a token via
username+password) **always returns 401** when `AUTH_DEV_MODE=false`.

**Affected:** `seaf-cli` and any script using username+password auth.
**Not affected:** Seafile desktop app and mobile app (both use browser SSO).

**Workaround for testing:** Keep `AUTH_DEV_MODE=true` with specific tokens in
`config.prod.yaml → auth.dev_tokens`.

**Permanent fix:** See [docs/TECHNICAL-DEBT.md](TECHNICAL-DEBT.md) for options
(Personal Access Tokens, OIDC Device Flow).

---

### Other limitations

- **OIDC JWT signature verification** is incomplete — the app validates issuer,
  nonce, and expiry but not the cryptographic signature of the ID token.
  Risk is low in authorization code flow (tokens come server-to-server),
  but this should be patched before high-security deployments.
- **No rate limiting** beyond the basic nginx `limit_req` — add a WAF or
  API gateway for stricter protection.
- **Single Cassandra node** — suitable for testing and early production.
  Add nodes for high availability in critical deployments.
- **No Cassandra backup** configured — set up snapshots before storing
  important data.
