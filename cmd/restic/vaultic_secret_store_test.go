package main

import (
	"errors"
	"strings"
	"testing"
)

func TestVaulticSecretKeyIsStableOpaqueAndPerRepository(t *testing.T) {
	a := vaulticSecretKey("s3:s3.example.invalid/bucket/a")
	if a != vaulticSecretKey(" s3:s3.example.invalid/bucket/a ") {
		t.Fatal("the key must ignore surrounding spaces")
	}
	if a == vaulticSecretKey("s3:s3.example.invalid/bucket/b") {
		t.Fatal("two repositories must not share a credential")
	}
	if !strings.HasPrefix(a, "Vaultic:") || strings.Contains(a, "bucket") || strings.Contains(a, "example") {
		t.Fatalf("the key must not reveal the address: %q", a)
	}
}

func TestVaulticMemorySecretStoreContract(t *testing.T) {
	s := newVaulticMemorySecretStore()
	if _, err := s.Get("k"); !errors.Is(err, errSecretNotFound) {
		t.Fatalf("missing secret: %v", err)
	}
	if err := s.Set("k", "pw"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get("k"); err != nil || got != "pw" {
		t.Fatalf("get: %q %v", got, err)
	}
	if err := s.Delete("k"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("k"); err != nil {
		t.Fatalf("deleting a missing secret is not an error: %v", err)
	}
	s.unavailable = true
	if s.Available() || s.Set("k", "x") == nil {
		t.Fatal("an unavailable store must refuse")
	}
}

func TestVaulticSecretToolArgumentsNeverContainTheSecret(t *testing.T) {
	key := vaulticSecretKey("local:/repo")
	for _, op := range []string{"store", "lookup", "clear"} {
		args := vaulticSecretToolArgs(op, key)
		if len(args) == 0 || args[0] != op {
			t.Fatalf("%s: %v", op, args)
		}
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "service vaultic account "+key) {
			t.Fatalf("%s must identify the credential: %v", op, args)
		}
	}
	if vaulticSecretToolArgs("bogus", key) != nil {
		t.Fatal("unknown operation")
	}
}

func TestVaulticSystemSecretStoreReportsAvailability(t *testing.T) {
	s := newVaulticSystemSecretStore()
	if s.Name() == "" {
		t.Fatal("the store needs a name for the interface")
	}
	_ = s.Available() // platform dependent; must not panic
}
