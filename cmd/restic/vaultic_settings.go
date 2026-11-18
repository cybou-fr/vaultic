package main

import (
    "encoding/json"
    "os"
    "path/filepath"
    "strings"
)

type vaulticSettings struct {
    Repository string `json:"repository"`
    Sources []string `json:"sources"`
}

func vaulticSettingsPath() string {
    if exe,err:=os.Executable(); err==nil {
        dir:=filepath.Dir(exe)
        if _,err:=os.Stat(filepath.Join(dir,"portable.flag")); err==nil {
            return filepath.Join(dir,"vaultic.json")
        }
    }
    dir,err:=os.UserConfigDir()
    if err!=nil { dir="." }
    return filepath.Join(dir,"Vaultic","settings.json")
}

func vaulticLoadSettings() vaulticSettings {
    var s vaulticSettings
    if data,err:=os.ReadFile(vaulticSettingsPath()); err==nil { _=json.Unmarshal(data,&s) }
    return s
}

func vaulticSaveSettings(s vaulticSettings) error {
    path:=vaulticSettingsPath()
    if err:=os.MkdirAll(filepath.Dir(path),0700);err!=nil{return err}
    if strings.TrimSpace(s.Repository)=="" { s.Repository="" }
    data,err:=json.MarshalIndent(s,"","  ")
    if err!=nil{return err}
    f,err:=os.CreateTemp(filepath.Dir(path),".vaultic-*")
    if err!=nil{return err}
    defer os.Remove(f.Name())
    if err=f.Chmod(0600);err!=nil{_ = f.Close();return err}
    if _,err=f.Write(data);err!=nil{_ = f.Close();return err}
    if err=f.Close();err!=nil{return err}
    return os.Rename(f.Name(),path)
}
