package db

import (
	"strings"
	"testing"
)

// TestRedactPasswordArg guards against a real leak: the "-p" value in a
// mydumper/myloader argv is a plaintext DB password, and it was once
// logged verbatim via cmd.Args at debug level. Every arg list built with
// the "-h", host, "-P", port, "-u", user, "-p", pass pattern used
// throughout this package must come out with the password replaced.
func TestRedactPasswordArg(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "password redacted, everything else untouched",
			args: []string{"-h", "127.0.0.1", "-P", "3306", "-u", "root", "-p", "hunter2", "-o", "/tmp/out"},
			want: []string{"-h", "127.0.0.1", "-P", "3306", "-u", "root", "-p", "****", "-o", "/tmp/out"},
		},
		{
			name: "no password flag present",
			args: []string{"-h", "127.0.0.1", "-P", "3306", "-u", "root", "-o", "/tmp/out"},
			want: []string{"-h", "127.0.0.1", "-P", "3306", "-u", "root", "-o", "/tmp/out"},
		},
		{
			name: "trailing -p with nothing after it is left alone rather than panicking",
			args: []string{"-h", "127.0.0.1", "-p"},
			want: []string{"-h", "127.0.0.1", "-p"},
		},
		{
			name: "empty args",
			args: []string{},
			want: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := redactPasswordArg(tc.args)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("got %v, want %v", got, tc.want)
					break
				}
			}
		})
	}
}

// TestRedactPasswordArg_DoesNotMutateInput ensures the original argv (the
// slice actually passed to exec.Command) is never altered — only the
// logged copy should be redacted.
func TestRedactPasswordArg_DoesNotMutateInput(t *testing.T) {
	args := []string{"-u", "root", "-p", "hunter2"}
	_ = redactPasswordArg(args)
	if args[3] != "hunter2" {
		t.Errorf("redactPasswordArg mutated its input: %v", args)
	}
}

// TestRedactPasswordArg_JoinedOutputNeverContainsRealPassword is the same
// check the debug log line actually does: join the redacted args and make
// sure the real password string is nowhere in it.
func TestRedactPasswordArg_JoinedOutputNeverContainsRealPassword(t *testing.T) {
	const realPassword = "s3cr3t-db-password"
	args := []string{"-h", "db.internal", "-P", "3306", "-u", "root", "-p", realPassword, "-o", "/tmp/out"}

	joined := strings.Join(redactPasswordArg(args), " ")
	if strings.Contains(joined, realPassword) {
		t.Errorf("redacted command line still contains the real password: %q", joined)
	}
}
