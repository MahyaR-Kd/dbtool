package job

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC)
	if err != nil {
		panic(err)
	}
	return t
}

func TestNextRun(t *testing.T) {
	cases := []struct {
		schedule string
		from     string
		want     string
	}{
		{"0 2 * * *", "2026-11-09 01:30:00", "2026-11-09 02:00:00"},
		{"0 2 * * *", "2026-11-09 02:00:00", "2026-11-10 02:00:00"}, // strictly after
		{"0 2 * * *", "2026-11-09 02:00:30", "2026-11-10 02:00:00"},
		{"*/15 * * * *", "2026-11-09 10:07:45", "2026-11-09 10:15:00"},
		{"*/15 * * * *", "2026-11-09 23:59:00", "2026-11-10 00:00:00"},
		{"* * * * *", "2026-11-09 10:07:45", "2026-11-09 10:08:00"},
		{"30 4 1 * *", "2026-11-09 10:00:00", "2026-12-01 04:30:00"},
		{"0 0 1 1 *", "2026-11-09 10:00:00", "2027-01-01 00:00:00"},
		{"0 9 * * 1", "2026-11-08 12:00:00", "2026-11-09 09:00:00"}, // Sun → Mon
		{"0 9 * * 7", "2026-11-09 12:00:00", "2026-11-15 09:00:00"}, // 7 = Sunday
	}

	for _, c := range cases {
		got, ok := NextRun(c.schedule, at(c.from))
		if !ok {
			t.Errorf("NextRun(%q, %s): no run found, want %s", c.schedule, c.from, c.want)
			continue
		}
		if !got.Equal(at(c.want)) {
			t.Errorf("NextRun(%q, %s) = %s, want %s", c.schedule, c.from, got.Format(time.DateTime), c.want)
		}
		if !MatchSchedule(c.schedule, got) {
			t.Errorf("NextRun(%q) returned %s which MatchSchedule rejects", c.schedule, got)
		}
	}
}

func TestNextRunNone(t *testing.T) {
	for _, s := range []string{"", "bogus", "61 * * * *", "* * * *", "0 0 31 2 *", "0 0 29 2 *"} {
		// "0 0 29 2 *": no Feb 29 in 2027, and 2028 is beyond the 366-day horizon.
		if got, ok := NextRun(s, at("2026-11-09 00:00:00")); ok {
			t.Errorf("NextRun(%q) = %s, want none", s, got)
		}
	}
}

func TestDescribeNext(t *testing.T) {
	now := at("2026-11-09 22:48:00") // a Monday
	cases := map[string]string{
		"0 2 * * *":    "in 3h12m (Tue 02:00)",
		"0 23 * * *":   "in 12m (Mon 23:00)",
		"48 1 * * *":   "in 3h (Tue 01:48)",
		"0 2 * * 3":    "in 1d3h (Wed 02:00)",
		"0 2 * * 5":    "in 3d (Fri 02:00)",
		"0 2 1 * *":    "in 21d (Dec 1 02:00)",
		"bad schedule": "",
	}
	for schedule, want := range cases {
		if got := DescribeNext(schedule, now); got != want {
			t.Errorf("DescribeNext(%q) = %q, want %q", schedule, got, want)
		}
	}
}

func BenchmarkNextRunYearly(b *testing.B) {
	from := at("2026-11-09 00:00:00")
	for i := 0; i < b.N; i++ {
		NextRun("0 0 1 1 *", from)
	}
}
