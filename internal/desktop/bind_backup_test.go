package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/peltonapp/Pelton/internal/themepack"
)

func TestImportSettingsKeepsPresetThemes(t *testing.T) {
	a := newAccountTestApp(t)
	a.dataDir = t.TempDir()

	doc := BackupFileDTO{
		Tag:     backupFileTag,
		Version: 1,
		Settings: map[string]string{
			settingThemeID:      "nord",
			settingThemesSeeded: "true",
		},
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "backup.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := a.ImportData(path, []string{backupCategorySettings}, ""); err != nil {
		t.Fatalf("import: %v", err)
	}

	themes, err := a.ListThemes()
	if err != nil {
		t.Fatalf("list themes: %v", err)
	}
	if len(themes) != len(themepack.Presets()) {
		t.Errorf("got %d themes after import, want the %d presets", len(themes), len(themepack.Presets()))
	}
	if got := a.stringSetting(settingThemeID, ""); got != "nord" {
		t.Errorf("theme_id = %q, want nord", got)
	}
}

func TestExportSettingsOmitsThemeSeedMarker(t *testing.T) {
	a := newAccountTestApp(t)
	if err := a.store.Set(a.ctx, settingThemesSeeded, "true"); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Set(a.ctx, settingThemeID, "nord"); err != nil {
		t.Fatal(err)
	}
	got, err := a.exportSettings()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[settingThemesSeeded]; ok {
		t.Error("themes_seeded must not be exported")
	}
	if got[settingThemeID] != "nord" {
		t.Errorf("theme_id = %q, want nord", got[settingThemeID])
	}
}
