package main

import (
 "fmt"
 "sort"
 "strconv"
 "strings"

 "fyne.io/fyne/v2"
 "fyne.io/fyne/v2/container"
 "fyne.io/fyne/v2/dialog"
 "fyne.io/fyne/v2/theme"
 "fyne.io/fyne/v2/widget"
)

const (
 vaulticDiffAll = "Toutes"
 vaulticDiffAdded = "Ajoutés"
 vaulticDiffModified = "Modifiés"
 vaulticDiffRemoved = "Supprimés"
)

func vaulticDiffCategory(modifier string) string {
 switch {
 case strings.Contains(modifier, "+"): return vaulticDiffAdded
 case strings.Contains(modifier, "-"): return vaulticDiffRemoved
 default: return vaulticDiffModified
 }
}

func vaulticDiffFilterCount(c vaulticSnapshotComparison, category string) int {
 switch category {
 case vaulticDiffAdded: return c.AddedChanges
 case vaulticDiffModified: return c.ModifiedChanges
 case vaulticDiffRemoved: return c.RemovedChanges
 default: return c.TotalChanges
 }
}

// Filter only already loaded paths. Full totals are kept by the streaming engine.
func vaulticFilterDiffChanges(changes []vaulticDiffRecord, category, search string) []vaulticDiffRecord {
 search = strings.ToLower(strings.TrimSpace(search))
 filtered := make([]vaulticDiffRecord, 0, len(changes))
 for _, change := range changes {
  if category != vaulticDiffAll && vaulticDiffCategory(change.Modifier) != category { continue }
  if search != "" && !strings.Contains(strings.ToLower(change.Path), search) { continue }
  filtered = append(filtered, change)
 }
 sort.SliceStable(filtered, func(i,j int)bool{
  return strings.ToLower(filtered[i].Path)<strings.ToLower(filtered[j].Path)
 })
 return filtered
}


// Restore removed elements from Before; additions/modifications from After.
// Restic directory changes sometimes end in '/', which is only stripped here.
func vaulticDiffRestoreSelection(c vaulticSnapshotComparison,entry vaulticDiffRecord)(string,string,error){
    if entry.Truncated{return "","",fmt.Errorf("Chemin trop long pour une restauration directe. Ouvrez l'instantané dans l'explorateur.")}
    itemPath:=strings.TrimSuffix(entry.Path,"/")
    if itemPath=="/" || vaulticValidateSnapshotPath(itemPath)!=nil {
        return "","",fmt.Errorf("Chemin impossible à restaurer : %q",entry.Path)
    }
    id:=c.TargetID
    if vaulticDiffCategory(entry.Modifier)==vaulticDiffRemoved{id=c.SourceID}
    if !vaulticSnapshotID.MatchString(id){return "","",fmt.Errorf("Identifiant d'instantané invalide")}
    return id,itemPath,nil
}

func vaulticDiffDisplayPath(p string)string{return strconv.Quote(p)}

func vaulticDiffColumns(change vaulticDiffRecord)(before,after string){
 item:=vaulticDiffDisplayPath(change.Path)
 switch vaulticDiffCategory(change.Modifier){
 case vaulticDiffAdded: return "—","+  "+item
 case vaulticDiffRemoved: return "−  "+item,"—"
 default: return "●  "+item,"●  "+item
 }
}

func vaulticDiffRowDescription(change vaulticDiffRecord)string{
 status:="Contenu modifié"
 switch {
 case vaulticDiffCategory(change.Modifier)==vaulticDiffAdded:
  status="Ajouté dans l'instantané après"
 case vaulticDiffCategory(change.Modifier)==vaulticDiffRemoved:
  status="Présent seulement dans l'instantané avant"
 case strings.Contains(change.Modifier,"T"): status="Type de fichier modifié"
 case strings.Contains(change.Modifier,"U"): status="Métadonnées modifiées"
 }
 return status+" · "+vaulticDiffDisplayPath(change.Path)
}

func vaulticDiffVisibleStatus(c vaulticSnapshotComparison,category string,displayed int,search string)string{
 total:=vaulticDiffFilterCount(c,category)
 if strings.TrimSpace(search)!=""{
  return fmt.Sprintf("%d résultat(s) parmi les chemins chargés · %d modification(s) dans cette catégorie. La recherche ne porte que sur l'aperçu.",displayed,total)
 }
 if displayed<total{
  return fmt.Sprintf("%d modification(s) affichée(s) sur %d · aperçu limité à %d par catégorie.",displayed,total,vaulticDiffPreviewLimit)
 }
 return fmt.Sprintf("%d modification(s) affichée(s) sur %d.",displayed,total)
}

// One virtualized table synchronizes the Before and After columns when scrolling.
func vaulticShowVisualComparison(app fyne.App, comparison vaulticSnapshotComparison, openForRestore func(snapshotID,filepath string)) {
 w:=app.NewWindow("Vaultic — Avant / Après")
 w.Resize(fyne.NewSize(1060,740))
 heading:=vaulticHeading("Comparer les instantanés",25)
 before:=widget.NewLabelWithStyle("AVANT  ·  "+comparison.SourceID[:min(len(comparison.SourceID),12)],fyne.TextAlignLeading,fyne.TextStyle{Bold:true})
 after:=widget.NewLabelWithStyle("APRÈS  ·  "+comparison.TargetID[:min(len(comparison.TargetID),12)],fyne.TextAlignLeading,fyne.TextStyle{Bold:true})
 headers:=container.NewGridWithColumns(2,vaulticSurfaceRaised(before),vaulticSurfaceRaised(after))
 stats:=container.NewGridWithColumns(3,
  vaulticStatCard("AJOUTÉS",vaulticHeading(fmt.Sprintf("%d",comparison.AddedChanges),23)),
  vaulticStatCard("MODIFIÉS",vaulticHeading(fmt.Sprintf("%d",comparison.ModifiedChanges),23)),
  vaulticStatCard("SUPPRIMÉS",vaulticHeading(fmt.Sprintf("%d",comparison.RemovedChanges),23)),
 )
 category:=widget.NewSelect([]string{vaulticDiffAll,vaulticDiffAdded,vaulticDiffModified,vaulticDiffRemoved},nil)
 category.SetSelected(vaulticDiffAll)
 query:=widget.NewEntry()
 query.SetPlaceHolder("Rechercher un chemin dans l'aperçu…")
 rows:=vaulticFilterDiffChanges(comparison.Changes,vaulticDiffAll,"")
 details:=vaulticWrappedLabel("Sélectionnez une ligne pour afficher le type de changement et le chemin.")
 restoreSelected:=widget.NewButtonWithIcon("Ouvrir pour restaurer…",theme.FolderOpenIcon(),nil)
 restoreSelected.Disable()
 var selected *vaulticDiffRecord
 restoreSelected.OnTapped=func(){
    if selected==nil || openForRestore==nil{return}
    id,focused,err:=vaulticDiffRestoreSelection(comparison,*selected)
    if err!=nil{
        dialog.ShowError(err,w)
        return
    }
    openForRestore(id,focused)
 }
 counter:=vaulticWrappedLabel(vaulticDiffVisibleStatus(comparison,vaulticDiffAll,len(rows),""))
 table:=widget.NewTable(
  func()(int,int){return len(rows),2},
  func()fyne.CanvasObject{
   label:=widget.NewLabel("")
   label.Truncation=fyne.TextTruncateEllipsis
   return label
  },
  func(cell widget.TableCellID,obj fyne.CanvasObject){
   if cell.Row<0||cell.Row>=len(rows){return}
   left,right:=vaulticDiffColumns(rows[cell.Row])
   if cell.Col==0{obj.(*widget.Label).SetText(left)}else{obj.(*widget.Label).SetText(right)}
  },
 )
 table.SetColumnWidth(0,450)
 table.SetColumnWidth(1,450)
 table.OnSelected=func(cell widget.TableCellID){
  if cell.Row<0||cell.Row>=len(rows){return}
  entry:=rows[cell.Row]
  selected=&entry
  details.SetText(vaulticDiffRowDescription(entry))
  if openForRestore!=nil && !entry.Truncated{restoreSelected.Enable()}else{restoreSelected.Disable()}
 }
 refresh:=func(){
  rows=vaulticFilterDiffChanges(comparison.Changes,category.Selected,query.Text)
  table.UnselectAll()
  selected=nil
  restoreSelected.Disable()
  table.Refresh()
  details.SetText("Sélectionnez une ligne pour afficher le type de changement et le chemin.")
  counter.SetText(vaulticDiffVisibleStatus(comparison,category.Selected,len(rows),query.Text))
 }
 category.OnChanged=func(string){refresh()}
 query.OnChanged=func(string){refresh()}
 content:=container.NewBorder(
  container.NewVBox(
   heading,
   vaulticCaption("Deux instantanés comparés par le moteur restic intégré."),
   stats,
   container.NewBorder(nil,nil,category,nil,query),
   headers,
  ),
  container.NewVBox(
   details,
   restoreSelected,
   counter,
   vaulticCaption("Lecture seule · la restauration s'effectue dans le navigateur du snapshot après confirmation."),
   vaulticCaption("Éléments inchangés non affichés · métadonnées seules non comparées"),
  ),
  nil,nil,
  vaulticSurface(table),
 )
 w.SetContent(container.NewPadded(content))
 w.Show()
}
