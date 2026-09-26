package ui

var paintRoleByName = map[string]PaintRole{
	"primary": PaintPrimary, "on_primary": PaintOnPrimary,
	"primary_container": PaintPrimaryContainer, "on_primary_container": PaintOnPrimaryContainer,
	"secondary": PaintSecondary, "on_secondary": PaintOnSecondary,
	"secondary_container": PaintSecondaryContainer, "on_secondary_container": PaintOnSecondaryContainer,
	"tertiary": PaintTertiary, "on_tertiary": PaintOnTertiary,
	"tertiary_container": PaintTertiaryContainer, "on_tertiary_container": PaintOnTertiaryContainer,
	"error": PaintError, "on_error": PaintOnError,
	"error_container": PaintErrorContainer, "on_error_container": PaintOnErrorContainer,
	"surface": PaintSurface, "on_surface": PaintOnSurface,
	"surface_variant": PaintSurfaceVariant, "on_surface_variant": PaintOnSurfaceVariant,
	"surface_container_low": PaintSurfaceContainerLow, "surface_container": PaintSurfaceContainer,
	"surface_container_high": PaintSurfaceContainerHigh, "surface_container_highest": PaintSurfaceContainerHighest,
	"outline": PaintOutline, "outline_variant": PaintOutlineVariant,
}

// PaintRoleFor resolves a theme.ColorRoleNames entry to the role the painter
// reads from its role table.
func PaintRoleFor(name string) (PaintRole, bool) {
	r, ok := paintRoleByName[name]
	return r, ok
}
