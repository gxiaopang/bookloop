#!/usr/bin/env bash
# A release contains Compose files, this script and a digest-only release.env.
set -Eeuo pipefail
umask 077

die() { echo "ERROR: $*" >&2; exit 1; }
[[ $# -ge 2 ]] || die "usage: release.sh deploy ROOT RELEASE_ID | rollback ROOT | backup ROOT"
operation=$1
root=$(realpath -e "$2")
[[ -f "$root/.env" ]] || die "Create $root/.env with production credentials first"
[[ $(stat -c %a "$root/.env") == 600 ]] || die "$root/.env must have mode 600"
if grep -Eq 'replace-with-|ci-only-password' "$root/.env"; then
  die "Replace all example credentials in $root/.env before production deployment"
fi
exec 9>"$root/.deploy.lock"
flock -n 9 || die "Another deployment or backup is running"

compose() {
  local release=$1
  shift
  env -u BOOKLOOP_BACKEND_IMAGE -u BOOKLOOP_NGINX_IMAGE \
    docker compose --project-name bookloop --project-directory "$release" \
    --env-file "$root/.env" --env-file "$release/release.env" \
    -f "$release/compose.yaml" -f "$release/compose.production.yaml" "$@"
}

validate_release() {
  local release=$1
  [[ -f "$release/release.env" && -f "$release/compose.production.yaml" ]] || die "Incomplete release: $release"
  # Do not source a manifest or a production .env as shell code.
  [[ $(wc -l < "$release/release.env") -eq 3 ]] || die "Release manifest must contain exactly three lines"
  grep -Eq '^BOOKLOOP_RELEASE_SHA=[a-f0-9]{40}$' "$release/release.env" || die "Invalid release SHA"
  grep -Eq '^BOOKLOOP_BACKEND_IMAGE=ghcr\.io/[a-z0-9_./-]+@sha256:[a-f0-9]{64}$' "$release/release.env" || die "Backend must use an immutable image digest"
  grep -Eq '^BOOKLOOP_NGINX_IMAGE=ghcr\.io/[a-z0-9_./-]+@sha256:[a-f0-9]{64}$' "$release/release.env" || die "Nginx must use an immutable image digest"
  compose "$release" config --quiet
}

schema() {
  compose "$1" exec -T mysql sh -c \
    'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot -N -B -D "$MYSQL_DATABASE" -e "SELECT version, checksum FROM schema_migrations ORDER BY version"'
}

start_app() {
  # --no-deps prevents a second implicit migration or infrastructure update.
  # Nginx must be recreated: its upstream DNS was resolved at startup.
  compose "$1" up -d --no-build --pull never --no-deps --force-recreate \
    --wait --wait-timeout 180 backend nginx &&
    compose "$1" up -d --no-build --pull never --no-deps --force-recreate worker &&
    compose "$1" exec -T nginx wget -q -O /dev/null http://127.0.0.1/readyz
}

point_to() {
  local name=$1 release=$2
  ln -s "$release" "$root/.$name.next"
  mv -Tf "$root/.$name.next" "$root/$name"
}

backup_path=
backup_stopped=false
backup_release=
resume_failed_backup() {
  local status=$?
  if [[ $backup_stopped == true ]]; then
    echo "Backup failed; restarting the unchanged application" >&2
    if ! start_app "$backup_release"; then
      echo "ERROR: restarting after backup failure also failed; inspect Docker services" >&2
    fi
  fi
  exit "$status"
}
trap resume_failed_backup EXIT

backup() {
  local release=$1
  backup_release=$release
  backup_path="$root/backups/$(date -u +%Y%m%dT%H%M%SZ)-$$"
  mkdir -p "$backup_path"
  chmod 700 "$root/backups" "$backup_path"
  # Stop every application writer, including the notification worker.
  backup_stopped=true
  compose "$release" stop nginx backend worker
  schema "$release" > "$backup_path/schema.txt"
  compose "$release" exec -T mysql sh -c \
    'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysqldump -uroot --single-transaction --no-tablespaces --routines --triggers --events --databases "$MYSQL_DATABASE"' \
    > "$backup_path/mysql.sql"
  compose "$release" run --rm --no-deps -T --entrypoint tar nginx \
    -czf - -C /srv/bookloop/uploads . > "$backup_path/uploads.tar.gz"
  test -s "$backup_path/mysql.sql"
  tar -tzf "$backup_path/uploads.tar.gz" > /dev/null
  cp "$release/release.env" "$backup_path/release.env"
  (cd "$backup_path" && sha256sum mysql.sql uploads.tar.gz schema.txt release.env > SHA256SUMS)
  backup_stopped=false
  echo "Database and upload backup: $backup_path"
}

old=
if [[ -L "$root/current" ]]; then
  old=$(realpath -e "$root/current")
  [[ "$old" == "$root/releases/"* ]] || die "current must point inside releases/"
  validate_release "$old"
elif [[ -e "$root/current" ]]; then
  die "current must be a release symlink"
fi

case "$operation" in
  backup)
    [[ -n "$old" ]] || die "No managed release to back up"
    backup "$old"
    start_app "$old"
    exit
    ;;
  rollback)
    [[ -n "$old" && -L "$root/previous" ]] || die "No previous managed release"
    target=$(realpath -e "$root/previous")
    [[ "$target" == "$root/releases/"* ]] || die "previous must point inside releases/"
    validate_release "$target"
    [[ -f "$target/schema.txt" ]] || die "Previous release has no schema record"
    live_schema=$(schema "$old")
    [[ "$live_schema" == "$(cat "$target/schema.txt")" ]] || die "Schema changed; review migration compatibility and restore procedure before rolling back"
    ;;
  deploy)
    [[ $# -eq 3 && $3 =~ ^[a-f0-9]{40}-[0-9]+-[0-9]+$ ]] || die "Invalid release ID"
    target=$(realpath -e "$root/releases/$3")
    [[ "$target" == "$root/releases/$3" ]] || die "Release must not be a symlink"
    validate_release "$target"
    grep -qx "BOOKLOOP_RELEASE_SHA=${3:0:40}" "$target/release.env" || die "Release ID does not match source SHA"
    ;;
  *) die "Unknown operation: $operation" ;;
esac

[[ "$target" != "$old" ]] || die "This release is already current"
if [[ -z "$old" ]]; then
  # Refuse to adopt an existing Compose installation without its release history.
  existing=$(docker ps -aq --filter label=com.docker.compose.project=bookloop)
  existing_volumes=$(docker volume ls -q --filter label=com.docker.compose.project=bookloop)
  [[ -z "$existing" && -z "$existing_volumes" ]] || die "Unmanaged bookloop containers or volumes exist; back up and plan migration first"
else
  old_infra=$(compose "$old" config --hash mysql,redis,rabbitmq)
  new_infra=$(compose "$target" config --hash mysql,redis,rabbitmq)
  [[ "$old_infra" == "$new_infra" ]] || die "Infrastructure configuration changed; plan that update separately before releasing the application"
fi

# Download before the maintenance window; registry failures leave the old app up.
compose "$target" pull --policy missing backend nginx
if [[ -n "$old" ]]; then
  backup "$old"
  # Infrastructure upgrades are a separate operation, never an implicit release.
  compose "$old" up -d --no-build --pull never --wait --wait-timeout 180 mysql redis rabbitmq
else
  compose "$target" up -d --no-build --wait --wait-timeout 180 mysql redis rabbitmq
fi

if ! compose "$target" run --rm --no-deps -T migrate; then
  echo "Migration failed. Application stays stopped; MySQL DDL may have partially committed." >&2
  echo "Inspect migration logs and backup: ${backup_path:-first installation, no previous database}" >&2
  exit 1
fi
schema "$target" > "$target/schema.txt"

if ! start_app "$target"; then
  compose "$target" ps -a >&2
  compose "$target" stop nginx backend worker
  if [[ -n "$old" ]] && cmp -s "$backup_path/schema.txt" "$target/schema.txt"; then
    echo "Health check failed; restoring the previous application with unchanged schema" >&2
    if ! start_app "$old"; then
      echo "ERROR: previous application failed health checks too" >&2
    fi
  else
    echo "Health check failed after schema change or first installation; manual recovery required." >&2
  fi
  echo "Backup: ${backup_path:-none}" >&2
  exit 1
fi

if [[ -n "$old" ]]; then
  point_to previous "$old"
fi
point_to current "$target"
echo "Release healthy: $target"
