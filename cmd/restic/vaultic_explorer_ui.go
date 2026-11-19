package main

import (
    "context"
    "fmt"
    "path"

    "fyne.io/fyne/v2"
    "fyne.io/fyne/v2/container"
    "fyne.io/fyne/v2/dialog"
    "fyne.io/fyne/v2/widget"

    "github.com/restic/restic/internal/global"
)

// A dedicated screen for browsing one snapshot. No background job is started:
// every operation uses the application's existing single-operation runner.
func vaulticOpenExplorer(app fyne.App,snapshot vaulticSnapshot,run func(string,func(context.Context,global.Options)error)){
    w:=app.NewWindow("Vaultic — Instantané "+snapshot.ShortID)
    directory:="/"
    var nodes []vaulticSnapshotNode
    selected:=-1
    location:=widget.NewLabel(directory)
    location.Wrapping=fyne.TextWrapWord
    selection:=widget.NewLabel("Sélectionnez un fichier ou un dossier.")
    selection.Wrapping=fyne.TextWrapWord
    files:=widget.NewList(
        func()int{return len(nodes)},
        vaulticListLabel,
        func(id widget.ListItemID,obj fyne.CanvasObject){
            if int(id)<0 || int(id)>=len(nodes){return}
            n:=nodes[int(id)]
            kind:="Fichier"
            if n.Type=="dir"{kind="Dossier"}
            if n.Type=="symlink"{kind="Lien"}
            size:=""
            if n.Size!=nil{size=fmt.Sprintf(" · %d octets",*n.Size)}
            obj.(*widget.Label).SetText(kind+" · "+n.Name+size)
        },
    )
    back:=widget.NewButton("Dossier parent",nil)
    open:=widget.NewButton("Ouvrir le dossier",nil)
    restore:=widget.NewButton("Restaurer la sélection…",nil)
    back.Disable()
    open.Disable()
    restore.Disable()
    files.OnSelected=func(id widget.ListItemID){
        selected=int(id)
        if selected<0 || selected>=len(nodes){return}
        n:=nodes[selected]
        selection.SetText(n.Path)
        if n.Type=="dir"{open.Enable()}else{open.Disable()}
        if n.Type=="file" || n.Type=="dir" || n.Type=="symlink"{restore.Enable()}else{restore.Disable()}
    }
    files.OnUnselected=func(id widget.ListItemID){
        if int(id)==selected{
            selected=-1
            open.Disable()
            restore.Disable()
        }
    }
    load:=func(next string){
        run("Exploration de l'instantané",func(ctx context.Context,opts global.Options)error{
            found,err:=vaulticListSnapshotDirectory(ctx,opts,snapshot.ID,next)
            if err!=nil{return err}
            fyne.Do(func(){
                directory=next
                nodes=found
                selected=-1
                files.UnselectAll()
                files.Refresh()
                location.SetText(next)
                if next=="/"{back.Disable()}else{back.Enable()}
                open.Disable()
                restore.Disable()
                selection.SetText(fmt.Sprintf("%d élément(s) dans ce dossier",len(nodes)))
            })
            return nil
        })
    }
    back.OnTapped=func(){if directory!="/"{load(path.Dir(directory))}}
    open.OnTapped=func(){
        if selected>=0 && selected<len(nodes) && nodes[selected].Type=="dir"{load(nodes[selected].Path)}
    }
    restore.OnTapped=func(){
        if selected<0 || selected>=len(nodes){return}
        n:=nodes[selected]
        if n.Type!="dir" && n.Type!="file" && n.Type!="symlink"{return}
        dialog.NewFolderOpen(func(uri fyne.ListableURI,err error){
            if err!=nil{dialog.ShowError(err,w);return}
            if uri==nil{return}
            destination,err:=vaulticRestoreTarget(uri.Path())
            if err!=nil{dialog.ShowError(err,w);return}
            dialog.ShowConfirm("Confirmer la restauration",
                "Élément à restaurer :\n"+n.Path+"\n\nDestination :\n"+destination+
                "\n\nLe dossier doit être vide. Les fichiers existants ne seront pas écrasés.",
                func(ok bool){
                    if !ok{return}
                    run("Restauration de la sélection",func(ctx context.Context,opts global.Options)error{
                        return vaulticRestoreSelected(ctx,opts,snapshot.ID,n,destination)
                    })
                },w)
        },w).Show()
    }
    w.SetContent(container.NewBorder(
        container.NewPadded(container.NewVBox(
            vaulticPageHeading("Contenu de l'instantané","Sélectionnez un fichier ou un dossier à restaurer."),
            location,container.NewHBox(back,open),
        )),
        container.NewPadded(vaulticSurface(container.NewVBox(selection,restore))),
        nil,nil,container.NewPadded(vaulticSurface(files)),
    ))
    vaulticShowWindow(w,fyne.NewSize(900,600),fyne.NewSize(500,375))
    load("/")
}
