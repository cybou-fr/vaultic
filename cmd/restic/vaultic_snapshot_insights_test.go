package main

import (
    "bytes"
    "context"
    "encoding/json"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "github.com/restic/restic/internal/backend/all"
    "github.com/restic/restic/internal/data"
    "github.com/restic/restic/internal/global"
)

func TestVaulticDiffCollectorStreamBounded(t *testing.T){
    c:=&vaulticDiffCollector{}
    for i:=0;i<220;i++{
        line:=[]byte("{\"message_type\":\"change\",\"path\":\"/f.txt\",\"modifier\":\"+\"}\n")
        half:=len(line)/2
        c.Write(line[:half]);c.Write(line[half:])
    }
    c.Write([]byte("{\"message_type\":\"statistics\",\"added\":{\"files\":220}}\n"))
    if c.decodeErr!=nil||!c.gotStats||c.result.TotalChanges!=220||len(c.result.Changes)!=vaulticDiffPreviewLimit{
        t.Fatalf("collector: %+v err=%v",c.result,c.decodeErr)
    }
    if c.result.Stats.Added.Files!=220{t.Fatal("missing statistics")}
}

func TestVaulticDiffCollectorRejectsOversizedLine(t *testing.T){
    c:=&vaulticDiffCollector{}
    c.Write(bytes.Repeat([]byte("x"),vaulticDiffMaxLineBytes+1))
    if c.decodeErr==nil{t.Fatal("oversized line accepted")}
}

func TestVaulticCompareSnapshotsNativeRoundTrip(t *testing.T){
    root:=t.TempDir()
    source:=filepath.Join(root,"source")
    repo:=filepath.Join(root,"repo")
    if err:=os.MkdirAll(source,0700);err!=nil{t.Fatal(err)}
    oldFile:=filepath.Join(source,"first.txt")
    if err:=os.WriteFile(oldFile,[]byte("before"),0600);err!=nil{t.Fatal(err)}
    term:=&vaulticTerminal{}
    opts:=global.Options{Repo:repo,Password:"vaultic-diff-fixture",Backends:all.Backends(),Term:term,JSON:true,Verbosity:1}
    ctx:=context.Background()
    if err:=runInit(ctx,InitOptions{RepositoryVersion:"stable"},opts,nil,term);err!=nil{t.Fatal(err)}
    seen:=make(map[string]bool)
    backup:=func()string{
        t.Helper()
        if err:=runBackup(ctx,BackupOptions{Host:"vaultic-diff",GroupBy:data.SnapshotGroupByOptions{Host:true,Path:true}},opts,term,[]string{source});err!=nil{t.Fatal(err)}
        var out bytes.Buffer
        term.output=&out
        defer func(){term.output=nil}()
        if err:=runSnapshots(ctx,SnapshotOptions{},opts,nil,term);err!=nil{t.Fatal(err)}
        var list []vaulticSnapshot
        if err:=json.Unmarshal(out.Bytes(),&list);err!=nil{t.Fatal(err)}
        if len(list)==0{t.Fatal("snapshots missing")}
        for _,item:=range list{
            if !seen[item.ID]{seen[item.ID]=true;return item.ID}
        }
        t.Fatal("new snapshot missing")
        return ""
    }
    first:=backup()
    if err:=os.WriteFile(oldFile,[]byte("after"),0600);err!=nil{t.Fatal(err)}
    if err:=os.WriteFile(filepath.Join(source,"new.txt"),[]byte("new"),0600);err!=nil{t.Fatal(err)}
    second:=backup()
    if second==first {t.Fatal("snapshot did not change")}
    metrics,err:=vaulticMeasureSnapshot(ctx,opts,second)
    if err!=nil{t.Fatal(err)}
    if metrics.Files!=2||metrics.LogicalFileBytes!=8{t.Fatalf("snapshot metrics: %+v",metrics)}
    diff,err:=vaulticCompareSnapshots(ctx,opts,first,second)
    if err!=nil{t.Fatal(err)}
    if diff.Stats.Added.Files!=1||diff.Stats.ChangedFiles!=1 {
        t.Fatalf("snapshot diff: %+v",diff.Stats)
    }
    text:=vaulticFormatComparison(diff)
    if !strings.Contains(text,"new.txt")||!strings.Contains(text,"first.txt"){t.Fatalf("missing changed files: %s",text)}
    if err:=os.Remove(filepath.Join(source,"new.txt"));err!=nil{t.Fatal(err)}
    third:=backup()
    reduced,err:=vaulticCompareSnapshots(ctx,opts,second,third)
    if err!=nil{t.Fatal(err)}
    if reduced.Stats.Removed.Files!=1 {t.Fatalf("removed file not detected: %+v",reduced.Stats)}
    if _,err:=vaulticCompareSnapshots(ctx,opts,first,first);err==nil{
        t.Fatal("identical snapshots were accepted")
    }
}

func TestVaulticSuggestedComparisonBasePrefersSameSources(t *testing.T){
    now:=time.Date(2026,11,20,12,0,0,0,time.UTC)
    entries:=[]vaulticSnapshot{
        {ID:"aaaaaaaa",Hostname:"desk",Paths:[]string{"/docs"},Time:now},
        {ID:"bbbbbbbb",Hostname:"other",Paths:[]string{"/photos"},Time:now.Add(-time.Hour)},
        {ID:"cccccccc",Hostname:"desk",Paths:[]string{"/docs"},Time:now.Add(-3*time.Hour)},
        {ID:"dddddddd",Hostname:"desk",Paths:[]string{"/docs"},Time:now.Add(-5*time.Hour)},
    }
    if id:=vaulticSuggestedComparisonBase(entries,entries[0]);id!="cccccccc"{
        t.Fatalf("default base should use same host/sources, got %q",id)
    }
    if id:=vaulticSuggestedComparisonBase(entries[:1],entries[0]);id!=""{
        t.Fatalf("single snapshot yielded base %q",id)
    }
}
