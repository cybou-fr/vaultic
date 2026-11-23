package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// A rotating log on disk, so that a problem that happened at night, or a crash,
// can be explained afterwards. It holds the same messages as the Activity page
// plus start/stop and crash records. Messages are single-line, length-limited
// and pass through a secret filter; the file is private to the user (0600).

const (
	vaulticLogFileSize  = 1 << 20 // rotate after 1 MiB
	vaulticLogFileKeep  = 5       // vaultic.log + 4 older files
	vaulticLogLineLimit = 2000
)

type vaulticLogFile struct {
	mu      sync.Mutex
	dir     string
	path    string
	maxSize int64
	keep    int
	now     func() time.Time
	secrets []string
}

// vaulticLogDir is the folder of the log, next to the settings file.
func vaulticLogDir(settingsPath string) string {
	return filepath.Join(filepath.Dir(settingsPath), "logs")
}

func newVaulticLogFile(dir string) *vaulticLogFile {
	return &vaulticLogFile{dir: dir, path: filepath.Join(dir, "vaultic.log"), maxSize: vaulticLogFileSize, keep: vaulticLogFileKeep, now: time.Now}
}

// Dir is the folder that holds the log files.
func (l *vaulticLogFile) Dir() string { return l.dir }

// Path is the current log file.
func (l *vaulticLogFile) Path() string { return l.path }

// AddSecret makes the log replace a value (a password, a key) by a marker.
func (l *vaulticLogFile) AddSecret(value string) {
	if len(value) < 4 { // very short values would blank out ordinary text
		return
	}
	l.mu.Lock()
	l.secrets = append(l.secrets, value)
	l.mu.Unlock()
}

var (
	vaulticLogControl = regexp.MustCompile(`[\x00-\x08\x0b-\x1f\x7f]`)
	// credentials in URLs and key=value pairs that look like secrets
	vaulticLogURLUser = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^/\s@]+@`)
	vaulticLogBearer  = regexp.MustCompile(`(?i)\b(bearer|basic)\s+\S+`)
	vaulticLogKeyed   = regexp.MustCompile(`(?i)\b(password|passwd|secret|token|sas|key|authorization)\b(\s*[=:]\s*)(?:(?:bearer|basic)\s+)?\S+`)
)

// cleanLine makes one safe log line out of a message.
func (l *vaulticLogFile) cleanLine(message string, limit int) string {
	for _, secret := range l.secrets {
		message = strings.ReplaceAll(message, secret, "[hidden]")
	}
	message = vaulticLogURLUser.ReplaceAllString(message, "${1}[hidden]@")
	message = vaulticLogBearer.ReplaceAllString(message, "${1} [hidden]")
	message = vaulticLogKeyed.ReplaceAllString(message, "${1}${2}[hidden]")
	message = strings.ReplaceAll(message, "\r\n", " | ")
	message = strings.ReplaceAll(message, "\n", " | ")
	message = vaulticLogControl.ReplaceAllString(message, "")
	if runes := []rune(message); len(runes) > limit {
		message = string(runes[:limit]) + "…"
	}
	return message
}

// Write appends one line. Problems with the log itself never reach the user's
// operation: a failing log is silently skipped.
func (l *vaulticLogFile) Write(level, message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	limit := vaulticLogLineLimit
	if level == "panic" {
		limit = 16000 // a stack trace must survive
	}
	line := fmt.Sprintf("%s [%s] %s\n", l.now().Format("2006-01-02 15:04:05.000"), level, l.cleanLine(message, limit))
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return
	}
	l.rotateLocked(int64(len(line)))
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
}

// rotateLocked renames vaultic.log to vaultic.log.1 (and so on) when the next
// line would make it larger than the limit. The oldest file is dropped.
func (l *vaulticLogFile) rotateLocked(next int64) {
	info, err := os.Stat(l.path)
	if err != nil || info.Size()+next <= l.maxSize {
		return
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", l.path, l.keep-1))
	for i := l.keep - 2; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", l.path, i), fmt.Sprintf("%s.%d", l.path, i+1))
	}
	_ = os.Rename(l.path, l.path+".1")
}

// Info logs an ordinary message.
func (l *vaulticLogFile) Info(message string) { l.Write("info", message) }

// Start records the program's identity at launch.
func (l *vaulticLogFile) Start(language string) {
	l.Write("info", fmt.Sprintf("Vaultic %s started (%s/%s, language %s)", vaulticVersionLine(), runtime.GOOS, runtime.GOARCH, language))
}

// RecoverAndRepanic records an unrecovered panic with its stack, then lets it
// continue. Use it as the first deferred call: defer log.RecoverAndRepanic().
func (l *vaulticLogFile) RecoverAndRepanic() {
	if r := recover(); r != nil {
		l.Write("panic", fmt.Sprintf("%v | %s", r, string(debug.Stack())))
		panic(r)
	}
}
