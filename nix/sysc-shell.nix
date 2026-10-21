{ lib, buildGoModule, commit ? "dirty" }:

let
  # No release is tagged yet (bd sysc-1158 owns that decision), so there is no
  # honest version to claim. internal/buildinfo defaults to `development`, and
  # the commit is the source revision: `sysc-shell development (a1b2c3d4)` says
  # what is true, where a tag-shaped string would say something false.
  version = "development";
in
buildGoModule {
  pname = "sysc-shell";
  inherit version;
  src = ../.;

  # Refresh after any go.mod or go.sum change:
  #   nix build .#default   # then copy the `got:` hash Nix prints here
  # A go.mod change without this refresh fails the build loudly, which is the
  # point of a fixed-output derivation. The weather plugin shares this go.mod,
  # so nix/plugin-weather.nix carries the same hash.
  vendorHash = "sha256-+lyN7hiOfvUJOOjJGJ5dEV6XPLrFk5j0284o3DjF7x4=";

  # Only the shell binary. The weather plugin has its own output because a
  # plugin is a directory with a manifest, not a bare binary; building it here
  # as well would compile the same source twice under a second derivation.
  subPackages = [ "cmd/sysc-shell" ];

  # CGO_ENABLED goes in env: as a plain derivation attribute it collides with
  # the value the Go builder already puts there.
  env.CGO_ENABLED = 0;
  buildVcsInfo = false;

  # -s -w only. -trimpath is a build flag, not a linker flag: the builder
  # already puts it in GOFLAGS, and here it makes the linker reject the link.
  ldflags = [
    "-s"
    "-w"
    "-X github.com/Nomadcxx/sysc-shell/internal/buildinfo.Version=${version}"
    "-X github.com/Nomadcxx/sysc-shell/internal/buildinfo.Commit=${commit}"
  ];

  meta = {
    description = "Wayland shell surfaces for Niri";
    license = lib.licenses.bsd3;
    mainProgram = "sysc-shell";
    platforms = [
      "x86_64-linux"
      "aarch64-linux"
    ];
  };
}