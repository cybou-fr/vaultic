package main

import "time"

// Messages that live in package-level tables or constants cannot call
// vaulticT while the program initialises (the language is not known yet).
// They start in French; vaulticLocalizeTables rebuilds them in the selected
// language, once at start-up after the language was chosen.

// vaulticL marks a French message in a package-level table so that the
// catalog check knows it. It returns the message unchanged; the code that shows
// the table translates the value with vaulticT.
func vaulticL(french string) string { return french }

// vaulticMonthShort is the abbreviated month name.
func vaulticMonthShort(m time.Month) string {
	names := [...]string{"", vaulticT("janv."), vaulticT("févr."), vaulticT("mars"), vaulticT("avr."), vaulticT("mai"), vaulticT("juin"),
		vaulticT("juil."), vaulticT("août"), vaulticT("sept."), vaulticT("oct."), vaulticT("nov."), vaulticT("déc.")}
	if m < 1 || m > 12 {
		return ""
	}
	return names[m]
}

// vaulticDayShort is the abbreviated weekday name.
func vaulticDayShort(d time.Weekday) string {
	names := [...]string{vaulticT("dim."), vaulticT("lun."), vaulticT("mar."), vaulticT("mer."), vaulticT("jeu."), vaulticT("ven."), vaulticT("sam.")}
	return names[int(d)%7]
}

// vaulticWeekdayName is the full weekday name (0 = Sunday).
func vaulticWeekdayName(d int) string {
	names := [...]string{vaulticT("dimanche"), vaulticT("lundi"), vaulticT("mardi"), vaulticT("mercredi"), vaulticT("jeudi"), vaulticT("vendredi"), vaulticT("samedi")}
	return names[((d%7)+7)%7]
}

// vaulticWeekdayShortName is the label of a weekday check box.
func vaulticWeekdayShortName(d time.Weekday) string {
	names := [...]string{vaulticT("Dim"), vaulticT("Lun"), vaulticT("Mar"), vaulticT("Mer"), vaulticT("Jeu"), vaulticT("Ven"), vaulticT("Sam")}
	return names[int(d)%7]
}

// vaulticBackendCatalogSource keeps the French table as written; the shown
// catalog is rebuilt from it by vaulticLocalizeTables.
var vaulticBackendCatalogSource = append([]vaulticBackendInfo(nil), vaulticBackendCatalog...)

// vaulticLocalizeTables rebuilds the constants and tables in the selected
// language. Call it once the language is set and before any window is built.
func vaulticLocalizeTables() {
	vaulticFreqManual = vaulticT("Manuel uniquement")
	vaulticFreqHourly = vaulticT("Toutes les N heures")
	vaulticFreqDaily = vaulticT("Tous les jours")
	vaulticFreqWeekly = vaulticT("Chaque semaine")
	vaulticFreqMonthly = vaulticT("Chaque mois")
	vaulticFrequencyKinds = map[string]string{
		vaulticFreqManual: vaulticScheduleManual, vaulticFreqHourly: vaulticScheduleHourly, vaulticFreqDaily: vaulticScheduleDaily,
		vaulticFreqWeekly: vaulticScheduleWeekly, vaulticFreqMonthly: vaulticScheduleMonthly,
	}
	vaulticDiffAll = vaulticT("Toutes")
	vaulticDiffAdded = vaulticT("Ajoutés")
	vaulticDiffModified = vaulticT("Modifiés")
	vaulticDiffRemoved = vaulticT("Supprimés")
	vaulticNoStorageAnalysis = vaulticT("Aucune analyse effectuée pour ce dépôt.")
	vaulticNoSnapshotSelected = vaulticT("Sélectionnez un instantané pour le parcourir ou le restaurer.")
	vaulticNavLabels = []string{vaulticT("Vue d'ensemble"), vaulticT("Sauvegarde"), vaulticT("Instantanés"), vaulticT("Stockage"),
		vaulticT("Intégrité"), vaulticT("Activité"), vaulticT("Dépôts"), vaulticT("Paramètres")}
	for i, source := range vaulticBackendCatalogSource {
		vaulticBackendCatalog[i] = vaulticBackendInfo{
			Scheme: source.Scheme, External: source.External,
			Label: vaulticT(source.Label), Example: vaulticT(source.Example),
			Description: vaulticT(source.Description), Requirements: vaulticT(source.Requirements),
		}
	}
}
