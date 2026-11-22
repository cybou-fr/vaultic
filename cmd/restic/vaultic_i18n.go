package main

import (
	"strings"
	"sync/atomic"
)

// Interface language. The source text of every message is French and is its
// own key: vaulticT("Sauvegarder maintenant") returns the French text, or its
// English translation when English is selected and the catalog has one. A
// missing translation falls back to French, never to an empty string.

const (
	vaulticLangAuto = "auto"
	vaulticLangFR   = "fr"
	vaulticLangEN   = "en"
)

var vaulticActiveLanguage atomic.Value // string: "fr" or "en"

func init() { vaulticActiveLanguage.Store(vaulticLangFR) }

// vaulticResolveLanguage turns the saved choice ("auto", "fr", "en") and the
// system's locale (for example "fr-FR", "en_US") into "fr" or "en".
func vaulticResolveLanguage(setting, systemLocale string) string {
	switch strings.ToLower(strings.TrimSpace(setting)) {
	case vaulticLangFR:
		return vaulticLangFR
	case vaulticLangEN:
		return vaulticLangEN
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(systemLocale)), "fr") {
		return vaulticLangFR
	}
	return vaulticLangEN
}

// vaulticSetLanguage selects the language of every message translated from now on.
func vaulticSetLanguage(code string) {
	if code != vaulticLangEN {
		code = vaulticLangFR
	}
	vaulticActiveLanguage.Store(code)
}

func vaulticLanguage() string {
	if code, ok := vaulticActiveLanguage.Load().(string); ok {
		return code
	}
	return vaulticLangFR
}

// vaulticT translates a message. The argument must be a string literal so the
// catalog check can find it.
func vaulticT(french string) string {
	if vaulticLanguage() == vaulticLangEN {
		if english, ok := vaulticEnglish[french]; ok && english != "" {
			return english
		}
	}
	return french
}
