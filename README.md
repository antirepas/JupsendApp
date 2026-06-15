# Email Tracker

Email Tracker is a self-hosted email outreach and analytics application built with Go, Gin, SQLite, and server-rendered HTML templates styled with Tailwind CSS. It lets you create reusable email templates with `{{variable}}` placeholders, manage a contact database with per-recipient personalization fields (including bulk import via Excel or pasted rows), and send individual emails or multi-contact **campaigns** over SMTP (e.g. Gmail). Campaigns support optional **A/B testing** by alternating two templates across contacts, can be sent immediately or **scheduled** for a specific date and time (a background job checks every 30 seconds and sends when due), and include a detailed management view that shows how each contact’s variables map to their assigned variant plus rendered subject/body previews. Every outbound message is tracked: opens are recorded via an embedded tracking pixel and clicks via rewritten links that redirect through the app before landing on the original URL. A web dashboard summarizes sends, opens, clicks, and rates over time with charts and recent activity; dedicated **campaign analytics** pages add engagement funnels, variant comparisons, per-contact timelines, hourly activity, link performance, and variable coverage metrics. Templates, contacts, and campaigns can be created and deleted from the UI (campaign templates are fixed after creation), and a small JSON API remains available for programmatic template, contact, and send operations. Tracking requires `BASE_URL` to point at your public server address (e.g. ngrok or a production domain) so email clients can load pixels and click redirects correctly.

## Features

- **Email templates** with `{{variable}}` personalization
- **Contact management** with per-contact variables
- **Email sending** via SMTP with HTML + plain-text multipart messages
- **Open tracking** via 1x1 tracking pixel
- **Click tracking** with automatic link rewriting and redirect
- **Dashboard** with stats, charts, and event timelines

## Prerequisites

- Go 1.25+
- SMTP credentials (Gmail app password or other provider)

## Setup

1. Clone the repository and navigate to the project directory.

2. Create a `.env` file:

```env
PORT=8080
BASE_URL=http://localhost:8080
SESSION_SECRET=change-me-to-a-long-random-string
```

SMTP credentials are configured per user in **Settings** after sign-up (not in `.env`).

Legacy `.env` SMTP variables (`SMTP_HOST`, `SMTP_USER`, etc.) are optional fallbacks for `BASE_URL` only; they no longer bootstrap a global sending account.

3. **Important:** If you have an existing `my.db` from a previous version, delete it before first run unless you want to claim legacy data on first signup (orphan rows are assigned to the first registered user).

```bash
rm my.db   # Windows: del my.db
```

4. Run the server:

```bash
go run .
```

5. Open [http://localhost:8080/signup](http://localhost:8080/signup) to create an account, then configure SMTP and tracking URL in **Settings**.

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
├── db/              # SQLite setup and schema
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
| `OUTBOUND_WORKER_INTERVAL` | Outbound worker seconds | `8` |
| `IMAP_POLL_INTERVAL` | IMAP bounce poll seconds | `180` |
| `OUTBOUND_BATCH_SIZE` | Jobs per worker batch | `10` |

Set each user's `BASE_URL` in Settings (or global `BASE_URL` as fallback) so tracking pixels and links work in production.
