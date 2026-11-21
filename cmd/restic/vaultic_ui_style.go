package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Vaultic's visual language: midnight surfaces, restrained aqua accent,
// high-contrast typography and rounded cards. All controls stay native Fyne.
var (
	vaulticBackground  = color.NRGBA{R: 10, G: 17, B: 29, A: 255}
	vaulticSidebar     = color.NRGBA{R: 13, G: 23, B: 39, A: 255}
	vaulticPanel       = color.NRGBA{R: 20, G: 32, B: 51, A: 255}
	vaulticPanelRaised = color.NRGBA{R: 26, G: 41, B: 64, A: 255}
	vaulticStroke      = color.NRGBA{R: 48, G: 66, B: 88, A: 255}
	vaulticAccent      = color.NRGBA{R: 81, G: 227, B: 193, A: 255}
	vaulticText        = color.NRGBA{R: 239, G: 246, B: 253, A: 255}
	vaulticMuted       = color.NRGBA{R: 153, G: 171, B: 192, A: 255}
	vaulticWarning     = color.NRGBA{R: 255, G: 196, B: 111, A: 255}
)

type vaulticVisualTheme struct{}

var _ fyne.Theme = (*vaulticVisualTheme)(nil)

func (*vaulticVisualTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return vaulticBackground
	case theme.ColorNameForeground:
		return vaulticText
	case theme.ColorNamePrimary, theme.ColorNameFocus, theme.ColorNameHyperlink:
		return vaulticAccent
	case theme.ColorNameButton:
		return vaulticPanelRaised
	case theme.ColorNameDisabledButton:
		return vaulticSidebar
	case theme.ColorNameForegroundOnPrimary, theme.ColorNameForegroundOnSuccess:
		return vaulticBackground
	case theme.ColorNameHeaderBackground:
		return vaulticPanel
	case theme.ColorNameInputBackground, theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return vaulticSidebar
	case theme.ColorNameInputBorder, theme.ColorNameSeparator:
		return vaulticStroke
	case theme.ColorNamePlaceHolder, theme.ColorNameDisabled:
		return vaulticMuted
	case theme.ColorNameHover, theme.ColorNameSelection:
		return color.NRGBA{R: 81, G: 227, B: 193, A: 45}
	case theme.ColorNamePressed:
		return color.NRGBA{R: 81, G: 227, B: 193, A: 75}
	case theme.ColorNameSuccess:
		return vaulticAccent
	case theme.ColorNameWarning:
		return vaulticWarning
	case theme.ColorNameError:
		return color.NRGBA{R: 255, G: 117, B: 136, A: 255}
	}
	return theme.DefaultTheme().Color(name, theme.VariantDark)
}

func (*vaulticVisualTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (*vaulticVisualTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (*vaulticVisualTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 8
	case theme.SizeNameInnerPadding:
		return 10
	case theme.SizeNameButtonRadius, theme.SizeNameInputRadius:
		return 10
	case theme.SizeNameCardRadius:
		return 16
	}
	return theme.DefaultTheme().Size(name)
}

func vaulticSurface(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(vaulticPanel)
	bg.CornerRadius = 16
	bg.StrokeColor = vaulticStroke
	bg.StrokeWidth = 1
	return container.NewStack(bg, container.NewPadded(container.NewPadded(content)))
}

func vaulticHeading(text string, size float32) *canvas.Text {
	result := canvas.NewText(text, vaulticText)
	result.TextSize = size
	result.TextStyle = fyne.TextStyle{Bold: true}
	return result
}

func vaulticCaption(text string) *canvas.Text {
	result := canvas.NewText(text, vaulticMuted)
	result.TextSize = 12
	return result
}

func vaulticPageHeading(title, description string) fyne.CanvasObject {
	return container.NewVBox(
		vaulticHeading(title, 26),
		vaulticWrappedLabel(description),
	)
}

func vaulticStatCard(title string, value fyne.CanvasObject) fyne.CanvasObject {
	return vaulticSurface(container.NewVBox(
		vaulticCaption(title),
		value,
	))
}

func vaulticWrappedLabel(text string) *widget.Label {
	label := widget.NewLabel(text)
	label.Wrapping = fyne.TextWrapWord
	return label
}

func vaulticEmptyState(label *widget.Label) *fyne.Container {
	label.Alignment = fyne.TextAlignCenter
	return container.NewVBox(layout.NewSpacer(), label, layout.NewSpacer())
}

func vaulticListLabel() fyne.CanvasObject {
	label := widget.NewLabel("")
	label.Truncation = fyne.TextTruncateEllipsis
	return label
}

func vaulticSetButtonEnabled(button *widget.Button, enabled bool) {
	if enabled {
		button.Enable()
	} else {
		button.Disable()
	}
}

// vaulticPrepareWindow gives the window its minimum size without showing it.
func vaulticPrepareWindow(window fyne.Window, minimum fyne.Size) {
	bounds := canvas.NewRectangle(color.Transparent)
	bounds.SetMinSize(minimum)
	window.SetContent(container.NewStack(bounds, window.Content()))
	window.Resize(minimum)
}

func vaulticShowWindow(window fyne.Window, preferred, minimum fyne.Size) {
	vaulticPrepareWindow(window, minimum)
	window.CenterOnScreen()
	window.Show()

	// Fyne detects the display scale when the native window is created.
	scale := max(1, window.Canvas().Scale())
	window.Resize(fyne.NewSize(max(minimum.Width, preferred.Width/scale), max(minimum.Height, preferred.Height/scale)))
	window.CenterOnScreen()
}

// vaulticSurfaceRaised is a lighter, borderless panel used for sub-sections
// nested inside a surface (for instance the before/after headers).
func vaulticSurfaceRaised(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(vaulticPanelRaised)
	bg.CornerRadius = 12
	return container.NewStack(bg, container.NewPadded(content))
}
