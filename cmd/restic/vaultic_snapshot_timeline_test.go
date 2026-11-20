package main

import (
 "strings"
 "testing"
 "time"
)

func TestVaulticTimelineStartsNewDayOnce(t *testing.T){
 base:=time.Date(2026,11,20,18,40,0,0,time.Local)
 first:=vaulticSnapshot{ID:"abcdef1234567890",ShortID:"abcdef12",Hostname:"PC",Time:base}
 second:=vaulticSnapshot{ID:"fedcba1234567890",ShortID:"fedcba12",Hostname:"PC",Time:base.Add(-time.Minute*50)}
 previousDay:=vaulticSnapshot{ID:"0000aaaabbbbcccc",ShortID:"0000aaaa",Time:base.Add(-time.Hour*28)}
 header,description:=vaulticTimelineRow(first,nil,nil)
 if !strings.Contains(header,"●")||!strings.Contains(header,"20/11/2026")||!strings.Contains(description,"abcdef12"){t.Fatalf("first row: %q %q",header,description)}
 header,_=vaulticTimelineRow(second,&first,nil)
 if strings.Contains(header,"●") || !strings.Contains(header,"│"){t.Fatalf("same day should continue timeline: %q",header)}
 header,_=vaulticTimelineRow(previousDay,&second,nil)
 if !strings.Contains(header,"●"){t.Fatalf("new day marker missing: %q",header)}
 metrics:=&vaulticSnapshotMetrics{Files:3,LogicalFileBytes:4096}
 _,description=vaulticTimelineRow(first,nil,metrics)
 if !strings.Contains(description,"3 fichiers"){t.Fatalf("metrics missing: %q",description)}
}
