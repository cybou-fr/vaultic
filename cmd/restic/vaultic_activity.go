package main

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const (
	vaulticLogLimit = 16000 // characters kept before trimming
	vaulticLogKeep  = 12000 // characters kept after trimming
)

// vaulticAppendLogText appends one timestamped line and trims the oldest text
// once the buffer exceeds its limit. Blank messages are ignored.
func vaulticAppendLogText(text, message string, now time.Time) string {
	if strings.TrimSpace(message) == "" {
		return text
	}
	text += now.Format("15:04:05") + "  " + message + "\n"
	if len(text) > vaulticLogLimit {
		text = text[len(text)-vaulticLogKeep:]
	}
	return text
}

// vaulticLogView is the read-only session log shown on the Activité page.
type vaulticLogView struct {
	Entry *widget.Entry
	text  string
}

func newVaulticLogView() *vaulticLogView {
	entry := widget.NewMultiLineEntry()
	entry.SetMinRowsVisible(4)
	entry.SetPlaceHolder("Aucun événement dans cette session.")
	entry.Wrapping = fyne.TextWrapWord
	entry.Disable()
	return &vaulticLogView{Entry: entry}
}

// Append adds a line to the log. It must run on the UI thread.
func (l *vaulticLogView) Append(message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	l.text = vaulticAppendLogText(l.text, message, time.Now())
	l.Entry.SetText(l.text)
}

// Clear empties the log.
func (l *vaulticLogView) Clear() {
	l.text = ""
	l.Entry.SetText("")
}

// vaulticStatusPanel groups the status line, the progress bar and the cancel
// button shown under every page.
type vaulticStatusPanel struct {
	Status   *widget.Label
	Progress *widget.ProgressBar
	Cancel   *widget.Button

	placeholder *canvas.Rectangle
	slot        *fyne.Container
}

func newVaulticStatusPanel(onCancel func()) *vaulticStatusPanel {
	p := &vaulticStatusPanel{Status: widget.NewLabel("Prêt"), Progress: widget.NewProgressBar()}
	p.Status.Wrapping = fyne.TextWrapWord
	p.Progress.Hide()
	p.placeholder = canvas.NewRectangle(vaulticBackground)
	p.placeholder.SetMinSize(fyne.NewSize(0, p.Progress.MinSize().Height))
	p.placeholder.Hide()
	p.slot = container.NewStack(p.placeholder, p.Progress)
	p.slot.Hide()
	p.Cancel = widget.NewButton("Arrêter", onCancel)
	p.Cancel.Hide()
	return p
}

// Bar returns the strip placed under the pages.
func (p *vaulticStatusPanel) Bar() fyne.CanvasObject {
	return vaulticStatusBar(p.slot, p.Status, p.Cancel)
}

// Reserve shows the empty progress area so the layout does not jump when the
// first progress event arrives.
func (p *vaulticStatusPanel) Reserve() {
	p.placeholder.Show()
	p.slot.Show()
}

// SetProgress shows the bar at value and reports whether it just appeared.
func (p *vaulticStatusPanel) SetProgress(value float64) (appeared bool) {
	p.Progress.SetValue(value)
	if !p.Progress.Visible() {
		p.Progress.Show()
		p.slot.Show()
		return true
	}
	return false
}

// Busy shows the cancel button; Idle hides it and the progress area.
func (p *vaulticStatusPanel) Busy() {
	p.Cancel.Enable()
	p.Cancel.Show()
}

func (p *vaulticStatusPanel) Idle() {
	p.Cancel.Hide()
	p.Progress.Hide()
	p.placeholder.Hide()
	p.slot.Hide()
}

// vaulticEventView is what a restic JSON event means for the interface.
type vaulticEventView struct {
	HasProgress bool
	Progress    float64
	Status      string // new status text, "" to leave it unchanged
	Log         string // replacement line to log; "" keeps the original line
	Suppress    bool   // the event must not be logged at all
}

// vaulticInterpretEvent turns one engine event into interface updates. title
// is the running operation; isBackup tells whether a backup report collects the
// events (the report then owns the final summary). Events that need no
// interpretation leave Log empty and Suppress false: the caller logs the
// original line.
func vaulticInterpretEvent(e vaulticEngineEvent, title string, isBackup bool) vaulticEventView {
	var view vaulticEventView
	switch e.MessageType {
	case "status":
		if e.TotalBytes > 0 && e.PercentDone >= 0 && e.PercentDone <= 1 {
			view.HasProgress, view.Progress = true, e.PercentDone
		}
		if isBackup {
			files := fmt.Sprintf("%d fichiers", e.FilesDone)
			if e.TotalFiles > 0 {
				files = fmt.Sprintf("%d / %d fichiers", e.FilesDone, e.TotalFiles)
			}
			metrics := files + " · " + vaulticBytes(e.BytesDone)
			if e.TotalBytes > 0 {
				metrics += " / " + vaulticBytes(e.TotalBytes)
			}
			if e.SecondsElapsed > 0 {
				metrics += " · " + vaulticBytes(e.BytesDone/e.SecondsElapsed) + "/s"
			}
			view.Status = metrics
		} else if e.TotalFiles > 0 {
			view.Status = fmt.Sprintf("%s en cours · %d / %d fichiers", title, e.FilesDone+e.FilesRestored, e.TotalFiles)
		}
		view.Suppress = true
	case "summary":
		if isBackup {
			view.Suppress = true
			return view
		}
		if e.SnapshotID != "" {
			view.Log = fmt.Sprintf("Instantané %s · %d fichier(s) traité(s), %d octets", e.SnapshotID, e.FilesProcessed, e.BytesProcessed)
		} else if e.TotalFiles > 0 {
			view.Log = fmt.Sprintf("%d élément(s) restauré(s) sur %d · %d octets", e.FilesRestored, e.TotalFiles, e.BytesRestored)
		} else {
			view.Suppress = true
		}
	case "error":
		view.Log = e.Error.Message
	}
	return view
}
