package main

import (
 "fmt"

 "fyne.io/fyne/v2"
 "fyne.io/fyne/v2/container"
 "fyne.io/fyne/v2/widget"
)

// The history remains newest-first, while a new date marker visually starts
// each day's group. No additional repository requests are required.
func vaulticTimelineRow(snapshot vaulticSnapshot, previous *vaulticSnapshot, metrics *vaulticSnapshotMetrics)(string,string){
 date:=snapshot.Time.Local().Format("2006-01-02")
 newDay:=previous==nil || previous.Time.Local().Format("2006-01-02")!=date
 title:="│   "
 if newDay{title="●   "+snapshot.Time.Local().Format("02/01/2006")+"    "}
 title+=snapshot.Time.Local().Format("15:04")
 host:=snapshot.Hostname
 if host==""{host="Machine non renseignée"}
 shortID:=snapshot.ShortID
 if shortID==""{shortID=snapshot.ID[:min(len(snapshot.ID),12)]}
 subtitle:=fmt.Sprintf("      %s   ·   %s",shortID,host)
 if metrics!=nil{
  subtitle+=fmt.Sprintf("   ·   %d fichiers   ·   %s",metrics.Files,vaulticBytes(metrics.LogicalFileBytes))
 }
 return title,subtitle
}

func vaulticTimelineTemplate() fyne.CanvasObject{
 title:=widget.NewLabel("")
 title.TextStyle=fyne.TextStyle{Bold:true}
 title.Truncation=fyne.TextTruncateEllipsis
 description:=widget.NewLabel("")
 description.Truncation=fyne.TextTruncateEllipsis
 return container.NewVBox(title,description)
}

func vaulticUpdateTimelineRow(object fyne.CanvasObject,snapshot vaulticSnapshot,previous *vaulticSnapshot,metrics *vaulticSnapshotMetrics){
 labels,ok:=object.(*fyne.Container)
 if !ok||len(labels.Objects)<2{return}
 header,ok1:=labels.Objects[0].(*widget.Label)
 body,ok2:=labels.Objects[1].(*widget.Label)
 if !ok1||!ok2{return}
 title,detail:=vaulticTimelineRow(snapshot,previous,metrics)
 header.SetText(title)
 body.SetText(detail)
}
