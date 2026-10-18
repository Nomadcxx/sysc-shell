# The unit is the repository's own file, not text written here. The only
# difference for a Nix install is where the binary lives, so that is the only
# substitution: keeping the rest of the file means one install story, and a fix
# to Restart or KillMode reaches every user of it.
#
# --replace-fail, not --replace: if the committed unit stops naming the
# installer path, the module fails here instead of deploying a unit that starts
# a binary nobody installed.
#
# The deploy-guard drop-in beside it is deliberately not installed: its job is
# to refuse a hand-copied binary, and a store path cannot be replaced by hand.
#
# The home-manager module and the NixOS module both take the unit from here, so
# the two cannot drift on what the deployed file says.
{ pkgs, lib, package }:
pkgs.runCommand "sysc-shell.service" { } ''
  install -m 0644 ${../packaging/systemd/sysc-shell.service} $out
  substituteInPlace $out \
    --replace-fail '%h/.local/bin/sysc-shell' '${lib.getExe package}'
''
