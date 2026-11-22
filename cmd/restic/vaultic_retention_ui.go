package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/restic/restic/internal/global"
)

// vaulticRetentionHooks connects the Conservation tab to the application.
type vaulticRetentionHooks struct {
	Window  fyne.Window
	Profile func() vaulticBackupProfile
	// SaveRetention stores the policy of the selected profile.
	SaveRetention func(vaulticRetention) error
	Repository    func() string
	// Run runs an engine operation on the active repository through the
	// single gate; finish runs on the UI thread when it succeeded.
	Run           func(title string, fn func(context.Context, global.Options) error, finish func() error)
	History       *vaulticHistoryStore
	Log           func(string)
	UpdateActions func()
	Now           func() time.Time
}

// vaulticRetentionController is the Conservation tab: edit the policy, preview
// what it would delete, delete only that after confirmation, and free space as
// a separate, explicit step. Nothing here runs automatically.
type vaulticRetentionController struct {
	hooks vaulticRetentionHooks

	Content  fyne.CanvasObject
	entries  [5]*widget.Entry
	Save     *widget.Button
	Defaults *widget.Button
	Preview  *widget.Button
	Apply    *widget.Button
	Prune    *widget.Button
	summary  *widget.Label
	details  *widget.Label

	preview *vaulticRetentionPreview
	ready   bool // false while the controller is being built: hooks may still point at nothing
}

func newVaulticRetentionController(hooks vaulticRetentionHooks) *vaulticRetentionController {
	if hooks.Now == nil {
		hooks.Now = time.Now
	}
	c := &vaulticRetentionController{hooks: hooks}
	labels := [5]string{"Derniers instantanés", "Un par jour pendant (jours)", "Un par semaine pendant (semaines)", "Un par mois pendant (mois)", "Un par an pendant (années)"}
	items := make([]*widget.FormItem, 0, 5)
	for i := range c.entries {
		e := widget.NewEntry()
		e.SetPlaceHolder("0")
		e.OnChanged = func(string) { c.invalidate() }
		c.entries[i] = e
		items = append(items, widget.NewFormItem(labels[i], e))
	}
	c.Defaults = widget.NewButton("Valeurs recommandées", func() { c.fill(vaulticRecommendedRetention()) })
	c.Save = widget.NewButton("Enregistrer la politique", c.save)
	c.Preview = widget.NewButton("Prévisualiser (rien n'est supprimé)", c.runPreview)
	c.Preview.Importance = widget.HighImportance
	c.Apply = widget.NewButton("Supprimer les instantanés listés…", c.confirmApply)
	c.Apply.Importance = widget.DangerImportance
	c.Prune = widget.NewButton("Libérer l'espace disque…", c.confirmPrune)
	c.summary = vaulticWrappedLabel("Aucun aperçu. Lancez « Prévisualiser » pour voir ce que la politique supprimerait.")
	c.details = widget.NewLabel("")
	scroll := container.NewVScroll(c.details)
	scroll.SetMinSize(fyne.NewSize(0, 150))
	warning := vaulticWrappedLabel("Rien n'est supprimé automatiquement, ni après une sauvegarde ni par la planification. " +
		"La suppression n'a lieu qu'après un aperçu et votre confirmation, et ne concerne que les instantanés de ce profil.")
	warning.Importance = widget.WarningImportance
	c.Content = container.NewVBox(
		vaulticHeading("Conservation des instantanés", 20),
		warning,
		widget.NewForm(items...),
		container.NewHBox(c.Defaults, c.Save),
		c.Preview,
		c.summary,
		scroll,
		c.Apply,
		vaulticWrappedLabel("Après une suppression, l'espace disque n'est rendu qu'après « Libérer l'espace ». Cette opération peut être longue ; laissez Vaultic ouvert pendant son exécution."),
		c.Prune,
	)
	c.Load()
	c.ready = true
	return c
}

func (c *vaulticRetentionController) fill(r vaulticRetention) {
	values := [5]int{r.KeepLast, r.KeepDaily, r.KeepWeekly, r.KeepMonthly, r.KeepYearly}
	for i, e := range c.entries {
		if values[i] == 0 {
			e.SetText("")
		} else {
			e.SetText(strconv.Itoa(values[i]))
		}
	}
}

// Load shows the policy of the selected profile and forgets any preview.
func (c *vaulticRetentionController) Load() {
	var r vaulticRetention
	if p := c.hooks.Profile(); p.Retention != nil {
		r = *p.Retention
	}
	c.fill(r)
	c.invalidate()
}

// vaulticParseRetention reads the five entries; blank means 0.
func vaulticParseRetention(texts [5]string) (vaulticRetention, error) {
	var n [5]int
	for i, text := range texts {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		v, err := strconv.Atoi(text)
		if err != nil {
			return vaulticRetention{}, errors.New("Saisissez des nombres entiers (ou laissez vide pour 0).")
		}
		n[i] = v
	}
	r := vaulticRetention{KeepLast: n[0], KeepDaily: n[1], KeepWeekly: n[2], KeepMonthly: n[3], KeepYearly: n[4]}
	return r, r.Validate()
}

func (c *vaulticRetentionController) policy() (vaulticRetention, error) {
	var texts [5]string
	for i, e := range c.entries {
		texts[i] = e.Text
	}
	return vaulticParseRetention(texts)
}

// invalidate forgets the preview: changing the policy, the profile or the
// repository means the preview no longer describes what would happen.
func (c *vaulticRetentionController) invalidate() {
	if c.preview != nil {
		c.preview = nil
	}
	if c.summary != nil {
		c.summary.SetText("Aucun aperçu. Lancez « Prévisualiser » pour voir ce que la politique supprimerait.")
		c.details.SetText("")
	}
	if c.ready && c.hooks.UpdateActions != nil {
		c.hooks.UpdateActions()
	}
}

// UpdateState enables the controls for the application state.
func (c *vaulticRetentionController) UpdateState(busy, hasRepository, hasSources bool) {
	vaulticSetButtonEnabled(c.Preview, !busy && hasRepository && hasSources)
	vaulticSetButtonEnabled(c.Apply, !busy && hasRepository && c.preview != nil && len(c.preview.Remove) > 0)
	vaulticSetButtonEnabled(c.Prune, !busy && hasRepository)
	vaulticSetButtonEnabled(c.Save, !busy)
}

func (c *vaulticRetentionController) save() {
	policy, err := c.policy()
	if err != nil {
		dialog.ShowError(err, c.hooks.Window)
		return
	}
	if err := c.hooks.SaveRetention(policy); err != nil {
		dialog.ShowError(err, c.hooks.Window)
		return
	}
	c.hooks.Log("Politique de conservation enregistrée : " + policy.Describe() + " (rien n'a été supprimé).")
}

func (c *vaulticRetentionController) context() (profile vaulticBackupProfile, host string, err error) {
	profile = c.hooks.Profile()
	if len(profile.Sources) == 0 {
		return profile, "", errors.New("Ce profil n'a aucune source.")
	}
	host, _ = os.Hostname()
	return profile, host, nil
}

func (c *vaulticRetentionController) runPreview() {
	policy, err := c.policy()
	if err != nil {
		dialog.ShowError(err, c.hooks.Window)
		return
	}
	profile, host, err := c.context()
	if err != nil {
		dialog.ShowError(err, c.hooks.Window)
		return
	}
	repository := c.hooks.Repository()
	var built vaulticRetentionPreview
	c.hooks.Run("Aperçu de la conservation", func(ctx context.Context, g global.Options) error {
		sources, err := vaulticSourcesForBackup(g.Repo, profile.Sources)
		if err != nil {
			return err
		}
		all, err := vaulticListSnapshots(ctx, g)
		if err != nil {
			return err
		}
		built, err = vaulticPreviewRetention(all, profile.Name, repository, host, sources, policy, c.hooks.Now())
		return err
	}, func() error {
		c.preview = &built
		c.render()
		c.hooks.UpdateActions()
		return nil
	})
}

func (c *vaulticRetentionController) render() {
	p := c.preview
	if p == nil {
		return
	}
	total := len(p.Keep) + len(p.Remove)
	c.summary.SetText(fmt.Sprintf("%s\nInstantanés de ce profil : %d · conservés : %d · à supprimer : %d · autres instantanés (non concernés) : %d.",
		p.Policy.Describe(), total, len(p.Keep), len(p.Remove), p.Others))
	var b strings.Builder
	if len(p.Remove) > 0 {
		fmt.Fprintf(&b, "À SUPPRIMER (%d)\n%s\n\n", len(p.Remove), vaulticRetentionLines(p.Remove, false))
	} else {
		b.WriteString("Rien à supprimer avec cette politique.\n\n")
	}
	fmt.Fprintf(&b, "CONSERVÉS (%d)\n%s", len(p.Keep), vaulticRetentionLines(p.Keep, true))
	c.details.SetText(b.String())
}

// vaulticRetentionConfirmText is the question asked before deleting.
func vaulticRetentionConfirmText(p vaulticRetentionPreview) string {
	oldest, newest := p.Remove[len(p.Remove)-1].Time, p.Remove[0].Time
	return fmt.Sprintf("Supprimer définitivement %d instantané(s) du profil « %s » ?\n\nDe %s à %s.\n%d instantané(s) seront conservés.\n\n"+
		"Cette action est irréversible : les fichiers qui n'existent que dans ces instantanés ne pourront plus être restaurés.",
		len(p.Remove), p.Profile, oldest.Local().Format("02/01/2006 15:04"), newest.Local().Format("02/01/2006 15:04"), len(p.Keep))
}

func (c *vaulticRetentionController) confirmApply() {
	p := c.preview
	if p == nil || len(p.Remove) == 0 {
		dialog.ShowInformation("Aperçu requis", "Lancez d'abord « Prévisualiser » : seuls les instantanés listés peuvent être supprimés.", c.hooks.Window)
		return
	}
	policy, err := c.policy()
	if err != nil || policy != p.Policy || c.hooks.Profile().Name != p.Profile || c.hooks.Repository() != p.Repository {
		c.invalidate()
		dialog.ShowInformation("Aperçu périmé", "La politique, le profil ou le dépôt a changé : relancez l'aperçu.", c.hooks.Window)
		return
	}
	reviewed := *p
	dialog.ShowConfirm("Supprimer les instantanés", vaulticRetentionConfirmText(reviewed), func(ok bool) {
		if ok {
			c.apply(reviewed)
		}
	}, c.hooks.Window)
}

func (c *vaulticRetentionController) apply(reviewed vaulticRetentionPreview) {
	profile, host, err := c.context()
	if err != nil {
		dialog.ShowError(err, c.hooks.Window)
		return
	}
	started := c.hooks.Now()
	var deleted int
	c.hooks.Run("Suppression des instantanés", func(ctx context.Context, g global.Options) error {
		sources, err := vaulticSourcesForBackup(g.Repo, profile.Sources)
		if err != nil {
			return err
		}
		current, err := vaulticListSnapshots(ctx, g)
		if err != nil {
			return err
		}
		ids, err := vaulticConfirmRetention(reviewed, current, host, sources, c.hooks.Now())
		if err != nil {
			return err
		}
		if err := vaulticForgetSnapshots(ctx, g, ids); err != nil {
			return err
		}
		deleted = len(ids)
		return nil
	}, func() error {
		summary := fmt.Sprintf("%d instantané(s) supprimé(s) selon la politique. %s", deleted, reviewed.Policy.Describe())
		c.record(reviewed, summary, started)
		c.hooks.Log("Conservation du profil « " + reviewed.Profile + " » : " + summary + " L'espace disque sera rendu après « Libérer l'espace ».")
		c.invalidate()
		return nil
	})
}

func (c *vaulticRetentionController) confirmPrune() {
	dialog.ShowConfirm("Libérer l'espace disque",
		"Vaultic va réorganiser le dépôt pour rendre l'espace des données qu'aucun instantané n'utilise plus.\n\n"+
			"Aucun instantané existant n'est supprimé par cette étape, mais elle peut durer longtemps et le dépôt est verrouillé pendant ce temps. Continuer ?",
		func(ok bool) {
			if ok {
				c.prune()
			}
		}, c.hooks.Window)
}

func (c *vaulticRetentionController) prune() {
	profile := c.hooks.Profile()
	started := c.hooks.Now()
	c.hooks.Run("Libération de l'espace", vaulticPruneRepository, func() error {
		c.record(vaulticRetentionPreview{Profile: profile.Name, Repository: c.hooks.Repository()}, "Espace disque libéré (prune).", started)
		c.hooks.Log("Espace disque libéré (prune) pour le dépôt actif.")
		return nil
	})
}

// vaulticRetentionRecord is the history entry of a deletion or a prune.
func vaulticRetentionRecord(profile, repository, summary string, started, ended time.Time) vaulticRunRecord {
	return vaulticRunRecord{
		ID:      fmt.Sprintf("%d-retention", ended.UnixMilli()),
		Profile: profile, Repository: vaulticSafeLocation(repository),
		Trigger: vaulticTriggerRetention, Status: vaulticStatusSuccess,
		Started: started, Ended: ended, DurationMs: ended.Sub(started).Milliseconds(),
		Summary: vaulticSanitizeSummary(summary, nil),
	}
}

func (c *vaulticRetentionController) record(p vaulticRetentionPreview, summary string, started time.Time) {
	if c.hooks.History == nil {
		return
	}
	if err := c.hooks.History.Append(vaulticRetentionRecord(p.Profile, p.Repository, summary, started, c.hooks.Now())); err != nil {
		c.hooks.Log("Historique : " + err.Error())
	}
}
