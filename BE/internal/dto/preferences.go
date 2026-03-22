package dto

type UserPreferencesResponse struct {
	FontSize       string `json:"font_size"`
	PointerCursors bool   `json:"pointer_cursors"`
	ThemeMode      string `json:"theme_mode"`
	LightTheme     string `json:"light_theme"`
	DarkTheme      string `json:"dark_theme"`
}

type UpdatePreferencesRequest struct {
	FontSize       *string `json:"font_size" validate:"omitempty,oneof=small default large"`
	PointerCursors *bool   `json:"pointer_cursors"`
	ThemeMode      *string `json:"theme_mode" validate:"omitempty,oneof=system light dark"`
	LightTheme     *string `json:"light_theme" validate:"omitempty,oneof=light rose-light blue-light"`
	DarkTheme      *string `json:"dark_theme" validate:"omitempty,oneof=dark dark-gray amethyst-dark emerald-dark cyber-77 blade-49 pipboy"`
}
