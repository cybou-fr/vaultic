package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/restic/restic/internal/global"
	"github.com/restic/restic/internal/repository"
	"github.com/restic/restic/internal/restic"
	"github.com/restic/restic/internal/ui"
)

// vaulticFindSavedDepot returns the saved repository at a location. Local
// paths are compared after resolution, so the same folder written two ways is
// one repository.
func vaulticFindSavedDepot(depots []vaulticDepot, location string) (vaulticDepot, bool) {
	for _, depot := range depots {
		if depot.Location == location || vaulticResolvedRepository(depot.Location) == vaulticResolvedRepository(location) {
			return depot, true
		}
	}
	return vaulticDepot{}, false
}

// vaulticNormalizeAddress stores local paths in their portable form and
// leaves remote addresses untouched.
func vaulticNormalizeAddress(address string) string {
	if vaulticLocalRepository(address) {
		localPath := strings.TrimPrefix(address, "local:")
		if filepath.IsAbs(localPath) {
			return vaulticPortablePath(localPath)
		}
		return filepath.Clean(localPath)
	}
	return address
}

// vaulticSupportsSessionAuth reports whether a backend takes credentials that
// are typed for the session (and kept in memory only).
func vaulticSupportsSessionAuth(address string) bool {
	info, ok := vaulticBackendForAddress(address)
	if !ok {
		return false
	}
	switch info.Scheme {
	case "s3", "b2", "azure", "rest", "gs", "swift":
		return true
	}
	return false
}

// vaulticRepositoriesHooks connects the repository page to the application.
type vaulticRepositoriesHooks struct {
	Window fyne.Window
	// Persist saves depots and the active repository with the other settings.
	Persist       func(depots []vaulticDepot, active string) error
	Busy          func() bool
	Log           func(string)
	Status        func(string)
	UpdateActions func()
	// Run runs an engine operation through the single gate.
	Run func(title, repo, password string, opts vaulticRunOptions, fn func(context.Context, global.Options) error, finish func() error)
	// CancelRun stops the running operation.
	CancelRun func()
	// Activated runs after another repository became active, so views that
	// show its data can reset.
	Activated func()
}

// vaulticRepositoriesController owns the saved repositories, the active one,
// its session password and credentials, and the controls of the page.
type vaulticRepositoriesController struct {
	hooks    vaulticRepositoriesHooks
	settings *vaulticSettings
	active   string
	editing  string // location being edited, "" when adding

	Sessions *vaulticCloudSessions
	Password *widget.Entry
	Value    *widget.Label
	ListBox  *fyne.Container
	Setup    *vaulticStorageSetupUI

	Verify, EditAuth, ClearAuth *widget.Button

	rowButtons []*widget.Button
	repoHooks  vaulticRepositoryHooks
}

func newVaulticRepositoriesController(settings *vaulticSettings, hooks vaulticRepositoriesHooks) *vaulticRepositoriesController {
	c := &vaulticRepositoriesController{hooks: hooks, settings: settings, active: settings.Repository, Sessions: &vaulticCloudSessions{}}
	c.Value = widget.NewLabel("Aucun dépôt configuré")
	c.Value.Wrapping = fyne.TextWrapWord
	c.updateLabel()
	c.Password = widget.NewPasswordEntry()
	c.Password.SetPlaceHolder("Mot de passe du dépôt — jamais enregistré")
	c.ListBox = container.NewVBox()

	c.Verify = widget.NewButton("Vérifier la connexion", func() { c.Test(c.active, c.Password.Text, false) })
	c.EditAuth = widget.NewButtonWithIcon("Configurer l'accès du dépôt…", theme.SettingsIcon(), c.editSessionAuth)
	c.ClearAuth = widget.NewButtonWithIcon("Effacer les identifiants de la session", theme.DeleteIcon(), func() {
		if c.active == "" || hooks.Busy() {
			return
		}
		c.Sessions.forget(c.active)
		hooks.UpdateActions()
		dialog.ShowInformation("Identifiants effacés", "Les identifiants en mémoire ont été effacés pour ce dépôt. Les données restic et la configuration système ne sont pas modifiées.", hooks.Window)
	})
	c.repoHooks = vaulticRepositoryHooks{Connect: c.connectSaved, Edit: c.edit, Test: c.testSaved, Forget: c.forget}
	c.Setup = vaulticNewStorageSetupUI(vaulticStorageSetupHooks{
		Window: hooks.Window,
		Browse: func(set func(string)) {
			dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
				if err != nil {
					dialog.ShowError(err, hooks.Window)
					return
				}
				if uri != nil {
					set(uri.Path())
				}
			}, hooks.Window).Show()
		},
		Submit:     c.submit,
		Cancel:     hooks.CancelRun,
		Wizard:     c.showCloudWizard,
		CancelEdit: func() { c.editing = ""; c.Setup.Reset() },
	})
	c.Setup.SetBusy(hooks.Busy())
	return c
}

// Active is the active repository address ("" when none).
func (c *vaulticRepositoriesController) Active() string { return c.active }

// Depots returns the saved repositories.
func (c *vaulticRepositoriesController) Depots() []vaulticDepot { return c.settings.Depots }

// Buttons lists the controls disabled while an operation runs.
func (c *vaulticRepositoriesController) Buttons() []*widget.Button {
	return []*widget.Button{c.Verify, c.EditAuth, c.ClearAuth}
}

func (c *vaulticRepositoriesController) updateLabel() {
	if value := strings.TrimSpace(c.active); value != "" {
		c.Value.SetText(value)
	} else {
		c.Value.SetText("Aucun dépôt configuré")
	}
}

// SetBusy follows the running state of the application.
func (c *vaulticRepositoriesController) SetBusy(on bool) {
	if on {
		c.Password.Disable()
	} else {
		c.Password.Enable()
	}
	c.Setup.SetBusy(on)
}

// UpdateState enables the controls for the active repository and state.
func (c *vaulticRepositoriesController) UpdateState(busy bool) {
	vaulticSetButtonEnabled(c.Verify, !busy && c.active != "")
	vaulticSetButtonEnabled(c.EditAuth, !busy && vaulticSupportsSessionAuth(c.active))
	vaulticSetButtonEnabled(c.ClearAuth, !busy && c.active != "" && c.Sessions.get(c.active).hasSecret())
	for _, b := range c.rowButtons {
		vaulticSetButtonEnabled(b, !busy)
	}
}

// Wipe forgets every secret kept for the session (password, credentials).
func (c *vaulticRepositoriesController) Wipe() {
	c.Password.SetText("")
	c.Setup.pass.SetText("")
	c.Sessions.clearAll()
}

// Activate makes address the active repository with the given password.
func (c *vaulticRepositoriesController) Activate(address, password string) {
	c.active = address
	c.Password.SetText(password)
	c.updateLabel()
	if c.hooks.Activated != nil {
		c.hooks.Activated()
	}
}

// Refresh redraws the list of saved repositories.
func (c *vaulticRepositoriesController) Refresh() {
	c.rowButtons = nil
	rows := make([]fyne.CanvasObject, 0, len(c.settings.Depots))
	for _, depot := range c.settings.Depots {
		row, buttons := vaulticRepositoryRow(depot, depot.Location == c.active, c.repoHooks)
		rows = append(rows, row)
		c.rowButtons = append(c.rowButtons, buttons...)
	}
	if len(rows) == 0 {
		rows = append(rows, vaulticWrappedLabel("Aucun dépôt enregistré. Ajoutez-en un ci-dessous."))
	}
	c.ListBox.Objects = rows
	c.ListBox.Refresh()
	c.hooks.UpdateActions()
}

func (c *vaulticRepositoriesController) savedDepot(location string) (vaulticDepot, bool) {
	return vaulticFindSavedDepot(c.settings.Depots, location)
}

// recordProbe stores the outcome of a connection test (UI thread only).
func (c *vaulticRepositoriesController) recordProbe(location string, res vaulticProbeResult) {
	depot, ok := c.savedDepot(location)
	if !ok || res.State == vaulticConnNotTested {
		return
	}
	next := vaulticRecordVerification(c.settings.Depots, depot.Location, res.State, time.Now())
	if err := c.hooks.Persist(next, c.active); err != nil {
		c.hooks.Log("Erreur d'enregistrement des préférences : " + err.Error())
		return
	}
	c.settings.Depots = next
	c.Refresh()
}

// probe runs on the worker goroutine; the detail text is redacted because it
// may quote engine errors.
func (c *vaulticRepositoriesController) probe(ctx context.Context, g global.Options, location string) vaulticProbeResult {
	return vaulticProbeRedacted(ctx, g, vaulticProbeTimeout, c.Sessions.get(location), vaulticVerifyDepot)
}

// Test checks a repository without changing it. Without a password it can
// only prove reachability, never a successful connection.
func (c *vaulticRepositoriesController) Test(location, password string, fromForm bool) {
	if c.hooks.Busy() {
		return
	}
	if fromForm {
		c.Setup.SetState(vaulticProbeResult{State: vaulticConnTesting})
	}
	c.hooks.Run("Test de la connexion", location, password, vaulticRunOptions{AllowNoPassword: true, Inline: true}, func(ctx context.Context, g global.Options) error {
		res := c.probe(ctx, g, location)
		if _, known := c.savedDepot(location); !known {
			c.Sessions.forget(location)
		}
		fyne.Do(func() {
			if fromForm {
				c.Setup.SetState(res)
			}
			c.recordProbe(location, res)
		})
		switch res.State {
		case vaulticConnConnected, vaulticConnReachable:
			return nil
		case vaulticConnNotTested:
			return context.Canceled
		}
		return errors.New(res.State.Label() + " — " + res.Detail)
	}, nil)
}

func (c *vaulticRepositoriesController) connectNew(setup vaulticStorageSetup, create bool) {
	w := c.hooks.Window
	name, address := setup.Name, vaulticNormalizeAddress(setup.Address)
	if _, exists := c.savedDepot(address); exists {
		dialog.ShowInformation("Dépôt déjà enregistré", "Utilisez un nom et un emplacement différents, ou sélectionnez le dépôt enregistré.", w)
		return
	}
	for _, depot := range c.settings.Depots {
		if strings.EqualFold(depot.Name, name) {
			dialog.ShowInformation("Dépôt déjà enregistré", "Utilisez un nom et un emplacement différents, ou sélectionnez le dépôt enregistré.", w)
			return
		}
	}
	if setup.Auth.hasValues() {
		if err := c.Sessions.set(address, setup.Auth); err != nil {
			dialog.ShowError(err, w)
			return
		}
	}
	password := setup.Password
	title := "Connexion au dépôt"
	if create {
		title = "Création du dépôt"
	}
	c.Setup.SetState(vaulticProbeResult{State: vaulticConnTesting})
	var outcome vaulticProbeResult
	c.hooks.Run(title, address, password, vaulticRunOptions{Create: create, Inline: true}, func(ctx context.Context, g global.Options) error {
		var err error
		if create {
			createCtx, stop := context.WithTimeout(ctx, 2*vaulticProbeTimeout)
			defer stop()
			printer := ui.NewProgressPrinter(g.JSON, g.Verbosity, g.Term)
			var repo *repository.Repository
			repo, err = global.CreateRepository(createCtx, g, restic.StableRepoVersion, nil, printer)
			if err == nil {
				err = repo.Close()
			}
			if ctx.Err() == nil && createCtx.Err() != nil {
				err = createCtx.Err()
			}
			outcome = vaulticClassifyProbe(err, true)
			if err == nil {
				outcome.Detail = "Nouveau dépôt créé et ouvert."
			}
		} else {
			outcome = c.probe(ctx, g, address)
			switch outcome.State {
			case vaulticConnConnected:
			case vaulticConnNotTested:
				err = context.Canceled
			default:
				err = errors.New(outcome.State.Label() + " — " + outcome.Detail)
			}
		}
		if err != nil {
			c.Sessions.forget(address)
		}
		outcome.Detail = vaulticRedactCloudSecrets(vaulticRedactOperation(password), setup.Auth)(outcome.Detail)
		final := outcome
		fyne.Do(func() { c.Setup.SetState(final) })
		return err
	}, func() error {
		depots := append(append([]vaulticDepot(nil), c.settings.Depots...), vaulticDepot{
			Name: name, Location: address,
			LastVerified: time.Now().UTC().Format(time.RFC3339), LastVerifiedState: vaulticConnConnected.Key(),
		})
		if err := c.hooks.Persist(depots, address); err != nil {
			return err
		}
		c.settings.Depots = depots
		c.Activate(address, password)
		c.Refresh()
		final := outcome
		c.Setup.Reset()
		c.Setup.SetState(final)
		return nil
	})
}

func (c *vaulticRepositoriesController) saveEdited(setup vaulticStorageSetup) {
	w := c.hooks.Window
	old, address := c.editing, vaulticNormalizeAddress(setup.Address)
	updated, err := vaulticEditSavedDepot(c.settings.Depots, old, setup.Name, address)
	if err != nil {
		dialog.ShowError(err, w)
		return
	}
	active := c.active
	if active == old {
		active = address
	}
	if err := c.hooks.Persist(updated, active); err != nil {
		dialog.ShowError(err, w)
		return
	}
	c.settings.Depots = updated
	if old != address {
		c.Sessions.forget(old)
		if setup.Auth.hasValues() {
			if err := c.Sessions.set(address, setup.Auth); err != nil {
				dialog.ShowError(err, w)
			}
		}
		if c.active == old {
			c.Activate(address, "")
		}
	}
	c.editing = ""
	c.Setup.Reset()
	c.Refresh()
	c.hooks.Log("Dépôt modifié dans Vaultic : " + setup.Name + " (aucune donnée distante modifiée).")
}

func (c *vaulticRepositoriesController) submit(setup vaulticStorageSetup, action string) {
	if c.hooks.Busy() {
		return
	}
	switch action {
	case vaulticActionTest:
		address := vaulticNormalizeAddress(setup.Address)
		if setup.Auth.hasValues() {
			if err := c.Sessions.set(address, setup.Auth); err != nil {
				dialog.ShowError(err, c.hooks.Window)
				return
			}
		}
		c.Test(address, setup.Password, true)
	case vaulticActionSave:
		c.saveEdited(setup)
	default:
		c.connectNew(setup, action == vaulticActionCreate)
	}
}

func (c *vaulticRepositoriesController) showCloudWizard(scheme string) {
	if c.hooks.Busy() {
		return
	}
	vaulticShowCloudWizard(c.hooks.Window, scheme, func(setup vaulticCloudSetup, create bool) error {
		if c.hooks.Busy() {
			return errors.New("Une opération est déjà en cours.")
		}
		if _, exists := c.savedDepot(setup.Address); exists {
			return errors.New("Ce nom ou emplacement est déjà enregistré dans Vaultic.")
		}
		c.connectNew(vaulticStorageSetup{Name: setup.Name, Address: setup.Address, Password: setup.Password, Auth: setup.Auth}, create)
		return nil
	})
}

func (c *vaulticRepositoriesController) editSessionAuth() {
	w := c.hooks.Window
	if c.hooks.Busy() || c.active == "" {
		return
	}
	info, ok := vaulticBackendForAddress(c.active)
	if !ok {
		return
	}
	vaulticShowCloudSessionWizard(w, info.Scheme, func(auth vaulticCloudAuth) error {
		if c.hooks.Busy() {
			return errors.New("Une opération est déjà en cours.")
		}
		if err := c.Sessions.set(c.active, auth); err != nil {
			return err
		}
		c.hooks.UpdateActions()
		if c.Password.Text != "" {
			c.Test(c.active, c.Password.Text, false)
		} else {
			dialog.ShowInformation("Mot de passe requis", "Identifiants configurés pour cette session. Saisissez le mot de passe restic du dépôt puis utilisez « Vérifier la connexion ».", w)
		}
		return nil
	})
}

func (c *vaulticRepositoriesController) connectSaved(depot vaulticDepot) {
	if c.hooks.Busy() {
		return
	}
	if depot.Location == c.active && c.Password.Text != "" {
		c.Test(depot.Location, c.Password.Text, false)
		return
	}
	if depot.Location != c.active {
		if err := c.hooks.Persist(c.settings.Depots, depot.Location); err != nil {
			dialog.ShowError(err, c.hooks.Window)
			return
		}
		c.Activate(depot.Location, "")
	}
	c.Refresh()
	c.hooks.Status("Dépôt « " + depot.Name + " » sélectionné — saisissez son mot de passe puis vérifiez la connexion.")
	c.hooks.Window.Canvas().Focus(c.Password)
}

func (c *vaulticRepositoriesController) edit(depot vaulticDepot) {
	if c.hooks.Busy() {
		return
	}
	if err := c.Setup.Edit(depot.Name, depot.Location); err != nil {
		dialog.ShowError(err, c.hooks.Window)
		return
	}
	c.editing = depot.Location
}

func (c *vaulticRepositoriesController) testSaved(depot vaulticDepot) {
	if c.hooks.Busy() {
		return
	}
	password := ""
	if depot.Location == c.active {
		password = c.Password.Text
	}
	c.Test(depot.Location, password, false)
}

func (c *vaulticRepositoriesController) forget(depot vaulticDepot) {
	w := c.hooks.Window
	location, name := depot.Location, depot.Name
	dialog.ShowConfirm("Oublier le dépôt",
		"Retirer « "+name+" » de la liste Vaultic ?\n\nAucun fichier de sauvegarde ni instantané ne sera supprimé, ni localement ni à distance. Vous pourrez reconnecter le dépôt plus tard.",
		func(ok bool) {
			if !ok || c.hooks.Busy() {
				return
			}
			next, active, err := vaulticForgetSavedDepot(c.settings.Depots, c.active, location)
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if err := c.hooks.Persist(next, active); err != nil {
				dialog.ShowError(err, w)
				return
			}
			c.settings.Depots = next
			c.Sessions.forget(location)
			if c.active == location {
				c.Activate(active, "")
			}
			if c.editing == location {
				c.editing = ""
				c.Setup.Reset()
			}
			c.Refresh()
			c.hooks.Log("Dépôt retiré de la liste Vaultic : " + name + " (sauvegardes non supprimées).")
		}, w)
}
