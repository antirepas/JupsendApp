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
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your-email@gmail.com
APP_PASSWORD=your-app-password
SMTP_FROM=your-email@gmail.com
```

3. **Important:** If you have an existing `my.db` from a previous version, delete it before first run. The schema has been updated and there is no migration system.

```bash
rm my.db   # Windows: del my.db
```

4. Run the server:

```bash
go run .
```

5. Open the dashboard at [http://localhost:8080](http://localhost:8080)

## Usage

### Dashboard

- **Dashboard** — overview stats, 14-day chart, recent activity
- **Templates** — create/edit HTML email templates with variables
- **Contacts** — add recipients with personalization fields
- **Sends** — view send history, send new emails, inspect per-email events

### API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/template` | Create template (JSON) |
| `POST` | `/api/v1/contact` | Create contacts (JSON) |
| `POST` | `/api/v1/send` | Send email (JSON) |
| `GET` | `/api/v1/track/open/:id` | Open tracking pixel |
| `GET` | `/api/v1/track/click/:id` | Click tracking redirect |

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
├── config/          # Environment configuration
├── db/              # SQLite setup and schema
├── model/           # Data models and queries
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
| `BASE_URL` | Base URL for tracking links/pixels | `http://localhost:8080` |
| `SMTP_HOST` | SMTP server hostname | `smtp.gmail.com` |
| `SMTP_PORT` | SMTP port | `587` |
| `SMTP_USER` | SMTP username | — |
| `APP_PASSWORD` | SMTP password | — |
| `SMTP_FROM` | From address | `SMTP_USER` |

Set `BASE_URL` to your public URL in production so tracking pixels and links work correctly.
