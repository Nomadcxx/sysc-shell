package catalog

import "testing"

func TestLocalizedFieldsFallBackToEnglish(t *testing.T) {
	e := Entry{
		Name:            "Timer",
		Description:     "A pomodoro timer.",
		NameI18n:        map[string]string{"ja": "タイマー", "ru": "", "de": "Timer DE"},
		DescriptionI18n: map[string]string{"ja": "ポモドーロ・タイマー。", "de": "Pomodoro DE"},
	}
	tests := []struct {
		locale, wantName, wantDesc string
	}{
		{"ja", "タイマー", "ポモドーロ・タイマー。"},
		{"ru", "Timer", "A pomodoro timer."}, // empty variant → English
		{"de", "Timer DE", "Pomodoro DE"},    // extra locale in catalog is honored
		{"en", "Timer", "A pomodoro timer."}, //
		{"", "Timer", "A pomodoro timer."},   // follow-system unresolved
	}
	for _, tt := range tests {
		if got := e.LocalizedName(tt.locale); got != tt.wantName {
			t.Errorf("LocalizedName(%q) = %q, want %q", tt.locale, got, tt.wantName)
		}
		if got := e.LocalizedDescription(tt.locale); got != tt.wantDesc {
			t.Errorf("LocalizedDescription(%q) = %q, want %q", tt.locale, got, tt.wantDesc)
		}
	}
	bare := Entry{Name: "Cat", Description: "Pictures."}
	if bare.LocalizedName("es") != "Cat" || bare.LocalizedDescription("es") != "Pictures." {
		t.Error("missing maps must fall back to English")
	}
}
