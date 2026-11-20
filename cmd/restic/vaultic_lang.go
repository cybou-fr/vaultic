package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/lang"
)

// Fyne ships no French text for the default Select placeholder, which made it
// log a "Translation failure" every time a drop-down was created.
func init() {
	_ = lang.AddTranslationsForLocale([]byte(`{"(Select one)":"Choisir…"}`), fyne.Locale("fr"))
}
