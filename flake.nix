{
  description = "sysc-shell for Nix: two binaries, one overlay, one home-manager module";

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
      commit = self.shortRev or "dirty";
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

      # The module harness runs as a check so it is not a file nothing runs.
      # The plugin fixture is a derivation, so evaluation reads its manifest
      # through import-from-derivation, which is on by default.
      checks = forAllSystems (
        system:
        let
          pkgs = pkgsFor system;
          results = import ./nix/tests/module.nix { inherit pkgs; };
          failed = builtins.filter (n: results.${n} != true) (builtins.attrNames results);
        in
        {
          module-harness = pkgs.runCommand "sysc-shell-module-harness" { } ''
            ${lib.optionalString (failed != [ ]) ''
              echo "failed assertions: ${lib.concatStringsSep ", " failed}"
              exit 1
            ''}
            echo "all module harness assertions hold" > $out
          '';
        }
      );

      # The same two derivations, reachable from a plain nixpkgs without the
      # flake. Both delegate to callPackage, so an overlay user gets the same
      # store paths as `nix build`.
      overlays.default = final: _prev: {
        sysc-shell = final.callPackage ./nix/sysc-shell.nix { inherit commit; };
        sysc-shell-plugin-weather = final.callPackage ./nix/plugin-weather.nix { };
      };

      # home-manager only. The module writes home-manager options (xdg.configFile,
      # home.homeDirectory), so exporting it as a NixOS module would promise a
      # configuration that cannot evaluate.
      homeModules.forAllSystems = forAllSystems (
        system:
        {
          default =
            { lib, ... }:
            {
              imports = [ ./nix/module.nix ];
              # mkDefault, not a plain value: a user pointing the option at
              # their own build wins over this flake's.
              programs.sysc-shell.package = lib.mkDefault self.packages.${system}.default;
            };
        }
      );
    };
}