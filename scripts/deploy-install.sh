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
	echo "by=${USER:-unknown}@$(hostname)"
	echo "force=$reason"
} >"$state/deployed"

if [ -z "${SYSC_DEPLOY_NO_RESTART:-}" ]; then
	systemctl --user daemon-reload
	systemctl --user restart sysc-shell.service
	sleep 3
	if ! systemctl --user is-active --quiet sysc-shell.service; then
		echo "deploy: sysc-shell did not stay up on $(hostname); rollback: $rollback" >&2
		exit 1
	fi
	journalctl --user -u sysc-shell.service --since "-10s" --no-pager | grep -iE 'panic|closing surface' || true
fi
echo "deploy: $(hostname) now runs ${rev:0:12} (rollback: ${rollback:-none})"
