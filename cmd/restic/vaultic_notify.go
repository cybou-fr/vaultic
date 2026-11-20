package main

import (
	"sync"
	"time"
)

const (
	vaulticNotifyCompleted = "completed"
	vaulticNotifyFailed    = "failed"
	vaulticNotifySkipped   = "skipped"
	vaulticNotifyOverdue   = "overdue"

	// No more than vaulticNotifyBurst notifications in vaulticNotifyWindow,
	// and the same event key never twice within vaulticNotifyMemory.
	vaulticNotifyBurst  = 4
	vaulticNotifyWindow = 10 * time.Minute
	vaulticNotifyMemory = 24 * time.Hour
)

// vaulticNotifyPrefs selects which categories produce a desktop notification.
type vaulticNotifyPrefs struct {
	Completed bool `json:"completed"`
	Failed    bool `json:"failed"`
	Skipped   bool `json:"skipped"`
	Overdue   bool `json:"overdue"`
}

func vaulticDefaultNotifyPrefs() vaulticNotifyPrefs {
	return vaulticNotifyPrefs{Completed: true, Failed: true, Skipped: true, Overdue: true}
}

func (p vaulticNotifyPrefs) enabled(category string) bool {
	switch category {
	case vaulticNotifyCompleted:
		return p.Completed
	case vaulticNotifyFailed:
		return p.Failed
	case vaulticNotifySkipped:
		return p.Skipped
	case vaulticNotifyOverdue:
		return p.Overdue
	}
	return false
}

// vaulticNotifier decides whether a notification is sent. It is independent of
// the toolkit: the sender is injected, and so is the clock.
type vaulticNotifier struct {
	mu     sync.Mutex
	prefs  func() vaulticNotifyPrefs
	send   func(title, body string)
	now    func() time.Time
	seen   map[string]time.Time
	recent []time.Time
}

func newVaulticNotifier(prefs func() vaulticNotifyPrefs, send func(title, body string), now func() time.Time) *vaulticNotifier {
	return &vaulticNotifier{prefs: prefs, send: send, now: now, seen: map[string]time.Time{}}
}

// Notify sends the notification unless the category is disabled, the same
// event key was already notified, or too many notifications were just sent.
// It reports whether a notification was sent.
func (n *vaulticNotifier) Notify(category, key, title, body string) bool {
	if n == nil || n.send == nil {
		return false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.prefs().enabled(category) {
		return false
	}
	now := n.now()
	for k, at := range n.seen {
		if now.Sub(at) > vaulticNotifyMemory || at.After(now.Add(time.Hour)) {
			delete(n.seen, k)
		}
	}
	dedupe := category + "|" + key
	if _, done := n.seen[dedupe]; done {
		return false
	}
	kept := n.recent[:0]
	for _, at := range n.recent {
		if now.Sub(at) < vaulticNotifyWindow && !at.After(now) {
			kept = append(kept, at)
		}
	}
	n.recent = kept
	if len(n.recent) >= vaulticNotifyBurst {
		return false
	}
	n.seen[dedupe] = now
	n.recent = append(n.recent, now)
	n.send(title, body)
	return true
}
