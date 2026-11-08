package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// ---------------------------------------------------------------- Task

// Task is one stage of a multi-step command (lock, upload, verify, ...).
// While running it shows an animated spinner on the live line; when it ends
// the spinner is replaced by a permanent result line:
//
//	✔ Uploading to S3  s3://bucket/prod_2026-11-08  4.2s
//	⚠ Verifying dump  2 tables may have lost column data
//	✖ Uploading to S3  access denied
//
// When stdout is not a terminal nothing animates and only the result line
// is printed, so cron/CI logs stay one line per stage.
type Task struct {
	mu    sync.Mutex
	label string
	start time.Time
	frame int
	stop  func()
	ended bool
}

// StartTask begins a stage labelled by the formatted text.
func StartTask(format string, a ...any) *Task {
	t := &Task{label: fmt.Sprintf(format, a...), start: time.Now()}
	t.stop = startLive(t)
	return t
}

func (t *Task) render(width int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.frame++
	return fg(ColorAccent).Render(spinnerFrame(t.frame)) + " " + t.label + Muted("…  "+Duration(time.Since(t.start)))
}

// Update changes the running stage's label.
func (t *Task) Update(format string, a ...any) {
	t.mu.Lock()
	t.label = fmt.Sprintf(format, a...)
	t.mu.Unlock()
	refreshLive()
}

// Elapsed returns how long the stage has been running.
func (t *Task) Elapsed() time.Duration { return time.Since(t.start) }

func (t *Task) end() (label string, elapsed time.Duration, ok bool) {
	t.mu.Lock()
	if t.ended {
		t.mu.Unlock()
		return "", 0, false
	}
	t.ended = true
	label = t.label
	t.mu.Unlock()
	// stop() takes the output lock, and the redraw goroutine holding that
	// lock calls render() which takes t.mu — so never hold t.mu here.
	t.stop()
	return label, time.Since(t.start), true
}

func resultLine(icon, label, detail string, elapsed time.Duration) string {
	line := icon + " " + label
	if detail != "" {
		line += "  " + detail
	}
	if elapsed >= 100*time.Millisecond {
		line += "  " + Muted(Duration(elapsed))
	}
	return line + "\n"
}

// Done ends the stage successfully. detail (optional) is shown dimmed after
// the label — a path, a size, a version.
func (t *Task) Done(detail string) {
	if label, el, ok := t.end(); ok {
		write(Stdout, resultLine(fg(ColorSuccess).Bold(true).Render(Icon(IconOK)), label, Muted(detail), el))
	}
}

// Warn ends the stage with a warning; the command continues.
func (t *Task) Warn(msg string) {
	if label, el, ok := t.end(); ok {
		write(Stderr, resultLine(fg(ColorWarn).Bold(true).Render(Icon(IconWarn)), label, Yellow(msg), el))
	}
}

// Fail ends the stage with an error.
func (t *Task) Fail(err error) {
	if label, el, ok := t.end(); ok {
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		write(Stderr, resultLine(fg(ColorError).Bold(true).Render(Icon(IconFail)), label, Red(msg), el))
	}
}

// Skip ends the stage as not applicable, dimmed: "○ Telegram  disabled".
func (t *Task) Skip(reason string) {
	if label, _, ok := t.end(); ok {
		write(Stdout, Muted(Icon(IconPending)+" "+label+"  "+reason)+"\n")
	}
}

// Skipped prints a dimmed "○ label  reason" line for a stage that never
// started — so the full stage list stays visible in the output.
func Skipped(label, reason string) {
	write(Stdout, Muted(Icon(IconPending)+" "+label+"  "+reason)+"\n")
}

// ---------------------------------------------------------------- Bar

// Bar is a progress bar that redraws on the live line:
//
//	⠋ Dumping shop  ████████████░░░░░░░░  42/100 tables · 37s · ETA 51s
//
// With an unknown total (<= 0) it shows a spinner, the label, the count so
// far and the elapsed time. Its method set mirrors the progressbar library
// it replaced (Set / Add / Finish) so call sites stay simple. All methods
// are safe for concurrent use.
type Bar struct {
	mu      sync.Mutex
	label   string
	unit    string
	total   int
	current int
	start   time.Time
	frame   int
	stop    func()
	ended   bool
}

// NewBar starts a progress bar. total <= 0 means "unknown" (spinner mode);
// unit names what is being counted ("tables").
func NewBar(total int, label, unit string) *Bar {
	b := &Bar{label: label, unit: unit, total: total, start: time.Now()}
	b.stop = startLive(b)
	return b
}

func (b *Bar) render(width int) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.frame++
	elapsed := time.Since(b.start)
	head := fg(ColorAccent).Render(spinnerFrame(b.frame)) + " " + b.label

	if b.total <= 0 {
		tail := Muted(Duration(elapsed))
		if b.current > 0 {
			tail = fmt.Sprintf("%d %s", b.current, b.unit) + Muted(" · "+Duration(elapsed))
		}
		return head + "  " + tail
	}

	cur := b.current
	if cur > b.total {
		cur = b.total
	}
	stats := fmt.Sprintf("%d/%d %s", cur, b.total, b.unit)
	meta := " · " + Duration(elapsed)
	if cur > 0 && cur < b.total {
		eta := time.Duration(float64(elapsed) / float64(cur) * float64(b.total-cur))
		meta += " · ETA " + Duration(eta)
	}
	pct := float64(cur) / float64(b.total)

	fixed := ansi.StringWidth(head) + 2 + 2 + ansi.StringWidth(stats) + ansi.StringWidth(meta) + 5
	barW := width - fixed
	if barW > 40 {
		barW = 40
	}
	if barW < 10 {
		return head + "  " + stats + Muted(meta)
	}
	filled := int(pct * float64(barW))
	full, empty := "█", "░"
	if ASCII() {
		full, empty = "#", "-"
	}
	bar := fg(ColorAccent).Render(strings.Repeat(full, filled)) + Muted(strings.Repeat(empty, barW-filled))
	return fmt.Sprintf("%s  %s %3d%%  %s%s", head, bar, int(pct*100), stats, Muted(meta))
}

// Set moves the bar to n. Values lower than the current one are ignored so
// the bar never jumps backwards.
func (b *Bar) Set(n int) error {
	b.mu.Lock()
	if n > b.current {
		b.current = n
	}
	b.mu.Unlock()
	refreshLive()
	return nil
}

// Add advances the bar by n.
func (b *Bar) Add(n int) error {
	b.mu.Lock()
	b.current += n
	b.mu.Unlock()
	if n != 0 {
		refreshLive()
	}
	return nil
}

// SetTotal switches the bar to a known total (e.g. once the tool reports
// "[x/y]"), turning a spinner into a real bar in place.
func (b *Bar) SetTotal(total int) {
	b.mu.Lock()
	b.total = total
	b.mu.Unlock()
	refreshLive()
}

// Total returns the bar's total (<= 0 if unknown).
func (b *Bar) Total() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.total
}

// Current returns the bar's current value.
func (b *Bar) Current() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.current
}

// SetLabel changes the bar's label.
func (b *Bar) SetLabel(label string) {
	b.mu.Lock()
	b.label = label
	b.mu.Unlock()
	refreshLive()
}

// Elapsed returns the time since the bar started.
func (b *Bar) Elapsed() time.Duration { return time.Since(b.start) }

// Finish stops the bar and erases it, printing nothing. Follow it with
// Done/Fail (or your own message) to leave a permanent result line.
func (b *Bar) Finish() error {
	b.mu.Lock()
	if b.ended {
		b.mu.Unlock()
		return nil
	}
	b.ended = true
	b.mu.Unlock()
	b.stop()
	return nil
}

// Done finishes the bar with "✔ label  detail  elapsed".
func (b *Bar) Done(label, detail string) {
	_ = b.Finish()
	write(Stdout, resultLine(fg(ColorSuccess).Bold(true).Render(Icon(IconOK)), label, Muted(detail), b.Elapsed()))
}

// Fail finishes the bar with "✖ label  message  elapsed".
func (b *Bar) Fail(label string, err error) {
	_ = b.Finish()
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	write(Stderr, resultLine(fg(ColorError).Bold(true).Render(Icon(IconFail)), label, Red(msg), b.Elapsed()))
}
