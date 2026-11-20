package main

import (
	"fmt"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ---------------------------------------------------------------------------
// Backend icons: simple line icons, recoloured through the Fyne theme.

// vaulticLineIconSVG draws the icon with a fixed stroke colour (Fyne only
// recolours fills, so the two tints are baked into separate resources).
func vaulticLineIconSVG(paths ...string) []string {
	build := func(stroke string) string {
		var b strings.Builder
		fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="%s" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">`, stroke)
		for _, p := range paths {
			fmt.Fprintf(&b, `<path fill="none" d="%s"/>`, p)
		}
		b.WriteString(`</svg>`)
		return b.String()
	}
	return []string{build("#EFF6FD"), build("#51E3C1")}
}

const vaulticCloudPath = "M7 18a4 4 0 0 1-.6-7.96A6 6 0 0 1 18 9.6 4.2 4.2 0 0 1 17.5 18z"

var vaulticBackendIcons = map[string][]string{
	"local":  vaulticLineIconSVG("M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"),
	"sftp":   vaulticLineIconSVG("M4 5h16a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1z", "M7 10l3 2-3 2", "M12 15h5"),
	"rest":   vaulticLineIconSVG("M4 4h16v6H4z", "M4 14h16v6H4z", "M7.5 7h.01", "M7.5 17h.01", "M11 7h6", "M11 17h6"),
	"s3":     vaulticLineIconSVG("M5 6c0-1.7 3.1-3 7-3s7 1.3 7 3-3.1 3-7 3-7-1.3-7-3z", "M5 6l1.5 13c.1 1 2.6 2 5.5 2s5.4-1 5.5-2L19 6"),
	"b2":     vaulticLineIconSVG("M12 3c1 4 5 5.5 5 10a5 5 0 0 1-10 0c0-2 1-3 2-4 0 2 1 3 2 3 0-3-1-6 1-9z"),
	"azure":  vaulticLineIconSVG(vaulticCloudPath),
	"gs":     vaulticLineIconSVG(vaulticCloudPath, "M12 16v-4.5", "M9.8 13.5L12 11.3l2.2 2.2"),
	"swift":  vaulticLineIconSVG("M12 3l9 5-9 5-9-5z", "M3 12.5l9 5 9-5", "M3 16.5l9 5 9-5"),
	"rclone": vaulticLineIconSVG("M4.5 12A7.5 7.5 0 0 1 18 7.6L19.5 9", "M19.5 4.5V9H15", "M19.5 12A7.5 7.5 0 0 1 6 16.4L4.5 15", "M4.5 19.5V15H9"),
}

var vaulticBackendTagline = map[string]string{
	"local": "Disque, USB, NAS", "sftp": "Serveur SSH", "rest": "Serveur restic", "s3": "AWS, MinIO…",
	"b2": "Backblaze", "azure": "Microsoft", "gs": "Cloud Storage", "swift": "OpenStack", "rclone": "Autres fournisseurs",
}

var vaulticBackendTitle = map[string]string{
	"local": "Local", "sftp": "SFTP", "rest": "REST", "s3": "S3", "b2": "Backblaze B2",
	"azure": "Azure Blob", "gs": "Google Cloud", "swift": "OpenStack Swift", "rclone": "rclone",
}

func vaulticBackendIcon(scheme string, accent bool) fyne.Resource {
	index := 0
	if accent {
		index = 1
	}
	return fyne.NewStaticResource(fmt.Sprintf("vaultic-%s-%d.svg", scheme, index), []byte(vaulticBackendIcons[scheme][index]))
}

// ---------------------------------------------------------------------------
// Backend card: tappable, focusable (Tab), keyboard operable (Enter / Space /
// arrows) with a clear selected state.

type vaulticBackendCard struct {
	widget.BaseWidget
	info       vaulticBackendInfo
	selected   bool
	focused    bool
	hovered    bool
	onSelect   func()
	onNavigate func(fyne.KeyName)
}

var (
	_ fyne.Focusable     = (*vaulticBackendCard)(nil)
	_ fyne.Tappable      = (*vaulticBackendCard)(nil)
	_ desktop.Hoverable  = (*vaulticBackendCard)(nil)
	_ desktop.Cursorable = (*vaulticBackendCard)(nil)
)

func newVaulticBackendCard(info vaulticBackendInfo, onSelect func(), onNavigate func(fyne.KeyName)) *vaulticBackendCard {
	c := &vaulticBackendCard{info: info, onSelect: onSelect, onNavigate: onNavigate}
	c.ExtendBaseWidget(c)
	return c
}

func (c *vaulticBackendCard) setSelected(on bool) { c.selected = on; c.Refresh() }
func (c *vaulticBackendCard) Tapped(*fyne.PointEvent) {
	if c.onSelect != nil {
		c.onSelect()
	}
}
func (c *vaulticBackendCard) FocusGained()                   { c.focused = true; c.Refresh() }
func (c *vaulticBackendCard) FocusLost()                     { c.focused = false; c.Refresh() }
func (c *vaulticBackendCard) TypedRune(rune)                 {}
func (c *vaulticBackendCard) MouseIn(*desktop.MouseEvent)    { c.hovered = true; c.Refresh() }
func (c *vaulticBackendCard) MouseMoved(*desktop.MouseEvent) {}
func (c *vaulticBackendCard) MouseOut()                      { c.hovered = false; c.Refresh() }
func (c *vaulticBackendCard) Cursor() desktop.Cursor         { return desktop.PointerCursor }
func (c *vaulticBackendCard) TypedKey(e *fyne.KeyEvent) {
	switch e.Name {
	case fyne.KeyReturn, fyne.KeyEnter, fyne.KeySpace:
		c.Tapped(nil)
	case fyne.KeyLeft, fyne.KeyRight, fyne.KeyUp, fyne.KeyDown, fyne.KeyHome, fyne.KeyEnd:
		if c.onNavigate != nil {
			c.onNavigate(e.Name)
		}
	}
}

func (c *vaulticBackendCard) CreateRenderer() fyne.WidgetRenderer {
	r := &vaulticBackendCardRenderer{card: c}
	r.bg = canvas.NewRectangle(vaulticPanel)
	r.bg.CornerRadius = 12
	r.icon = canvas.NewImageFromResource(vaulticBackendIcon(c.info.Scheme, false))
	r.icon.FillMode = canvas.ImageFillContain
	r.title = canvas.NewText(vaulticBackendTitle[c.info.Scheme], vaulticText)
	r.title.TextSize = 14
	r.title.TextStyle = fyne.TextStyle{Bold: true}
	r.tagline = canvas.NewText(vaulticBackendTagline[c.info.Scheme], vaulticMuted)
	r.tagline.TextSize = 11
	return r
}

type vaulticBackendCardRenderer struct {
	card           *vaulticBackendCard
	bg             *canvas.Rectangle
	icon           *canvas.Image
	title, tagline *canvas.Text
}

func (r *vaulticBackendCardRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	const pad, iconSize = 12, 28
	r.icon.Move(fyne.NewPos(pad, (size.Height-iconSize)/2))
	r.icon.Resize(fyne.NewSize(iconSize, iconSize))
	x := float32(pad*2 + iconSize)
	th, gh := r.title.MinSize().Height, r.tagline.MinSize().Height
	top := (size.Height - th - gh) / 2
	r.title.Move(fyne.NewPos(x, top))
	r.title.Resize(fyne.NewSize(size.Width-x-pad, th))
	r.tagline.Move(fyne.NewPos(x, top+th))
	r.tagline.Resize(fyne.NewSize(size.Width-x-pad, gh))
}
func (r *vaulticBackendCardRenderer) MinSize() fyne.Size { return fyne.NewSize(172, 60) }
func (r *vaulticBackendCardRenderer) Refresh() {
	c := r.card
	r.bg.FillColor, r.bg.StrokeColor, r.bg.StrokeWidth = vaulticPanel, vaulticStroke, 1
	accentIcon := false
	switch {
	case c.selected:
		r.bg.FillColor = color.NRGBA{R: 81, G: 227, B: 193, A: 38}
		r.bg.StrokeColor, r.bg.StrokeWidth = vaulticAccent, 2
		accentIcon = true
	case c.hovered:
		r.bg.FillColor = vaulticPanelRaised
	}
	if c.focused {
		r.bg.StrokeColor, r.bg.StrokeWidth = vaulticText, 2 // visible keyboard focus ring
	}
	r.icon.Resource = vaulticBackendIcon(c.info.Scheme, accentIcon)
	r.title.Color = vaulticText
	if c.selected {
		r.title.Color = vaulticAccent
	}
	r.bg.Refresh()
	r.icon.Refresh()
	r.title.Refresh()
	r.tagline.Refresh()
}
func (r *vaulticBackendCardRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.icon, r.title, r.tagline}
}
func (r *vaulticBackendCardRenderer) Destroy() {}

// ---------------------------------------------------------------------------
// Storage setup screen.

const (
	vaulticActionTest   = "test"
	vaulticActionOpen   = "open"
	vaulticActionCreate = "create"
	vaulticActionSave   = "save"
)

type vaulticStorageSetupHooks struct {
	Window fyne.Window
	// Browse lets the user pick a local folder; set stores the chosen path.
	Browse func(set func(string))
	// Submit runs an action. For "test" the password may be empty.
	Submit func(setup vaulticStorageSetup, action string)
	// Cancel stops a running test or connection attempt.
	Cancel func()
	// Wizard opens the step-by-step cloud dialog (S3, B2, Azure).
	Wizard func(scheme string)
	// CancelEdit leaves edit mode.
	CancelEdit func()
}

type vaulticStorageSetupUI struct {
	Content fyne.CanvasObject

	hooks   vaulticStorageSetupHooks
	scheme  string
	cards   []*vaulticBackendCard
	cardBox *fyne.Container

	title       *canvas.Text
	description *widget.Label
	fieldBox    *fyne.Container
	entries     map[string]fyne.CanvasObject
	name, pass  *widget.Entry
	preview     *widget.Label
	problem     *widget.Label
	badge       *canvas.Text
	detail      *widget.Label
	busyBar     *widget.ProgressBarInfinite
	wizardBtn   *widget.Button

	testBtn, openBtn, createBtn, saveBtn, cancelBtn, cancelEditBtn *widget.Button
	editing                                                        bool
	busy                                                           bool
}

func vaulticNewStorageSetupUI(h vaulticStorageSetupHooks) *vaulticStorageSetupUI {
	u := &vaulticStorageSetupUI{hooks: h, entries: map[string]fyne.CanvasObject{}}
	u.title = vaulticHeading("Ajouter un dépôt", 20)
	u.description = vaulticWrappedLabel("")
	u.fieldBox = container.NewVBox()
	u.name = widget.NewEntry()
	u.name.SetPlaceHolder("Ex. : Disque externe")
	u.name.Validator = func(s string) error { _, err := vaulticValidateSavedName(s); return err }
	u.name.OnChanged = func(string) { u.refresh() }
	u.pass = widget.NewPasswordEntry()
	u.pass.SetPlaceHolder("Mot de passe de chiffrement restic — jamais enregistré")
	u.pass.OnChanged = func(string) { u.refresh() }
	u.preview = vaulticWrappedLabel("")
	u.problem = vaulticWrappedLabel("")
	u.problem.Importance = widget.DangerImportance
	u.badge = vaulticHeading("", 15)
	u.detail = vaulticWrappedLabel("")
	u.busyBar = widget.NewProgressBarInfinite()
	u.busyBar.Hide()

	u.cardBox = container.NewGridWrap(fyne.NewSize(176, 64))
	for _, info := range vaulticBackendCatalog {
		scheme := info.Scheme
		card := newVaulticBackendCard(info, func() { u.SetScheme(scheme) }, func(k fyne.KeyName) { u.navigate(scheme, k) })
		u.cards = append(u.cards, card)
		u.cardBox.Add(card)
	}

	u.wizardBtn = widget.NewButtonWithIcon("Assistant pas à pas…", theme.HelpIcon(), func() {
		if u.hooks.Wizard != nil {
			u.hooks.Wizard(u.scheme)
		}
	})
	u.wizardBtn.Importance = widget.LowImportance
	u.testBtn = widget.NewButtonWithIcon("Tester la connexion", theme.ViewRefreshIcon(), func() { u.submit(vaulticActionTest) })
	u.openBtn = widget.NewButtonWithIcon("Ouvrir le dépôt existant", theme.FolderOpenIcon(), func() { u.submit(vaulticActionOpen) })
	u.openBtn.Importance = widget.HighImportance
	u.createBtn = widget.NewButtonWithIcon("Créer un nouveau dépôt", theme.ContentAddIcon(), func() { u.submit(vaulticActionCreate) })
	u.saveBtn = widget.NewButtonWithIcon("Enregistrer les modifications", theme.DocumentSaveIcon(), func() { u.submit(vaulticActionSave) })
	u.saveBtn.Importance = widget.HighImportance
	u.cancelEditBtn = widget.NewButton("Annuler la modification", func() {
		if u.hooks.CancelEdit != nil {
			u.hooks.CancelEdit()
		}
	})
	u.cancelBtn = widget.NewButtonWithIcon("Annuler le test", theme.CancelIcon(), func() {
		if u.hooks.Cancel != nil {
			u.hooks.Cancel()
		}
	})
	u.cancelBtn.Hide()

	state := container.NewVBox(
		container.NewHBox(vaulticCaption("ÉTAT DE LA CONNEXION"), u.badge),
		u.detail, u.busyBar, u.cancelBtn,
	)
	u.Content = vaulticSurface(container.NewVBox(
		u.title,
		vaulticCaption("1 · Choisissez le type de stockage"),
		u.cardBox,
		u.description,
		container.NewHBox(u.wizardBtn),
		widget.NewSeparator(),
		vaulticCaption("2 · Renseignez l'emplacement"),
		u.fieldBox,
		vaulticCaption("3 · Nommez le dépôt et protégez-le"),
		widget.NewForm(widget.NewFormItem("Nom dans Vaultic", u.name), widget.NewFormItem("Mot de passe restic", u.pass)),
		u.preview, u.problem,
		widget.NewSeparator(),
		state,
		container.NewGridWrap(fyne.NewSize(215, 40), u.testBtn, u.openBtn, u.createBtn, u.saveBtn, u.cancelEditBtn),
		vaulticWrappedLabel("Les identifiants cloud restent en mémoire pour cette session. Le mot de passe restic n'est jamais enregistré."),
	))
	u.SetState(vaulticProbeResult{State: vaulticConnNotTested})
	u.SetScheme(vaulticBackendCatalog[0].Scheme)
	return u
}

func (u *vaulticStorageSetupUI) navigate(from string, key fyne.KeyName) {
	index := 0
	for i, c := range u.cards {
		if c.info.Scheme == from {
			index = i
		}
	}
	cols := max(1, int(u.cardBox.Size().Width/(176+theme.Padding())))
	switch key {
	case fyne.KeyLeft:
		index--
	case fyne.KeyRight:
		index++
	case fyne.KeyUp:
		index -= cols
	case fyne.KeyDown:
		index += cols
	case fyne.KeyHome:
		index = 0
	case fyne.KeyEnd:
		index = len(u.cards) - 1
	}
	if index < 0 || index >= len(u.cards) {
		return
	}
	if u.hooks.Window != nil {
		u.hooks.Window.Canvas().Focus(u.cards[index])
	}
}

// SetScheme switches the backend, rebuilding the dynamic form.
func (u *vaulticStorageSetupUI) SetScheme(scheme string) {
	info, ok := vaulticBackendByScheme(scheme)
	if !ok {
		return
	}
	u.scheme = scheme
	for _, c := range u.cards {
		c.setSelected(c.info.Scheme == scheme)
	}
	u.description.SetText(vaulticBackendDescription(info))
	if scheme == "s3" || scheme == "b2" || scheme == "azure" {
		u.wizardBtn.Show()
	} else {
		u.wizardBtn.Hide()
	}
	u.entries = map[string]fyne.CanvasObject{}
	u.fieldBox.Objects = nil
	items := []*widget.FormItem{}
	for _, f := range vaulticStorageFields(scheme) {
		field := f
		var control fyne.CanvasObject
		switch {
		case len(field.Choices) > 0:
			sel := widget.NewSelect(field.Choices, func(string) { u.refresh() })
			sel.SetSelected(field.Default)
			control = sel
		default:
			var e *widget.Entry
			if field.Secret {
				e = widget.NewPasswordEntry()
			} else {
				e = widget.NewEntry()
			}
			e.SetPlaceHolder(field.Placeholder)
			e.SetText(field.Default)
			e.Validator = func(s string) error { return vaulticValidateStorageField(scheme, field.Key, s) }
			e.OnChanged = func(string) { u.refresh() }
			control = e
			if field.Browse && u.hooks.Browse != nil {
				browse := widget.NewButtonWithIcon("Parcourir…", theme.FolderOpenIcon(), func() { u.hooks.Browse(func(p string) { e.SetText(p) }) })
				control = container.NewBorder(nil, nil, nil, browse, e)
			}
		}
		u.entries[field.Key] = control
		item := widget.NewFormItem(field.Label, control)
		item.HintText = field.Help
		items = append(items, item)
	}
	u.fieldBox.Add(widget.NewForm(items...))
	u.fieldBox.Refresh()
	u.refresh()
}

func (u *vaulticStorageSetupUI) control(key string) fyne.CanvasObject {
	c := u.entries[key]
	if b, ok := c.(*fyne.Container); ok && len(b.Objects) > 0 {
		return b.Objects[0]
	}
	return c
}

func (u *vaulticStorageSetupUI) setField(key, value string) {
	switch c := u.control(key).(type) {
	case *widget.Entry:
		c.SetText(value)
	case *widget.Select:
		c.SetSelected(value)
	}
}

func (u *vaulticStorageSetupUI) values() map[string]string {
	v := map[string]string{"name": u.name.Text, "password": u.pass.Text}
	for key := range u.entries {
		switch c := u.control(key).(type) {
		case *widget.Entry:
			v[key] = c.Text
		case *widget.Select:
			v[key] = c.Selected
		}
	}
	return v
}

// build validates the form. Without a password (test only) a placeholder lets
// the builder validate the location; the real value is restored afterwards.
func (u *vaulticStorageSetupUI) build(action string) (vaulticStorageSetup, error) {
	v := u.values()
	realPassword := v["password"]
	if action == vaulticActionTest || action == vaulticActionSave {
		v["password"] = "placeholder"
		if strings.TrimSpace(v["name"]) == "" {
			v["name"] = "Test"
		}
	}
	setup, err := vaulticBuildStorageSetup(u.scheme, v)
	if err == nil {
		setup.Password = realPassword
	}
	return setup, err
}

func (u *vaulticStorageSetupUI) refresh() {
	if u.preview == nil || u.problem == nil {
		return
	}
	setup, err := u.build(vaulticActionTest)
	if err != nil {
		u.preview.SetText("")
		u.problem.SetText(err.Error())
	} else {
		u.preview.SetText("Adresse : " + vaulticSafeLocation(setup.Address))
		u.problem.SetText("")
	}
	u.updateButtons()
}

func (u *vaulticStorageSetupUI) updateButtons() {
	set := func(b *widget.Button, enabled bool) {
		if enabled {
			b.Enable()
		} else {
			b.Disable()
		}
	}
	_, locationErr := u.build(vaulticActionTest)
	ready := !u.busy && locationErr == nil
	set(u.testBtn, ready)
	set(u.openBtn, ready && u.pass.Text != "")
	set(u.createBtn, ready && u.pass.Text != "")
	set(u.saveBtn, ready)
	if u.editing {
		u.openBtn.Hide()
		u.createBtn.Hide()
		u.saveBtn.Show()
		u.cancelEditBtn.Show()
	} else {
		u.openBtn.Show()
		u.createBtn.Show()
		u.saveBtn.Hide()
		u.cancelEditBtn.Hide()
	}
}

func (u *vaulticStorageSetupUI) submit(action string) {
	setup, err := u.build(action)
	if err != nil {
		u.problem.SetText(err.Error())
		return
	}
	if u.hooks.Submit != nil {
		u.hooks.Submit(setup, action)
	}
}

// SetState shows the connection lifecycle state. It must run on the UI thread.
func (u *vaulticStorageSetupUI) SetState(r vaulticProbeResult) {
	u.badge.Text = r.State.Label()
	switch r.State {
	case vaulticConnConnected, vaulticConnReachable:
		u.badge.Color = vaulticAccent
	case vaulticConnAuthFailed:
		u.badge.Color = vaulticWarning
	case vaulticConnUnavailable:
		u.badge.Color = color.NRGBA{R: 255, G: 117, B: 136, A: 255}
	default:
		u.badge.Color = vaulticMuted
	}
	u.badge.Refresh()
	switch r.State {
	case vaulticConnNotTested:
		if r.Detail == "" {
			r.Detail = "Aucun test effectué. Tester la connexion ne modifie jamais le dépôt."
		}
	case vaulticConnTesting:
		r.Detail = "Vérification en cours — vous pouvez annuler à tout moment."
	}
	u.detail.SetText(r.Detail)
}

// SetBusy disables the form while an operation runs.
func (u *vaulticStorageSetupUI) SetBusy(on bool) {
	u.busy = on
	if on {
		u.busyBar.Show()
		u.busyBar.Start()
		u.cancelBtn.Show()
	} else {
		u.busyBar.Stop()
		u.busyBar.Hide()
		u.cancelBtn.Hide()
	}
	u.updateButtons()
}

// Reset clears the form and returns to the creation mode.
func (u *vaulticStorageSetupUI) Reset() {
	u.editing = false
	u.title.Text = "Ajouter un dépôt"
	u.title.Refresh()
	u.name.SetText("")
	u.pass.SetText("")
	u.SetScheme(u.scheme)
	u.SetState(vaulticProbeResult{State: vaulticConnNotTested})
}

// Edit loads an existing repository for modification of its location and name.
func (u *vaulticStorageSetupUI) Edit(name, address string) error {
	scheme, values, err := vaulticParseStorageAddress(address)
	if err != nil {
		return err
	}
	u.editing = true
	u.title.Text = "Modifier le dépôt"
	u.title.Refresh()
	u.SetScheme(scheme)
	for key, value := range values {
		u.setField(key, value)
	}
	u.name.SetText(name)
	u.pass.SetText("")
	u.SetState(vaulticProbeResult{State: vaulticConnNotTested})
	u.refresh()
	return nil
}
