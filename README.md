# ELID Coding Exam (Lite)

## Stack
- Backend: Go 1.23, Gin, pgx
- DB: Postgres 16
- Frontend: React + Vite (dev server)
- Orchestration: Docker Compose

## How to run
1) Ensure Docker Desktop is running.
2) In project root:
   ```
   docker compose up -d db
   docker compose up -d api
   docker compose up -d web
   ```
3) API: http://localhost:8080/health Web: http://localhost:5173

## .env
POSTGRES_USER=elid
POSTGRES_PASSWORD=elidpass
POSTGRES_DB=elidapp
DATABASE_URL=postgres://elid:elidpass@db:5432/elidapp?sslmode=disable
JWT_SECRET=supersecretchangeme

# Endpoints
POST /auth/login → {token}
GET /api/devices
POST /api/devices {name, location}
POST /api/devices/:id/toggle
POST /api/devices/:id/activate
POST /api/devices/:id/deactivate
GET /api/transactions?limit=50

# Notes
Notes (design)
Activate starts a per-device goroutine that inserts randomized events at 0.5–3s intervals; Deactivate cancels it.
DB writes via pgxpool, context-propagated from HTTP.
JWT is minimal (demo), easy to extend with real users/password hashing.
CORS enabled for http://localhost:5173 only (dev).
Schema & seed in db/init.sql.
