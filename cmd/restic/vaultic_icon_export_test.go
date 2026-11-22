package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

// vaulticIconBoldSVG is the same drawing with heavier strokes, used for the
// smallest icon sizes where the thin lines of the window icon would vanish.
const vaulticIconBoldSVG = "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"128\" height=\"128\" viewBox=\"0 0 128 128\">\n<defs><linearGradient id=\"a\" x1=\"0\" y1=\"0\" x2=\"1\" y2=\"1\"><stop stop-color=\"#7FF4D9\"/><stop offset=\"1\" stop-color=\"#38BDB4\"/></linearGradient></defs>\n<rect width=\"128\" height=\"128\" rx=\"30\" fill=\"#101C30\"/>\n<path d=\"M64 13 107 32v31c0 30-16 47-43 54C37 110 21 93 21 63V32L64 13Z\" fill=\"none\" stroke=\"url(#a)\" stroke-width=\"11\"/>\n<path d=\"m40 48 24 43 24-43\" fill=\"none\" stroke=\"#EAF8F6\" stroke-width=\"15\" stroke-linecap=\"round\" stroke-linejoin=\"round\"/>\n</svg>"

// TestVaulticExportIcon renders the application icon (the same SVG the window
// uses) to a 512 px PNG file. scripts/make-icon.py runs it to build
// vaultic.ico. It does nothing unless VAULTIC_ICON_DIR is set.
func TestVaulticExportIcon(t *testing.T) {
	dir := os.Getenv("VAULTIC_ICON_DIR")
	if dir == "" {
		t.Skip("set VAULTIC_ICON_DIR to export the icon")
	}
	test.NewTempApp(t)
	variants := map[string]fyne.Resource{
		"icon":      vaulticAppIcon(),
		"icon-bold": fyne.NewStaticResource("vaultic-bold.svg", []byte(vaulticIconBoldSVG)),
	}
	for name, resource := range variants {
		const size = float32(512)
		img := canvas.NewImageFromResource(resource)
		img.FillMode = canvas.ImageFillStretch
		img.ScaleMode = canvas.ImageScaleSmooth
		img.SetMinSize(fyne.NewSize(size, size))
		rendered := software.Render(img, theme.DarkTheme())
		file, err := os.Create(filepath.Join(dir, fmt.Sprintf("%s-%d.png", name, int(size))))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, rendered); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if b := rendered.Bounds(); b.Dx() != int(size) || b.Dy() != int(size) {
			t.Fatalf("icon %v rendered at %dx%d", size, b.Dx(), b.Dy())
		}
	}
}
