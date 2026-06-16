# Email Tracker

Email Tracker is a self-hosted email outreach and analytics application built with Go, Gin, PostgreSQL, and server-rendered HTML templates styled with Tailwind CSS.

## Features

- **Email templates** with `{{variable}}` personalization
- **Contact management** with per-contact variables
- **Email sending** via SMTP with HTML + plain-text multipart messages
- **Open tracking** via 1x1 tracking pixel
- **Click tracking** with automatic link rewriting and redirect
- **Dashboard** with stats, charts, and event timelines

## Prerequisites

- Go 1.25+
- PostgreSQL 16+ (local Docker Compose, or hosted e.g. [Supabase](https://supabase.com))
- SMTP credentials configured per user in Settings after sign-up

## Setup

### Local development (Docker Postgres)

1. Clone the repository and navigate to the project directory.

2. Start PostgreSQL and the app:

```bash
docker compose up --build
```

3. Or run Postgres only and start the app locally:

```bash
docker compose up postgres -d
```

4. Create a `.env` file:

```env
PORT=8080
BASE_URL=http://localhost:8080
SESSION_SECRET=change-me-to-a-long-random-string
DATABASE_URL=postgres://emailtracker:emailtracker@localhost:5432/emailtracker?sslmode=disable
```

5. Run the server:

```bash
go run .
```

6. Open [http://localhost:8080/signup](http://localhost:8080/signup) to create an account, then configure SMTP and tracking URL in **Settings**.

### Production (DigitalOcean VPS + Supabase)

Keep the app on your VPS; use Supabase for managed PostgreSQL with Table Editor, SQL metrics, and backups.

1. Create a [Supabase](https://supabase.com) project.
2. In **Project Settings → Database**, copy the **Connection pooling** URI (port `6543`, host `*.pooler.supabase.com`).
3. Add to `/opt/emailtracker/.env` on your droplet:

```env
DATABASE_URL=postgres://postgres.[project-ref]:[password]@aws-0-[region].pooler.supabase.com:6543/postgres?sslmode=require
SESSION_SECRET=your-production-secret
BASE_URL=https://your-public-url
```

4. Deploy/restart the app container. Schema tables are created automatically on first connect.

**Supabase SQL examples** (SQL Editor):

```sql
SELECT COUNT(*) FROM users;
SELECT status, COUNT(*) FROM send_jobs GROUP BY status;
SELECT COUNT(*) FROM email_sends WHERE sent_at > NOW() - INTERVAL '7 days';
```

Supabase Auth is **not** used by this app — only PostgreSQL hosting.

## Authentication

- **Sign up** (`/signup`) — email + password (8+ characters). Each account is fully isolated (templates, contacts, campaigns, sends).
- **Login** (`/login`) / **Logout** (sidebar).
- Sessions use a signed HTTP-only cookie; set `SESSION_SECRET` in production (if unset, a random dev secret is generated and sessions reset on restart).
- **Settings** (`/settings`) — change password, per-user `BASE_URL` for tracking links/pixels, and your single SMTP+IMAP sending profile (rate limits and warmup).

Protected routes redirect to login; JSON API routes under `/api/v1/*` return `401` when unauthenticated. Tracking endpoints (`/api/v1/track/*`) remain public.

## Usage

### Dashboard

- **Dashboard** — overview stats, 14-day chart, recent activity
- **Campaigns** — create, schedule, and analyze outreach campaigns
- **Workflows** — automation builder
- **Templates** — create/edit HTML email templates with variables
- **Contacts** — add recipients with personalization fields
- **Sends** — view send history, send new emails, inspect per-email events
- **Settings** — account password, tracking URL, SMTP/IMAP profile
- **Suppressions** — bounced/suppressed contacts

### API Endpoints

Requires session auth (login first) or use the same cookie from the browser.

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/template` | Create template (JSON) |
| `POST` | `/api/v1/contact` | Create contacts (JSON) |
| `POST` | `/api/v1/send` | Queue email send (JSON) |
| `GET` | `/api/v1/track/open/:id` | Open tracking pixel (public) |
| `GET` | `/api/v1/track/click/:id` | Click tracking redirect (public) |

### Template Example

```json
{
  "template": {
    "name": "Welcome",
    "subject": "Hello {{name}}",
    "body": "<p>Hi {{name}}, welcome to {{company}}!</p><a href=\"https://example.com\">Visit us</a>"
  },
  "template_variables": [{ "key": "name" }, { "key": "company" }]
}
```

### Contact Example

```json
{
  "contacts": [{ "id": 1, "email": "user@example.com" }],
  "contact_variables": [
    { "contact_id": 1, "key": "name", "value": "John" },
    { "contact_id": 1, "key": "company", "value": "Acme" }
  ]
}
```

## Outbound delivery

Email sends go through a **SQLite-backed job queue** processed by a background worker (not inline SMTP).

### Single SMTP profile per user

- Configure SMTP and IMAP under **Settings** (one sending profile per account).
- The outbound worker uses the logged-in user's account when enqueueing; rate limits and warmup apply per profile.

### Rate limits & warmup (defaults per account)

| Setting | Default |
|---------|---------|
| `per_minute_limit` | 2 |
| `daily_limit` | 50 |
| `min_seconds_between_sends` | 30s |
| Warmup | enabled: starts at 5/day, +5/day until target |

### Retries

Transient SMTP errors are retried with backoff (1m, 5m, 15m, 60m), up to 5 attempts. Permanent errors (e.g. 535 auth, 550 invalid mailbox) fail immediately; some hard bounces auto-suppress the contact.

### Bounces (IMAP)

Your Settings IMAP profile polls for bounces (Mailer-Daemon, DSN, `X-Failed-Recipients`). Matches correlate via `X-EmailTracker-Send-ID` on outbound mail. Suppressed contacts are skipped on enqueue.

### Env knobs

| Variable | Description | Default |
|----------|-------------|---------|
| `OUTBOUND_WORKER_INTERVAL` | Worker tick (seconds) | `8` |
| `IMAP_POLL_INTERVAL` | IMAP poll tick (seconds) | `180` |
| `OUTBOUND_BATCH_SIZE` | Jobs claimed per tick | `10` |

### API

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/send-jobs?campaign_id=` | Campaign queue counts (pending/sent/failed/dead) |

## Docker

```bash
docker build -t email-tracker .
docker run -p 8080:8080 --env-file .env email-tracker
```

Or with Docker Compose:

```bash
docker compose up --build
```

## CI/CD

GitHub Actions runs on every push and pull request to `main`:

| Job | When | What it does |
|-----|------|----------------|
| **Test & Build** | PR + push to `main` | `go vet`, `go test -race`, compile binary |
| **Docker** | Push to `main` only | Build image and push to GitHub Container Registry |

### Setup

1. Initialize git and push to GitHub:

```bash
git init
git add .
git commit -m "Initial commit"
git branch -M main
git remote add origin https://github.com/YOUR_USER/emailTracker.git
git push -u origin main
```

2. Enable **Actions** in your GitHub repo (on by default).

3. On merge to `main`, the image is published as:

```
ghcr.io/YOUR_USER/emailTracker:latest
ghcr.io/YOUR_USER/emailTracker:<commit-sha>
```

4. Pull and run in production:

```bash
docker pull ghcr.io/YOUR_USER/emailTracker:latest
docker run -p 8080:8080 --env-file .env ghcr.io/YOUR_USER/emailTracker:latest
```

For private repos, make the package public under **Packages → Package settings → Change visibility**, or authenticate with a personal access token to pull.

## Testing

```bash
go test ./...
```

## Project Structure

```
├── auth/            # Password hashing and session helpers
├── config/          # Environment configuration
├── db/              # PostgreSQL setup and schema
├── model/           # Data models and queries
├── outbound/        # Queue, worker, router, IMAP bounces
├── routes/          # HTTP handlers (web + API)
├── templates/       # Go HTML templates (dashboard UI)
├── static/          # CSS assets
├── util/            # SMTP, templating, tracking helpers
└── main.go
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | Server port | `8080` |
| `BASE_URL` | Default tracking base URL (per-user override in Settings) | `http://localhost:8080` |
| `SESSION_SECRET` | Cookie signing secret for sessions | — (random in dev) |
| `DATABASE_URL` | PostgreSQL connection string (required) | — |
| `TEST_DATABASE_URL` | Postgres URL for integration tests | same as local compose test DB |
| `OUTBOUND_WORKER_INTERVAL` | Outbound worker seconds | `8` |
| `IMAP_POLL_INTERVAL` | IMAP bounce poll seconds | `180` |
| `OUTBOUND_BATCH_SIZE` | Jobs per worker batch | `10` |

Set each user's `BASE_URL` in Settings (or global `BASE_URL` as fallback) so tracking pixels and links work in production.
