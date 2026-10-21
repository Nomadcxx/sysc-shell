{
  description = "sysc-shell for Nix: two binaries, one overlay, a home-manager module and a NixOS module";

  # One input, pinned by revision in flake.lock. The pin is what makes a build
  # reproducible; moving it is its own commit with its own reason.
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
      pkgsFor = system: import nixpkgs { inherit system; };
      lib = nixpkgs.lib;
      # dirtyShortRev when the tree is dirty, so a dirty build reports the
      # revision it was cut from instead of the word "dirty".
      commit = self.shortRev or self.dirtyShortRev or "dirty";
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = pkgsFor system;
        in
        {
          default = pkgs.callPackage ./nix/sysc-shell.nix { inherit commit; };
          sysc-shell-plugin-weather = pkgs.callPackage ./nix/plugin-weather.nix { };
        }
      );

      # The module harnesses run as checks so they are not files nothing runs.
      # The plugin fixture is a derivation, so evaluation reads its manifest
      # through import-from-derivation, which is on by default.
      checks = forAllSystems (
        system:
        let
          pkgs = pkgsFor system;

          harness =
            name: path: message:
            let
              results = import path { inherit pkgs; };
              failed = builtins.filter (n: results.${n} != true) (builtins.attrNames results);
            in
            {
              ${name} = pkgs.runCommand "sysc-shell-${name}" { } ''
                ${lib.optionalString (failed != [ ]) ''
                  echo "failed assertions: ${lib.concatStringsSep ", " failed}"
                  exit 1
                ''}
                echo "${message}" > $out
              '';
            };
        in
        harness "module-harness" ./nix/tests/module.nix "all module harness assertions hold"
        // harness "nixos-module-harness" ./nix/tests/nixos-module.nix "all NixOS module harness assertions hold"
      );

      # The same two derivations, reachable from a plain nixpkgs without the
      # flake. Both delegate to callPackage, so an overlay user gets the same
      # store paths as `nix build`.
      overlays.default = final: _prev: {
        sysc-shell = final.callPackage ./nix/sysc-shell.nix { inherit commit; };
        sysc-shell-plugin-weather = final.callPackage ./nix/plugin-weather.nix { };
      };

      # The user-level module, and one module rather than one per system. It
      # writes home-manager options (xdg.configFile, home.activation,
      # home.packages), so it is not the system module; nixosModules is that.
      #
      # The package default is looked up from the evaluating side's pkgs, so a
      # configuration never has to name a system, and a user on either system
      # this flake supports gets that system's package.
      homeModules.default =
        { pkgs, lib, ... }:
        {
          imports = [ ./nix/module.nix ];
          # mkDefault, not a plain value: a user pointing the option at their
          # own build wins over this flake's.
          programs.sysc-shell.package = lib.mkDefault self.packages.${pkgs.stdenv.hostPlatform.system}.default;
        };

      # The machine-level module, for a system without home-manager: the package
      # in environment.systemPackages and the same substituted unit under
      # /etc/systemd/user, which every user's service manager reads. Plugin
      # directories, shell settings and the guard against an installer copy are
      # per user, so those stay with homeModules.
      nixosModules.default =
        { pkgs, lib, ... }:
        {
          imports = [ ./nix/nixos-module.nix ];
          programs.sysc-shell.package = lib.mkDefault self.packages.${pkgs.stdenv.hostPlatform.system}.default;
        };
    };
}