//go:build !windows

package main

import (
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// A lock file (flock) is the guard and a Unix socket carries the "show your
// window" request. Both live in the temporary folder under a name derived from
// the settings file, so they vanish with the session. This path has been
// compiled for Linux and macOS but never run there.

func vaulticInstancePaths(key string) (lock, socket string) {
	dir := os.TempDir()
	return filepath.Join(dir, "vaultic-"+key+".lock"), filepath.Join(dir, "vaultic-"+key+".sock")
}

func vaulticAcquireInstance(key string) (inst vaulticInstance, running bool, err error) {
	lockPath, socketPath := vaulticInstancePaths(key)
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if err == syscall.EWOULDBLOCK {
			if conn, dialErr := net.Dial("unix", socketPath); dialErr == nil {
				_, _ = conn.Write([]byte("show\n"))
				_ = conn.Close()
			}
			return nil, true, nil
		}
		return nil, false, err
	}
	_ = os.Remove(socketPath) // a leftover from a crashed instance
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		_ = file.Close()
		return nil, false, err
	}
	_ = os.Chmod(socketPath, 0o600)
	return &vaulticUnixInstance{file: file, listener: listener, socket: socketPath}, false, nil
}

type vaulticUnixInstance struct {
	file     *os.File
	listener net.Listener
	socket   string
	once     sync.Once
}

func (i *vaulticUnixInstance) OnShow(show func()) {
	go func() {
		for {
			conn, err := i.listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
			show()
		}
	}()
}

func (i *vaulticUnixInstance) Release() {
	i.once.Do(func() {
		_ = i.listener.Close()
		_ = os.Remove(i.socket)
		_ = syscall.Flock(int(i.file.Fd()), syscall.LOCK_UN)
		_ = i.file.Close()
	})
}
