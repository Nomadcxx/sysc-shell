# Harness for the home-manager module: it proves what the module writes, not
# what home-manager does with it.
#
# Run it against a pinned nixpkgs, for example:
#
#   nix-instantiate --eval --strict nix/tests/module.nix
#
# The flake also exposes it as the `module-harness` check. Every value printed
# must be true. The module itself needs no new flake input.

{ pkgs ? import <nixpkgs> { } }:

let
  inherit (pkgs) lib;

  # Stubs for the home-manager options the module writes. Only their shapes
  # matter here.
  stubs = {
    options = {
      assertions = lib.mkOption {
        type = lib.types.listOf lib.types.anything;
        default = [ ];
      };
      home.homeDirectory = lib.mkOption {
        type = lib.types.str;
        default = "/home/tester";
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
        { config._module.args.pkgs = pkgs; }
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

  wantsFile = "systemd/user/graphical-session.target.wants/sysc-shell.service";
  configFileNames = cfg: builtins.attrNames cfg.xdg.configFile;

  unitFile = on.config.xdg.configFile."systemd/user/sysc-shell.service".source;
  # A derivation source is read through import-from-derivation; a plain string
  # source (a stub) is used as-is.
  unitText = if builtins.isString unitFile then unitFile else builtins.readFile unitFile;
  says = text: needle: builtins.match ".*${needle}.*" text != null;
in
{
  disabledWritesNothing = off.config.xdg.configFile == { };

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

  # The deploy-guard drop-in exists to undo hand-copied binaries; it is
  # deliberately not installed beside a store path.
  enabledInstallsNoDeployGuard = !(lib.any (n: lib.hasInfix "deploy-guard" n) (configFileNames on.config));

  autostartWantsAtGraphicalSession = builtins.elem wantsFile (configFileNames on.config);

  autostartFalseOmitsWants = !(builtins.elem wantsFile (configFileNames manual.config));

  pluginIsLinkedUnderItsManifestId = builtins.elem "sysc-shell/plugins/org.test.plugin" (configFileNames withPlugin.config);
}
