package progress

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/schollz/progressbar/v3"
)

// tableProgressRe matches "[current/total]" as emitted by some versions of
// mydumper/myloader in their stderr output, e.g.:
//
//	** (mydumper:1234): MESSAGE … [42/100]
var tableProgressRe = regexp.MustCompile(`\[(\d+)/(\d+)\]`)

// ParseTableProgress extracts the current and total table counts from a
// mydumper/myloader stderr line.  It returns ok=true only when both numbers
// are present and total > 0.
func ParseTableProgress(line string) (current, total int, ok bool) {
	m := tableProgressRe.FindStringSubmatch(line)
	if m == nil {
		return 0, 0, false
	}
	cur, err1 := strconv.Atoi(m[1])
	tot, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil || tot == 0 {
		return 0, 0, false
	}
	return cur, tot, true
}

// dumpingSchemaRe matches the verbose (-v 3) mydumper per-table progress line:
//
//	** Message: HH:MM:SS.mmm: Thread N dumping schema for `schema`.`table`
var dumpingSchemaRe = regexp.MustCompile("Thread \\d+ dumping (schema|data) for")

// ParseDumpingTable returns true when line is one of the verbose mydumper
// progress messages that indicates a table is being processed (schema or data
// phase).  Callers can count matching lines against the pre-queried total to
// drive the progress bar.
func ParseDumpingTable(line string) bool {
	return dumpingSchemaRe.MatchString(line)
}

// restoringTableRe matches the verbose (-v 3) myloader per-table progress line:
//
//	** Message: HH:MM:SS.mmm: Thread N restoring `schema`.`table` part X of Y
var restoringTableRe = regexp.MustCompile("Thread \\d+ restoring")

// ParseRestoringTable returns true when line is one of the verbose myloader
// progress messages that indicates a table is being restored.  Callers can
// count matching lines against the pre-counted total to drive the progress bar.
func ParseRestoringTable(line string) bool {
	return restoringTableRe.MatchString(line)
}

// New returns a styled, count-based progress bar for use when the total number
// of tables is known.
func New(total int, description string) *progressbar.ProgressBar {
	return progressbar.NewOptions(total,
		progressbar.OptionSetDescription(fmt.Sprintf("%-12s", description)),
		progressbar.OptionSetWidth(30),
		progressbar.OptionShowCount(),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "=",
			SaucerHead:    ">",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionFullWidth(),
	)
}

// SpinnerBar returns an indefinite spinner for use before the total count is
// known.  It is safe to call Finish on it and replace it with a New bar once
// the total becomes available.
func SpinnerBar(description string) *progressbar.ProgressBar {
	return progressbar.NewOptions(-1,
		progressbar.OptionSetDescription(fmt.Sprintf("%-12s", description)),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetWidth(30),
		progressbar.OptionSetRenderBlankState(true),
	)
}
