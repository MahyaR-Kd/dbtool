package db

import "testing"

// TestIsMyloaderRestoreCompletedLine guards a real false-failure: myloader
// (like mydumper) increments an internal error counter for any non-fatal
// MySQL warning, and exits non-zero purely based on that counter
// (src/myloader/myloader.c: `exit_code = errors ? EXIT_FAILURE :
// EXIT_SUCCESS;`), the same class of bug as mydumper/mydumper#1300 on the
// dump side. myloader logs "Restore completed" unconditionally as the very
// last thing it does in main(), after schema, data, checksums, and cleanup
// have all finished — so its presence in the captured log distinguishes a
// real restore failure from this false one.
func TestIsMyloaderRestoreCompletedLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{
			name: "real production log line (glib g_message format)",
			line: "** Message: 17:21:18.909: Restore completed",
			want: true,
		},
		{
			name: "exact marker alone",
			line: "Restore completed",
			want: true,
		},
		{
			name: "unrelated schema checksum line",
			line: "** Message: 17:21:18.870: Schema create checksum confirmed for shopping_cart",
			want: false,
		},
		{
			name: "empty line",
			line: "",
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isMyloaderRestoreCompletedLine(tc.line); got != tc.want {
				t.Errorf("isMyloaderRestoreCompletedLine(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}
