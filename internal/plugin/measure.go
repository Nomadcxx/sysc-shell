package plugin

import "github.com/Nomadcxx/sysc-shell/internal/ui"

// Measure is the host's text metric for plugin views: eight logical pixels a
// byte, sixteen a line, whatever the role. It is deliberately crude — a view
// is laid out at the host's logical size before a real face is resolved, and
// a plugin must not be able to make the shell measure with a font. It lives
// here, exported inside the module, because the layout checker has to measure
// with exactly the metric the host lays out with.
func Measure(text string, _ ui.TextAttrs) (int, int) { return len(text) * 8, 16 }
