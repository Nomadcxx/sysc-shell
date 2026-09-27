#!/usr/bin/env bash
# Checks for scripts/deploy, deploy-install.sh and sysc-shell-guard. Runs in
# temporary directories only: it never touches the real shell, its binary or
# its unit. Run: bash scripts/deploy_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fails=0
pass() { echo "ok   $1"; }
fail() { echo "FAIL $1"; fails=$((fails + 1)); }

# --- guard ---------------------------------------------------------------
guard_env() {
	export HOME=$work/home XDG_STATE_HOME=$work/home/.local/state
	export SYSC_SHELL_BIN=$work/home/.local/bin/sysc-shell
	rm -rf "$work/home"
	mkdir -p "$work/home/.local/bin" "$XDG_STATE_HOME/sysc-shell"
}
stamp_as() { # stamp the given file as the deployed build
	cp "$1" "$XDG_STATE_HOME/sysc-shell/sysc-shell.deployed"
	printf 'revision=abc123def456\nsha256=%s\n' "$(sha256sum "$1" | cut -d' ' -f1)" >"$XDG_STATE_HOME/sysc-shell/deployed"
}

guard_env
echo new >"$SYSC_SHELL_BIN"
bash "$here/sysc-shell-guard" 2>/dev/null
[ "$(cat "$SYSC_SHELL_BIN")" = new ] && pass "guard: no stamp leaves the binary alone" || fail "guard: no stamp changed the binary"

guard_env
echo good >"$SYSC_SHELL_BIN"
stamp_as "$SYSC_SHELL_BIN"
bash "$here/sysc-shell-guard" 2>/dev/null
[ "$(cat "$SYSC_SHELL_BIN")" = good ] && pass "guard: a stamped binary starts" || fail "guard: a stamped binary was replaced"

guard_env
echo good >"$work/good"
stamp_as "$work/good"
echo stale >"$SYSC_SHELL_BIN"
msg=$(bash "$here/sysc-shell-guard" 2>&1)
if [ "$(cat "$SYSC_SHELL_BIN")" = good ] && [ "$(cat "$XDG_STATE_HOME/sysc-shell/refused-latest")" = stale ] && [[ $msg == *"refused an unstamped binary"* ]]; then
	pass "guard: a hand-copied binary is set aside and the stamped build restored"
else
	fail "guard: a hand-copied binary survived ($(cat "$SYSC_SHELL_BIN"); $msg)"
fi

guard_env
echo good >"$work/good"
stamp_as "$work/good"
rm "$XDG_STATE_HOME/sysc-shell/sysc-shell.deployed"
echo stale >"$SYSC_SHELL_BIN"
bash "$here/sysc-shell-guard" 2>/dev/null; rc=$?
[ $rc = 0 ] && [ "$(cat "$SYSC_SHELL_BIN")" = stale ] && pass "guard: a missing stamped copy never blocks the start" || fail "guard: missing stamped copy (rc $rc)"

# --- install -------------------------------------------------------------
guard_env
export XDG_CONFIG_HOME=$work/home/.config SYSC_DEPLOY_NO_RESTART=1
echo old >"$SYSC_SHELL_BIN"
echo built >"$work/built"
out=$(bash "$here/deploy-install.sh" "$work/built" "$here/sysc-shell-guard" "$here/../packaging/systemd/sysc-shell.service.d/deploy-guard.conf" deadbeefcafe main "" 2>&1)
if [ "$(cat "$SYSC_SHELL_BIN")" = built ] &&
	ls "$SYSC_SHELL_BIN".before-deploy-* >/dev/null 2>&1 &&
	[ -x "$work/home/.local/bin/sysc-shell-guard" ] &&
	[ -f "$XDG_CONFIG_HOME/systemd/user/sysc-shell.service.d/deploy-guard.conf" ] &&
	grep -q '^revision=deadbeefcafe$' "$XDG_STATE_HOME/sysc-shell/deployed"; then
	pass "install: binary, rollback, guard, drop-in and stamp in place"
else
	fail "install: $out"
fi
bash "$here/sysc-shell-guard" 2>/dev/null
[ "$(cat "$SYSC_SHELL_BIN")" = built ] && pass "install: its own stamp satisfies the guard" || fail "install: the guard rejected a fresh install"
unset XDG_CONFIG_HOME SYSC_DEPLOY_NO_RESTART

# --- deploy refusals (dry run, throwaway repositories) ---------------------
export HOME=$work/home GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t
git init -q --bare -b main "$work/origin.git"
git clone -q "$work/origin.git" "$work/repo" 2>/dev/null
mkdir -p "$work/repo/scripts" && cp "$here/deploy" "$work/repo/scripts/deploy"
(cd "$work/repo" && git add -A && git commit -qm base && git push -q origin HEAD:main)
base=$(git -C "$work/repo" rev-parse HEAD)

deploy() { (cd "$work/repo" && bash scripts/deploy --dry-run "$@" 2>&1); }

out=$(export SYSC_DEPLOY_CURRENT_REV=$base; deploy) && [[ $out == *"dry run"* ]] && pass "deploy: an up-to-date clean tree passes" || fail "deploy: clean tree refused: $out"

echo dirty >>"$work/repo/scripts/deploy"
out=$(export SYSC_DEPLOY_CURRENT_REV=$base; deploy); rc=$?
[ $rc != 0 ] && [[ $out == *"uncommitted changes"* ]] && pass "deploy: refuses a dirty tree" || fail "deploy: dirty tree accepted: $out"
git -C "$work/repo" checkout -q -- scripts/deploy

git clone -q "$work/origin.git" "$work/other" 2>/dev/null
(cd "$work/other" && echo more >more && git add more && git commit -qm "merged elsewhere" && git push -q origin HEAD:main)
ahead=$(git -C "$work/other" rev-parse HEAD)
out=$(export SYSC_DEPLOY_CURRENT_REV=$base; deploy); rc=$?
[ $rc != 0 ] && [[ $out == *"lacks origin/main"* ]] && pass "deploy: refuses a tree without origin/main" || fail "deploy: stale tree accepted: $out"

out=$(export SYSC_DEPLOY_CURRENT_REV=$base; deploy --force "owner asked"); rc=$?
[ $rc = 0 ] && [[ $out == *"overriding"* ]] && pass "deploy: --force overrides with its reason" || fail "deploy: --force did not override: $out"

git -C "$work/repo" pull -q --ff-only origin main
(cd "$work/other" && echo feature >feature && git add feature && git commit -qm "someone's feature")
feature=$(git -C "$work/other" rev-parse HEAD)
git -C "$work/other" push -q origin HEAD:refs/heads/feature
out=$(export SYSC_DEPLOY_CURRENT_REV=$feature; deploy); rc=$?
[ $rc != 0 ] && [[ $out == *"has commits this build lacks"* ]] && pass "deploy: refuses to overwrite a build with newer work" || fail "deploy: overwrote newer work: $out"

out=$(export SYSC_DEPLOY_CURRENT_REV=$ahead; deploy); rc=$?
[ $rc = 0 ] && pass "deploy: replaces a build its HEAD contains" || fail "deploy: refused an older build: $out"

out=$(export SYSC_DEPLOY_CURRENT_REV=; deploy); rc=$?
[ $rc = 0 ] && pass "deploy: a binary without a revision does not block" || fail "deploy: no-revision binary blocked: $out"

echo
[ $fails = 0 ] && echo "all deploy checks passed" || { echo "$fails deploy check(s) failed"; exit 1; }
