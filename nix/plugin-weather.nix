{ lib, buildGoApplication }:

# A plugin is a directory, not a binary: internal/plugin/discovery.go reads the
# manifest and execs the relative path it names. So this output contains exactly
# what discovery looks for and nothing else.
buildGoApplication {
  pname = "sysc-plugin-weather";
  version = "development";
  src = ../.;
  CGO_ENABLED = 0;
  ldflags = [
    "-s"
    "-w"
    "-trimpath"
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