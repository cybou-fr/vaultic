package main

import (
    "bytes"
    "context"
    "encoding/json"
    "os"
    "path/filepath"
    "testing"

    "github.com/restic/restic/internal/backend/all"
    "github.com/restic/restic/internal/global"
)

func TestVaulticSelectionFilter(t *testing.T){
    f:=vaulticSelectionFilter("/alpha/report[1].txt",false)
    for _,x:=range []struct{
        path string
        dir bool
        selectItem bool
        descend bool
    }{
        {"/alpha",true,false,true},
        {"/alpha/report[1].txt",false,true,false},
        {"/alpha/report1.txt",false,false,false},
        {"/alpha/report[1].txt2",false,false,false},
    }{
        selected,descend:=f(x.path,x.dir)
        if selected!=x.selectItem || descend!=x.descend{
            t.Errorf("file %q: got (%v,%v)",x.path,selected,descend)
        }
    }
    d:=vaulticSelectionFilter("/alpha/my-dir",true)
    for _,x:=range []struct{
        path string
        dir bool
        selectItem bool
        descend bool
    }{
        {"/alpha",true,false,true},
        {"/alpha/my-dir",true,true,true},
        {"/alpha/my-dir/file",false,true,false},
        {"/alpha/my-dir/nested",true,true,true},
        {"/alpha/my-dir2/file",false,false,false},
    }{
        selected,descend:=d(x.path,x.dir)
        if selected!=x.selectItem || descend!=x.descend {
            t.Errorf("folder %q: got (%v,%v)",x.path,selected,descend)
        }
    }
}

func TestVaulticSelectiveRestoreRoundTrip(t *testing.T){
    root:=t.TempDir()
    source:=filepath.Join(root,"source")
    repo:=filepath.Join(root,"repository")
    target:=filepath.Join(root,"restored")
    if err:=os.MkdirAll(source,0700);err!=nil{t.Fatal(err)}
    if err:=os.MkdirAll(target,0700);err!=nil{t.Fatal(err)}
    want:=[]byte("vaultic exact restore\n")
    if err:=os.WriteFile(filepath.Join(source,"report[1].txt"),want,0600);err!=nil{t.Fatal(err)}
    if err:=os.WriteFile(filepath.Join(source,"other.txt"),[]byte("not selected"),0600);err!=nil{t.Fatal(err)}
    var output bytes.Buffer
    term:=&vaulticTerminal{output:&output}
    opts:=global.Options{Repo:repo,Password:"vaultic-test-password",Backends:all.Backends(),Term:term,JSON:true,Verbosity:1}
    ctx:=context.Background()
    if err:=runInit(ctx,InitOptions{RepositoryVersion:"stable"},opts,nil,term);err!=nil{t.Fatal(err)}
    if err:=runBackup(ctx,BackupOptions{Host:"vaultic-test"},opts,term,[]string{source});err!=nil{t.Fatal(err)}
    output.Reset()
    if err:=runSnapshots(ctx,SnapshotOptions{},opts,nil,term);err!=nil{t.Fatal(err)}
    var snapshots []vaulticSnapshot
    if err:=json.Unmarshal(output.Bytes(),&snapshots);err!=nil{t.Fatal(err)}
    if len(snapshots)!=1{t.Fatalf("expected 1 snapshot, got %d",len(snapshots))}
    id:=snapshots[0].ID
    var selected *vaulticSnapshotNode
    var descend func(string)error
    descend=func(dir string)error{
        nodes,err:=vaulticListSnapshotDirectory(ctx,opts,id,dir)
        if err!=nil{return err}
        for _,n:=range nodes{
            if n.Type=="file" && n.Name=="report[1].txt"{
                cp:=n;selected=&cp;return nil
            }
            if n.Type=="dir"{
                if err:=descend(n.Path);err!=nil{return err}
                if selected!=nil{return nil}
            }
        }
        return nil
    }
    if err:=descend("/");err!=nil{t.Fatal(err)}
    if selected==nil{t.Fatal("selected file absent")}
    if err:=vaulticRestoreSelected(ctx,opts,id,*selected,target);err!=nil{t.Fatal(err)}
    var files []string
    if err:=filepath.WalkDir(target,func(p string,d os.DirEntry,err error)error{
        if err!=nil{return err}
        if !d.IsDir(){files=append(files,p)}
        return nil
    });err!=nil{t.Fatal(err)}
    if len(files)!=1{t.Fatalf("expected one restored file, found: %v",files)}
    got,err:=os.ReadFile(files[0])
    if err!=nil{t.Fatal(err)}
    if !bytes.Equal(got,want){t.Fatal("restored bytes differ")}
}
