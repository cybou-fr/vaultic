package main

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

// Fyne moves keyboard focus with Tab but never scrolls a Scroll container to
// show the newly focused control, so a keyboard user would tab into content
// that is off screen. These helpers keep the focused control visible.

// vaulticScrollIntoView scrolls so that obj lies inside the viewport of scroll.
func vaulticScrollIntoView(scroll *container.Scroll, obj fyne.CanvasObject) {
	d := fyne.CurrentApp().Driver()
	top := d.AbsolutePositionForObject(obj).Y - d.AbsolutePositionForObject(scroll).Y
	bottom := top + obj.Size().Height
	view := scroll.Size().Height
	const margin = 16
	offset := scroll.Offset.Y
	switch {
	case top < margin:
		offset += top - margin
	case bottom > view-margin:
		offset += bottom - view + margin
	default:
		return
	}
	if offset < 0 {
		offset = 0
	}
	if offset != scroll.Offset.Y {
		scroll.Offset.Y = offset
		scroll.Refresh()
	}
}

// vaulticInsideContent reports whether obj lies in the scrollable content of
// scroll (even when it is currently outside the viewport).
func vaulticInsideContent(scroll *container.Scroll, obj fyne.CanvasObject) bool {
	d := fyne.CurrentApp().Driver()
	if d.CanvasForObject(scroll) == nil || d.CanvasForObject(obj) == nil {
		return false
	}
	rel := d.AbsolutePositionForObject(obj).Subtract(d.AbsolutePositionForObject(scroll))
	return rel.X >= 0 && rel.X < scroll.Size().Width &&
		rel.Y >= -scroll.Offset.Y && rel.Y < scroll.Content.Size().Height-scroll.Offset.Y
}

// vaulticRevealFocused scrolls the container holding the focused control.
func vaulticRevealFocused(canvas fyne.Canvas, scrolls []*container.Scroll) {
	focused, ok := canvas.Focused().(fyne.CanvasObject)
	if !ok {
		return
	}
	for _, scroll := range scrolls {
		if scroll.Visible() && vaulticInsideContent(scroll, focused) {
			vaulticScrollIntoView(scroll, focused)
			return
		}
	}
}

// vaulticKeepFocusVisible follows the keyboard focus for the life of the window.
func vaulticKeepFocusVisible(w fyne.Window, scrolls []*container.Scroll) (stop func()) {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		var last fyne.Focusable
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				fyne.Do(func() {
					if f := w.Canvas().Focused(); f != last {
						last = f
						vaulticRevealFocused(w.Canvas(), scrolls)
					}
				})
			}
		}
	}()
	return func() { close(done) }
}
