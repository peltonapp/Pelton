# Shared by run.sh and manual.sh. Sourced, not run.
# shellcheck shell=bash disable=SC2034  # APP is for the sourcing scripts

E2E="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP="$(cd "$E2E/.." && pwd)"

# Host ports. IMAP and SMTP stay on the defaults the account form fills in.
E2E_HTTP_PORT="${E2E_HTTP_PORT:-18081}"
E2E_HTTPS_PORT="${E2E_HTTPS_PORT:-443}"
# The e2e app's Wails dev server (make e2e-app). Never 34115: that is the dev
# server of `make run`, i.e. the developer's Pelton-dev app.
E2E_DEVSERVER_PORT="${E2E_DEVSERVER_PORT:-34125}"
export E2E_HTTP_PORT E2E_HTTPS_PORT E2E_DEVSERVER_PORT

compose() {
  (cd "$E2E" && docker compose -p pelton-e2e -f docker-compose.yml "$@")
}

die() {
  echo "$*" >&2
  exit 1
}

# e2e_data_dir prints the data directory of the app built as channel $1,
# the same path storage.DefaultPathForChannel picks without PELTON_DEV:
# os.UserConfigDir()/Pelton-<channel>.
e2e_data_dir() {
  local base
  case "$(uname -s)" in
    Darwin) base="${HOME}/Library/Application Support" ;;
    *) base="${XDG_CONFIG_HOME:-${HOME}/.config}" ;;
  esac
  printf '%s/Pelton-%s\n' "$base" "$1"
}

# e2e_wipe_data deletes the data directory of channel $1, and only ever a
# Pelton-e2e* one.
e2e_wipe_data() {
  local dir
  dir="$(e2e_data_dir "$1")"
  case "$dir" in
    */Pelton-e2e | */Pelton-e2e-*) ;;
    *) die "refusing to delete $dir: not an e2e data directory" ;;
  esac
  rm -rf "$dir"
}

# e2e_clear_keyring deletes the keyring entries of channel $1. The app names
# its keyring service after the data directory, so these are only the e2e
# app's entries.
e2e_clear_keyring() {
  local service="Pelton-$1" n=0
  case "$(uname -s)" in
    Darwin)
      while security delete-generic-password -s "$service" >/dev/null 2>&1; do
        n=$((n + 1))
        (( n < 200 )) || break
      done
      ;;
    *)
      if command -v secret-tool >/dev/null 2>&1; then
        secret-tool clear service "$service" 2>/dev/null || true
      fi
      ;;
  esac
}

# e2e_valid_port reports whether $1 is a TCP port written plainly: digits
# only, no leading zero (034115 is not 34115 to every tool), 1 to 65535.
e2e_valid_port() {
  [[ "$1" =~ ^[1-9][0-9]{0,4}$ ]] && (( 10#$1 <= 65535 ))
}

# e2e_port_busy reports whether something already listens on 127.0.0.1:$1.
e2e_port_busy() {
  (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null
}

# e2e_check_prereqs fails before anything is started or deleted: tools on
# PATH, Docker reachable, certificates present, no pelton-e2e project running
# (unless E2E_FORCE=1), the Stalwart ports and the e2e dev server port free.
e2e_check_prereqs() {
  local cmd port missing=()
  for cmd in "$@"; do
    command -v "$cmd" >/dev/null 2>&1 || missing+=("$cmd")
  done
  if (( ${#missing[@]} )); then
    die "missing on PATH: ${missing[*]}. See e2e/README.md."
  fi
  docker info >/dev/null 2>&1 || die "Docker is not running."

  # Without them Docker would create e2e/certs itself, owned by root on Linux.
  local f
  for f in ca.crt server-fullchain.crt server.key; do
    if [[ ! -f "$E2E/certs/$f" ]]; then
      die "e2e/certs/$f is missing. Run e2e/scripts/trust-cert-macos.sh or e2e/scripts/trust-cert-linux.sh first."
    fi
  done

  # A running pelton-e2e may be another run.sh or manual.sh session.
  if [[ -n "$(compose ps -q 2>/dev/null)" ]]; then
    if [[ "${E2E_FORCE:-}" != "1" ]]; then
      die "pelton-e2e is already running (another run.sh or manual.sh?). Stop it first, or rerun with E2E_FORCE=1 to take it down."
    fi
    echo "E2E_FORCE=1: taking down the running pelton-e2e project"
    compose down -v
  fi

  local name
  for name in E2E_HTTP_PORT E2E_HTTPS_PORT E2E_DEVSERVER_PORT; do
    e2e_valid_port "${!name}" || die "$name must be a port number from 1 to 65535 without leading zeros, got '${!name}'."
  done
  if [[ "$E2E_DEVSERVER_PORT" == 34115 ]]; then
    die "E2E_DEVSERVER_PORT must not be 34115, the dev server of make run (Pelton-dev)."
  fi
  for port in "$E2E_HTTP_PORT" "$E2E_HTTPS_PORT" 993 465 "$E2E_DEVSERVER_PORT"; do
    if e2e_port_busy "$port"; then
      die "127.0.0.1:$port is in use. Free it, or see e2e/README.md for the port overrides."
    fi
  done
}

# e2e_listener_pids prints the PID of every process listening on TCP port $1,
# with lsof, or ss where lsof is missing. It fails if neither is installed.
e2e_listener_pids() {
  if command -v lsof >/dev/null 2>&1; then
    { lsof -nP -t -iTCP:"$1" -sTCP:LISTEN 2>/dev/null || true; } | sort -u
  elif command -v ss >/dev/null 2>&1; then
    { ss -Hltnp "sport = :$1" 2>/dev/null || true; } | { grep -Eo 'pid=[0-9]+' || true; } | cut -d= -f2 | sort -u
  else
    return 1
  fi
}

# e2e_owns_port fails unless every listener on TCP port $2 is $1 or one of its
# descendants, and there is at least one. It waits up to 30 s for the listener:
# wails dev logs the URL just before its server binds.
e2e_owns_port() {
  local root="$1" port="$2" pids pid tree i
  for (( i = 0; i < 30; i++ )); do
    pids="$(e2e_listener_pids "$port")" || die "lsof or ss is needed to check who listens on $port."
    [[ -n "$pids" ]] && break
    sleep 1
  done
  if [[ -z "$pids" ]]; then
    echo "nothing listens on 127.0.0.1:$port" >&2
    return 1
  fi
  tree=" $root $(e2e_descendants "$root" | tr '\n' ' ') "
  for pid in $pids; do
    if [[ "$tree" != *" $pid "* ]]; then
      echo "127.0.0.1:$port is served by PID $pid ($(ps -o command= -p "$pid" 2>/dev/null || echo '?')), not by the e2e app this script started" >&2
      return 1
    fi
  done
}

# e2e_descendants prints every descendant PID of $1.
e2e_descendants() {
  local child
  for child in $(pgrep -P "$1" 2>/dev/null || true); do
    echo "$child"
    e2e_descendants "$child"
  done
}

# e2e_stop_tree stops $1 and everything it started (make, wails, the app,
# vite), and nothing else.
e2e_stop_tree() {
  local root="$1" pids pid i
  [[ -n "$root" ]] || return 0
  # shellcheck disable=SC2207
  pids=($(e2e_descendants "$root") "$root")
  kill "${pids[@]}" 2>/dev/null || true
  for (( i = 0; i < 20; i++ )); do
    for pid in "${pids[@]}"; do
      if kill -0 "$pid" 2>/dev/null; then
        sleep 0.5
        continue 2
      fi
    done
    return 0
  done
  kill -9 "${pids[@]}" 2>/dev/null || true
}
