package job

import (
	"fmt"
	"strings"
	"time"
)

// nextRunHorizon bounds how far ahead NextRun searches. A year (plus a day
// for leap years) covers every schedule the 5-field cron syntax can express
// except impossible dates such as "0 0 31 2 *".
const nextRunHorizon = 366 * 24 * time.Hour

// NextRun returns the first minute strictly after from at which schedule
// fires (as decided by MatchSchedule), searching up to 366 days ahead. It
// returns false if the schedule is invalid or never fires in that window.
//
// The search walks forward minute by minute but skips whole months and
// hours whose month/hour field cannot match, so even a yearly schedule is
// found in a few thousand checks rather than half a million.
func NextRun(schedule string, from time.Time) (time.Time, bool) {
	if ValidateSchedule(schedule) != nil {
		return time.Time{}, false
	}
	fields := strings.Fields(schedule)
	monthField, hourField, minuteField := fields[3], fields[1], fields[0]

	loc := from.Location()
	t := from.Truncate(time.Minute).Add(time.Minute)
	// Truncate works on absolute time; re-anchor to a whole local minute so
	// zones with odd offsets still land on :00 seconds.
	t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, loc)
	if !t.After(from) {
		t = t.Add(time.Minute)
	}
	limit := from.Add(nextRunHorizon)

	for !t.After(limit) {
		next := t
		switch {
		case !fieldMatches(monthField, int(t.Month()), 1, 12):
			next = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, loc)
		case !fieldMatches(hourField, t.Hour(), 0, 23):
			next = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, loc)
		case !fieldMatches(minuteField, t.Minute(), 0, 59):
			next = t.Add(time.Minute)
		case MatchSchedule(schedule, t):
			return t, true
		default:
			next = t.Add(time.Minute)
		}
		// Guard against DST transitions mapping a jump onto the same or an
		// earlier instant: always make progress.
		if !next.After(t) {
			next = t.Add(time.Minute)
		}
		t = next
	}
	return time.Time{}, false
}

func fieldMatches(field string, val, min, max int) bool {
	ok, err := cronField(field, val, min, max)
	return err == nil && ok
}

// DescribeNext describes when schedule next fires relative to now, compactly:
// "in 12m (Mon 02:00)", "in 3h12m (Mon 02:00)", "in 2d (Wed 02:00)",
// "in 12d (Jan 20 02:00)". It returns "" if the schedule is invalid or
// never fires within a year.
func DescribeNext(schedule string, now time.Time) string {
	next, ok := NextRun(schedule, now)
	if !ok {
		return ""
	}
	return fmt.Sprintf("in %s (%s)", relative(next.Sub(now)), absolute(next, now))
}

// relative renders a positive duration in at most two units.
func relative(d time.Duration) string {
	// Round up to whole minutes: a run 30 seconds away is "in 1m", not "in 0m".
	mins := int((d + time.Minute - 1) / time.Minute)
	if mins < 1 {
		mins = 1
	}
	switch {
	case mins < 60:
		return fmt.Sprintf("%dm", mins)
	case mins < 24*60:
		h, m := mins/60, mins%60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%02dm", h, m)
	default:
		days, h := mins/(24*60), (mins%(24*60))/60
		if days < 3 && h > 0 {
			return fmt.Sprintf("%dd%dh", days, h)
		}
		return fmt.Sprintf("%dd", days)
	}
}

// absolute renders the wall-clock time of next: weekday + time within the
// coming week, month + day + time beyond it.
func absolute(next, now time.Time) string {
	if next.Sub(now) < 7*24*time.Hour {
		return next.Format("Mon 15:04")
	}
	return next.Format("Jan 2 15:04")
}
