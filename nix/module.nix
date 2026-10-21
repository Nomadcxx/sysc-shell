{
  config,
  lib,
  pkgs,
  ...

}:

let
  cfg = config.programs.sysc-shell;

  # The substituted unit, shared with the NixOS module. nix/unit.nix carries the
  # reasoning about why the file is deployed rather than a unit written here.
  unit = import ./unit.nix { inherit pkgs lib; package = cfg.package; };

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
        The sysc-shell derivation the unit starts. The flake's homeModules.default
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
    # The store binary is what the unit starts, and it is also the binary the
    # documentation's read-only checks run (`sysc-shell ipc status`). Nothing
    # else puts it on PATH, so it goes in the profile.
    home.packages = [ cfg.package ];

    # The conflict guard runs at activation, not at evaluation. In pure
    # evaluation -- how a flake is evaluated -- builtins.pathExists answers
    # false for every path outside the store, so an assertion here would never
    # fire. Activation reads the real home directory, so the guard holds for
    # flake users and config.nix users alike.
    #
    # The entry is written out rather than built with lib.hm.dag.entryAfter
    # because nix/tests/module.nix evaluates this file against plain nixpkgs
    # lib. All three keys are required: hm.dag.isEntry takes a value as an
    # entry only when data, after and before are all present, and merges
    # anything else as the entry's payload.
    home.activation.syscShellConflict = {
      after = [ "writeBoundary" ];
      before = [ ];
      data = ''
        if [[ -e ${lib.escapeShellArg "${config.home.homeDirectory}/.local/bin/sysc-shell"} ]]; then
          echo "programs.sysc-shell.enable is set and ~/.local/bin/sysc-shell exists." >&2
          echo "Two shells would fight over the bar. Remove the installer or AUR copy," >&2
          echo "or let scripts/deploy own it and disable this option." >&2
          exit 1
        fi
      '';
    };

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

  # home-manager class: imported into a NixOS evaluation, the class mismatch is
  # the error, instead of the first home.* option.
  _class = "homeManager";
}