package main

import (
 "strings"
 "testing"
)

func TestVaulticDiffFiltersAndTwoColumnLayout(t *testing.T) {
 cases:=[]struct{modifier,category string; before,after bool}{
  {"+",vaulticDiffAdded,false,true},
  {"-",vaulticDiffRemoved,true,false},
  {"M",vaulticDiffModified,true,true},
  {"T",vaulticDiffModified,true,true},
  {"U",vaulticDiffModified,true,true},
 }
 for _,x:=range cases {
  if got:=vaulticDiffCategory(x.modifier);got!=x.category{
   t.Fatalf("modifier %q: %q != %q",x.modifier,got,x.category)
  }
  before,after:=vaulticDiffColumns(vaulticDiffRecord{Path:"/docs/test.txt",Modifier:x.modifier})
  if (before!="—")!=x.before || (after!="—")!=x.after {
   t.Fatalf("modifier %q, before %q, after %q",x.modifier,before,after)
  }
 }
}

func TestVaulticDiffSearchAndCategoryCounts(t *testing.T) {
 records:=[]vaulticDiffRecord{
  {Path:"/Alpha/three.txt",Modifier:"M"},
  {Path:"/Alpha/new.txt",Modifier:"+"},
  {Path:"/Beta/old.txt",Modifier:"-"},
  {Path:"/Alpha/type",Modifier:"T"},
 }
 checks:=[]struct{category,search string; expected int}{
  {vaulticDiffAll,"",4},
  {vaulticDiffAdded,"ALPHA",1},
  {vaulticDiffModified,"alpha",2},
  {vaulticDiffRemoved,"alpha",0},
  {vaulticDiffRemoved,"old.txt",1},
 }
 for _,x:=range checks{
  got:=vaulticFilterDiffChanges(records,x.category,x.search)
  if len(got)!=x.expected{t.Errorf("filter %q/%q: %d != %d",x.category,x.search,len(got),x.expected)}
 }
 sorted:=vaulticFilterDiffChanges(records,vaulticDiffAll,"")
 if len(sorted)!=4 || sorted[0].Path!="/Alpha/new.txt"{t.Fatalf("sort order: %v",sorted)}
 stats:=vaulticSnapshotComparison{TotalChanges:235,AddedChanges:200,ModifiedChanges:20,RemovedChanges:15}
 if count:=vaulticDiffFilterCount(stats,vaulticDiffRemoved);count!=15{t.Fatalf("removed count: %d",count)}
 if text:=vaulticDiffVisibleStatus(stats,vaulticDiffAdded,180,"");!strings.Contains(text,"180")||!strings.Contains(text,"200")||!strings.Contains(text,"limité"){t.Fatalf("missing truncation warning: %q",text)}
 if text:=vaulticDiffVisibleStatus(stats,vaulticDiffAdded,2,"file");!strings.Contains(text,"ne porte que"){t.Fatalf("search scope missing: %q",text)}
}

func TestVaulticDiffPathNewlineEscaped(t *testing.T) {
 text:=vaulticDiffDisplayPath("/folder/line\nbreak.txt")
 if strings.Contains(text,"\n")||!strings.Contains(text,"\\n"){t.Fatalf("unsafe display: %q",text)}
}

func TestVaulticDiffRestoreSelectionUsesCorrectSnapshot(t *testing.T){
 comparison:=vaulticSnapshotComparison{SourceID:"abcdef1234567890",TargetID:"fedcba1234567890"}
 cases:=[]struct{modifier,expected string}{
  {"+",comparison.TargetID},
  {"M",comparison.TargetID},
  {"-",comparison.SourceID},
  {"T",comparison.TargetID},
 }
 for _,tc:=range cases{
  id,p,err:=vaulticDiffRestoreSelection(comparison,vaulticDiffRecord{Path:"/Documents/report.txt",Modifier:tc.modifier})
  if err!=nil||id!=tc.expected||p!="/Documents/report.txt"{t.Errorf("%q => %q %q %v",tc.modifier,id,p,err)}
 }
 if _,p,err:=vaulticDiffRestoreSelection(comparison,vaulticDiffRecord{Path:"/Folder/",Modifier:"-"});err!=nil||p!="/Folder"{
  t.Fatalf("directory: %q %v",p,err)
 }
 for _,invalid:=range []string{"/","relative/path","/folder/../secret","/bad\x00file"} {
  if _,_,err:=vaulticDiffRestoreSelection(comparison,vaulticDiffRecord{Path:invalid,Modifier:"+"});err==nil{
   t.Errorf("unsafe path accepted: %q",invalid)
  }
 }
}

func TestVaulticDiffRefusesTruncatedRestoration(t *testing.T){
 c:=vaulticSnapshotComparison{SourceID:"abcdef1234567890",TargetID:"fedcba1234567890"}
 _,_,err:=vaulticDiffRestoreSelection(c,vaulticDiffRecord{Path:"/long…",Modifier:"+",Truncated:true})
 if err==nil{t.Fatal("truncated path could be restored")}
}
