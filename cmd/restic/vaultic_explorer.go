package main

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "path"
    "path/filepath"
    "sort"
    "strings"

    "github.com/restic/restic/internal/data"
    "github.com/restic/restic/internal/global"
    "github.com/restic/restic/internal/restorer"
    "github.com/restic/restic/internal/ui"
    restoreui "github.com/restic/restic/internal/ui/restore"
)

// A restic ls --json node. Repository paths use forward slashes on all OSes.
type vaulticSnapshotNode struct {
    Name string `json:"name"`
    Path string `json:"path"`
    Type string `json:"type"`
    Size *uint64 `json:"size,omitempty"`
    MessageType string `json:"message_type"`
}

type vaulticLimitedBuffer struct {
    bytes.Buffer
    Limit int
}

func (b *vaulticLimitedBuffer) Write(data []byte) (int,error) {
    if len(data) > b.Limit-b.Len() {
        return 0, errors.New("Le dossier contient trop d'éléments pour être affiché")
    }
    return b.Buffer.Write(data)
}

func vaulticValidateSnapshotPath(p string) error {
    if p == "" || !strings.HasPrefix(p, "/") || path.Clean(p) != p || strings.ContainsRune(p, '\x00') {
        return fmt.Errorf("Chemin d'instantané invalide : %q",p)
    }
    return nil
}

// Calls upstream restic's in-process ls implementation, without any CLI process.
func vaulticListSnapshotDirectory(ctx context.Context,opts global.Options,id,dir string)([]vaulticSnapshotNode,error){
    if !vaulticSnapshotID.MatchString(id){return nil,errors.New("Identifiant d'instantané invalide")}
    if err:=vaulticValidateSnapshotPath(dir);err!=nil{return nil,err}
    buffer:=&vaulticLimitedBuffer{Limit:16<<20}
    term:=&vaulticTerminal{output:buffer}
    opts.Term=term
    opts.JSON=true
    args:=[]string{id}
    if dir!="/"{args=append(args,dir)}
    if err:=runLs(ctx,LsOptions{},opts,args,term);err!=nil{return nil,err}
    dec:=json.NewDecoder(bytes.NewReader(buffer.Bytes()))
    list:=make([]vaulticSnapshotNode,0)
    for {
        var n vaulticSnapshotNode
        if err:=dec.Decode(&n);err!=nil {
            if errors.Is(err,io.EOF){break}
            return nil,fmt.Errorf("Lecture des éléments : %w",err)
        }
        if n.MessageType!="node"{continue}
        if err:=vaulticValidateSnapshotPath(n.Path);err!=nil{return nil,err}
        if n.Path!="/" && path.Dir(n.Path)==dir{list=append(list,n)}
    }
    sort.SliceStable(list,func(i,j int)bool {
        ai,aj:=list[i].Type=="dir",list[j].Type=="dir"
        if ai!=aj{return ai}
        return strings.ToLower(list[i].Name)<strings.ToLower(list[j].Name)
    })
    return list,nil
}

// The exact selected node (and descendants for a directory), with only the
// ancestor traversal needed to reach it. No glob patterns or name escaping.
func vaulticSelectionFilter(selected string,isDirectory bool)func(string,bool)(bool,bool){
    selected=path.Clean(selected)
    return func(item string,isDir bool)(bool,bool){
        item=path.Clean(filepath.ToSlash(item))
        if item==selected{return true,isDirectory && isDir}
        if isDirectory && strings.HasPrefix(item,selected+"/"){return true,isDir}
        if isDir && strings.HasPrefix(selected,item+"/"){return false,true}
        return false,false
    }
}

// Restores one selected node through the original restic repository/restorer
// packages, with no subprocess, deletions or overwriting existing files.
func vaulticRestoreSelected(ctx context.Context,opts global.Options,id string,node vaulticSnapshotNode,destination string)error{
    if !vaulticSnapshotID.MatchString(id){return errors.New("Identifiant d'instantané invalide")}
    if node.Path=="/"{return errors.New("Sélectionnez un fichier ou un dossier")}
    if err:=vaulticValidateSnapshotPath(node.Path);err!=nil{return err}
    if node.Type!="dir" && node.Type!="file" && node.Type!="symlink" {
        return errors.New("Type d'élément non pris en charge")
    }
    target,err:=vaulticRestoreTarget(destination)
    if err!=nil{return err}
    printer:=restoreui.NewJSONProgress(opts.Term,opts.Verbosity)
    ctx,repo,unlock,err:=openWithReadLock(ctx,opts,opts.NoLock,printer)
    if err!=nil{return err}
    defer unlock()
    var filter data.SnapshotFilter
    snapshot,subfolder,err:=filter.FindLatest(ctx,repo,repo,id)
    if err!=nil{return err}
    if err:=repo.LoadIndex(ctx,printer);err!=nil{return err}
    snapshot.Tree,err=data.FindTreeDirectory(ctx,repo,snapshot.Tree,subfolder)
    if err!=nil{return err}
    progress:=restoreui.NewProgress(printer,ui.CalculateProgressInterval(!opts.Quiet,true,opts.Term.CanUpdateStatus()))
    r:=restorer.NewRestorer(repo,snapshot,restorer.Options{
        Progress:progress,
        Overwrite:restorer.OverwriteNever,
        InheritWindowsPermissions:true,
        Delete:false,
    })
    r.SelectFilter=vaulticSelectionFilter(node.Path,node.Type=="dir")
    r.Error=func(location string,e error)error{
        printer.E("Erreur de restauration de %q : %v",location,e)
        return e
    }
    _,err=r.RestoreTo(ctx,target)
    progress.Finish()
    return err
}
