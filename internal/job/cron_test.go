package job

import (
	"dbtool/internal/filelock"
	"dbtool/internal/paths"
	"testing"
	"time"
)

func TestScheduleValidationAndMatching(t *testing.T) {
	for _, schedule := range []string{"*/0 * * * *", "*/-1 * * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8", "1-0 * * * *", "* * *", "bad * * * *"} {
		if ValidateSchedule(schedule) == nil {
			t.Errorf("accepted %q", schedule)
		}
		if MatchSchedule(schedule, time.Now()) {
			t.Errorf("matched invalid %q", schedule)
		}
	}
	date := time.Date(2026, 9, 13, 2, 10, 0, 0, time.UTC) // Sunday.
	for _, schedule := range []string{"*/5 2 * * *", "0,10,20 2 * * *", "5-20/5 2 * * *", "10 2 * * 7", "10 2 1 * 0"} {
		if err := ValidateSchedule(schedule); err != nil {
			t.Fatal(err)
		}
		if !MatchSchedule(schedule, date) {
			t.Errorf("did not match %s", schedule)
		}
	}
}

func TestRunJobSkipsLockedJob(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := paths.DbtoolDir()
	if err != nil {
		t.Fatal(err)
	}
	release, err := filelock.Acquire(dir, "job:busy", false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	// This would terminate the process trying to select a missing config if run.
	RunJob(Config{Name: "busy", Type: "dump", Schedule: "* * * * *", SrcConfigName: "missing"})
}
