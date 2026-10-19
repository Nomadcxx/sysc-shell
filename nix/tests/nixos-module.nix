# Harness for the NixOS module, the counterpart of nix/tests/module.nix: it
# proves what the module writes, not what NixOS does with it.
#
# Run it against a pinned nixpkgs, for example. The file is a function, so the
# expression applies it; the argument defaults to <nixpkgs>, which -I names.
#
#   nix-instantiate --eval --strict -I nixpkgs=<a nixpkgs tree> \
#     --expr 'import ./nix/tests/nixos-module.nix { }'
#
# or through the flake, which pins nixpkgs itself:
#
#   nix build .#checks.<system>.nixos-module-harness
#
# Reading the substituted unit means import-from-derivation, so the harness
# needs a store that can build it: a `nix flake check --no-build` cannot run it.
#
# Every value printed must be true. The module itself needs no new flake input.

{ pkgs ? import <nixpkgs> { } }:

let
  inherit (pkgs) lib;

  # Stubs for the NixOS options the module writes. Only their shapes matter
  # here: `source` has no default, so a unit the module forgot to point
  # somewhere fails here the way it would in a system build.
  stubs = {
    options = {
      environment.systemPackages = lib.mkOption {
        type = lib.types.listOf lib.types.package;
        default = [ ];
      };
      environment.etc = lib.mkOption {
        type = lib.types.attrsOf (
          lib.types.submodule {
            options.source = lib.mkOption { type = lib.types.anything; };
          }
        );
        default = { };
      };
    };
  };

  evaluate = extra:
    lib.evalModules {
      modules = [
        stubs
        ../nixos-module.nix
        ({ ... }: extra)
        { config._module.args.pkgs = pkgs; }
      ];
    };

  # Any store package will do: what is under test is that the unit and the
  # system packages name it, not which derivation it is.
  package = pkgs.writeShellScriptBin "test-sysc-shell" "exit 0";

  off = evaluate { programs.sysc-shell.enable = false; };
  on = evaluate {
    programs.sysc-shell.enable = true;
    programs.sysc-shell.package = package;
  };
  manual = evaluate {
    programs.sysc-shell.enable = true;
    programs.sysc-shell.package = package;
    programs.sysc-shell.autostart = false;
  };

  wantsFile = "systemd/user/graphical-session.target.wants/sysc-shell.service";
  etcNames = cfg: builtins.attrNames cfg.environment.etc;

  unitFile = on.config.environment.etc."systemd/user/sysc-shell.service".source;
  # A derivation source is read through import-from-derivation; a plain string
  # source (a stub) is used as-is.
  unitText = if builtins.isString unitFile then unitFile else builtins.readFile unitFile;
  says = text: needle: builtins.match ".*${needle}.*" text != null;
in
{
  disabledWritesNothing = off.config.environment.etc == { }
    && off.config.environment.systemPackages == [ ];

  # The binary the unit runs is the one PATH gets, so `sysc-shell ipc status`
  # works for every user without a home-manager install.
  enabledInstallsThePackage = builtins.elem package.outPath
    (map (p: p.outPath) on.config.environment.systemPackages);

  enabledDeploysTheUnit = builtins.elem "systemd/user/sysc-shell.service" (etcNames on.config);

  enabledStartsAStorePath = says unitText "ExecStart=/nix/store/";

  # The committed file keeps its own directives: the module substitutes a path
  # and nothing else.
  enabledKeepsTheCommittedUnit = says unitText "graphical-session.target"
    && says unitText "Restart=on-failure"
    && says unitText "KillMode=mixed"
    && says unitText "TimeoutStopSec=8"
    && says unitText "Slice=session.slice";

  # The installer path must not survive the substitution, or the unit would run
  # a different binary than the one Nix built.
  enabledDroppedTheInstallerPath = builtins.match ".*%h/.local/bin.*" unitText == null;

  autostartWantsAtGraphicalSession = builtins.elem wantsFile (etcNames on.config);

  # The link has to point at the unit. A wants symlink aimed at anything else
  # still carries the right name, so the name alone proves nothing.
  autostartWantsLinksTheUnit =
    on.config.environment.etc.${wantsFile}.source.outPath
    == on.config.environment.etc."systemd/user/sysc-shell.service".source.outPath;

  autostartFalseOmitsWants = !(builtins.elem wantsFile (etcNames manual.config));
}
