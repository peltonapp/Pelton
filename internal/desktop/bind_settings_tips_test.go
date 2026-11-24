package desktop

import "testing"

func TestShowTipsDefaultsOnAndFollowsTheSetting(t *testing.T) {
	a := newChatTestApp(t)

	prefs, err := a.GetUIPrefs()
	if err != nil {
		t.Fatal(err)
	}
	if !prefs.ShowTips {
		t.Fatal("tips are off by default")
	}
	if prefs.AppVimMode {
		t.Fatal("lists default to vim keys instead of arrow keys")
	}

	if err := a.SetSetting(settingShowTips, "false"); err != nil {
		t.Fatal(err)
	}
	prefs, err = a.GetUIPrefs()
	if err != nil {
		t.Fatal(err)
	}
	if prefs.ShowTips {
		t.Fatal("tips stayed on after being switched off")
	}
}
