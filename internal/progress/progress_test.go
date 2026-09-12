package progress

import "testing"

func TestParseTableProgress(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantCurrent int
		wantTotal   int
		wantOk      bool
	}{
		{
			name:        "valid progress line",
			line:        "** (mydumper:1234): MESSAGE: Dumping `mydb`.`users` [42/100]",
			wantCurrent: 42,
			wantTotal:   100,
			wantOk:      true,
		},
		{
			name:   "no bracket pair",
			line:   "** (mydumper:1234): Starting dump",
			wantOk: false,
		},
		{
			name:   "total is zero – guard against divide-by-zero",
			line:   "** (mydumper:1234): [5/0]",
			wantOk: false,
		},
		{
			name:        "first table out of many",
			line:        "[1/200]",
			wantCurrent: 1,
			wantTotal:   200,
			wantOk:      true,
		},
		{
			name:   "empty line",
			line:   "",
			wantOk: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cur, tot, ok := ParseTableProgress(tc.line)
			if ok != tc.wantOk {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOk)
			}
			if !ok {
				return
			}
			if cur != tc.wantCurrent {
				t.Errorf("current = %d, want %d", cur, tc.wantCurrent)
			}
			if tot != tc.wantTotal {
				t.Errorf("total = %d, want %d", tot, tc.wantTotal)
			}
		})
	}
}

func TestParseDumpingTable(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{
			name: "verbose schema line",
			line: "** Message: 02:41:37.493: Thread 1 dumping schema for `catalog`.`products`",
			want: true,
		},
		{
			name: "verbose data line",
			line: "** Message: 02:41:38.100: Thread 3 dumping data for `catalog`.`orders`",
			want: true,
		},
		{
			name: "thread number with multiple digits",
			line: "** Message: 02:41:37.499: Thread 12 dumping schema for `mydb`.`users`",
			want: true,
		},
		{
			name: "unrelated log line",
			line: "** Message: 02:41:37.000: Connected to MySQL server",
			want: false,
		},
		{
			name: "empty line",
			line: "",
			want: false,
		},
		{
			name: "starting dump line",
			line: "** Message: 02:41:36.000: Starting dump at 2024-01-01",
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseDumpingTable(tc.line)
			if got != tc.want {
				t.Errorf("ParseDumpingTable(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

func TestParseRestoringTable(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{
			name: "verbose restoring line",
			line: "** Message: 02:41:37.493: Thread 1 restoring `catalog`.`products` part 1 of 1",
			want: true,
		},
		{
			name: "thread number with multiple digits",
			line: "** Message: 02:41:37.499: Thread 12 restoring `mydb`.`users` part 1 of 4",
			want: true,
		},
		{
			name: "unrelated log line",
			line: "** Message: 02:41:37.000: Connected to MySQL server",
			want: false,
		},
		{
			name: "empty line",
			line: "",
			want: false,
		},
		{
			name: "starting restore line",
			line: "** Message: 02:41:36.000: Starting restore at 2024-01-01",
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseRestoringTable(tc.line)
			if got != tc.want {
				t.Errorf("ParseRestoringTable(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}
