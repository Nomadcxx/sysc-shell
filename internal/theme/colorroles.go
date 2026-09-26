package theme

// colorRoleNames is the Material colour-role vocabulary a configuration may
// name. It lives here because theme imports nothing: config validates against
// it and ui maps it to paint roles without either importing the other.
var colorRoleNames = []string{
	"primary", "on_primary", "primary_container", "on_primary_container",
	"secondary", "on_secondary", "secondary_container", "on_secondary_container",
	"tertiary", "on_tertiary", "tertiary_container", "on_tertiary_container",
	"error", "on_error", "error_container", "on_error_container",
	"surface", "on_surface", "surface_variant", "on_surface_variant",
	"surface_container_low", "surface_container", "surface_container_high", "surface_container_highest",
	"outline", "outline_variant",
}

// ColorRoleNames returns the vocabulary in a stable order.
func ColorRoleNames() []string { return append([]string(nil), colorRoleNames...) }

// ValidColorRole reports whether name is in the vocabulary. Matching is exact.
func ValidColorRole(name string) bool {
	for _, n := range colorRoleNames {
		if n == name {
			return true
		}
	}
	return false
}
