package desktop

import "testing"

func TestNormalizeQuickShortcut(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Ctrl+Alt+M", "Ctrl+Alt+M", true},
		{"alt+ctrl+m", "Ctrl+Alt+M", true},
		{"Ctrl+Option+m", "Ctrl+Option+M", true},
		{"Shift+Cmd+K", "Shift+Cmd+K", true},
		{"Ctrl+Shift+F5", "Ctrl+Shift+f5", true},
		{"Super+space", "Super+space", true},
		{"Ctrl+Page Up", "Ctrl+page up", true},
		{" Ctrl + Alt + 1 ", "Ctrl+Alt+1", true},
		{"M", "", false},
		{"Shift+M", "", false},
		{"Ctrl+", "", false},
		{"Ctrl+Hyper+M", "", false},
		{"Ctrl+Alt+Enterprise", "", false},
		{"Ctrl+Alt+é", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := normalizeQuickShortcut(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("normalizeQuickShortcut(%q) = %q, %v, want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestQuickComposeSettingsGoThroughTheirOwnBinding(t *testing.T) {
	a := newLockTestApp(t, nil)
	for _, key := range []string{settingQuickCompose, settingQuickComposeShortcut} {
		if err := a.SetSetting(key, "true"); err == nil {
			t.Errorf("SetSetting(%q) was allowed", key)
		}
	}
	status, err := a.SetQuickCompose(false, "Shift+M")
	if err != nil || status.Error != "invalid" {
		t.Fatalf("invalid shortcut: %+v %v", status, err)
	}
	status, err = a.SetQuickCompose(false, "alt+ctrl+k")
	if err != nil || status.Enabled || status.Shortcut != "Ctrl+Alt+K" || status.Registered {
		t.Fatalf("store while off: %+v %v", status, err)
	}
	// without a running app there is nothing to register with, which reads
	// as refused, and the setting stays off.
	status, err = a.SetQuickCompose(true, "Ctrl+Alt+K")
	if err != nil || status.Error != "taken" || status.Enabled {
		t.Fatalf("enable without an app: %+v %v", status, err)
	}
}
