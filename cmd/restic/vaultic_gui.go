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
    "github.com/restic/restic/internal/restorer"
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
        if real, e := filepath.EvalSymlinks(abs); e == nil { abs = real }
        if _, err := os.Stat(abs); err != nil { return nil, fmt.Errorf("Source %q : %w", abs, err) }
        if localRepo && vaulticContained(abs, repoAbs) {
            return nil, fmt.Errorf("Le dépôt %q ne doit pas se trouver dans une source sauvegardée (%q)", repoAbs, abs)
        }
        cleaned = append(cleaned, abs)
    }
    return cleaned, nil
}

var vaulticSnapshotID = regexp.MustCompile(`^[a-fA-F0-9]{8,64}$`)

func vaulticRestoreTarget(target string) (string, error) {
    abs, err := filepath.Abs(target)
    if err != nil { return "", err }
    entries, err := os.ReadDir(abs)
    if err != nil { return "", fmt.Errorf("Choisissez un dossier de restauration existant et vide : %w", err) }
    if len(entries) > 0 { return "", errors.New("Pour éviter tout écrasement, choisissez un dossier de restauration vide") }
    return abs, nil
}

func vaulticMain() {
    a := app.NewWithID("fr.cybou.vaultic")
    a.SetIcon(vaulticAppIcon())
    a.Settings().SetTheme(&vaulticVisualTheme{})
    w := a.NewWindow("Vaultic — sauvegardes chiffrées")
    w.SetMaster()

    saved := vaulticLoadSettings()
    sources := append([]string(nil), saved.Sources...)
    var snapshots []vaulticSnapshot
    selectedSource, selectedSnapshot := -1, -1
    sourceCountValue:=vaulticHeading(fmt.Sprintf("%d",len(sources)),24)
    snapshotCountValue:=vaulticHeading("—",24)
    recentRunValue:=widget.NewLabelWithStyle("Aucune",fyne.TextAlignLeading,fyne.TextStyle{Bold:true})
    recentRunValue.Truncation=fyne.TextTruncateEllipsis
    recentSnapshotValue:=widget.NewLabel("Aucun instantané consulté")
    recentSnapshotValue.Wrapping=fyne.TextWrapWord

    repoEntry := widget.NewEntry()
    repoEntry.SetPlaceHolder("Ex. : D:\\Sauvegardes\\Vaultic ou /media/ssd/vaultic")
    repoEntry.SetText(saved.Repository)
    repoValue:=widget.NewLabel("Aucun dépôt configuré")
    repoValue.Wrapping=fyne.TextWrapWord
    repoEntry.OnChanged=func(value string){
        if strings.TrimSpace(value)==""{repoValue.SetText("Aucun dépôt configuré")}else{repoValue.SetText(strings.TrimSpace(value))}
    }
    repoEntry.OnChanged(saved.Repository)
    passEntry := widget.NewPasswordEntry()
    passEntry.SetPlaceHolder("Mot de passe du dépôt — jamais enregistré")

    status := widget.NewLabel("Prêt")
    status.Wrapping = fyne.TextWrapWord
    progress := widget.NewProgressBar()
    progress.Hide()
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
    save := func() {
        if err := vaulticSaveSettings(vaulticSettings{Repository: strings.TrimSpace(repoEntry.Text), Sources: append([]string(nil), sources...)}); err != nil {
            appendLog("Erreur d'enregistrement des préférences : "+err.Error())
        }
    }

    var busy atomic.Bool
    var cancelOp context.CancelFunc
    var actionButtons []*widget.Button
    var updateActions func()
    var windowContent *fyne.Container
    cancelButton := widget.NewButton("Arrêter", func() {
        if cancelOp != nil { cancelOp(); status.SetText("Annulation…") }
    })
    cancelButton.Hide()
    setBusy := func(on bool) {
        if on {
            cancelButton.Enable()
            cancelButton.Show()
            repoEntry.Disable()
            passEntry.Disable()
        } else {
            cancelButton.Hide()
            progress.Hide()
            repoEntry.Enable()
            passEntry.Enable()
        }
        for _, b := range actionButtons { if on { b.Disable() } else { b.Enable() } }
        updateActions()
        windowContent.Refresh()
    }

    // Called from the restic worker's goroutines; all widget access is marshaled
    // onto Fyne's UI thread. restic emits native JSON backup progress.
    onOutput := func(ctx context.Context, title, line string) {
        if line == "" { return }
        var e struct {
            MessageType string `json:"message_type"`
            PercentDone float64 `json:"percent_done"`
            FilesDone int64 `json:"files_done"`
            FilesRestored int64 `json:"files_restored"`
            TotalFiles int64 `json:"total_files"`
            TotalBytes int64 `json:"total_bytes"`
            FilesProcessed int64 `json:"total_files_processed"`
            BytesProcessed int64 `json:"total_bytes_processed"`
            BytesRestored int64 `json:"bytes_restored"`
            SnapshotID string `json:"snapshot_id"`
            Error struct {
                Message string `json:"message"`
            } `json:"error"`
        }
        if json.Unmarshal([]byte(line), &e) == nil {
            switch e.MessageType {
            case "status":
                fyne.Do(func(){
                if ctx.Err() != nil { return }
                if e.TotalBytes > 0 && e.PercentDone >= 0 && e.PercentDone <= 1 {
                    progress.SetValue(e.PercentDone)
                    if !progress.Visible() {
                        progress.Show()
                        windowContent.Refresh()
                    }
                }
                if e.TotalFiles > 0 {
                    status.SetText(fmt.Sprintf("%s en cours · %d / %d fichiers", title, e.FilesDone + e.FilesRestored, e.TotalFiles))
                }
                })
                return
            case "summary":
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
        fyne.Do(func(){ appendLog(line) })
    }

    run := func(title string, fn func(context.Context, global.Options) error) {
        repo, password := strings.TrimSpace(repoEntry.Text), passEntry.Text
        if repo == "" || password == "" {
            dialog.ShowInformation("Informations manquantes", "Indiquez un dépôt et son mot de passe.", w)
            return
        }
        if !busy.CompareAndSwap(false, true) {
            dialog.ShowInformation("Opération en cours", "Une seule opération est autorisée à la fois.", w)
            return
        }
        save()
        ctx, cancel := context.WithCancel(context.Background())
        cancelOp = cancel
        setBusy(true)
        progress.SetValue(0)
        status.SetText(title+"…")
        appendLog("Début : "+title)
        go func(){
            terminal := &vaulticTerminal{line:func(line string){onOutput(ctx, title, line)},output:io.Discard}
            gopts := global.Options{
                Repo: vaulticResolvedRepository(repo), Password: password, Backends: all.Backends(),
                Term:terminal, JSON:true, Verbosity:1,
            }
            err := fn(ctx, gopts)
            fyne.Do(func(){
                cancel()
                cancelOp = nil
                busy.Store(false)
                setBusy(false)
                if errors.Is(err, context.Canceled) {
                    status.SetText(title + " — annulé")
                    appendLog("Annulé : " + title)
                    return
                }
                if err != nil {
                    status.SetText("Erreur : "+title)
                    appendLog(err.Error())
                    dialog.ShowError(err,w)
                    return
                }
                status.SetText(title+" — terminé")
                recentRunValue.SetText(title)
                appendLog("Terminé : "+title)
            })
        }()
    }

    browseRepo := widget.NewButton("Parcourir…", func(){
        dialog.NewFolderOpen(func(uri fyne.ListableURI, err error){
            if err != nil { dialog.ShowError(err,w); return }
            if uri != nil { repoEntry.SetText(uri.Path()); save() }
        },w).Show()
    })
    initButton := widget.NewButton("Initialiser le dépôt",func(){
        run("Initialisation",func(ctx context.Context,g global.Options)error{
            return runInit(ctx,InitOptions{RepositoryVersion:"stable"},g,nil,g.Term)
        })
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
        save()
    }
    addFolder := widget.NewButton("+ Dossier",func(){
        dialog.NewFolderOpen(func(uri fyne.ListableURI,err error){
            if err != nil { dialog.ShowError(err,w);return }
            if uri != nil { sources=vaulticAppendUnique(sources,uri.Path()); refreshSources() }
        },w).Show()
    })
    addFile := widget.NewButton("+ Fichier",func(){
        dialog.NewFileOpen(func(f fyne.URIReadCloser,err error){
            if err != nil {dialog.ShowError(err,w);return}
            if f != nil { path:=f.URI().Path(); _=f.Close(); sources=vaulticAppendUnique(sources,path); refreshSources() }
        },w).Show()
    })
    removeSource := widget.NewButton("Retirer",func(){
        if selectedSource<0 || selectedSource>=len(sources){return}
        sources=append(sources[:selectedSource],sources[selectedSource+1:]...)
        sourcesList.UnselectAll()
        refreshSources()
    })
    backupButton := widget.NewButton("Sauvegarder maintenant",func(){
        selected := append([]string(nil), sources...)
        repo := strings.TrimSpace(repoEntry.Text)
        clean,err:=vaulticSourcesForBackup(repo,selected)
        if err != nil {dialog.ShowError(err,w);return}
        run("Sauvegarde",func(ctx context.Context,g global.Options)error{
            host, _ := os.Hostname()
            opts:= BackupOptions{Host:host,GroupBy:data.SnapshotGroupByOptions{Host:true,Path:true}}
            return runBackup(ctx,opts,g,g.Term,clean)
        })
    })
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
        repository := vaulticResolvedRepository(repoEntry.Text)
        vaulticOpenExplorer(a,snapshots[selectedSnapshot],func(title string, operation func(context.Context, global.Options) error) {
            if repository != vaulticResolvedRepository(repoEntry.Text) {
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
                        if _,err:=vaulticRestoreTarget(dest);err!=nil{return err}
                        return runRestore(ctx,RestoreOptions{Target:dest, Overwrite:restorer.OverwriteNever, InheritWindowsPermissions:true},g,g.Term,[]string{id})
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

    actionButtons=[]*widget.Button{browseRepo,initButton,addFolder,addFile,refreshButton,checkButton}
    updateActions = func() {
        if len(sources) == 0 { sourcesEmpty.Show() } else { sourcesEmpty.Hide() }
        if len(snapshots) == 0 { snapshotEmpty.Show() } else { snapshotEmpty.Hide() }
        if snapshotsLoaded {
            snapshotEmptyLabel.SetText("Ce dépôt ne contient aucun instantané.")
        } else {
            snapshotEmptyLabel.SetText("Actualisez l’historique pour afficher les instantanés du dépôt.")
        }
        vaulticSetButtonEnabled(backupButton, !busy.Load() && len(sources) > 0)
        vaulticSetButtonEnabled(removeSource, !busy.Load() && selectedSource >= 0)
        canRestore := !busy.Load() && selectedSnapshot >= 0
        vaulticSetButtonEnabled(explorerButton, canRestore)
        vaulticSetButtonEnabled(restoreButton, canRestore)
    }
    updateRepositoryLabel := repoEntry.OnChanged
    repository := strings.TrimSpace(repoEntry.Text)
    repoEntry.OnChanged = func(value string) {
        updateRepositoryLabel(value)
        next := strings.TrimSpace(value)
        if next == repository { return }
        repository = next
        snapshots = nil
        snapshotsLoaded = false
        resetSnapshotSelection()
        snapshotList.Refresh()
        snapshotCountValue.Text = "—"
        snapshotCountValue.Refresh()
        recentSnapshotValue.SetText("Aucun instantané consulté")
    }

    // Every page has one purpose. Operations still invoke the original Go
    // restic engine; this shell only changes presentation and navigation.
    fields:=widget.NewForm(
        widget.NewFormItem("Dépôt",container.NewBorder(nil,nil,nil,browseRepo,repoEntry)),
        widget.NewFormItem("Mot de passe",passEntry),
    )
    repositoryConfiguration:=vaulticSurface(container.NewVBox(
        vaulticHeading("Dépôt de sauvegarde",20),
        vaulticWrappedLabel("Indiquez le chemin d’un dépôt existant ou initialisez un nouveau dépôt."),
        fields,
        container.NewHBox(initButton),
    ))

    startBackup:=widget.NewButtonWithIcon("Préparer une sauvegarde",theme.MediaPlayIcon(),nil)
    startBackup.Importance=widget.HighImportance
    setupButton:=widget.NewButtonWithIcon("Configurer le dépôt",theme.SettingsIcon(),nil)
    viewSnapshots:=widget.NewButtonWithIcon("Voir les instantanés",theme.HistoryIcon(),nil)
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
        container.NewHBox(startBackup,setupButton),statGrid,snapshotOverview,
    )))
    backupPage:=container.NewBorder(
        container.NewPadded(vaulticPageHeading("Sauvegarde","Choisissez les fichiers et dossiers à sauvegarder.")),
        container.NewPadded(vaulticSurface(container.NewVBox(
            vaulticWrappedLabel("Le dépôt doit se trouver en dehors des dossiers sauvegardés."),
            backupButton,
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
        vaulticSurface(container.NewVBox(
            vaulticHeading("À propos de Vaultic",19),
            vaulticWrappedLabel("Les dépôts Vaultic utilisent le format restic."),
            vaulticWrappedLabel("Licence Apache-2.0 (Vaultic), BSD-2-Clause (restic)."),
            widget.NewLabel("Moteur intégré — "+global.Version),
        )),
    )))

    pages:=[]fyne.CanvasObject{overviewPage,backupPage,snapshotPage,checkPage,journalPage,settingsPage}
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
    navLabels:=[]string{"Vue d'ensemble","Sauvegarde","Instantanés","Intégrité","Activité","Paramètres"}
    navIcons:=[]fyne.Resource{theme.HomeIcon(),theme.StorageIcon(),theme.HistoryIcon(),theme.ConfirmIcon(),theme.ListIcon(),theme.SettingsIcon()}
    for i:=range navLabels{
        index:=i
        button:=widget.NewButtonWithIcon(navLabels[i],navIcons[i],func(){navigate(index)})
        button.Alignment=widget.ButtonAlignLeading
        button.Importance=widget.LowImportance
        navButtons=append(navButtons,button)
    }
    setupButton.OnTapped=func(){navigate(5)}
    startBackup.OnTapped=func(){navigate(1)}
    viewSnapshots.OnTapped=func(){navigate(2)}
    sidebarBg:=canvas.NewRectangle(vaulticSidebar)
    brand:=container.NewHBox(container.NewGridWrap(fyne.NewSize(40,40),widget.NewIcon(vaulticAppIcon())),container.NewCenter(vaulticHeading("Vaultic",24)))
    sidebar:=container.NewStack(sidebarBg,container.NewPadded(container.NewBorder(
        container.NewVBox(brand,widget.NewSeparator()), nil,
        nil,nil,
        container.NewVScroll(container.New(layout.NewCustomPaddedVBoxLayout(4),navButtons[0],navButtons[1],navButtons[2],navButtons[3],navButtons[4],navButtons[5])),
    )))
    statusTitle:=widget.NewLabelWithStyle("ÉTAT",fyne.TextAlignLeading,fyne.TextStyle{Bold:true})
    statusBar:=container.NewVBox(
        widget.NewSeparator(),
        container.NewBorder(nil,nil,statusTitle,cancelButton,status),
        progress,
    )
    navigate(0)
    windowContent = container.NewBorder(nil,container.NewPadded(statusBar),sidebar,nil,mainPage)
    updateActions()
    w.SetContent(windowContent)
    w.SetCloseIntercept(func(){
        if busy.Load(){dialog.ShowInformation("Opération en cours","Arrêtez l'opération avant de quitter Vaultic.",w);return}
        save()
        w.SetCloseIntercept(nil)
        w.Close()
    })
    vaulticShowWindow(w, fyne.NewSize(1180,760), fyne.NewSize(640,365))
    a.Run()
}

func vaulticAppendUnique(paths []string, path string) []string {
    path=filepath.Clean(path)
    for _, old:=range paths { if old==path{return paths} }
    return append(paths,path)
}
