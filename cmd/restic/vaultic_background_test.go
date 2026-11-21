package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
)

func TestVaulticAutostartCommandsAreQuotedAndHideTheWindow(t *testing.T) {
	cmd := vaulticAutostartCommand(`C:\Program Files\Vaultic\vaultic.exe`)
	if cmd != `"C:\Program Files\Vaultic\vaultic.exe" --background` {
		t.Fatalf("command: %s", cmd)
	}
	desktop := vaulticDesktopEntry("/opt/my apps/vaultic")
	if !strings.Contains(desktop, `Exec=/opt/my\ apps/vaultic --background`) || !strings.Contains(desktop, "[Desktop Entry]") {
		t.Fatalf("desktop entry: %s", desktop)
	}
	plist := vaulticLaunchAgent("/Applications/Vaultic.app/Contents/MacOS/vaultic")
	if !strings.Contains(plist, "<string>--background</string>") || !strings.Contains(plist, "<key>RunAtLoad</key><true/>") {
		t.Fatalf("launch agent: %s", plist)
	}
}

func TestVaulticFileAutostartRoundTripOnLinuxAndMacOS(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		home := t.TempDir()
		a, err := vaulticAutostartFor(goos, home)
		if err != nil {
			t.Fatal(err)
		}
		if on, err := a.Enabled(); err != nil || on {
			t.Fatalf("%s: fresh home must not autostart: %v %v", goos, on, err)
		}
		if err := a.Set(true, ""); err == nil {
			t.Fatalf("%s: an empty executable must be refused", goos)
		}
		if err := a.Set(true, "/opt/vaultic/vaultic"); err != nil {
			t.Fatal(err)
		}
		if on, _ := a.Enabled(); !on {
			t.Fatalf("%s: entry not created", goos)
		}
		path := a.(vaulticFileAutostart).path
		if !strings.HasPrefix(path, home) {
			t.Fatalf("%s: entry must stay inside the user's home: %s", goos, path)
		}
		if content, _ := os.ReadFile(path); !strings.Contains(string(content), "--background") {
			t.Fatalf("%s: entry content: %s", goos, content)
		}
		if err := a.Set(false, ""); err != nil {
			t.Fatal(err)
		}
		if on, _ := a.Enabled(); on {
			t.Fatalf("%s: entry not removed", goos)
		}
		if err := a.Set(false, ""); err != nil {
			t.Fatalf("%s: removing a missing entry is not an error: %v", goos, err)
		}
	}
	if _, err := vaulticAutostartFor("plan9", t.TempDir()); err == nil {
		t.Fatal("an unsupported system must be reported")
	}
	_ = filepath.Join // keep the import explicit for readers
}

type fakeAutostart struct {
	enabled bool
	err     error
	setErr  error
	exe     string
}

func (f *fakeAutostart) Name() string           { return "Démarrage de test" }
func (f *fakeAutostart) Enabled() (bool, error) { return f.enabled, f.err }
func (f *fakeAutostart) Set(on bool, exe string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.enabled, f.exe = on, exe
	return nil
}

type backgroundFixture struct {
	c        *vaulticBackgroundController
	settings *vaulticSettings
	auto     *fakeAutostart
	persists int
	failSave error
	logs     []string
}

func newBackgroundFixture(t *testing.T, tray bool) *backgroundFixture {
	t.Helper()
	test.NewTempApp(t)
	w := test.NewWindow(nil)
	t.Cleanup(w.Close)
	f := &backgroundFixture{settings: &vaulticSettings{}, auto: &fakeAutostart{}}
	f.c = newVaulticBackgroundController(vaulticBackgroundHooks{
		Window: w, Settings: f.settings,
		Persist: func() error {
			f.persists++
			return f.failSave
		},
		Autostart: f.auto, Executable: func() (string, error) { return "/opt/vaultic", nil },
		TrayAvailable: tray, Log: func(s string) { f.logs = append(f.logs, s) },
	})
	return f
}

func TestVaulticBackgroundCloseToTrayIsSavedAndDrivesTheWindowClose(t *testing.T) {
	f := newBackgroundFixture(t, true)
	if f.c.ShouldHideOnClose() {
		t.Fatal("closing must quit by default")
	}
	f.c.CloseToTray.SetChecked(true)
	if !f.settings.CloseToTray || f.persists != 1 || !f.c.ShouldHideOnClose() {
		t.Fatalf("choice not applied: %+v persists=%d", f.settings, f.persists)
	}
	f.failSave = errors.New("disque plein")
	f.c.CloseToTray.SetChecked(false)
	if !f.settings.CloseToTray || !f.c.CloseToTray.Checked {
		t.Fatal("a failed save must keep the previous choice")
	}
}

func TestVaulticBackgroundAutostartFollowsTheSystemList(t *testing.T) {
	f := newBackgroundFixture(t, true)
	f.c.Autostart.SetChecked(true)
	if !f.auto.enabled || f.auto.exe != "/opt/vaultic" {
		t.Fatalf("startup entry not created: %+v", f.auto)
	}
	f.auto.setErr = errors.New("accès refusé")
	f.c.Autostart.SetChecked(false)
	if !f.c.Autostart.Checked {
		t.Fatal("a failed change must show the real system state")
	}
	f.auto.setErr = nil
	f.auto.enabled = true // changed outside Vaultic, in the system's startup list
	f.c.Sync()
	if !f.c.Autostart.Checked {
		t.Fatal("the check box must follow the system's startup list")
	}
}

func TestVaulticBackgroundOptionsAreDisabledWithoutANotificationArea(t *testing.T) {
	f := newBackgroundFixture(t, false)
	if !f.c.CloseToTray.Disabled() || !f.c.Autostart.Disabled() {
		t.Fatal("without a tray the window must never be hidden")
	}
	f.settings.CloseToTray = true
	if f.c.ShouldHideOnClose() {
		t.Fatal("no tray: never hide the window")
	}
}

func TestVaulticTrayMenuOffersOpenBackupAndQuit(t *testing.T) {
	var called []string
	menu := vaulticTrayMenu(vaulticTrayHooks{
		Show: func() { called = append(called, "show") }, BackupNow: func() { called = append(called, "backup") }, Quit: func() { called = append(called, "quit") },
	})
	var labels []string
	for _, item := range menu.Items {
		if item.IsSeparator {
			continue
		}
		labels = append(labels, item.Label)
		item.Action()
	}
	if strings.Join(labels, "|") != "Ouvrir Vaultic|Sauvegarder maintenant|Quitter Vaultic" || strings.Join(called, ",") != "show,backup,quit" {
		t.Fatalf("menu: %v calls: %v", labels, called)
	}
	var none *vaulticTray
	if none.Installed() {
		t.Fatal("a missing tray is not installed")
	}
}

func TestVaulticWindowCloserHidesToTrayButQuitStillQuits(t *testing.T) {
	f := newCloserFixture()
	hidden := 0
	f.c.HideToTray = func() bool { hidden++; return true }
	f.c.Intercept()
	if hidden != 1 || f.closed != 0 {
		t.Fatal("closing the window must hide it when the user chose the tray")
	}
	f.c.Quit()
	if f.closed != 1 {
		t.Fatal("the tray's Quit must really quit")
	}
	f2 := newCloserFixture()
	f2.c.HideToTray = func() bool { return false }
	f2.c.Intercept()
	if f2.closed != 1 {
		t.Fatal("without the tray option the window closes normally")
	}
}
