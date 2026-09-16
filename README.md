# Ticket System (Go)

A small backend service where a user can register, log in, create tickets, view only their
own tickets, and update the status of their own tickets.

## Tech

- Go (standard `net/http`, no framework)
- SQLite via `modernc.org/sqlite` (pure Go, no CGO)
- JWT auth via `golang-jwt/jwt/v5`
- Password hashing via `bcrypt`

## Endpoints

| Method | Endpoint               | Auth | Purpose                     |
|--------|------------------------|------|------------------------------|
| GET    | /health                | no   | Health check                |
| POST   | /auth/register         | no   | Register user                |
| POST   | /auth/login            | no   | Login, returns JWT          |
| POST   | /tickets               | yes  | Create ticket                |
| GET    | /tickets               | yes  | List logged-in user's tickets|
| GET    | /tickets/{id}          | yes  | Get own ticket by ID          |
| PATCH  | /tickets/{id}/status   | yes  | Update own ticket status      |

Protected endpoints require `Authorization: Bearer <token>`.

### Status flow

`open -> in_progress -> closed`. A closed ticket cannot be reopened, and statuses cannot be
skipped (e.g. `open -> closed` directly is rejected) — only the next status in the sequence
is accepted.

### Ownership

A ticket that does not exist, or that belongs to another user, returns `404 Not Found` in
both cases so ticket existence can't be probed by a non-owner.

## Environment variables

See `.env.example`:

```
PORT=8080
DB_PATH=tickets.db
JWT_SECRET=change-me-to-a-long-random-secret
```

## Run locally (without Docker)

```bash
go build -o ticket-system .
./ticket-system
curl http://localhost:8080/health
```

## Run with Docker

```bash
docker build -t ticket-system .
docker run -p 8080:8080 ticket-system
curl http://localhost:8080/health
```

Expected response:

```json
{"status": "ok"}
```

## Example usage

```bash
# Register
curl -X POST localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"password123"}'

# Login
curl -X POST localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"password123"}'
# -> {"token": "..."}

TOKEN="<paste token here>"

# Create ticket
curl -X POST localhost:8080/tickets \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Printer broken","description":"Jams on page 2"}'

# List own tickets
curl localhost:8080/tickets -H "Authorization: Bearer $TOKEN"

# Get one ticket
curl localhost:8080/tickets/<id> -H "Authorization: Bearer $TOKEN"

# Update status
curl -X PATCH localhost:8080/tickets/<id>/status \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"status":"in_progress"}'
```

## Deployment

- Deployed URL: TODO
- Public health check: TODO/health

## Assumptions

- Passwords must be at least 8 characters.
- Email is case-insensitive and normalized to lowercase before storage/lookup.
- Status transitions must follow the sequence exactly (no skipping from `open` to `closed`
  directly, and no reopening a `closed` ticket).
- A single SQLite connection is used (`SetMaxOpenConns(1)`) to avoid "database is locked"
  errors on concurrent writes, which is acceptable given the scope of this assignment.
- Ticket IDs are UUIDs (v4).
