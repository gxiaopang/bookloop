#!/usr/bin/env bash
# Exercise recovery decisions with a fake Docker executable; no daemon or data.
set -Eeuo pipefail
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
mkdir -p "$scratch/bin" "$scratch/empty"
export MOCK_EMPTY="$scratch/empty"
cat > "$scratch/bin/docker" <<'MOCK'
#!/usr/bin/env bash
set -eu
printf '%s\n' "$*" >> "$MOCK_STATE/calls"
if [[ $1 == ps || $1 == volume ]]; then
  [[ ${UNMANAGED:-0} == 0 ]] || echo existing-container
  exit 0
fi
release=
for ((i=1; i<=$#; i++)); do
  if [[ ${!i} == --project-directory ]]; then
    j=$((i+1)); release=${!j}
  fi
done
case " $* " in
  *' config --quiet '*) exit 0 ;;
  *' pull '*) [[ ${FAIL_AT:-} != pull ]] ;;
  *' mysqldump '*)
    [[ ${FAIL_AT:-} != backup ]] || exit 1
    echo '-- consistent SQL backup'
    ;;
  *'SELECT version, checksum'*)
    printf '1\tchecksum-one\n'
    if [[ ${CHANGE_SCHEMA:-0} == 1 && -f "$MOCK_STATE/migrated" ]]; then
      printf '2\tchecksum-two\n'
    fi
    ;;
  *' --entrypoint tar '*) tar -czf - -C "$MOCK_EMPTY" . ;;
  *' run --rm --no-deps -T migrate '*)
    [[ ${FAIL_AT:-} != migration ]] || exit 1
    touch "$MOCK_STATE/migrated"
    ;;
  *' --force-recreate '*)
    if [[ "$release" == "$MOCK_TARGET" && ${FAIL_AT:-} == health ]]; then exit 1; fi
    echo "$release" >> "$MOCK_STATE/started"
    ;;
esac
MOCK
chmod +x "$scratch/bin/docker"
export PATH="$scratch/bin:$PATH"

fixture() {
  export MOCK_STATE="$scratch/$1/state"
  root="$scratch/$1/root"
  mkdir -p "$MOCK_STATE" "$root/releases"
  printf 'APP_ENV=production\n' > "$root/.env"
  chmod 600 "$root/.env"
  old_id="$(printf 'a%.0s' {1..40})-1-1"
  new_id="$(printf 'b%.0s' {1..40})-2-1"
  old="$root/releases/$old_id"
  new="$root/releases/$new_id"
  export MOCK_TARGET="$new"
  for release in "$old" "$new"; do
    mkdir -p "$release"
    touch "$release/compose.yaml" "$release/compose.production.yaml"
    printf 'BOOKLOOP_RELEASE_SHA=%s\nBOOKLOOP_BACKEND_IMAGE=ghcr.io/test/backend@sha256:%s\nBOOKLOOP_NGINX_IMAGE=ghcr.io/test/nginx@sha256:%s\n' \
      "$(basename "$release" | cut -c1-40)" "$(printf 'c%.0s' {1..64})" "$(printf 'd%.0s' {1..64})" > "$release/release.env"
    printf '1\tchecksum-one\n' > "$release/schema.txt"
  done
  ln -s "$old" "$root/current"
  export FAIL_AT= CHANGE_SCHEMA=0 UNMANAGED=0
}
run_release() { bash "$script_dir/release.sh" "$@" > "$MOCK_STATE/output" 2>&1; }
expect_failure() {
  if run_release "$@"; then cat "$MOCK_STATE/output"; echo 'Expected failure' >&2; exit 1; fi
}
assert_old() { [[ $(readlink -f "$root/current") == "$old" ]]; }

fixture success
run_release deploy "$root" "$new_id"
[[ $(readlink -f "$root/current") == "$new" && $(readlink -f "$root/previous") == "$old" ]]
test -s "$new/schema.txt"
backup_dir=$(find "$root/backups" -mindepth 1 -maxdepth 1 -type d)
(cd "$backup_dir" && sha256sum --status -c SHA256SUMS)
echo 'PASS: successful release records state and verified joint backup'

fixture backup-failure
export FAIL_AT=backup
expect_failure deploy "$root" "$new_id"
assert_old
grep -qx "$old" "$MOCK_STATE/started"
! test -e "$MOCK_STATE/migrated"
echo 'PASS: failed backup resumes old app and prevents migration'

fixture pull-failure
export FAIL_AT=pull
expect_failure deploy "$root" "$new_id"
assert_old
! grep -q ' stop ' "$MOCK_STATE/calls"
echo 'PASS: registry failure leaves running app untouched'

fixture migration-failure
export FAIL_AT=migration
expect_failure deploy "$root" "$new_id"
assert_old
! test -e "$MOCK_STATE/started"
echo 'PASS: failed migration stops publication without unsafe restart'

fixture health-failure
export FAIL_AT=health
expect_failure deploy "$root" "$new_id"
assert_old
grep -qx "$old" "$MOCK_STATE/started"
echo 'PASS: failed health check restores old app with unchanged schema'

fixture changed-schema
export FAIL_AT=health CHANGE_SCHEMA=1
expect_failure deploy "$root" "$new_id"
assert_old
! test -e "$MOCK_STATE/started"
echo 'PASS: schema change prevents automatic rollback'

fixture rollback
ln -s "$new" "$root/previous"
run_release rollback "$root"
[[ $(readlink -f "$root/current") == "$new" && $(readlink -f "$root/previous") == "$old" ]]
echo 'PASS: explicit rollback preserves release history'

fixture incompatible-rollback
printf '0\tolder-schema\n' > "$new/schema.txt"
ln -s "$new" "$root/previous"
expect_failure rollback "$root"
assert_old
! grep -q ' stop ' "$MOCK_STATE/calls"
echo 'PASS: incompatible rollback rejected before downtime'

fixture unmanaged
rm "$root/current"
export UNMANAGED=1
expect_failure deploy "$root" "$new_id"
! grep -q ' pull ' "$MOCK_STATE/calls"
echo 'PASS: existing unmanaged installation is protected'

fixture first-install
rm "$root/current"
run_release deploy "$root" "$new_id"
[[ $(readlink -f "$root/current") == "$new" ]]
! test -e "$root/previous"
echo 'PASS: first installation initializes release state'
