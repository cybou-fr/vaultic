package main

import (
 "strings"
 "testing"
)

func TestVaulticGuidedStorageAddresses(t *testing.T) {
 tests := []struct{ form vaulticGuidedStorageForm; want string }{
  {vaulticGuidedStorageForm{Scheme:"local",Path:"/tmp/vaultic-test"}, "local:/tmp/vaultic-test"},
  {vaulticGuidedStorageForm{Scheme:"rest",Path:"https://backup.example/repo"}, "rest:https://backup.example/repo"},
  {vaulticGuidedStorageForm{Scheme:"sftp",Username:"backup",Host:"host.example",Path:"/vaultic"}, "sftp:backup@host.example:/vaultic"},
  {vaulticGuidedStorageForm{Scheme:"gs",Bucket:"my-bucket",Prefix:"vaultic/primary"}, "gs:my-bucket:/vaultic/primary"},
  {vaulticGuidedStorageForm{Scheme:"swift",Bucket:"my-container",Prefix:"backup"}, "swift:my-container:/backup"},
  {vaulticGuidedStorageForm{Scheme:"rclone",Remote:"cloud",Prefix:"vaultic"}, "rclone:cloud:vaultic"},
 }
 for _, tc := range tests {
  got,err := vaulticBuildGuidedStorageAddress(tc.form)
  if err != nil || got != tc.want { t.Errorf("%s: got %q, error %v, want %q",tc.form.Scheme,got,err,tc.want) }
 }
}

func TestVaulticGuidedStorageRejectsUnsafeInputs(t *testing.T) {
 tests := []vaulticGuidedStorageForm{
  {Scheme:"rest",Path:"http://backup.example/repo"},
  {Scheme:"rest",Path:"https://user:secret@backup.example/repo"},
  {Scheme:"rest",Path:"https://backup.example/repo?token=abc"},
  {Scheme:"rest",Path:"https://backup.example/repo#secret"},
  {Scheme:"sftp",Username:"user@evil",Host:"host",Path:"/repo"},
  {Scheme:"sftp",Username:"user",Host:"host",Path:"relative"},
  {Scheme:"sftp",Username:"user",Host:"host",Path:"/repo",Port:"2222"},
  {Scheme:"gs",Bucket:"bucket",Prefix:"../escape"},
  {Scheme:"swift",Bucket:"container",Prefix:"folder//sub"},
  {Scheme:"rclone",Remote:"remote:other",Prefix:"repo"},
  {Scheme:"local",Path:""},
  {Scheme:"unknown",Path:"/repo"},
 }
 for _, form := range tests {
  address,err := vaulticBuildGuidedStorageAddress(form)
  if err == nil { t.Errorf("unsafe %s input accepted as %q",form.Scheme,address) }
 }
}

func TestVaulticGuidedStorageDoesNotIncludeCredentials(t *testing.T) {
 address,err := vaulticBuildGuidedStorageAddress(vaulticGuidedStorageForm{Scheme:"rest",Path:"https://backup.example/snapshots"})
 if err != nil { t.Fatal(err) }
 if strings.Contains(address,"password") || strings.Contains(address,"secret") { t.Fatalf("secret in location: %s",address) }
}
