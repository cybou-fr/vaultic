package main

import (
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestVaulticInstanceKeyIsStablePerSettingsFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	if vaulticInstanceKey(p) != vaulticInstanceKey(p) || len(vaulticInstanceKey(p)) != 16 {
		t.Fatal("the key must be stable and short")
	}
	if vaulticInstanceKey(p) == vaulticInstanceKey(p+"2") {
		t.Fatal("different settings files need different guards (portable copies are independent)")
	}
}

func TestVaulticSecondInstanceIsRefusedAndAsksTheFirstToShow(t *testing.T) {
	key := "test-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + filepath.Base(t.TempDir())
	first, running, err := vaulticAcquireInstance(key)
	if err != nil || running || first == nil {
		t.Fatalf("the first instance must get the guard: %v %v", err, running)
	}
	defer first.Release()
	var shown atomic.Int32
	first.OnShow(func() { shown.Add(1) })

	second, running, err := vaulticAcquireInstance(key)
	if err != nil || !running || second != nil {
		t.Fatalf("the second instance must be told another one runs: %v %v", err, running)
	}
	deadline := time.Now().Add(5 * time.Second)
	for shown.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if shown.Load() == 0 {
		t.Fatal("the running instance was not asked to show its window")
	}
	// A different settings file is a different guard.
	other, running, err := vaulticAcquireInstance(key + "-other")
	if err != nil || running || other == nil {
		t.Fatalf("an independent guard must be free: %v %v", err, running)
	}
	other.Release()
}

func TestVaulticGuardIsReleasedAfterwards(t *testing.T) {
	key := "test-release-" + filepath.Base(t.TempDir())
	first, running, err := vaulticAcquireInstance(key)
	if err != nil || running {
		t.Fatal(err)
	}
	first.Release()
	first.Release() // releasing twice is harmless
	again, running, err := vaulticAcquireInstance(key)
	if err != nil || running || again == nil {
		t.Fatalf("after Release the guard must be free again: %v %v", err, running)
	}
	again.Release()
}
