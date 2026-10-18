/* GTK 3 semantic colors from the active sysc-shell palette. */
@define-color theme_bg_color {{.Surface}};
@define-color theme_fg_color {{.OnSurface}};
@define-color theme_base_color {{.Surface}};
@define-color theme_text_color {{.OnSurface}};

@define-color theme_selected_bg_color {{.Primary}};
@define-color theme_selected_fg_color {{.OnPrimary}};
@define-color theme_unfocused_selected_bg_color {{.Primary}};
@define-color theme_unfocused_selected_fg_color {{.OnPrimary}};

@define-color theme_unfocused_bg_color {{.Surface}};
@define-color theme_unfocused_fg_color {{.OnSurface}};
@define-color theme_unfocused_base_color {{.Surface}};
@define-color theme_unfocused_text_color {{.OnSurface}};

@define-color insensitive_bg_color {{.SurfaceVariant}};
@define-color insensitive_fg_color {{.OnSurfaceVariant}};
@define-color insensitive_base_color {{.SurfaceVariant}};
@define-color insensitive_text_color {{.OnSurfaceVariant}};
@define-color unfocused_insensitive_color {{.OnSurfaceVariant}};

@define-color borders {{.Outline}};
@define-color unfocused_borders {{.OutlineVariant}};
@define-color link_color {{contrast .Primary .Surface}};
@define-color warning_color {{contrast .Tertiary .Surface}};
@define-color error_color {{contrast .Error .Surface}};
@define-color success_color {{contrast .Secondary .Surface}};
