package main

import (
    "context"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestVaulticPreviewExclusionsMatchesFilesAndDirectories(t *testing.T){
    root:=t.TempDir()
    cache:=filepath.Join(root,"node_modules")
    if err:=os.MkdirAll(cache,0700);err!=nil{t.Fatal(err)}
    for _,p:=range []string{"keep.txt","skip.tmp",filepath.Join("node_modules","package.js")}{
        if err:=os.WriteFile(filepath.Join(root,p),[]byte("test"),0600);err!=nil{t.Fatal(err)}
    }
    preview,err:=vaulticPreviewExcludedPaths(context.Background(),[]string{root},[]string{"*.tmp","**/node_modules"})
    if err!=nil{t.Fatal(err)}
    if preview.Truncated || preview.Matched!=2{t.Fatalf("unexpected preview: %+v",preview)}
    joined:=strings.Join(preview.Examples,"\n")
    if !strings.Contains(joined,"skip.tmp")||!strings.Contains(joined,"node_modules"){
        t.Fatalf("missing exclusion paths: %+v",preview)
    }
    if strings.Contains(joined,"package.js"){t.Fatal("excluded directory was expanded")}
}

func TestVaulticPreviewExclusionsIsReadOnlyAndCancelable(t *testing.T){
    root:=t.TempDir()
    name:=filepath.Join(root,"important.tmp")
    if err:=os.WriteFile(name,[]byte("untouched"),0600);err!=nil{t.Fatal(err)}
    ctx,cancel:=context.WithCancel(context.Background());cancel()
    if _,err:=vaulticPreviewExcludedPaths(ctx,[]string{root},[]string{"*.tmp"});err!=context.Canceled{
        t.Fatalf("cancellation: %v",err)
    }
    if _,err:=vaulticPreviewExcludedPaths(context.Background(),[]string{root},[]string{"*.tmp"});err!=nil{t.Fatal(err)}
    data,err:=os.ReadFile(name)
    if err!=nil||string(data)!="untouched"{t.Fatalf("preview changed files: %q, %v",data,err)}
}
