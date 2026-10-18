{ lib, buildGoModule }:

# A plugin is a directory, not a binary: internal/plugin/discovery.go reads the
# manifest and execs the relative path it names. So this output contains exactly
# what discovery looks for and nothing else.
buildGoModule {
  pname = "sysc-plugin-weather";
  version = "development";
  src = ../.;

  # Refresh after any go.mod or go.sum change:
  #   nix build .#sysc-shell-plugin-weather   # then copy the `got:` hash here
  # The weather plugin shares the repository's go.mod, so this is the same hash
  # as nix/sysc-shell.nix holds.
  vendorHash = "sha256-zYnmJxcuCxY39ypoOg4jCOG/U6T1krb9Pw0cez7RaN8=";

  # Only the plugin binary. Without this the builder compiles (and tests) every
  # Go package in the repository, the shell binary included, which is the
  # double compile the separate output exists to avoid.
  subPackages = [ "cmd/sysc-plugin-weather" ];

  # CGO_ENABLED goes in env: as a plain derivation attribute it collides with
  # the value the Go builder already puts there.
  env.CGO_ENABLED = 0;

  # -trimpath is a build flag, not a linker flag: the builder already puts it
  # in GOFLAGS, and here it makes the linker reject the link.
  ldflags = [
    "-s"
    "-w"
  ];

  installPhase = ''
    runHook preInstall
    mkdir -p $out/bin
    install -m 0755 $GOPATH/bin/sysc-plugin-weather $out/bin/sysc-plugin-weather
    install -m 0644 plugins/reference/weather/manifest.json $out/manifest.json
    runHook postInstall
  '';

  meta = {
    description = "Weather panels and widgets for sysc-shell";
    license = lib.licenses.bsd3;
    platforms = [
      "x86_64-linux"
      "aarch64-linux"
    ];
  };
}
