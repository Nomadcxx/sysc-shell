{
  config,
  lib,
  pkgs,
  ...

}:

let
  cfg = config.programs.sysc-shell;

  # The same substituted unit the home-manager module deploys, from the one
  # helper: nix/unit.nix carries the reasoning.
  unit = import ./unit.nix { inherit pkgs lib; package = cfg.package; };

  wantsFile = "systemd/user/graphical-session.target.wants/sysc-shell.service";
in
{
  options.programs.sysc-shell = {
    enable = lib.mkEnableOption "the sysc-shell surfaces";

    # No default here on purpose. The flake's nixosModules.default sets it with
    # lib.mkDefault, so a system that enables the module without that module
    # gets the option error instead of a guessed store path.
    package = lib.mkOption {
      type = lib.types.package;
      description = ''
        The sysc-shell derivation the unit starts. The flake's
        nixosModules.default defaults it to that flake's package; with the
        overlay alone, set it to pkgs.sysc-shell.
      '';
    };

    autostart = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Whether to start the shell at graphical-session.target for every user.";
    };
  };

  # What this module deliberately does not carry: plugin directories and shell
  # settings are per user, and the guard against an installer copy at
  # ~/.local/bin runs in a user's home. Those stay with the home-manager module.
  config = lib.mkIf cfg.enable {
    # Machine-wide, so the binary the unit runs is also the one on PATH for a
    # user who wants `sysc-shell ipc status`.
    environment.systemPackages = [ cfg.package ];

    # User units under /etc/systemd/user are read by every user's service
    # manager. A home-manager install deploys the same substituted file into
    # ~/.config/systemd/user; here it is the machine-wide copy. The wants link
    # is the enablement, and omitting it is what autostart = false means.
    #
    # Enabled beside the home-manager module, the user-level unit shadows this
    # one for the users that have it, which is the same file either way.
    environment.etc = {
      "systemd/user/sysc-shell.service".source = unit;
    } // lib.optionalAttrs cfg.autostart {
      ${wantsFile}.source = unit;
    };
  };

  # NixOS class: imported into a home-manager evaluation, the class mismatch is
  # the error, instead of the first environment.* option.
  _class = "nixos";
}
