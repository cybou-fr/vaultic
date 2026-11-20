package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// Opt-in: VAULTIC_SCREENSHOT_DIR=<dir> renders the storage screens to PNG files
// with Fyne's software renderer (no display needed).
func TestVaulticCaptureStorageScreens(t *testing.T) {
	dir := os.Getenv("VAULTIC_SCREENSHOT_DIR")
	if dir == "" {
		t.Skip("set VAULTIC_SCREENSHOT_DIR to render screenshots")
	}
	app := test.NewTempApp(t)
	app.Settings().SetTheme(&vaulticVisualTheme{})
	capture := func(name string, content fyne.CanvasObject, size fyne.Size) {
		window := test.NewWindow(content)
		defer window.Close()
		window.Resize(size)
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, window.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
	}
	setup := vaulticNewStorageSetupUI(vaulticStorageSetupHooks{})
	setup.name.SetText("Serveur du bureau")
	setup.SetScheme("sftp")
	setup.setField("host", "backup.example.org")
	setup.setField("port", "2222")
	setup.setField("user", "vaultic")
	setup.setField("path", "/srv/vaultic")
	setup.pass.SetText("mot-de-passe")
	setup.SetState(vaulticProbeResult{State: vaulticConnReachable, Detail: "Dépôt joignable. Le mot de passe n'a pas encore été vérifié."})
	capture("storage-sftp.png", container.NewVScroll(container.NewPadded(setup.Content)), fyne.NewSize(1000, 1020))

	bad := vaulticNewStorageSetupUI(vaulticStorageSetupHooks{})
	bad.SetScheme("gs")
	bad.setField("bucket", "mon-bucket")
	bad.setField("prefix", "../x")
	bad.SetState(vaulticProbeResult{State: vaulticConnAuthFailed, Detail: "Le stockage a refusé les identifiants (clés cloud, SSH ou serveur)."})
	capture("storage-gcs-invalid.png", container.NewVScroll(container.NewPadded(bad.Content)), fyne.NewSize(1000, 960))

	narrow := vaulticNewStorageSetupUI(vaulticStorageSetupHooks{})
	narrow.SetScheme("s3")
	capture("storage-s3-narrow.png", container.NewVScroll(container.NewPadded(narrow.Content)), fyne.NewSize(560, 1100))

	hooks := vaulticRepositoryHooks{Connect: func(vaulticDepot) {}, Edit: func(vaulticDepot) {}, Test: func(vaulticDepot) {}, Forget: func(vaulticDepot) {}}
	r1, _ := vaulticRepositoryRow(vaulticDepot{Name: "Disque externe", Location: `D:\Sauvegardes\vaultic`, LastVerified: "2026-11-20T13:03:00Z", LastVerifiedState: "connected"}, true, hooks)
	r2, _ := vaulticRepositoryRow(vaulticDepot{Name: "Serveur du bureau", Location: "sftp://vaultic:pw@backup.example.org:2222//srv/vaultic", LastVerified: "2026-11-19T08:30:00Z", LastVerifiedState: "auth_failed"}, false, hooks)
	r3, _ := vaulticRepositoryRow(vaulticDepot{Name: "Bucket Google", Location: "gs:mon-bucket:/vaultic"}, false, hooks)
	capture("repository-list.png", container.NewPadded(container.NewVBox(widget.NewLabel("Dépôts enregistrés"), r1, r2, r3)), fyne.NewSize(900, 640))
}
