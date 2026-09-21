# Docker Compose deployment

Phase 8 provides a local, production-shaped Compose stack:

```text
host :8088 -> nginx -> backend -> MySQL / Redis
                    -> shared upload volume
```

Only Nginx publishes a host port. MySQL, Redis, and the backend are reachable
only inside the Compose networks.

## Start from a clean checkout

For a disposable local environment using the documented example values:

```bash
docker compose --env-file .env.example up --build -d --wait
```

This single command builds both application images, creates the named volumes,
waits for MySQL, runs pending migrations once, starts the backend, and finally
starts Nginx. Open <http://127.0.0.1:8088>.

For a long-lived environment, copy `.env.example` to `.env`, replace every
password and `JWT_SECRET`, restrict the file permissions, then run:

```bash
docker compose up --build -d --wait
```

The example credentials are development-only and must never be used on a
public host. TLS, certificates, DNS, public firewall rules, and production
backup automation belong to Phase 9.

## Operations

Inspect health and the one-shot migration result:

```bash
docker compose ps
docker compose logs migrate backend nginx
curl --fail http://127.0.0.1:8088/healthz
curl --fail http://127.0.0.1:8088/readyz
```

Apply newly added migrations explicitly to an already running stack:

```bash
docker compose run --rm migrate
```

Stop containers while preserving the database and uploaded images:

```bash
docker compose down
```

The named volumes are `bookloop_mysql_data` and `bookloop_book_uploads`.
Backups must cover both at a consistent point in time. `docker compose down -v`
deletes both volumes and is intentionally not part of the normal workflow.

## Image and runtime boundaries

- `bookloop-backend:local` contains only the compiled server, compiled migration
  command, CA/timezone data, and SQL migrations. It runs as UID/GID 10001.
- `bookloop-nginx:local` contains the compiled Vue assets and Nginx config, not
  frontend source or `node_modules`.
- `.env`, local uploads, logs, certificates, and build output are excluded from
  image contexts and Git.
- The backend mounts `book_uploads` read/write at `/app/uploads`; Nginx mounts
  the same volume read-only at `/srv/bookloop/uploads`.
- Redis uses `noeviction`. It remains auxiliary: clearing or recreating Redis
  does not remove authoritative MySQL orders.

