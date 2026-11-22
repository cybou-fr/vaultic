package main

import (
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// frenchLeak finds text that is still French: accents typical of French, the
// guillemets, or common French words.
var frenchLeak = regexp.MustCompile(`(?i)[àâçéèêëîïôùûœ«»]|\b(le|la|les|des|du|pour|dans|avec|vous|votre|aucun|aucune|sauvegarde|dépôt|instantanés?|mot de passe|planifi|chaque|toutes?)\b`)

// textsOf collects every user-visible text of a widget tree.
func textsOf(obj fyne.CanvasObject, out *[]string) {
	if obj == nil {
		return
	}
	add := func(s string) {
		if strings.TrimSpace(s) != "" {
			*out = append(*out, s)
		}
	}
	switch v := obj.(type) {
	case *widget.Label:
		add(v.Text)
	case *widget.Button:
		add(v.Text)
	case *widget.Check:
		add(v.Text)
	case *widget.Entry:
		add(v.PlaceHolder)
	case *widget.Select:
		add(v.PlaceHolder)
		for _, o := range v.Options {
			add(o)
		}
	case *canvas.Text:
		add(v.Text)
	case *widget.Form:
		for _, item := range v.Items {
			add(item.Text)
			textsOf(item.Widget, out)
		}
	case *container.Scroll:
		textsOf(v.Content, out)
	case *container.AppTabs:
		for _, item := range v.Items {
			add(item.Text)
			textsOf(item.Content, out)
		}
	case *widget.Accordion:
		for _, item := range v.Items {
			add(item.Title)
			textsOf(item.Detail, out)
		}
	case *widget.RadioGroup:
		for _, o := range v.Options {
			add(o)
		}
	case *fyne.Container:
		for _, child := range v.Objects {
			textsOf(child, out)
		}
	}
}

func assertNoFrench(t *testing.T, where string, texts []string) {
	t.Helper()
	seen := map[string]bool{}
	var leaks []string
	for _, text := range texts {
		if seen[text] {
			continue
		}
		seen[text] = true
		if text == vaulticLangChoiceFR || text == vaulticLangChoiceAuto { // language names are shown as they are
			continue
		}
		if frenchLeak.MatchString(text) {
			leaks = append(leaks, text)
		}
	}
	sort.Strings(leaks)
	if len(leaks) > 0 {
		t.Errorf("%s still shows French text in English mode:\n  %s", where, strings.Join(leaks, "\n  "))
	}
}

func useEnglish(t *testing.T) {
	t.Helper()
	vaulticSetLanguage(vaulticLangEN)
	vaulticLocalizeTables()
	t.Cleanup(func() {
		vaulticSetLanguage(vaulticLangFR)
		vaulticLocalizeTables()
	})
}

func TestVaulticEnglishInterfaceShowsNoFrenchOnItsMainScreens(t *testing.T) {
	useEnglish(t)

	t.Run("automation tab", func(t *testing.T) {
		f := newAutomationFixture(t)
		f.profile.Schedule = &vaulticSchedule{Kind: vaulticScheduleWeekly, Enabled: true, Time: "09:00", Weekdays: []int{1, 4}, Repository: "local:/repo"}
		f.ui.Load()
		f.ui.Refresh()
		var texts []string
		textsOf(f.ui.Content, &texts)
		assertNoFrench(t, "the automation tab", texts)
		sum := vaulticSummarize(f.ui.hooks)
		assertNoFrench(t, "the automation summary", []string{sum.Schedule, sum.Next, sum.LastSuccess, sum.Activity, sum.Warning, sum.Target})
	})

	t.Run("retention tab", func(t *testing.T) {
		f := newRetentionFixture(t, t.TempDir())
		var texts []string
		textsOf(f.c.Content, &texts)
		assertNoFrench(t, "the retention tab", texts)
	})

	t.Run("repositories and storage setup", func(t *testing.T) {
		f := newReposFixture(t)
		f.c.Refresh()
		var texts []string
		textsOf(f.c.ListBox, &texts)
		textsOf(f.c.Setup.Content, &texts)
		textsOf(f.c.Remember, &texts)
		textsOf(f.c.Verify, &texts)
		textsOf(f.c.EditAuth, &texts)
		textsOf(f.c.ClearAuth, &texts)
		assertNoFrench(t, "the repositories page", texts)
	})

	t.Run("snapshots, profiles, background, language", func(t *testing.T) {
		var texts []string
		s, _ := newSnapshotsFixture(t)
		for _, b := range []fyne.CanvasObject{s.Refresh, s.Explorer, s.DetailsButton, s.Compare, s.Restore, s.Empty, s.DetailsScroll, s.RecentValue} {
			textsOf(b, &texts)
		}
		p := newProfilesFixture(t)
		for _, b := range []fyne.CanvasObject{p.c.AddFolder, p.c.AddFile, p.c.RemoveSource, p.c.New, p.c.Rename, p.c.Duplicate, p.c.Delete, p.c.Exclusions, p.c.Preview, p.c.Section, p.c.ExclusionSummary} {
			textsOf(b, &texts)
		}
		bg := newBackgroundFixture(t, false)
		textsOf(bg.c.Content, &texts)
		lang := newVaulticLanguageController(vaulticLanguageHooks{Window: nil, Settings: &vaulticSettings{}, Persist: func() error { return nil }})
		textsOf(lang.Content, &texts)
		assertNoFrench(t, "the snapshot, profile, background and language controls", texts)
	})

	t.Run("static pages and navigation", func(t *testing.T) {
		var texts []string
		textsOf(vaulticSettingsPage(widget.NewLabel("x"), widget.NewLabel("y"), widget.NewLabel("z")), &texts)
		textsOf(vaulticIntegrityPage(widget.NewButton("c", nil)), &texts)
		textsOf(vaulticActivityPage(widget.NewLabel("l"), func() {}), &texts)
		nav := vaulticNewNavigation(make([]fyne.CanvasObject, 8))
		_ = nav
		texts = append(texts, vaulticNavLabels...)
		m := newVaulticMaintenanceController(vaulticMaintenanceHooks{})
		textsOf(m.Check, &texts)
		textsOf(m.Analyze, &texts)
		textsOf(m.Updated, &texts)
		assertNoFrench(t, "the static pages", texts)
	})
}

func TestVaulticEnglishMessagesAndLabelsHaveNoFrench(t *testing.T) {
	useEnglish(t)
	var texts []string
	for _, kind := range []string{vaulticScheduleManual, vaulticScheduleHourly, vaulticScheduleDaily, vaulticScheduleWeekly, vaulticScheduleMonthly} {
		s := vaulticSchedule{Kind: kind, Enabled: true, EveryHours: 1, Time: "09:00", Weekdays: []int{1, 2, 3, 4, 5, 6, 0}, MonthDay: 31}
		texts = append(texts, s.Describe())
		s.EveryHours = 6
		texts = append(texts, s.Describe())
	}
	loc := time.UTC
	texts = append(texts, vaulticFormatMoment(time.Date(2026, 8, 5, 14, 3, 0, 0, loc), loc), vaulticFormatDuration(3*time.Hour+5*time.Minute), vaulticFormatAge(72*time.Hour), vaulticBytes(5<<20))
	texts = append(texts, vaulticStaleMessage("Docs", vaulticStaleness{Stale: true, Age: 72 * time.Hour}), vaulticStaleMessage("Docs", vaulticStaleness{Stale: true, Never: true, Age: 5 * time.Hour}))
	texts = append(texts, vaulticRecommendedRetention().Describe(), vaulticRetention{KeepLast: 1, KeepDaily: 1}.Describe())
	for _, status := range []string{vaulticStatusSuccess, vaulticStatusFailure, vaulticStatusCancelled, vaulticStatusSkipped} {
		texts = append(texts, vaulticStatusLabel(status))
	}
	for _, trigger := range []string{vaulticTriggerManual, vaulticTriggerScheduled, vaulticTriggerRetention} {
		texts = append(texts, vaulticTriggerLabel(trigger))
	}
	for _, info := range vaulticBackendCatalog {
		texts = append(texts, vaulticBackendDescription(info), info.Label)
	}
	for state := vaulticConnNotTested; state <= vaulticConnConnected; state++ {
		texts = append(texts, state.Label())
	}
	assertNoFrench(t, "formatted messages and labels", texts)
}
