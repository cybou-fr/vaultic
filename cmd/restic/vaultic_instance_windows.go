//go:build windows

package main

import (
	"errors"
	"sync"

	"golang.org/x/sys/windows"
)

// vaulticAcquireInstance takes the per-settings guard. When another instance
// already holds it, it asks that instance to show its window and reports
// running=true; the caller must then exit.
func vaulticAcquireInstance(key string) (inst vaulticInstance, running bool, err error) {
	mutexName, err := windows.UTF16PtrFromString(`Local\VaulticInstance-` + key)
	if err != nil {
		return nil, false, err
	}
	eventName, err := windows.UTF16PtrFromString(`Local\VaulticShow-` + key)
	if err != nil {
		return nil, false, err
	}
	// An auto-reset event: one SetEvent wakes the waiting instance once.
	event, err := windows.CreateEvent(nil, 0, 0, eventName)
	if event == 0 {
		return nil, false, err
	}
	mutex, err := windows.CreateMutex(nil, false, mutexName)
	if mutex == 0 {
		_ = windows.CloseHandle(event)
		return nil, false, err
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.SetEvent(event) // wake the running instance
		_ = windows.CloseHandle(event)
		_ = windows.CloseHandle(mutex)
		return nil, true, nil
	}
	return &vaulticWindowsInstance{mutex: mutex, event: event, stop: make(chan struct{})}, false, nil
}

type vaulticWindowsInstance struct {
	mutex, event windows.Handle
	stop         chan struct{}
	once         sync.Once
}

func (i *vaulticWindowsInstance) OnShow(show func()) {
	go func() {
		for {
			select {
			case <-i.stop:
				return
			default:
			}
			// Wake up every second to notice Release.
			r, _ := windows.WaitForSingleObject(i.event, 1000)
			if r == windows.WAIT_OBJECT_0 {
				show()
			}
		}
	}()
}

func (i *vaulticWindowsInstance) Release() {
	i.once.Do(func() {
		close(i.stop)
		_ = windows.CloseHandle(i.mutex)
		_ = windows.CloseHandle(i.event)
	})
}
