package ui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// The live line is the single bottom-of-output line that a spinner or a
// progress bar redraws in place. Every regular print goes through write(),
// which erases the live line first and redraws it afterwards, so messages
// printed while a bar is running (a rotation notice, a Telegram chunk being
// sent, a warning) scroll up cleanly above it instead of being overwritten
// or garbling it.

type liveElement interface {
	// render returns the current single-line view, at most width cells wide.
	render(width int) string
}

var (
	outMu      sync.Mutex
	liveActive liveElement
	liveShown  bool
)

const eraseLine = "\r\x1b[2K"

// write prints s to w, keeping the live line (if any) at the bottom.
func write(w io.Writer, s string) {
	outMu.Lock()
	defer outMu.Unlock()
	hideLiveLocked()
	fmt.Fprint(w, s)
	drawLiveLocked()
}

func hideLiveLocked() {
	if liveShown {
		fmt.Fprint(Stdout, eraseLine)
		liveShown = false
	}
}

func drawLiveLocked() {
	if liveActive == nil || !IsTerminal() {
		return
	}
	width := Width() - 1
	line := liveActive.render(width)
	if ansi.StringWidth(line) > width {
		line = ansi.Truncate(line, width, "…")
	}
	fmt.Fprint(Stdout, eraseLine+line)
	liveShown = true
}

// startLive makes e the live element and starts redrawing it periodically.
// The returned stop function stops the animation and erases the line.
func startLive(e liveElement) (stop func()) {
	outMu.Lock()
	hideLiveLocked()
	liveActive = e
	drawLiveLocked()
	outMu.Unlock()

	if !IsTerminal() {
		return func() {
			outMu.Lock()
			if liveActive == e {
				liveActive = nil
			}
			outMu.Unlock()
		}
	}

	done := make(chan struct{})
	var once sync.Once
	go func() {
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				outMu.Lock()
				if liveActive == e {
					hideLiveLocked()
					drawLiveLocked()
				}
				outMu.Unlock()
			case <-done:
				return
			}
		}
	}()
	return func() {
		once.Do(func() {
			close(done)
			outMu.Lock()
			if liveActive == e {
				hideLiveLocked()
				liveActive = nil
			}
			outMu.Unlock()
		})
	}
}

// refreshLive redraws the live line right away (used after a state change
// such as a progress update, so the bar does not lag the 100ms tick).
func refreshLive() {
	outMu.Lock()
	defer outMu.Unlock()
	if liveActive != nil {
		hideLiveLocked()
		drawLiveLocked()
	}
}

// StopLive erases and detaches any active spinner or bar. Called before the
// process exits on a fatal error so the terminal is left clean.
func StopLive() {
	outMu.Lock()
	defer outMu.Unlock()
	hideLiveLocked()
	liveActive = nil
}

// Pause erases the live line and keeps it hidden until the returned resume
// function is called. Use it around anything that reads from the terminal
// (a password prompt) while a spinner might be running.
func Pause() (resume func()) {
	outMu.Lock()
	hideLiveLocked()
	prev := liveActive
	liveActive = nil
	outMu.Unlock()
	return func() {
		outMu.Lock()
		if liveActive == nil {
			liveActive = prev
		}
		drawLiveLocked()
		outMu.Unlock()
	}
}

func ensureNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}
