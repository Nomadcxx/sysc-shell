# Harness for the home-manager module: it proves what the module writes, not
# what home-manager does with it.
#
# Run it against a pinned nixpkgs, for example:
#
#   nix-shell -p nixpkgs --run 'nix-instantiate --eval --strict nix/tests/module.nix'
#
# Every value printed must be true. The module itself needs no new flake input.

{ pkgs ? import <nixpkgs> { } }:

let
  inherit (pkgs) lib;

  # Stubs for the three home-manager options the module writes. Only their
  # shapes matter here.
  stubs = {
    options = {
      home.homeDirectory = lib.mkOption {
        type = lib.types.str;
        default = "/home/tester";
      };
      systemd.user.services = lib.mkOption {
        type = lib.types.attrsOf (
          lib.types.submodule {
            options = {
              Unit = lib.mkOption {
                type = lib.types.lines;
                default = [ ];
              };
              Install = lib.mkOption {
                type = lib.types.attrs;
                default = { };
              };
            };
          }
        );
        default = { };
      };
      xdg.configFile = lib.mkOption {
        type = lib.types.attrs;
        default = { };
      };
    };
  };

  evaluate = extra:
    lib.evalModules {
      modules = [
        stubs
        ../module.nix
        ({ ... }: extra)
      ];
      check = true;
    };

  # Any store package will do: what is under test is that ExecStart becomes a
  # store path, not which derivation it names.
  package = pkgs.writeShellScriptBin "test-sysc-shell" "exit 0";

  plugin = pkgs.runCommand "test-plugin" { } ''
    mkdir -p $out
    echo '{"id":"org.test.plugin"}' > $out/manifest.json
  '';

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
  withPlugin = evaluate {
    programs.sysc-shell.enable = true;
    programs.sysc-shell.package = package;
    programs.sysc-shell.plugins = [ plugin ];
  };

  unitText = on.config.systemd.user.services.sysc-shell.Unit;
  says = text: needle: builtins.match ".*${needle}.*" text != null;
in
{
  disabledWritesNothing = off.config.systemd.user.services ? sysc-shell == false;

  enabledStartsAStorePath = says unitText "ExecStart=/nix/store/";

  # The committed file keeps its own directives: the module substitutes a path
  # and nothing else.
  enabledKeepsTheCommittedUnit = says unitText "graphical-session.target"
    && says unitText "Restart=on-failure"
    && says unitText "KillMode=mixed"
    && says unitText "TimeoutStopSec=5"
    && says unitText "Slice=session.slice";

  # The installer path must not survive the substitution, or the unit would run
  # a different binary than the one Nix built.
  enabledDroppedTheInstallerPath = builtins.match ".*%h/.local/bin.*" unitText == null;

  autostartInstallsAtGraphicalSession = on.config.systemd.user.services.sysc-shell.Install.WantedBy == [
    "graphical-session.target"
  ];

  autostartFalseOmitsInstall = manual.config.systemd.user.services.sysc-shell.Install == { };

  pluginIsLinkedUnderItsManifestId = withPlugin.config.xdg.configFile ? "sysc-shell/plugins/org.test.plugin";
}