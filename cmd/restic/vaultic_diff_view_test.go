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
