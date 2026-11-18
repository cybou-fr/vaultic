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
    "fyne.io/fyne/v2/layout"
    "fyne.io/fyne/v2/widget"

    "github.com/restic/restic/internal/backend/all"
    "github.com/restic/restic/internal/data"
    "github.com/restic/restic/internal/global"
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
        repoAbs, err = filepath.Abs(repo)
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
    w := a.NewWindow("Vaultic")
    w.Resize(fyne.NewSize(1040, 760))

    saved := vaulticLoadSettings()
    sources := append([]string(nil), saved.Sources...)
    var snapshots []vaulticSnapshot
    selectedSource, selectedSnapshot := -1, -1

    repoEntry := widget.NewEntry()
    repoEntry.SetPlaceHolder("Ex. : D:\\Sauvegardes\\Vaultic ou /media/ssd/vaultic")
    repoEntry.SetText(saved.Repository)
    passEntry := widget.NewPasswordEntry()
    passEntry.SetPlaceHolder("Mot de passe du dépôt — jamais enregistré")

    status := widget.NewLabel("Prêt")
    status.Wrapping = fyne.TextWrapWord
    progress := widget.NewProgressBar()
    logs := widget.NewMultiLineEntry()
    logs.SetMinRowsVisible(4)
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
    cancelButton := widget.NewButton("Arrêter", func() {
        if cancelOp != nil { cancelOp(); status.SetText("Annulation…") }
    })
    cancelButton.Disable()
    setBusy := func(on bool) {
        if on { cancelButton.Enable() } else { cancelButton.Disable() }
        for _, b := range actionButtons { if on { b.Disable() } else { b.Enable() } }
    }

    // Called from the restic worker's goroutines; all widget access is marshaled
    // onto Fyne's UI thread. restic emits native JSON backup progress.
    onOutput := func(line string) {
        if line == "" { return }
        var e struct {
            MessageType string `json:"message_type"`
            PercentDone float64 `json:"percent_done"`
            FilesDone int64 `json:"files_done"`
            TotalFiles int64 `json:"total_files"`
            SnapshotID string `json:"snapshot_id"`
            ErrorCount int64 `json:"error_count"`
        }
        if json.Unmarshal([]byte(line), &e) == nil && e.MessageType == "status" {
            fyne.Do(func(){
                if e.PercentDone >= 0 && e.PercentDone <= 1 { progress.SetValue(e.PercentDone) }
                status.SetText(fmt.Sprintf("Sauvegarde : %.1f %% — %d / %d fichiers", 100*e.PercentDone, e.FilesDone, e.TotalFiles))
            })
            return
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
            terminal := &vaulticTerminal{line:onOutput,output:io.Discard}
            gopts := global.Options{
                Repo: repo, Password: password, Backends: all.Backends(),
                Term:terminal, JSON:true, Verbosity:1,
            }
            err := fn(ctx, gopts)
            fyne.Do(func(){
                cancel()
                cancelOp = nil
                busy.Store(false)
                setBusy(false)
                if err != nil {
                    status.SetText("Erreur : "+title)
                    appendLog(err.Error())
                    dialog.ShowError(err,w)
                    return
                }
                status.SetText(title+" — terminé")
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
    versionLabel := widget.NewLabel("Moteur restic intégré — "+global.Version)

    sourcesList := widget.NewList(
        func()int{return len(sources)},
        func()fyne.CanvasObject{return widget.NewLabel("")},
        func(id widget.ListItemID,obj fyne.CanvasObject){if id>=0 && id<len(sources){obj.(*widget.Label).SetText(sources[id])}},
    )
    sourcesList.OnSelected=func(id widget.ListItemID){selectedSource=id}
    addFolder := widget.NewButton("+ Dossier",func(){
        dialog.NewFolderOpen(func(uri fyne.ListableURI,err error){
            if err != nil { dialog.ShowError(err,w);return }
            if uri != nil { sources=vaulticAppendUnique(sources,uri.Path());sourcesList.Refresh();save() }
        },w).Show()
    })
    addFile := widget.NewButton("+ Fichier",func(){
        dialog.NewFileOpen(func(f fyne.URIReadCloser,err error){
            if err != nil {dialog.ShowError(err,w);return}
            if f != nil { path:=f.URI().Path(); _=f.Close(); sources=vaulticAppendUnique(sources,path);sourcesList.Refresh();save() }
        },w).Show()
    })
    removeSource := widget.NewButton("Retirer",func(){
        if selectedSource<0 || selectedSource>=len(sources){return}
        sources=append(sources[:selectedSource],sources[selectedSource+1:]...)
        selectedSource=-1
        sourcesList.UnselectAll();sourcesList.Refresh();save()
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
        nil,nil,nil,sourcesList,
    )

    snapshotList:=widget.NewList(
        func()int{return len(snapshots)},
        func()fyne.CanvasObject{return widget.NewLabel("")},
        func(id widget.ListItemID,obj fyne.CanvasObject){
            if id>=0 && id<len(snapshots){
                sn:=snapshots[id]
                obj.(*widget.Label).SetText(sn.Time.Local().Format("02/01/2006 15:04")+"  ·  "+sn.ShortID+"  ·  "+sn.Hostname)
            }
        },
    )
    snapshotDetails:=widget.NewLabel("Sélectionnez un instantané à restaurer.")
    snapshotDetails.Wrapping=fyne.TextWrapWord
    snapshotList.OnSelected=func(id widget.ListItemID){
        selectedSnapshot=id
        if id>=0 && id<len(snapshots){
            sn:=snapshots[id]
            snapshotDetails.SetText(fmt.Sprintf("Instantané : %s\nMachine : %s\nSources :\n%s",sn.ID,sn.Hostname,strings.Join(sn.Paths,"\n")))
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
                selectedSnapshot=-1
                snapshotList.UnselectAll()
                snapshotList.Refresh()
                snapshotDetails.SetText(fmt.Sprintf("%d instantané(s)",len(list)))
            })
            return nil
        })
    })
    restoreButton:=widget.NewButton("Restaurer dans un dossier vide…",func(){
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
                "Restaurer l'instantané "+id[:12]+" dans :\n"+dest+"\n\nLe dossier doit être vide. Aucun fichier existant ne sera écrasé.",
                func(ok bool){
                    if !ok{return}
                    // Recheck immediately before starting the restoration.
                    run("Restauration",func(ctx context.Context,g global.Options)error{
                        if _,err:=vaulticRestoreTarget(dest);err!=nil{return err}
                        return runRestore(ctx,RestoreOptions{Target:dest},g,g.Term,[]string{id})
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

    actionButtons=[]*widget.Button{browseRepo,initButton,addFolder,addFile,removeSource,backupButton,refreshButton,restoreButton,checkButton}
    fields:=widget.NewForm(
        widget.NewFormItem("Dépôt",container.NewBorder(nil,nil,nil,browseRepo,repoEntry)),
        widget.NewFormItem("Mot de passe",passEntry),
    )
    header:=container.NewVBox(
        widget.NewLabelWithStyle("Vaultic",fyne.TextAlignLeading,fyne.TextStyle{Bold:true}),
        widget.NewLabel("Sauvegardes chiffrées, moteur restic natif et intégré"),
        fields,container.NewHBox(initButton,layout.NewSpacer(),versionLabel),
    )
    backupTab:=container.NewBorder(
        widget.NewLabel("Choisissez les fichiers et dossiers à sauvegarder."),
        container.NewVBox(widget.NewSeparator(),backupButton),nil,nil,sourceSection,
    )
    snapshotTab:=container.NewBorder(
        container.NewVBox(widget.NewLabel("Historique"),refreshButton),
        container.NewVBox(widget.NewSeparator(),snapshotDetails,restoreButton),
        nil,nil,snapshotList,
    )
    checkTab:=container.NewVBox(
        widget.NewLabel("Vérification de la structure du dépôt (lecture seule)."),
        checkButton,
    )
    tabs:=container.NewAppTabs(
        container.NewTabItem("Sauvegarde",backupTab),
        container.NewTabItem("Instantanés",snapshotTab),
        container.NewTabItem("Intégrité",checkTab),
    )
    tabs.SetTabLocation(container.TabLocationTop)
    bottom:=container.NewBorder(
        container.NewVBox(widget.NewSeparator(),container.NewHBox(status,layout.NewSpacer(),cancelButton),progress),
        nil,nil,nil,container.NewVScroll(logs),
    )
    split:=container.NewVSplit(tabs,bottom)
    split.Offset=0.68
    w.SetContent(container.NewBorder(header,nil,nil,nil,split))
    w.SetCloseIntercept(func(){
        if busy.Load(){dialog.ShowInformation("Opération en cours","Arrêtez l'opération avant de quitter Vaultic.",w);return}
        save()
        w.SetCloseIntercept(nil)
        w.Close()
    })
    appendLog("Moteur officiel restic intégré. Aucun programme restic externe nécessaire.")
    w.ShowAndRun()
}

func vaulticAppendUnique(paths []string, path string) []string {
    path=filepath.Clean(path)
    for _, old:=range paths { if old==path{return paths} }
    return append(paths,path)
}
