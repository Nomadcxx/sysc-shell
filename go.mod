module github.com/Nomadcxx/sysc-shell

// The module calls x509.Certificate.Verify and http.Client.Do, so it needs a
// standard library without GO-2026-6809 (fixed in go1.26.10) or GO-2026-6617
// (fixed in go1.26.9). CI reads this directive for the toolchain it installs,
// so the floor here is the floor everywhere.
go 1.26.10

require (
	github.com/Nomadcxx/sysc-clipboard v0.1.4-0.20261019192617-18b64bffd9e7
	github.com/Nomadcxx/sysc-wayland v0.3.2
	// hold: v0.2.0 declares go 1.27.1, and the toolchain floor is an
	// organisation-wide decision (see sysc-2015)
	github.com/go-freedesktop/desktopentry v0.1.0
	github.com/go-text/typesetting v0.3.5
	golang.org/x/image v0.47.0
	golang.org/x/sys v0.49.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/Nomadcxx/sysc-launch v0.5.0
	github.com/Nomadcxx/sysc-metrics v0.7.3
	github.com/Nomadcxx/sysc-notify v0.1.1
	// hold: the effect catalogue this module mirrors has to stay in step
	// with the released locker (see sysc-1733)
	github.com/Nomadcxx/sysc-terminal v0.2.2-0.20261022013002-57dd64b663ae
	github.com/Nomadcxx/sysc-tray v0.1.2
	github.com/godbus/dbus/v5 v5.2.4
	github.com/jfreymuth/oggvorbis v1.0.5
	github.com/jfreymuth/pulse v0.1.3
	github.com/junegunn/fzf v0.74.5
	github.com/rivo/uniseg v0.4.7
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef
	github.com/thegrumpylion/xkb-go v0.1.0
)

require (
	// hold: pinned with sysc-terminal so the effect catalogue stays in step
	// with the released locker (see sysc-1733, sysc-1691)
	github.com/Nomadcxx/sysc-Go v1.0.9 // indirect
	// hold: the module moved to the charm.land path and the set moves
	// together (see sysc-2021)
	github.com/charmbracelet/colorprofile v0.3.2 // indirect
	// hold: the module moved to the charm.land path and the set moves
	// together (see sysc-2021)
	github.com/charmbracelet/lipgloss/v2 v2.0.0-beta.3.0.20250917201909-41ff0bf215ea // indirect
	// hold: the module moved to the charm.land path and the set moves
	// together (see sysc-2021)
	github.com/charmbracelet/ultraviolet v0.0.0-20250915111650-81d4262876ef // indirect
	// hold: the module moved to the charm.land path and the set moves
	// together (see sysc-2021)
	github.com/charmbracelet/x/ansi v0.10.1 // indirect
	// hold: the module moved to the charm.land path and the set moves
	// together (see sysc-2021)
	github.com/charmbracelet/x/cellbuf v0.0.13 // indirect
	// hold: the module moved to the charm.land path and the set moves
	// together (see sysc-2021)
	github.com/charmbracelet/x/term v0.2.1 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/jfreymuth/vorbis v1.0.2 // indirect
	github.com/junegunn/go-shellwords v0.0.0-20250127100254-2aa3b3277741 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/xo/terminfo v1.2.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	gonum.org/v1/gonum v0.17.0 // indirect
)

require (
	github.com/Wifx/gonetworkmanager/v2 v2.2.0
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/rkoesters/xdg v0.0.1 // indirect
)

ignore ./docs/plans
