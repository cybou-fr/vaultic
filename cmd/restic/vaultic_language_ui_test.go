package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
)

func TestVaulticLanguageChoicesMapBothWays(t *testing.T) {
	for _, setting := range []string{vaulticLangAuto, vaulticLangFR, vaulticLangEN} {
		if got := vaulticLanguageSettingFor(vaulticLanguageChoiceFor(setting)); got != setting {
			t.Errorf("%q does not survive the round trip: %q", setting, got)
		}
	}
	if vaulticLanguageChoiceFor("") != vaulticLangChoiceAuto || vaulticLanguageSettingFor("nonsense") != vaulticLangAuto {
		t.Fatal("empty or unknown values mean automatic")
	}
}

func TestVaulticLanguageSelectorIsSavedAndNeverContainsSecrets(t *testing.T) {
	test.NewTempApp(t)
	w := test.NewWindow(nil)
	t.Cleanup(w.Close)
	settings := &vaulticSettings{}
	saves := 0
	var fail error
	c := newVaulticLanguageController(vaulticLanguageHooks{Window: w, Settings: settings, Persist: func() error { saves++; return fail }})
	if c.Select.Selected != vaulticLangChoiceAuto || saves != 0 {
		t.Fatalf("initial state: %q saves=%d", c.Select.Selected, saves)
	}
	c.Select.SetSelected(vaulticLangChoiceEN)
	if settings.Language != "en" || saves != 1 {
		t.Fatalf("choice not saved: %+v saves=%d", settings, saves)
	}
	fail = errors.New("disque plein")
	c.Select.SetSelected(vaulticLangChoiceFR)
	if settings.Language != "en" || c.Select.Selected != vaulticLangChoiceEN {
		t.Fatal("a failed save must keep the previous language")
	}
	raw, _ := json.Marshal(vaulticSettingsForSave(vaulticSettings{Language: "en", Profiles: []vaulticBackupProfile{{Name: "A", Sources: []string{"/a"}}}}, nil, "", 0))
	if !strings.Contains(string(raw), `"language":"en"`) {
		t.Fatalf("language not written to the settings: %s", raw)
	}
}
