package main

// Vaultic is built in the official restic source tree. All backup operations
// call restic's Go functions directly; no restic executable or shell is launched.
import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "regexp"
    "sort"
    "strings"
    "sync/atomic"
    "time"

    "fyne.io/fyne/v2"
    "fyne.io/fyne/v2/app"
    "fyne.io/fyne/v2/container"
    "fyne.io/fyne/v2/dialog"
    "fyne.io/fyne/v2/canvas"
    "fyne.io/fyne/v2/layout"
    "fyne.io/fyne/v2/theme"
    "fyne.io/fyne/v2/widget"

    "github.com/restic/restic/internal/backend/all"
    "github.com/restic/restic/internal/data"
    "github.com/restic/restic/internal/global"
    "github.com/restic/restic/internal/repository"
    "github.com/restic/restic/internal/restic"
    "github.com/restic/restic/internal/ui"
)

type vaulticSnapshot struct {
    ID string `json:"id"`
    ShortID string `json:"short_id"`
    Hostname string `json:"hostname"`
    Time time.Time `json:"time"`
    Paths []string `json:"paths"`
}

type vaulticTerminal struct {
    line func(string)
    output io.Writer
}

var _ ui.Terminal = (*vaulticTerminal)(nil)

func (t *vaulticTerminal) Print(s string) { if t.line != nil { t.line(strings.TrimSpace(s)) } }
func (t *vaulticTerminal) Error(s string) { if t.line != nil { t.line(strings.TrimSpace(s)) } }
func (t *vaulticTerminal) SetStatus(lines []string) {}
func (t *vaulticTerminal) CanUpdateStatus() bool { return false }
func (t *vaulticTerminal) InputRaw() io.ReadCloser { return io.NopCloser(strings.NewReader("")) }
func (t *vaulticTerminal) InputIsTerminal() bool { return false }
func (t *vaulticTerminal) ReadPassword(ctx context.Context, prompt string) (string, error) {
    return "", errors.New("Le mot de passe doit être fourni dans l'interface Vaultic")
}
func (t *vaulticTerminal) OutputWriter() io.Writer { if t.output != nil { return t.output }; return io.Discard }
func (t *vaulticTerminal) OutputRaw() io.Writer { return t.OutputWriter() }
func (t *vaulticTerminal) OutputIsTerminal() bool { return false }

func vaulticLocalRepository(repo string) bool {
    if strings.HasPrefix(repo, "local:") { return true }
    if strings.Contains(repo, "://") { return false }
    if len(repo) >= 2 && repo[1] == ':' {
        return (repo[0] >= 'A' && repo[0] <= 'Z') || (repo[0] >= 'a' && repo[0] <= 'z')
    }
    return !strings.Contains(repo, ":")
}

// Resolve relative local repositories against the application executable.
// This keeps a portable SSD installation stable when its drive letter changes.
func vaulticResolvedRepository(repo string) string {
    repo = strings.TrimSpace(repo)
    repo = strings.TrimPrefix(repo, "local:")
    if repo == "" || !vaulticLocalRepository(repo) || filepath.IsAbs(repo) {
        return repo
    }
    exe, err := os.Executable()
    if err != nil { return repo }
    return filepath.Join(filepath.Dir(exe), repo)
}

func vaulticContained(parent, child string) bool {
    rel, err := filepath.Rel(parent, child)
    return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func vaulticSourcesForBackup(repo string, sources []string) ([]string, error) {
    if len(sources) == 0 { return nil, errors.New("Sélectionnez au moins un dossier ou fichier") }
    cleaned := make([]string, 0, len(sources))
    localRepo := vaulticLocalRepository(repo)
    repoAbs := ""
    if localRepo {
        var err error
        repoAbs, err = filepath.Abs(vaulticResolvedRepository(repo))
        if err != nil { return nil, err }
        if real, e := filepath.EvalSymlinks(repoAbs); e == nil { repoAbs = real }
    }
    for _, source := range sources {
        abs, err := filepath.Abs(source)
        if err != nil { return nil, err }
        info, err := os.Lstat(abs)
        if err != nil {
            return nil, fmt.Errorf("Source inaccessible : %s\nVérifiez le chemin, les permissions et le branchement du disque.\n%w", abs, err)
        }
        if !info.IsDir() && !info.Mode().IsRegular() {
            return nil, fmt.Errorf("Choisissez directement un fichier ou un dossier ordinaire : %s", abs)
        }
        if real, e := filepath.EvalSymlinks(abs); e == nil { abs = real }
        if _, err := os.Stat(abs); err != nil {
            return nil, fmt.Errorf("Source inaccessible : %s\nVérifiez le chemin, les permissions et le branchement du disque.\n%w", abs, err)
        }
        if localRepo && vaulticContained(abs, repoAbs) {
            return nil, fmt.Errorf("Le dépôt %q ne doit pas se trouver dans une source sauvegardée (%q)", repoAbs, abs)
        }
        cleaned = append(cleaned, abs)
    }
    return cleaned, nil
}

var vaulticSnapshotID = regexp.MustCompile(`^[a-fA-F0-9]{8,64}$`)

func vaulticRestoreTarget(target string) (string, error) {
    if target == "" { return "", errors.New("Choisissez un dossier de restauration") }
    abs, err := filepath.Abs(target)
    if err != nil { return "", err }
    info, err := os.Lstat(abs)
    if err != nil { return "", fmt.Errorf("Dossier de restauration inaccessible : %w", err) }
    if !info.IsDir() || info.Mode() & (os.ModeSymlink | os.ModeIrregular) != 0 {
        return "", errors.New("Choisissez un dossier de restauration qui n’est pas un lien")
    }
    canonical, err := filepath.EvalSymlinks(abs)
    if err != nil { return "", err }
    entries, err := os.ReadDir(canonical)
    if err != nil { return "", fmt.Errorf("Choisissez un dossier de restauration existant et vide : %w", err) }
    if len(entries) > 0 { return "", errors.New("Pour éviter tout écrasement, choisissez un dossier de restauration vide") }
    return canonical, nil
}

func vaulticMain() {
    a := app.NewWithID("fr.cybou.vaultic")
    a.SetIcon(vaulticAppIcon())
    a.Settings().SetTheme(&vaulticVisualTheme{})
    w := a.NewWindow("Vaultic — sauvegardes chiffrées")
    w.SetMaster()

    saved, settingsErr := vaulticLoadSettings()
    profileIndex := saved.migrateBackupProfiles()
    for _, profile := range saved.Profiles {
        for i, source := range profile.Sources {
            if !filepath.IsAbs(source) {
                profile.Sources[i] = vaulticResolvedRepository(source)
            }
        }
    }
    sources := saved.Profiles[profileIndex].Sources
    var snapshots []vaulticSnapshot
    selectedSource, selectedSnapshot := -1, -1
    sourceCountValue:=vaulticHeading(fmt.Sprintf("%d",len(sources)),24)
    snapshotCountValue:=vaulticHeading("—",24)
    recentRunValue:=widget.NewLabelWithStyle("Aucune",fyne.TextAlignLeading,fyne.TextStyle{Bold:true})
    recentRunValue.Truncation=fyne.TextTruncateEllipsis
    recentSnapshotValue:=widget.NewLabel("Aucun instantané consulté")
    recentSnapshotValue.Wrapping=fyne.TextWrapWord
    storageVolume:=vaulticHeading("—",24)
    storageBlobs:=vaulticHeading("—",24)
    storageSnapshots:=vaulticHeading("—",24)
    storageUpdated:=vaulticWrappedLabel("Aucune analyse effectuée pour ce dépôt.")

    activeRepository := saved.Repository
    repoEntry := widget.NewEntry()
    repoEntry.SetPlaceHolder("Chemin local ou adresse rest:https://…")
    depotName := widget.NewEntry()
    depotName.SetPlaceHolder("Ex. : Disque externe")
    depotSelect := widget.NewSelect(nil, nil)
    depotSelect.PlaceHolder = "Aucun dépôt connecté"
    repoValue:=widget.NewLabel("Aucun dépôt configuré")
    repoValue.Wrapping=fyne.TextWrapWord
    updateRepositoryLabel := func(value string){
        if strings.TrimSpace(value)==""{repoValue.SetText("Aucun dépôt configuré")}else{repoValue.SetText(strings.TrimSpace(value))}
    }
    updateRepositoryLabel(activeRepository)
    passEntry := widget.NewPasswordEntry()
    passEntry.SetPlaceHolder("Mot de passe du dépôt — jamais enregistré")
    newPassword := widget.NewPasswordEntry()
    newPassword.SetPlaceHolder("Mot de passe du dépôt")

    status := widget.NewLabel("Prêt")
    status.Wrapping = fyne.TextWrapWord
    progress := widget.NewProgressBar()
    progress.Hide()
    progressPlaceholder := canvas.NewRectangle(vaulticBackground)
    progressPlaceholder.SetMinSize(fyne.NewSize(0, progress.MinSize().Height))
    progressPlaceholder.Hide()
    progressSlot := container.NewStack(progressPlaceholder, progress)
    progressSlot.Hide()
    logs := widget.NewMultiLineEntry()
    logs.SetMinRowsVisible(4)
    logs.SetPlaceHolder("Aucun événement dans cette session.")
    logs.Wrapping = fyne.TextWrapWord
    logs.Disable()
    var logText string
    appendLog := func(msg string) {
        if strings.TrimSpace(msg) == "" { return }
        logText += time.Now().Format("15:04:05")+"  "+msg+"\n"
        if len(logText) > 16000 { logText = logText[len(logText)-12000:] }
        logs.SetText(logText)
    }
    persist := func(depots []vaulticDepot, active string) error {
        if settingsErr != nil { return settingsErr }
        profiles := make([]vaulticBackupProfile, len(saved.Profiles))
        for i, profile := range saved.Profiles {
            storedSources := make([]string, len(profile.Sources))
            for j, source := range profile.Sources {
                storedSources[j] = vaulticPortablePath(source)
            }
            profiles[i] = vaulticBackupProfile{
                Name:profile.Name, Sources:storedSources, Excludes:append([]string(nil),profile.Excludes...),
            }
        }
        return vaulticSaveSettings(vaulticSettings{
            Repository: active, Depots: depots, Sources: profiles[profileIndex].Sources,
            Profiles: profiles, ActiveProfile: profiles[profileIndex].Name,
        })
    }
    save := func() {
        if err := persist(saved.Depots, activeRepository); err != nil {
            appendLog("Erreur d'enregistrement des préférences : "+err.Error())
        }
    }

    var busy atomic.Bool
    var cancelOp context.CancelFunc
    closing := false
    closePrompt := false
    closeWindow := func() {
        save()
        passEntry.SetText("")
        newPassword.SetText("")
        w.SetCloseIntercept(nil)
        w.Close()
    }
    var actionButtons []*widget.Button
    var updateActions func()
    var windowContent *fyne.Container
    var lastBackupReport string
    showBackupReport := func() {
        content := container.NewVScroll(vaulticWrappedLabel(lastBackupReport))
        content.SetMinSize(fyne.NewSize(340, 170))
        reportDialog := dialog.NewCustom("Rapport de sauvegarde", "Fermer", content, w)
        reportDialog.Resize(fyne.NewSize(490, 300))
        reportDialog.Show()
    }
    cancelButton := widget.NewButton("Arrêter", func() {
        if cancelOp != nil { cancelOp(); status.SetText("Annulation…") }
    })
    cancelButton.Hide()
    setBusy := func(on bool) {
        if on {
            cancelButton.Enable()
            cancelButton.Show()
            repoEntry.Disable()
            depotName.Disable()
            depotSelect.Disable()
            passEntry.Disable()
            newPassword.Disable()
        } else {
            cancelButton.Hide()
            progress.Hide()
            progressPlaceholder.Hide()
            progressSlot.Hide()
            repoEntry.Enable()
            depotName.Enable()
            passEntry.Enable()
            newPassword.Enable()
        }
        for _, b := range actionButtons { if on { b.Disable() } else { b.Enable() } }
        updateActions()
        windowContent.Refresh()
    }

    // Called from the restic worker's goroutines; all widget access is marshaled
    // onto Fyne's UI thread. restic emits native JSON backup progress.
    onOutput := func(ctx context.Context, title, line string, report *vaulticBackupReport, redact func(string) string) {
        if line == "" { return }
        var e vaulticEngineEvent
        if json.Unmarshal([]byte(line), &e) == nil {
            if report != nil { report.record(e) }
            switch e.MessageType {
            case "status":
                fyne.Do(func(){
                if ctx.Err() != nil { return }
                if e.TotalBytes > 0 && e.PercentDone >= 0 && e.PercentDone <= 1 {
                    progress.SetValue(e.PercentDone)
                    if !progress.Visible() {
                        progress.Show()
                        progressSlot.Show()
                        windowContent.Refresh()
                    }
                }
                if report != nil {
                    files := fmt.Sprintf("%d fichiers", e.FilesDone)
                    if e.TotalFiles > 0 { files = fmt.Sprintf("%d / %d fichiers", e.FilesDone, e.TotalFiles) }
                    metrics := files + " · " + vaulticBytes(e.BytesDone)
                    if e.TotalBytes > 0 { metrics += " / " + vaulticBytes(e.TotalBytes) }
                    if e.SecondsElapsed > 0 {
                        metrics += " · " + vaulticBytes(e.BytesDone / e.SecondsElapsed) + "/s"
                    }
                    status.SetText(metrics)
                } else if e.TotalFiles > 0 {
                    status.SetText(fmt.Sprintf("%s en cours · %d / %d fichiers", title, e.FilesDone + e.FilesRestored, e.TotalFiles))
                }
                })
                return
            case "summary":
                if report != nil { return }
                if e.SnapshotID != "" {
                    line = fmt.Sprintf("Instantané %s · %d fichier(s) traité(s), %d octets", e.SnapshotID, e.FilesProcessed, e.BytesProcessed)
                } else if e.TotalFiles > 0 {
                    line = fmt.Sprintf("%d élément(s) restauré(s) sur %d · %d octets", e.FilesRestored, e.TotalFiles, e.BytesRestored)
                } else {
                    return
                }
            case "error":
                if e.Error.Message != "" {
                    line = e.Error.Message
                }
            }
        }
        line = redact(line)
        fyne.Do(func(){ appendLog(line) })
    }

    runWithDepot := func(title, repo, password string, create bool, fn func(context.Context, global.Options) error, finish func() error, report *vaulticBackupReport) {
        if repo == "" || password == "" {
            dialog.ShowInformation("Informations manquantes", "Indiquez un dépôt et son mot de passe.", w)
            return
        }
        if !busy.CompareAndSwap(false, true) {
            dialog.ShowInformation("Opération en cours", "Une seule opération est autorisée à la fois.", w)
            return
        }
        ctx, cancel := context.WithCancel(context.Background())
        cancelOp = cancel
        if report != nil {
            progressPlaceholder.Show()
            progressSlot.Show()
        }
        setBusy(true)
        progress.SetValue(0)
        status.SetText(title+"…")
        appendLog("Début : "+title)
        redact := vaulticRedactOperation(password)
        go func(){
            terminal := &vaulticTerminal{line:func(line string){onOutput(ctx, title, line, report, redact)},output:io.Discard}
            gopts := global.Options{
                Repo: vaulticResolvedRepository(repo), Password: password, Backends: all.Backends(),
                Term:terminal, JSON:true, Verbosity:1,
            }
            var err error
            if !create && vaulticLocalRepository(repo) {
                if _, statErr := os.Stat(gopts.Repo); statErr != nil {
                    err = fmt.Errorf("dépôt inaccessible. Vérifiez le chemin et le branchement du disque : %w", statErr)
                }
            }
            if err == nil {
                err = fn(ctx, gopts)
            }
            fyne.Do(func(){
                cancel()
                cancelOp = nil
                busy.Store(false)
                if errors.Is(err, repository.ErrNoKeyFound) {
                    err = errors.New("Mot de passe incorrect ou aucune clé utilisable dans ce dépôt.")
                }
                if closing {
                    if err == nil && finish != nil { _ = finish() }
                    closeWindow()
                    return
                }
                if report != nil {
                    var outcome string
                    outcome, lastBackupReport, err = report.result(err)
                    lastBackupReport = redact(lastBackupReport)
                    status.SetText("Sauvegarde · " + outcome)
                    appendLog("Sauvegarde · " + lastBackupReport)
                    if err == nil { recentRunValue.SetText(title) }
                    setBusy(false)
                    showBackupReport()
                    return
                }
                setBusy(false)
                if errors.Is(err, context.Canceled) {
                    status.SetText(title + " — annulé")
                    appendLog("Annulé : " + title)
                    return
                }
                if err == nil && finish != nil {
                    err = finish()
                }
                if err != nil {
                    status.SetText("Erreur : "+title)
                    message := redact(err.Error())
                    appendLog(message)
                    dialog.ShowError(errors.New(message),w)
                    return
                }
                status.SetText(title+" — terminé")
                recentRunValue.SetText(title)
                appendLog("Terminé : "+title)
            })
        }()
    }
    run := func(title string, fn func(context.Context, global.Options) error) {
        runWithDepot(title, activeRepository, passEntry.Text, false, fn, nil, nil)
    }

    browseRepo := widget.NewButton("Parcourir…", func(){
        dialog.NewFolderOpen(func(uri fyne.ListableURI, err error){
            if err != nil { dialog.ShowError(err,w); return }
            if uri != nil { repoEntry.SetText(uri.Path()) }
        },w).Show()
    })
    var activateDepot func(string, string)
    var refreshDepots func()
    connectDepot := func(create bool) {
        name, address := strings.TrimSpace(depotName.Text), strings.TrimSpace(repoEntry.Text)
        if name == "" || newPassword.Text == "" {
            dialog.ShowInformation("Informations manquantes", "Indiquez le nom et le mot de passe du dépôt.", w)
            return
        }
        if err := vaulticValidateDepot(address); err != nil {
            dialog.ShowError(err, w)
            return
        }
        if vaulticLocalRepository(address) {
            localPath := strings.TrimPrefix(address, "local:")
            if filepath.IsAbs(localPath) {
                address = vaulticPortablePath(localPath)
            } else {
                address = filepath.Clean(localPath)
            }
        }
        for _, depot := range saved.Depots {
            if depot.Name == name || vaulticResolvedRepository(depot.Location) == vaulticResolvedRepository(address) {
                dialog.ShowInformation("Dépôt déjà enregistré", "Utilisez un nom et un emplacement différents, ou sélectionnez le dépôt enregistré.", w)
                return
            }
        }
        password := newPassword.Text
        title := "Connexion au dépôt"
        if create { title = "Création du dépôt" }
        runWithDepot(title, address, password, create, func(ctx context.Context, g global.Options) error {
            if create {
                printer := ui.NewProgressPrinter(g.JSON, g.Verbosity, g.Term)
                repo, err := global.CreateRepository(ctx, g, restic.StableRepoVersion, nil, printer)
                if err != nil { return err }
                return repo.Close()
            }
            return vaulticVerifyDepot(ctx, g)
        }, func() error {
            depots := append(append([]vaulticDepot(nil), saved.Depots...), vaulticDepot{Name:name, Location:address})
            if err := persist(depots, address); err != nil { return err }
            saved.Depots = depots
            activateDepot(address, password)
            refreshDepots()
            depotName.SetText("")
            repoEntry.SetText("")
            newPassword.SetText("")
            return nil
        }, nil)
    }
    initButton := widget.NewButton("Créer un dépôt", func(){ connectDepot(true) })
    connectButton := widget.NewButton("Ouvrir un dépôt existant", func(){ connectDepot(false) })
    verifyButton := widget.NewButton("Vérifier la connexion", func() {
        run("Vérification de la connexion", vaulticVerifyDepot)
    })

    sourcesEmpty := vaulticEmptyState(vaulticWrappedLabel("Ajoutez un fichier ou un dossier pour préparer votre sauvegarde."))
    sourcesList := widget.NewList(
        func()int{return len(sources)},
        vaulticListLabel,
        func(id widget.ListItemID,obj fyne.CanvasObject){if id>=0 && id<len(sources){obj.(*widget.Label).SetText(sources[id])}},
    )
    sourcePath := widget.NewLabel("")
    sourcesList.OnSelected=func(id widget.ListItemID){
        if id < 0 || id >= len(sources) { return }
        selectedSource=id
        sourcePath.SetText(sources[id])
        updateActions()
    }
    sourcesList.OnUnselected=func(id widget.ListItemID){
        if selectedSource == id {
            selectedSource = -1
            sourcePath.SetText("")
            updateActions()
        }
    }
    refreshSources := func() {
        sourcesList.Refresh()
        sourceCountValue.Text = fmt.Sprintf("%d",len(sources))
        sourceCountValue.Refresh()
        updateActions()
    }
    updateSources := func(next []string) {
        previous := sources
        saved.Profiles[profileIndex].Sources = next
        if err := persist(saved.Depots, activeRepository); err != nil {
            saved.Profiles[profileIndex].Sources = previous
            dialog.ShowError(err, w)
            return
        }
        sourcesList.UnselectAll()
        sources = next
        refreshSources()
    }
    addFolder := widget.NewButton("+ Dossier",func(){
        dialog.NewFolderOpen(func(uri fyne.ListableURI,err error){
            if err != nil { dialog.ShowError(err,w);return }
            if uri != nil { updateSources(vaulticAppendUnique(sources,uri.Path())) }
        },w).Show()
    })
    addFile := widget.NewButton("+ Fichier",func(){
        dialog.NewFileOpen(func(f fyne.URIReadCloser,err error){
            if err != nil {dialog.ShowError(err,w);return}
            if f != nil { path:=f.URI().Path(); _=f.Close(); updateSources(vaulticAppendUnique(sources,path)) }
        },w).Show()
    })
    removeSource := widget.NewButton("Retirer",func(){
        if selectedSource<0 || selectedSource>=len(sources){return}
        next := append([]string(nil), sources[:selectedSource]...)
        next = append(next, sources[selectedSource+1:]...)
        updateSources(next)
    })
    profileSelect := widget.NewSelect(nil, nil)
    exclusionSummary:=widget.NewLabel("Aucune exclusion")
    refreshProfiles := func() {
        names := make([]string, len(saved.Profiles))
        for i, profile := range saved.Profiles { names[i] = profile.Name }
        profileSelect.SetOptions(names)
        if profileSelect.Selected != names[profileIndex] {
            profileSelect.SetSelected(names[profileIndex])
        }
        if count:=len(saved.Profiles[profileIndex].Excludes);count>0 {
            exclusionSummary.SetText(fmt.Sprintf("%d exclusion(s)",count))
        }else{
            exclusionSummary.SetText("Aucune exclusion")
        }
    }
    applyProfiles := func(next []vaulticBackupProfile, index int) bool {
        previousProfiles, previousIndex := saved.Profiles, profileIndex
        saved.Profiles, profileIndex = next, index
        if err := persist(saved.Depots, activeRepository); err != nil {
            saved.Profiles, profileIndex = previousProfiles, previousIndex
            refreshProfiles()
            dialog.ShowError(err, w)
            return false
        }
        sourcesList.UnselectAll()
        sources = saved.Profiles[profileIndex].Sources
        refreshProfiles()
        refreshSources()
        return true
    }
    profileSelect.OnChanged = func(name string) {
        for index, profile := range saved.Profiles {
            if profile.Name == name && index != profileIndex {
                applyProfiles(saved.Profiles, index)
                return
            }
        }
    }
    editProfile := func(create bool) {
        title := "Renommer le profil"
        name := widget.NewEntry()
        if create {
            title = "Nouveau profil"
        } else {
            name.SetText(saved.Profiles[profileIndex].Name)
        }
        form := dialog.NewForm(title, "Enregistrer", "Annuler", []*widget.FormItem{
            widget.NewFormItem("Nom", name),
        }, func(ok bool) {
            if !ok { return }
            label := strings.TrimSpace(name.Text)
            if label == "" || len([]rune(label)) > 40 {
                dialog.ShowInformation("Nom du profil", "Saisissez un nom de 1 à 40 caractères.", w)
                return
            }
            for i, profile := range saved.Profiles {
                if strings.EqualFold(profile.Name, label) && (create || i != profileIndex) {
                    dialog.ShowInformation("Nom déjà utilisé", "Choisissez un autre nom pour ce profil.", w)
                    return
                }
            }
            next := append([]vaulticBackupProfile(nil), saved.Profiles...)
            index := profileIndex
            if create {
                index = len(next)
                next = append(next, vaulticBackupProfile{Name: label})
            } else {
                next[index].Name = label
            }
            applyProfiles(next, index)
        }, w)
        form.Show()
        w.Canvas().Focus(name)
    }
    newProfileButton := widget.NewButton("Nouveau…", func(){editProfile(true)})
    renameProfileButton := widget.NewButton("Renommer…", func(){editProfile(false)})
    exclusionsButton:=widget.NewButtonWithIcon("Exclusions…",theme.ContentRemoveIcon(),func(){
        targetProfile:=profileIndex
        editor:=widget.NewMultiLineEntry()
        editor.SetMinRowsVisible(10)
        editor.Wrapping=fyne.TextWrapWord
        editor.SetPlaceHolder("**/node_modules\n**/.cache\n*.tmp")
        editor.SetText(strings.Join(saved.Profiles[targetProfile].Excludes,"\n"))
        explanation:=vaulticWrappedLabel("Une règle restic par ligne. Les lignes vides ou commençant par # sont ignorées. Des exceptions peuvent commencer par !. Les exclusions ne concernent que ce profil.")
        content:=container.NewBorder(explanation,nil,nil,nil,container.NewVScroll(editor))
        content.SetMinSize(fyne.NewSize(500,290))
        dialog.NewCustomConfirm("Exclusions du profil","Enregistrer","Annuler",content,func(ok bool){
            if !ok{return}
            if targetProfile!=profileIndex{
                dialog.ShowInformation("Profil modifié","Sélectionnez à nouveau le profil avant de modifier ses exclusions.",w)
                return
            }
            rules,err:=vaulticParseExclusions(editor.Text)
            if err!=nil{dialog.ShowError(err,w);return}
            next:=append([]vaulticBackupProfile(nil),saved.Profiles...)
            next[targetProfile].Excludes=rules
            applyProfiles(next,targetProfile)
        },w).Show()
    })
    backupButton := widget.NewButton("Sauvegarder maintenant",func(){
        selected := append([]string(nil), sources...)
        selectedExcludes:=append([]string(nil),saved.Profiles[profileIndex].Excludes...)
        report := &vaulticBackupReport{started: time.Now(), profile: saved.Profiles[profileIndex].Name}
        runWithDepot("Sauvegarde", activeRepository, passEntry.Text, false, func(ctx context.Context,g global.Options)error{
            clean, err := vaulticSourcesForBackup(g.Repo, selected)
            if err != nil { return err }
            if err := ctx.Err(); err != nil { return err }
            host, _ := os.Hostname()
            opts:= BackupOptions{Host:host,GroupBy:data.SnapshotGroupByOptions{Host:true,Path:true}}
            opts.Excludes=selectedExcludes
            err = runBackup(ctx,opts,g,g.Term,clean)
            // restic can format the underlying context error as plain text.
            if err != nil && ctx.Err() != nil && !errors.Is(err, ErrInvalidSourceData) {
                return ctx.Err()
            }
            return err
        }, nil, report)
    })
    backupReportButton := widget.NewButton("Dernier rapport…", showBackupReport)
    backupButton.Importance=widget.HighImportance
    sourceSection:=container.NewBorder(
        container.NewVBox(widget.NewLabelWithStyle("Sources",fyne.TextAlignLeading,fyne.TextStyle{Bold:true}),container.NewHBox(addFolder,addFile,removeSource)),
        container.NewHScroll(sourcePath),nil,nil,container.NewStack(sourcesList,sourcesEmpty),
    )

    snapshotList:=widget.NewList(
        func()int{return len(snapshots)},
        vaulticListLabel,
        func(id widget.ListItemID,obj fyne.CanvasObject){
            if id>=0 && id<len(snapshots){
                sn:=snapshots[id]
                obj.(*widget.Label).SetText(sn.Time.Local().Format("02/01/2006 15:04")+"  ·  "+sn.ShortID+"  ·  "+sn.Hostname)
            }
        },
    )
    snapshotEmptyLabel := vaulticWrappedLabel("Actualisez l’historique pour afficher les instantanés du dépôt.")
    snapshotEmpty := vaulticEmptyState(snapshotEmptyLabel)
    snapshotDetails:=vaulticWrappedLabel("Sélectionnez un instantané pour le parcourir ou le restaurer.")
    snapshotDetailsScroll := container.NewVScroll(snapshotDetails)
    snapshotDetailsScroll.SetMinSize(fyne.NewSize(0, 64))
    var snapshotsLoaded bool
    snapshotList.OnSelected=func(id widget.ListItemID){
        if id>=0 && id<len(snapshots){
            selectedSnapshot=id
            sn:=snapshots[id]
            snapshotDetails.SetText(fmt.Sprintf("Instantané : %s\nMachine : %s\nSources :\n%s",sn.ID,sn.Hostname,strings.Join(sn.Paths,"\n")))
        }
        updateActions()
    }
    resetSnapshotSelection := func() {
        selectedSnapshot = -1
        snapshotList.UnselectAll()
        snapshotDetails.SetText("Sélectionnez un instantané pour le parcourir ou le restaurer.")
        updateActions()
    }
    snapshotList.OnUnselected = func(id widget.ListItemID) {
        if selectedSnapshot == id {
            selectedSnapshot = -1
            snapshotDetails.SetText("Sélectionnez un instantané pour le parcourir ou le restaurer.")
            updateActions()
        }
    }
    refreshButton:=widget.NewButton("Actualiser les instantanés",func(){
        run("Lecture des instantanés",func(ctx context.Context,g global.Options)error{
            var out bytes.Buffer
            term:=g.Term.(*vaulticTerminal)
            term.output=&out
            if err:=runSnapshots(ctx,SnapshotOptions{},g,nil,term);err!=nil{return err}
            var list []vaulticSnapshot
            if err:=json.Unmarshal(out.Bytes(),&list);err!=nil{return fmt.Errorf("Décodage des instantanés : %w",err)}
            sort.SliceStable(list, func(i, j int) bool { return list[i].Time.After(list[j].Time) })
            fyne.Do(func(){
                snapshots=list
                snapshotsLoaded = true
                resetSnapshotSelection()
                snapshotList.Refresh()
                snapshotCountValue.Text=fmt.Sprintf("%d",len(list))
                snapshotCountValue.Refresh()
                if len(list)>0{
                    latest:=list[0].Time
                    for _,candidate:=range list[1:]{if candidate.Time.After(latest){latest=candidate.Time}}
                    recentSnapshotValue.SetText("Dernier instantané : "+latest.Local().Format("02/01/2006 à 15:04"))
                }else{recentSnapshotValue.SetText("Aucun instantané dans ce dépôt")}
            })
            return nil
        })
    })
    explorerButton:=widget.NewButton("Parcourir les fichiers…",func(){
        if selectedSnapshot<0 || selectedSnapshot>=len(snapshots){
            dialog.ShowInformation("Sélection requise","Sélectionnez un instantané.",w)
            return
        }
        repository := vaulticResolvedRepository(activeRepository)
        vaulticOpenExplorer(a,snapshots[selectedSnapshot],func(title string, operation func(context.Context, global.Options) error) {
            if repository != vaulticResolvedRepository(activeRepository) {
                dialog.ShowInformation("Dépôt modifié", "Actualisez les instantanés du dépôt actif avant de les parcourir.", w)
                return
            }
            run(title, operation)
        })
    })
    restoreButton:=widget.NewButton("Restaurer le tout…",func(){
        if selectedSnapshot<0 || selectedSnapshot>=len(snapshots){
            dialog.ShowInformation("Sélection requise","Sélectionnez d'abord un instantané.",w);return
        }
        id:=snapshots[selectedSnapshot].ID
        if !vaulticSnapshotID.MatchString(id){dialog.ShowError(errors.New("Identifiant d'instantané non valide"),w);return}
        dialog.NewFolderOpen(func(uri fyne.ListableURI,err error){
            if err!=nil{dialog.ShowError(err,w);return}
            if uri==nil{return}
            dest,err:=vaulticRestoreTarget(uri.Path())
            if err!=nil{dialog.ShowError(err,w);return}
            dialog.ShowConfirm("Confirmer la restauration",
                "Restaurer l'instantané "+id[:min(len(id), 12)]+" dans :\n"+dest+"\n\nLe dossier doit être vide. Aucun fichier existant ne sera écrasé.",
                func(ok bool){
                    if !ok{return}
                    // Recheck immediately before starting the restoration.
                    run("Restauration",func(ctx context.Context,g global.Options)error{
                        return vaulticRestoreSnapshot(ctx,g,id,nil,dest)
                    })
                },w)
        },w).Show()
    })
    restoreButton.Importance=widget.HighImportance
    checkButton:=widget.NewButton("Vérifier l'intégrité",func(){
        run("Vérification",func(ctx context.Context,g global.Options)error{
            _,err:=runCheck(ctx,CheckOptions{},g,nil,g.Term)
            return err
        })
    })
    storageButton:=widget.NewButtonWithIcon("Analyser le stockage",theme.SearchIcon(),func(){
        run("Analyse du stockage",func(ctx context.Context,g global.Options)error{
            metrics,err:=vaulticCollectStorageStatistics(ctx,g)
            if err!=nil{return err}
            fyne.Do(func(){
                if ctx.Err()!=nil{return}
                storageVolume.Text=vaulticBytes(metrics.TotalSize)
                storageVolume.Refresh()
                storageBlobs.Text=fmt.Sprintf("%d",metrics.TotalBlobCount)
                storageBlobs.Refresh()
                storageSnapshots.Text=fmt.Sprintf("%d",metrics.SnapshotsCount)
                storageSnapshots.Refresh()
                storageUpdated.SetText("Dernière analyse : "+time.Now().Format("02/01/2006 à 15:04"))
            })
            return nil
        })
    })

    actionButtons=[]*widget.Button{browseRepo,initButton,connectButton,verifyButton,addFolder,addFile,refreshButton,checkButton,storageButton,newProfileButton,renameProfileButton,exclusionsButton}
    updateActions = func() {
        if len(sources) == 0 { sourcesEmpty.Show() } else { sourcesEmpty.Hide() }
        if len(snapshots) == 0 { snapshotEmpty.Show() } else { snapshotEmpty.Hide() }
        if snapshotsLoaded {
            snapshotEmptyLabel.SetText("Ce dépôt ne contient aucun instantané.")
        } else {
            snapshotEmptyLabel.SetText("Actualisez l’historique pour afficher les instantanés du dépôt.")
        }
        vaulticSetButtonEnabled(backupButton, !busy.Load() && len(sources) > 0)
        vaulticSetButtonEnabled(backupReportButton, !busy.Load() && lastBackupReport != "")
        vaulticSetButtonEnabled(exclusionsButton,!busy.Load())
        vaulticSetButtonEnabled(storageButton,!busy.Load() && activeRepository!="")
        vaulticSetButtonEnabled(removeSource, !busy.Load() && selectedSource >= 0)
        canRestore := !busy.Load() && selectedSnapshot >= 0
        vaulticSetButtonEnabled(explorerButton, canRestore)
        vaulticSetButtonEnabled(restoreButton, canRestore)
        vaulticSetButtonEnabled(verifyButton, !busy.Load() && activeRepository != "")
        if busy.Load() { profileSelect.Disable() } else { profileSelect.Enable() }
        if busy.Load() || len(saved.Depots) == 0 {
            depotSelect.Disable()
        } else {
            depotSelect.Enable()
        }
    }
    activateDepot = func(address, password string) {
        activeRepository = address
        passEntry.SetText(password)
        updateRepositoryLabel(address)
        snapshots = nil
        snapshotsLoaded = false
        resetSnapshotSelection()
        snapshotList.Refresh()
        snapshotCountValue.Text = "—"
        snapshotCountValue.Refresh()
        recentSnapshotValue.SetText("Aucun instantané consulté")
        storageVolume.Text="—"
        storageVolume.Refresh()
        storageBlobs.Text="—"
        storageBlobs.Refresh()
        storageSnapshots.Text="—"
        storageSnapshots.Refresh()
        storageUpdated.SetText("Aucune analyse effectuée pour ce dépôt.")
    }
    refreshDepots = func() {
        names := make([]string, 0, len(saved.Depots))
        activeName := ""
        for _, depot := range saved.Depots {
            names = append(names, depot.Name)
            if depot.Location == activeRepository { activeName = depot.Name }
        }
        depotSelect.SetOptions(names)
        if activeName != "" && depotSelect.Selected != activeName {
            depotSelect.SetSelected(activeName)
        }
        updateActions()
    }
    depotSelect.OnChanged = func(name string) {
        for _, depot := range saved.Depots {
            if depot.Name != name || depot.Location == activeRepository { continue }
            if err := persist(saved.Depots, depot.Location); err != nil {
                refreshDepots()
                dialog.ShowError(err, w)
                return
            }
            activateDepot(depot.Location, "")
            return
        }
    }

    // Every page has one purpose. Operations still invoke the original Go
    // restic engine; this shell only changes presentation and navigation.
    fields:=widget.NewForm(
        widget.NewFormItem("Nom",depotName),
        widget.NewFormItem("Emplacement",container.NewBorder(nil,nil,nil,browseRepo,repoEntry)),
        widget.NewFormItem("Mot de passe",newPassword),
    )
    repositoryConfiguration:=vaulticSurface(container.NewVBox(
        vaulticHeading("Ajouter un dépôt",20),
        vaulticWrappedLabel("Choisissez un dossier local ou un disque externe, ou saisissez une adresse de dépôt distant."),
        fields,
        container.NewHBox(initButton,connectButton),
    ))
    activeConfiguration:=vaulticSurface(container.NewVBox(
        vaulticHeading("Dépôt actif",20),
        depotSelect,
        vaulticWrappedLabel("Sélectionnez un dépôt enregistré, puis saisissez son mot de passe pour l’utiliser."),
        passEntry,
        verifyButton,
    ))

    startBackup:=widget.NewButtonWithIcon("Préparer une sauvegarde",theme.MediaPlayIcon(),nil)
    startBackup.Importance=widget.HighImportance
    setupButton:=widget.NewButtonWithIcon("Configurer le dépôt",theme.SettingsIcon(),nil)
    viewSnapshots:=widget.NewButtonWithIcon("Voir les instantanés",theme.HistoryIcon(),nil)
    viewStorage:=widget.NewButtonWithIcon("Espace de stockage",theme.StorageIcon(),nil)
    snapshotOverview:=vaulticSurface(container.NewVBox(
        vaulticHeading("Dernière activité",19),
        recentSnapshotValue,
        viewSnapshots,
    ))
    statGrid:=container.NewVBox(
        container.NewGridWithColumns(2,
            vaulticStatCard("SOURCES CONFIGURÉES",sourceCountValue),
            vaulticStatCard("INSTANTANÉS CONSULTÉS",snapshotCountValue),
        ),
        container.NewGridWithColumns(2,
            vaulticStatCard("DÉPÔT ACTIF",repoValue),
            vaulticStatCard("DERNIÈRE OPÉRATION RÉUSSIE",recentRunValue),
        ),
    )
    overviewPage:=container.NewVScroll(container.NewPadded(container.NewVBox(
        vaulticHeading("Vue d’ensemble",26),
        container.NewHBox(startBackup,setupButton,viewStorage),statGrid,snapshotOverview,
    )))
    backupPage:=container.NewBorder(
        container.NewPadded(container.NewVBox(
            vaulticHeading("Sauvegarde", 26),
            widget.NewForm(widget.NewFormItem("Profil", container.NewBorder(nil,nil,nil,
                container.NewHBox(newProfileButton, renameProfileButton), profileSelect))),
            container.NewHBox(exclusionsButton,exclusionSummary),
        )),
        container.NewPadded(vaulticSurface(container.NewVBox(
            vaulticWrappedLabel("Le dépôt doit se trouver en dehors des dossiers sauvegardés."),
            container.NewGridWithColumns(2, backupButton, backupReportButton),
        ))),
        nil,nil,
        container.NewPadded(vaulticSurface(sourceSection)),
    )
    snapshotPage:=container.NewBorder(
        container.NewPadded(container.NewVBox(
            vaulticPageHeading("Instantanés","Parcourez les fichiers d’un instantané ou restaurez-le en entier."),
            refreshButton,
        )),
        container.NewPadded(vaulticSurface(container.NewVBox(
            snapshotDetailsScroll,
            container.NewHBox(explorerButton,restoreButton),
        ))),
        nil,nil,
        container.NewPadded(vaulticSurface(container.NewStack(snapshotList,snapshotEmpty))),
    )
    storagePage:=container.NewVScroll(container.NewPadded(container.NewVBox(
        vaulticPageHeading("Stockage","Mesurez les données réellement référencées par les instantanés."),
        vaulticSurface(container.NewVBox(
            vaulticWrappedLabel("Cette analyse lit les métadonnées du dépôt. Elle peut prendre du temps sur les dépôts volumineux et peut être annulée."),
            storageButton,
            storageUpdated,
        )),
        container.NewGridWithColumns(2,
            vaulticStatCard("VOLUME DES BLOBS",storageVolume),
            vaulticStatCard("BLOBS RÉFÉRENCÉS",storageBlobs),
        ),
        vaulticStatCard("INSTANTANÉS ANALYSÉS",storageSnapshots),
        vaulticSurface(vaulticWrappedLabel("Mesure restic raw-data : taille des blocs référencés, hors métadonnées du dépôt, fichiers non référencés et espace disque libre. Ce n'est pas la taille physique complète du dépôt.")),
    )))
    checkDescription:=widget.NewLabel("La vérification contrôle la structure du dépôt. Elle ne supprime pas vos sauvegardes.")
    checkDescription.Wrapping=fyne.TextWrapWord
    checkPage:=container.NewVScroll(container.NewPadded(container.NewVBox(
        vaulticHeading("Intégrité",26),
        vaulticSurface(container.NewVBox(
            vaulticHeading("Contrôle du dépôt",20),
            checkDescription,
            checkButton,
        )),
    )))
    journalPage:=container.NewBorder(
        container.NewPadded(vaulticHeading("Activité",26)),
        nil,nil,nil,container.NewPadded(vaulticSurface(container.NewBorder(
            container.NewHBox(widget.NewLabel("Événements de cette session"),layout.NewSpacer(),widget.NewButtonWithIcon("Effacer",theme.ContentClearIcon(),func(){logText="";logs.SetText("")})),
            nil,nil,nil,container.NewVScroll(logs),
        ))),
    )
    settingsPage:=container.NewVScroll(container.NewPadded(container.NewVBox(
        vaulticHeading("Paramètres",26),
        repositoryConfiguration,
        activeConfiguration,
        vaulticSurface(container.NewVBox(
            vaulticHeading("À propos de Vaultic",19),
            vaulticWrappedLabel("Les dépôts Vaultic utilisent le format restic."),
            vaulticWrappedLabel("Licence Apache-2.0 (Vaultic), BSD-2-Clause (restic)."),
            widget.NewLabel("Moteur intégré — "+global.Version),
        )),
    )))

    pages:=[]fyne.CanvasObject{overviewPage,backupPage,snapshotPage,storagePage,checkPage,journalPage,settingsPage}
    mainPage:=container.NewStack(overviewPage)
    var navButtons []*widget.Button
    navigate:=func(index int){
        if index<0||index>=len(pages){return}
        mainPage.Objects=[]fyne.CanvasObject{pages[index]}
        mainPage.Refresh()
        for i,b:=range navButtons{
            if i==index{b.Importance=widget.HighImportance}else{b.Importance=widget.LowImportance}
            b.Refresh()
        }
    }
    navLabels:=[]string{"Vue d'ensemble","Sauvegarde","Instantanés","Stockage","Intégrité","Activité","Paramètres"}
    navIcons:=[]fyne.Resource{theme.HomeIcon(),theme.StorageIcon(),theme.HistoryIcon(),theme.StorageIcon(),theme.ConfirmIcon(),theme.ListIcon(),theme.SettingsIcon()}
    for i:=range navLabels{
        index:=i
        button:=widget.NewButtonWithIcon(navLabels[i],navIcons[i],func(){navigate(index)})
        button.Alignment=widget.ButtonAlignLeading
        button.Importance=widget.LowImportance
        navButtons=append(navButtons,button)
    }
    setupButton.OnTapped=func(){navigate(6)}
    startBackup.OnTapped=func(){navigate(1)}
    viewSnapshots.OnTapped=func(){navigate(2)}
    viewStorage.OnTapped=func(){navigate(3)}
    sidebarBg:=canvas.NewRectangle(vaulticSidebar)
    brand:=container.NewHBox(container.NewGridWrap(fyne.NewSize(40,40),widget.NewIcon(vaulticAppIcon())),container.NewCenter(vaulticHeading("Vaultic",24)))
    sidebar:=container.NewStack(sidebarBg,container.NewPadded(container.NewBorder(
        container.NewVBox(brand,widget.NewSeparator()), nil,
        nil,nil,
        container.NewVScroll(container.New(layout.NewCustomPaddedVBoxLayout(4),navButtons[0],navButtons[1],navButtons[2],navButtons[3],navButtons[4],navButtons[5],navButtons[6])),
    )))
    statusTitle:=widget.NewLabelWithStyle("ÉTAT",fyne.TextAlignLeading,fyne.TextStyle{Bold:true})
    statusBar:=container.NewVBox(
        widget.NewSeparator(),
        progressSlot,
        container.NewBorder(nil,nil,statusTitle,cancelButton,status),
    )
    if activeRepository == "" { navigate(6) } else { navigate(0) }
    windowContent = container.NewBorder(nil,container.NewPadded(statusBar),sidebar,nil,mainPage)
    refreshDepots()
    refreshProfiles()
    w.SetContent(windowContent)
    w.SetCloseIntercept(func(){
        if closing || closePrompt { return }
        if busy.Load() {
            closePrompt = true
            dialog.ShowConfirm("Quitter Vaultic", "Annuler l’opération et fermer Vaultic après son arrêt ?", func(ok bool) {
                closePrompt = false
                if !ok { return }
                if cancelOp == nil { closeWindow(); return }
                closing = true
                cancelOp()
                status.SetText("Annulation avant fermeture…")
            }, w)
            return
        }
        closeWindow()
    })
    vaulticShowWindow(w, fyne.NewSize(1180,760), fyne.NewSize(640,365))
    if settingsErr != nil {
        dialog.ShowError(fmt.Errorf("Enregistrement des préférences désactivé pour cette session : %w", settingsErr), w)
    }
    a.Run()
}

func vaulticVerifyDepot(ctx context.Context, g global.Options) error {
    g.NoCache = true
    printer := ui.NewProgressPrinter(g.JSON, g.Verbosity, g.Term)
    repo, err := global.OpenRepository(ctx, g, printer)
    if err != nil { return err }
    return repo.Close()
}

func vaulticAppendUnique(paths []string, path string) []string {
    path=filepath.Clean(path)
    for _, old:=range paths { if old==path{return paths} }
    return append(paths,path)
}
