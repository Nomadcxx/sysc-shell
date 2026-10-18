{
  config,
  lib,
  pkgs,
  ...

}:

let
  cfg = config.programs.sysc-shell;

  # The unit is the repository's own file, not text written here. The only
  # difference for a Nix install is where the binary lives, so that is the only
  # substitution: keeping the rest of the file means one install story, and a
  # fix to Restart or KillMode reaches every user of it.
  #
  # --replace-fail, not --replace: if the committed unit stops naming the
  # installer path, the module fails here instead of deploying a unit that
  # starts a binary nobody installed.
  #
  # The deploy-guard drop-in beside it is deliberately not installed: its job is
  # to refuse a hand-copied binary, and a store path cannot be replaced by hand.
  unit = pkgs.runCommand "sysc-shell.service" { } ''
    install -m 0644 ${../packaging/systemd/sysc-shell.service} $out
    substituteInPlace $out \
      --replace-fail '%h/.local/bin/sysc-shell' '${lib.getExe cfg.package}'
  '';

  # A plugin is keyed by the id in its own manifest, because that is what
  # internal/plugin/discovery.go looks up. A system root is a fixed /usr/share
  # path this module does not offer: a package cannot write there without root.
  pluginId = dir: (builtins.fromJSON (builtins.readFile (dir + "/manifest.json"))).id;
in
{
  options.programs.sysc-shell = {
    enable = lib.mkEnableOption "the sysc-shell surfaces";

    # No default here on purpose. The flake's homeModules sets it with
    # lib.mkDefault, so a user who enables the module without that module gets
    # home-manager's own "the option was accessed but has no value defined",
    # which names the option instead of guessing at a store path.
    package = lib.mkOption {
      type = lib.types.package;
      description = ''
        The sysc-shell derivation the unit starts. The flake's homeModules
        defaults it to that flake's package; with the overlay alone, set it to
        pkgs.sysc-shell.
      '';
    };

    autostart = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Whether to start the shell at graphical-session.target.";
    };

    plugins = lib.mkOption {
      type = lib.types.listOf lib.types.package;
      default = [ ];
      description = ''
        Plugin directories, each holding a manifest.json. Each is linked into the
        user plugin root under its manifest id, which is what discovery keys on.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = !(builtins.pathExists "${config.home.homeDirectory}/.local/bin/sysc-shell");
        message = ''
          programs.sysc-shell.enable is set and ~/.local/bin/sysc-shell exists.
          Two shells would fight over the bar. Remove the installer or AUR copy,
          or let scripts/deploy own it and disable this option.
        '';
      }
    ];

    # The unit is deployed as the repository's own substituted file, because
    # the key/value section model of systemd.user.services cannot carry a whole
    # unit. A file under systemd/user is what systemd itself reads; the wants
    # link is the enablement, and omitting it is what autostart = false means.
    xdg.configFile = lib.mkMerge [
      { "systemd/user/sysc-shell.service".source = unit; }
      (lib.mkIf cfg.autostart {
        "systemd/user/graphical-session.target.wants/sysc-shell.service".source = unit;
      })
      (
        lib.listToAttrs (
          map
            (dir: {
              name = "sysc-shell/plugins/${pluginId dir}";
              value = { source = dir; };
            })
            cfg.plugins
        )
      )
    ];
  };
}