package main

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2/theme"
)

func TestVaulticThemeUsesReadablePrimaryForeground(t *testing.T) {
	appearance := &vaulticVisualTheme{}
	primary := color.NRGBAModel.Convert(appearance.Color(theme.ColorNamePrimary, theme.VariantDark)).(color.NRGBA)
	foreground := color.NRGBAModel.Convert(appearance.Color(theme.ColorNameForegroundOnPrimary, theme.VariantDark)).(color.NRGBA)
	if primary == foreground {
		t.Fatal("primary action label and background must have different colors")
	}
	if appearance.Size(theme.SizeNameCardRadius) < 10 {
		t.Fatal("cards should preserve the Vaultic rounded-corner design")
	}
}
