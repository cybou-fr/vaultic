package main

import (
    "errors"
    "fmt"
    "strings"
    "unicode/utf8"
)

// These operations edit Vaultic's settings only. They never access, rename,
// delete or mutate any restic repository or snapshot.
func vaulticValidateSavedName(name string) (string,error) {
    name=strings.TrimSpace(name)
    if name=="" || utf8.RuneCountInString(name)>40 || strings.ContainsAny(name,"\r\n\x00") {
        return "",errors.New("Indiquez un nom de 1 à 40 caractères, sur une seule ligne.")
    }
    return name,nil
}

func vaulticRenameSavedDepot(depots []vaulticDepot,location,name string)([]vaulticDepot,error){
    label,err:=vaulticValidateSavedName(name)
    if err!=nil{return nil,err}
    index:=-1
    for i,depot:=range depots {
        if depot.Location==location{index=i}
        if depot.Location!=location && strings.EqualFold(strings.TrimSpace(depot.Name),label){
            return nil,errors.New("Ce nom de dépôt est déjà utilisé.")
        }
    }
    if index<0{return nil,errors.New("Le dépôt n'est plus enregistré.")}
    out:=append([]vaulticDepot(nil),depots...)
    out[index].Name=label
    return out,nil
}

func vaulticForgetSavedDepot(depots []vaulticDepot,active,location string)([]vaulticDepot,string,error){
    index:=-1
    for i,depot:=range depots {if depot.Location==location{index=i;break}}
    if index<0{return nil,active,errors.New("Le dépôt n'est plus enregistré.")}
    out:=append([]vaulticDepot(nil),depots[:index]...)
    out=append(out,depots[index+1:]...)
    if active==location{
        active=""
        if len(out)>0{active=out[0].Location}
    }
    return out,active,nil
}

func vaulticDuplicateSavedProfile(profiles []vaulticBackupProfile,index int,name string)([]vaulticBackupProfile,int,error){
    if index<0||index>=len(profiles){return nil,0,errors.New("Le profil n'existe plus.")}
    label,err:=vaulticValidateSavedName(name)
    if err!=nil{return nil,0,err}
    for _,p:=range profiles{
        if strings.EqualFold(strings.TrimSpace(p.Name),label){
            return nil,0,fmt.Errorf("Le profil %q existe déjà.",label)
        }
    }
    original:=profiles[index]
    clone:=vaulticBackupProfile{
        Name:label,
        Sources:append([]string(nil),original.Sources...),
        Excludes:append([]string(nil),original.Excludes...),
    }
    out:=append(append([]vaulticBackupProfile(nil),profiles...),clone)
    return out,len(out)-1,nil
}

func vaulticDeleteSavedProfile(profiles []vaulticBackupProfile,index int)([]vaulticBackupProfile,int,error){
    if len(profiles)<=1{return nil,0,errors.New("Il faut conserver au moins un profil de sauvegarde.")}
    if index<0||index>=len(profiles){return nil,0,errors.New("Le profil n'existe plus.")}
    out:=append([]vaulticBackupProfile(nil),profiles[:index]...)
    out=append(out,profiles[index+1:]...)
    next:=index
    if next>=len(out){next=len(out)-1}
    return out,next,nil
}

func vaulticSuggestedCopyName(profiles []vaulticBackupProfile,index int)string{
    if index<0||index>=len(profiles){return "Copie"}
    base:="Copie de "+profiles[index].Name
    for suffix:=1;suffix<10000;suffix++{
        tail:=""
        if suffix>1 {tail=fmt.Sprintf(" (%d)",suffix)}
        runes:=[]rune(base)
        if len(runes)>40-utf8.RuneCountInString(tail){runes=runes[:40-utf8.RuneCountInString(tail)]}
        candidate:=strings.TrimSpace(string(runes))+tail
        exists:=false
        for _,p:=range profiles{if strings.EqualFold(p.Name,candidate){exists=true;break}}
        if !exists{return candidate}
    }
    return "Copie"
}
