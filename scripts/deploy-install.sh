#!/usr/bin/env bash
# deploy-install.sh runs on the target machine. scripts/deploy calls it
# directly for the desktop and over ssh for the laptop, so both install the
# same way. It keeps a rollback copy, installs the build, the guard and its
# drop-in, writes the stamp the guard checks, and restarts the shell.
#
# Usage: deploy-install.sh BINARY GUARD DROPIN REVISION BRANCH [FORCE_REASON]
set -euo pipefail

new=$1 guard=$2 dropin=$3 rev=$4 branch=$5 reason=${6:-}
bin=${SYSC_SHELL_BIN:-$HOME/.local/bin/sysc-shell}
state=${XDG_STATE_HOME:-$HOME/.local/state}/sysc-shell
unitdir=${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/sysc-shell.service.d
bindir=$(dirname "$bin")
ts=$(date +%Y%m%dT%H%M%S)

mkdir -p "$state" "$unitdir" "$bindir"
rollback=""
if [ -f "$bin" ]; then
	rollback=$bin.before-deploy-$ts
	cp -p "$bin" "$rollback"
fi
# Five rollbacks are plenty; older ones only fill the disk.
ls -1t "$bin".before-deploy-* 2>/dev/null | tail -n +6 | xargs -r rm -f

install -m 0755 "$guard" "$bindir/sysc-shell-guard"
install -m 0644 "$dropin" "$unitdir/deploy-guard.conf"
install -m 0755 "$new" "$state/sysc-shell.deployed"
install -m 0755 "$new" "$bin.new"
mv -f "$bin.new" "$bin"

{
	echo "revision=$rev"
	echo "sha256=$(sha256sum "$bin" | cut -d' ' -f1)"
	echo "branch=$branch"
	echo "deployed=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	echo "by=${USER:-unknown}@$(uname -n)"
	echo "force=$reason"
} >"$state/deployed"

# stray_shell prints the pid holding the shell's IPC socket when that process
# runs outside sysc-shell.service. niri's spawn-at-startup starts one in its
# own scope; while it holds the socket the unit's new process exits "another
# sysc-shell is already running" and the old binary keeps running (sysc-2302).
stray_shell() {
	local sock pid
	sock=${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/sysc-shell/ipc.v1.sock
	pid=$(ss -Hlxp src "$sock" 2>/dev/null | grep -o 'pid=[0-9]*' | head -n 1 | cut -d= -f2)
	[ -n "$pid" ] || return 0
	grep -q '/sysc-shell\.service$' "/proc/$pid/cgroup" 2>/dev/null && return 0
	echo "$pid"
}

# stop_stray_shell stops it so the unit can run the new build. A scope of its
# own is stopped whole, which takes its children (the wallpaper) with it; any
# other cgroup belongs to something else, such as a terminal, so only the pid
# is signalled.
stop_stray_shell() {
	local pid=$1 scope
	scope=$(sed -n 's/^0:://p' "/proc/$pid/cgroup" 2>/dev/null)
	scope=${scope##*/}
	echo "deploy: pid $pid holds the sysc-shell socket outside sysc-shell.service (${scope:-no cgroup}); stopping it so the unit runs the new build" >&2
	case $scope in
	app-*sysc*shell*.scope)
		echo "deploy: whatever started it (niri spawn-at-startup) will start it again at the next login" >&2
		systemctl --user stop "$scope" || kill -TERM "$pid"
		;;
	*) kill -TERM "$pid" ;;
	esac
	for _ in $(seq 50); do
		kill -0 "$pid" 2>/dev/null || return 0
		sleep 0.1
	done
	echo "deploy: pid $pid did not exit; sysc-shell.service cannot start while it holds the socket" >&2
	return 1
}

if [ -z "${SYSC_DEPLOY_NO_RESTART:-}" ]; then
	if pid=$(stray_shell) && [ -n "$pid" ]; then
		stop_stray_shell "$pid" || exit 1
	fi
	systemctl --user daemon-reload
	systemctl --user restart sysc-shell.service
	sleep 3
	if ! systemctl --user is-active --quiet sysc-shell.service; then
		echo "deploy: sysc-shell did not stay up on $(uname -n); rollback: $rollback" >&2
		exit 1
	fi
	journalctl --user -u sysc-shell.service --since "-10s" --no-pager | grep -iE 'panic|closing surface' || true
fi
echo "deploy: $(uname -n) now runs ${rev:0:12} (rollback: ${rollback:-none})"
