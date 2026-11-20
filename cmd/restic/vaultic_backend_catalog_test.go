package main

import (
 "testing"

 "github.com/restic/restic/internal/backend/all"
 "github.com/restic/restic/internal/backend/location"
)

func TestVaulticBackendCatalogCorrespondsToEngine(t *testing.T){
 registry:=all.Backends()
 if len(vaulticBackendCatalog)!=9{t.Fatalf("expected 9 supported backends, got %d",len(vaulticBackendCatalog))}
 names:=make(map[string]bool)
 for _,item:=range vaulticBackendCatalog{
  if names[item.Scheme]{t.Fatalf("duplicate backend %q",item.Scheme)}
  names[item.Scheme]=true
  if registry.Lookup(item.Scheme)==nil{t.Fatalf("backend %q not registered by restic",item.Scheme)}
  parsed,err:=location.Parse(registry,item.Example)
  if err!=nil{t.Fatalf("invalid example for %q: %v",item.Scheme,err)}
  if parsed.Scheme!=item.Scheme{t.Fatalf("backend %q recognized as %q",item.Scheme,parsed.Scheme)}
  got,ok:=vaulticBackendByLabel(item.Label)
  if !ok||got.Scheme!=item.Scheme{t.Fatalf("label lookup failed for %q",item.Scheme)}
  if vaulticBackendDescription(item)==""{t.Fatalf("empty description: %q",item.Scheme)}
 }
 if _,ok:=vaulticBackendByScheme("random");ok{t.Fatal("unknown backend accepted")}
}

func TestVaulticBackendGuessFromLocation(t *testing.T){
 for _,tc:=range []struct{input,want string}{
  {"D:\\Vaultic\\Repo","local"},
  {"sftp:user@host:/backup","sftp"},
  {"rest:https://server.example/repo","rest"},
  {"s3:s3.amazonaws.com/bucket/vaultic","s3"},
  {"rclone:remote:path","rclone"},
 }{
  backend,ok:=vaulticBackendForAddress(tc.input)
  if !ok||backend.Scheme!=tc.want{t.Fatalf("%q recognized as %+v (%v)",tc.input,backend,ok)}
 }
}
