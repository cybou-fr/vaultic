package main

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// These start in French and are rebuilt by vaulticLocalizeTables.
var (
	vaulticFreqManual  = vaulticL("Manuel uniquement")
	vaulticFreqHourly  = vaulticL("Toutes les N heures")
	vaulticFreqDaily   = vaulticL("Tous les jours")
	vaulticFreqWeekly  = vaulticL("Chaque semaine")
	vaulticFreqMonthly = vaulticL("Chaque mois")
)

var vaulticFrequencyKinds = map[string]string{
	vaulticFreqManual: vaulticScheduleManual, vaulticFreqHourly: vaulticScheduleHourly, vaulticFreqDaily: vaulticScheduleDaily,
	vaulticFreqWeekly: vaulticScheduleWeekly, vaulticFreqMonthly: vaulticScheduleMonthly,
}

func vaulticFrequencyLabel(kind string) string {
	for label, k := range vaulticFrequencyKinds {
		if k == kind {
			return label
		}
	}
	return vaulticFreqManual
}

var vaulticHourChoices = []string{"1", "2", "3", "4", "6", "8", "12", "24", "48", "72", "168"}

// vaulticAutomationHooks connects the Automatisation section to the program.
// Every function runs on the interface thread.
type vaulticAutomationHooks struct {
	Profile          func() vaulticBackupProfile
	SetSchedule      func(vaulticSchedule) error
	Paused           func() bool
	SetPaused        func(bool)
	Notify           func() vaulticNotifyPrefs
	SetNotify        func(vaulticNotifyPrefs)
	Coordinator      *vaulticCoordinator
	History          *vaulticHistoryStore
	Now              func() time.Time
	Credentials      func() bool
	ActiveRepository func() string
	Location         func() *time.Location
}

type vaulticAutomationUI struct {
	Content fyne.CanvasObject
	hooks   vaulticAutomationHooks

	enabled   *widget.Check
	frequency *widget.Select
	hours     *widget.Select
	clock     *widget.Entry
	weekdays  [7]*widget.Check // index = time.Weekday
	monthDay  *widget.Select
	notify    [4]*widget.Check
	advanced  *widget.Accordion
	hoursRow  fyne.CanvasObject
	clockRow  fyne.CanvasObject
	weekRow   fyne.CanvasObject
	monthRow  fyne.CanvasObject

	saveButton  *widget.Button
	pauseButton *widget.Button
	message     *widget.Label

	scheduleValue, nextValue, lastValue, activityValue, targetValue *widget.Label
	warning                                                         *widget.Label
	historyBox                                                      *fyne.Container
	loading                                                         bool
}

// Display order: Monday first.
var vaulticWeekdayOrder = []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday}

func newVaulticAutomationUI(h vaulticAutomationHooks) *vaulticAutomationUI {
	u := &vaulticAutomationUI{hooks: h}
	u.enabled = widget.NewCheck(vaulticT("Planification activée pour ce profil"), func(bool) { u.controlsChanged() })
	labels := []string{vaulticFreqManual, vaulticFreqHourly, vaulticFreqDaily, vaulticFreqWeekly, vaulticFreqMonthly}
	u.frequency = widget.NewSelect(labels, func(string) { u.controlsChanged() })
	u.hours = widget.NewSelect(vaulticHourChoices, func(string) { u.controlsChanged() })
	u.clock = widget.NewEntry()
	u.clock.SetPlaceHolder(vaulticT("HH:MM"))
	u.clock.Validator = func(s string) error { _, _, err := vaulticParseClock(s); return err }
	u.clock.OnChanged = func(string) { u.controlsChanged() }
	days := make([]string, 31)
	for i := range days {
		days[i] = strconv.Itoa(i + 1)
	}
	u.monthDay = widget.NewSelect(days, func(string) { u.controlsChanged() })

	weekGrid := container.NewGridWithColumns(4) // fixed rows: a wrapping grid under-reports its height inside an accordion
	for _, day := range vaulticWeekdayOrder {
		check := widget.NewCheck(vaulticWeekdayShortName(day), func(bool) { u.controlsChanged() })
		u.weekdays[day] = check
		weekGrid.Add(check)
	}
	notifyLabels := []string{vaulticT("Sauvegarde terminée"), vaulticT("Sauvegarde en échec"), vaulticT("Sauvegarde ignorée"), vaulticT("Sauvegarde en retard")}
	notifyBox := container.NewVBox()
	for i, label := range notifyLabels {
		u.notify[i] = widget.NewCheck(label, func(bool) { u.notifyChanged() })
		notifyBox.Add(u.notify[i])
	}

	u.hoursRow = labeledRow(vaulticT("Intervalle (heures)"), u.hours)
	u.clockRow = labeledRow(vaulticT("Heure (locale)"), u.clock)
	u.weekRow = labeledRow(vaulticT("Jours de la semaine"), weekGrid)
	u.monthRow = labeledRow(vaulticT("Jour du mois"), u.monthDay)
	advancedBody := container.NewVBox(
		u.weekRow, u.monthRow,
		vaulticCaption(vaulticT("Notifications du bureau")), notifyBox,
		vaulticCaption(vaulticT("Jour 29 à 31 : dernier jour du mois si besoin.")),
	)
	u.advanced = widget.NewAccordion(widget.NewAccordionItem(vaulticT("Options avancées"), advancedBody))

	u.saveButton = widget.NewButtonWithIcon(vaulticT("Enregistrer la planification"), theme.DocumentSaveIcon(), u.save)
	u.saveButton.Importance = widget.HighImportance
	u.pauseButton = widget.NewButtonWithIcon(vaulticT("Mettre en pause"), theme.MediaPauseIcon(), u.togglePause)
	u.message = vaulticWrappedLabel("")
	u.message.Importance = widget.DangerImportance

	u.scheduleValue, u.nextValue = widget.NewLabel(""), vaulticWrappedLabel("")
	u.lastValue, u.activityValue = vaulticWrappedLabel(""), vaulticWrappedLabel("")
	u.targetValue = vaulticWrappedLabel("")
	u.scheduleValue.Truncation = fyne.TextTruncateEllipsis
	u.warning = vaulticWrappedLabel("")
	u.warning.Importance = widget.WarningImportance
	u.historyBox = container.NewVBox()

	status := container.NewVBox(
		vaulticStatusLine(vaulticT("Fréquence"), u.scheduleValue),
		vaulticStatusLine(vaulticT("Dépôt de destination"), u.targetValue),
		vaulticStatusLine(vaulticT("Prochaine exécution"), u.nextValue),
		vaulticStatusLine(vaulticT("Dernière sauvegarde réussie"), u.lastValue),
		vaulticStatusLine(vaulticT("Activité"), u.activityValue),
		u.warning,
	)
	form := container.NewVBox(
		u.enabled,
		labeledRow(vaulticT("Fréquence"), u.frequency),
		u.hoursRow, u.clockRow,
		u.advanced,
		u.message,
		container.NewGridWrap(fyne.NewSize(250, 40), u.saveButton, u.pauseButton),
	)
	u.Content = container.NewVBox(
		vaulticSurface(container.NewVBox(vaulticHeading(vaulticT("Automatisation"), 20), status)),
		vaulticSurface(container.NewVBox(vaulticHeading(vaulticT("Planification"), 18), form,
			vaulticWrappedLabel(vaulticT("Le dépôt actif est fixé à cette planification lors de l'enregistrement. Pour choisir un autre dépôt, activez-le dans Dépôts, puis enregistrez la planification. Vaultic doit rester ouvert et le mot de passe du dépôt doit être saisi. Aucun instantané n'est supprimé automatiquement.")))),
		vaulticSurface(container.NewVBox(vaulticHeading(vaulticT("Historique récent"), 18), u.historyBox)),
	)
	u.Load()
	return u
}

func labeledRow(label string, control fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(vaulticCaption(label), control)
}

func vaulticStatusLine(title string, value fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(vaulticCaption(strings.ToUpper(title)), value)
}

// Load shows the selected profile's schedule in the controls.
func (u *vaulticAutomationUI) Load() {
	u.loading = true
	profile := u.hooks.Profile()
	s := vaulticSchedule{Kind: vaulticScheduleManual, EveryHours: 24, Time: "02:00", MonthDay: 1}
	if profile.Schedule != nil {
		s = *profile.Schedule
	}
	u.enabled.SetChecked(s.Enabled)
	u.frequency.SetSelected(vaulticFrequencyLabel(s.Kind))
	hours := s.EveryHours
	if hours == 0 {
		hours = 24
	}
	u.hours.SetSelected(strconv.Itoa(hours))
	if s.Time == "" {
		s.Time = "02:00"
	}
	u.clock.SetText(s.Time)
	for _, day := range vaulticWeekdayOrder {
		u.weekdays[day].SetChecked(false)
	}
	for _, day := range s.Weekdays {
		if day >= 0 && day <= 6 {
			u.weekdays[time.Weekday(day)].SetChecked(true)
		}
	}
	if s.Kind == vaulticScheduleWeekly && len(s.Weekdays) == 0 {
		u.weekdays[time.Monday].SetChecked(true)
	}
	if s.MonthDay < 1 {
		s.MonthDay = 1
	}
	u.monthDay.SetSelected(strconv.Itoa(s.MonthDay))
	prefs := u.hooks.Notify()
	for i, v := range []bool{prefs.Completed, prefs.Failed, prefs.Skipped, prefs.Overdue} {
		u.notify[i].SetChecked(v)
	}
	u.message.SetText("")
	u.loading = false
	u.updateRows()
	u.Refresh()
}

func (u *vaulticAutomationUI) updateRows() {
	kind := vaulticFrequencyKinds[u.frequency.Selected]
	show := func(o fyne.CanvasObject, on bool) {
		if on {
			o.Show()
		} else {
			o.Hide()
		}
	}
	show(u.hoursRow, kind == vaulticScheduleHourly)
	show(u.clockRow, kind == vaulticScheduleDaily || kind == vaulticScheduleWeekly || kind == vaulticScheduleMonthly)
	show(u.weekRow, kind == vaulticScheduleWeekly)
	show(u.monthRow, kind == vaulticScheduleMonthly)
	if kind == vaulticScheduleManual {
		u.enabled.Disable()
	} else {
		u.enabled.Enable()
	}
}

func (u *vaulticAutomationUI) controlsChanged() {
	if u.loading {
		return
	}
	u.updateRows()
	u.message.SetText("")
}

// schedule reads the controls. The anchor is set by the caller.
func (u *vaulticAutomationUI) schedule() (vaulticSchedule, error) {
	s := vaulticSchedule{Kind: vaulticFrequencyKinds[u.frequency.Selected], Enabled: u.enabled.Checked}
	if s.Kind == "" || s.Kind == vaulticScheduleManual {
		return vaulticSchedule{Kind: vaulticScheduleManual}, nil
	}
	s.EveryHours, _ = strconv.Atoi(u.hours.Selected)
	s.Time = strings.TrimSpace(u.clock.Text)
	for _, day := range vaulticWeekdayOrder {
		if u.weekdays[day].Checked {
			s.Weekdays = append(s.Weekdays, int(day))
		}
	}
	s.MonthDay, _ = strconv.Atoi(u.monthDay.Selected)
	switch s.Kind {
	case vaulticScheduleHourly:
		s.Time, s.Weekdays, s.MonthDay = "", nil, 0
	case vaulticScheduleDaily:
		s.EveryHours, s.Weekdays, s.MonthDay = 0, nil, 0
	case vaulticScheduleWeekly:
		s.EveryHours, s.MonthDay = 0, 0
	case vaulticScheduleMonthly:
		s.EveryHours, s.Weekdays = 0, nil
	}
	return s, s.Validate()
}

func (u *vaulticAutomationUI) save() {
	s, err := u.schedule()
	if err != nil {
		u.message.SetText(err.Error())
		return
	}
	if err := u.hooks.SetSchedule(s); err != nil {
		u.message.SetText(err.Error())
		return
	}
	u.message.SetText("")
	u.Refresh()
}

func (u *vaulticAutomationUI) notifyChanged() {
	if u.loading {
		return
	}
	u.hooks.SetNotify(vaulticNotifyPrefs{
		Completed: u.notify[0].Checked, Failed: u.notify[1].Checked, Skipped: u.notify[2].Checked, Overdue: u.notify[3].Checked,
	})
}

func (u *vaulticAutomationUI) togglePause() {
	u.hooks.SetPaused(!u.hooks.Paused())
	u.Refresh()
}

// vaulticFormatMoment writes a date in the interface language: "mer. 10 juin 2026 à 14:03".
func vaulticFormatMoment(t time.Time, loc *time.Location) string {
	t = t.In(loc)
	return fmt.Sprintf(vaulticT("%s %d %s %d à %02d:%02d"), vaulticDayShort(t.Weekday()), t.Day(), vaulticMonthShort(t.Month()), t.Year(), t.Hour(), t.Minute())
}

func vaulticFormatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return "< 1 s"
	case d < time.Minute:
		return fmt.Sprintf(vaulticT("%d s"), int(d.Round(time.Second)/time.Second))
	case d < time.Hour:
		return fmt.Sprintf(vaulticT("%d min %02d s"), int(d/time.Minute), int(d%time.Minute/time.Second))
	}
	return fmt.Sprintf(vaulticT("%d h %02d min"), int(d/time.Hour), int(d%time.Hour/time.Minute))
}

// vaulticAutomationSummary computes the status texts (separate from widgets so
// that they can be tested).
type vaulticAutomationSummary struct {
	Schedule, Next, LastSuccess, Activity, Warning, Target, Repository string
	Paused                                                             bool
}

func vaulticSummarize(h vaulticAutomationHooks) vaulticAutomationSummary {
	loc, now := h.Location(), h.Now()
	profile := h.Profile()
	var out vaulticAutomationSummary
	out.Paused = h.Paused()
	activeRepo := ""
	if h.ActiveRepository != nil {
		activeRepo = h.ActiveRepository()
	}
	repo := activeRepo
	if profile.Schedule != nil && profile.Schedule.Repository != "" {
		repo = profile.Schedule.Repository
	}
	out.Repository = repo
	if repo != "" {
		out.Target = vaulticSafeLocation(repo)
	} else {
		out.Target = vaulticT("Aucun dépôt sélectionné")
	}
	switch {
	case profile.Schedule == nil || profile.Schedule.Kind == "" || profile.Schedule.Kind == vaulticScheduleManual:
		out.Schedule, out.Next = vaulticT("Manuel uniquement"), vaulticT("Aucune — lancez la sauvegarde vous-même.")
	case !profile.Schedule.Enabled:
		out.Schedule, out.Next = profile.Schedule.Describe()+vaulticT(" (désactivée)"), vaulticT("Aucune — la planification est désactivée.")
	default:
		out.Schedule = profile.Schedule.Describe()
		plan := vaulticPlan(*profile.Schedule, now, loc)
		switch {
		case out.Paused:
			out.Next = vaulticT("Aucune — la planification est en pause.")
		case h.Coordinator != nil && h.Coordinator.Overdue(profile.Name):
			out.Next = vaulticT("En retard — en attente du mot de passe du dépôt.")
		case plan.Due:
			out.Next = vaulticT("Imminente (au prochain contrôle).")
		case !plan.Next.IsZero():
			out.Next = vaulticFormatMoment(plan.Next, loc)
		default:
			out.Next = vaulticT("Indéterminée")
		}
		if out.Paused {
			out.Schedule += vaulticT(" — en pause")
		}
	}
	if last, ok := h.History.LastSuccessForRepository(profile.Name, repo); ok {
		out.LastSuccess = vaulticFormatMoment(last.Ended, loc) + vaulticT(" · instantané ") + shortID(last.SnapshotID)
	} else {
		out.LastSuccess = vaulticT("Aucune sauvegarde réussie enregistrée.")
	}
	out.Activity = vaulticT("Aucune sauvegarde en cours.")
	if h.Coordinator != nil {
		if run, active := h.Coordinator.Active(); active {
			out.Activity = fmt.Sprintf(vaulticT("Sauvegarde %s en cours depuis %s (profil « %s »)."),
				strings.ToLower(vaulticTriggerLabel(run.Job.Trigger)), vaulticFormatDuration(now.Sub(run.Started)), run.Job.Profile)
		}
		if h.Coordinator.Overdue(profile.Name) {
			if repo != activeRepo {
				out.Warning = vaulticT("Une sauvegarde planifiée est en retard : activez le dépôt de destination sur la page Dépôts. Elle ne sera pas considérée comme réalisée.")
			} else if !h.Credentials() {
				out.Warning = vaulticT("Une sauvegarde planifiée est en retard : saisissez le mot de passe du dépôt (page Dépôts). Elle ne sera pas considérée comme réalisée.")
			}
		} else if profile.Schedule != nil && profile.Schedule.Active() && repo != activeRepo {
			out.Warning = vaulticT("Le dépôt de destination n'est pas actif. Activez-le sur la page Dépôts avant la prochaine sauvegarde.")
		}
	}
	if profile.Schedule != nil && profile.Schedule.Active() {
		var last time.Time
		if rec, ok := h.History.LastSuccessForRepository(profile.Name, repo); ok {
			last = rec.Ended
		}
		if st := vaulticCheckStale(*profile.Schedule, last, now); st.Stale {
			note := vaulticStaleMessage(profile.Name, st)
			if out.Warning != "" {
				out.Warning += "\n" + note
			} else {
				out.Warning = note
			}
		}
	}
	if h.Coordinator != nil {
		if problem := h.Coordinator.PersistenceProblem(); problem != "" {
			note := vaulticT("Enregistrement impossible : ") + problem + vaulticT(" Libérez de l'espace ou vérifiez les droits du dossier de configuration.")
			if out.Warning != "" {
				out.Warning += "\n" + note
			} else {
				out.Warning = note
			}
		}
	}
	return out
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	if id == "" {
		return "—"
	}
	return id
}

func vaulticHistoryRow(r vaulticRunRecord, loc *time.Location) fyne.CanvasObject {
	statusColor := vaulticMuted
	switch r.Status {
	case vaulticStatusSuccess:
		statusColor = vaulticAccent
	case vaulticStatusFailure:
		statusColor = color.NRGBA{R: 255, G: 117, B: 136, A: 255}
	case vaulticStatusSkipped:
		statusColor = vaulticWarning
	}
	head := canvas.NewText(vaulticStatusLabel(r.Status), statusColor)
	head.TextStyle, head.TextSize = fyne.TextStyle{Bold: true}, 14
	when := vaulticCaption(vaulticFormatMoment(r.Started, loc))
	trigger := vaulticTriggerLabel(r.Trigger)
	if r.CatchUp {
		trigger += " (rattrapage)"
	}
	details := []string{trigger}
	if r.Status != vaulticStatusSkipped {
		details = append(details, vaulticFormatDuration(r.Duration()))
		if r.Files > 0 {
			details = append(details, fmt.Sprintf(vaulticT("%d fichier(s)"), r.Files), vaulticBytes(r.Bytes))
		}
		if r.SnapshotID != "" {
			details = append(details, vaulticT("instantané ")+shortID(r.SnapshotID))
		}
	}
	lines := []fyne.CanvasObject{container.NewHBox(head, when), vaulticWrappedLabel(strings.Join(details, " · "))}
	if r.Summary != "" {
		lines = append(lines, vaulticWrappedLabel(r.Summary))
	}
	return vaulticSurfaceRaised(container.NewVBox(lines...))
}

// Refresh updates the status block and the history list.
func (u *vaulticAutomationUI) Refresh() {
	sum := vaulticSummarize(u.hooks)
	u.scheduleValue.SetText(sum.Schedule)
	u.targetValue.SetText(sum.Target)
	u.nextValue.SetText(sum.Next)
	u.lastValue.SetText(sum.LastSuccess)
	u.activityValue.SetText(sum.Activity)
	u.warning.SetText(sum.Warning)
	if sum.Warning == "" {
		u.warning.Hide()
	} else {
		u.warning.Show()
	}
	if sum.Paused {
		u.pauseButton.SetText(vaulticT("Reprendre la planification"))
		u.pauseButton.SetIcon(theme.MediaPlayIcon())
	} else {
		u.pauseButton.SetText(vaulticT("Mettre en pause"))
		u.pauseButton.SetIcon(theme.MediaPauseIcon())
	}
	records := u.hooks.History.RecentForRepository(u.hooks.Profile().Name, sum.Repository, 8)
	rows := make([]fyne.CanvasObject, 0, len(records))
	loc := u.hooks.Location()
	for _, r := range records {
		rows = append(rows, vaulticHistoryRow(r, loc))
	}
	if len(rows) == 0 {
		rows = append(rows, vaulticWrappedLabel(vaulticT("Aucune exécution enregistrée pour ce profil.")))
	}
	u.historyBox.Objects = rows
	u.historyBox.Refresh()
}
