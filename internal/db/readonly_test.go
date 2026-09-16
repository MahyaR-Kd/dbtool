package db

import "testing"

// TestFirstReadOnlyVariableOn guards the decision waitUntilDestinationWritable
// polls on: whether the destination is currently rejecting writes.
func TestFirstReadOnlyVariableOn(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
		want   string
	}{
		{
			name:   "both off",
			values: map[string]string{"read_only": "OFF", "super_read_only": "OFF"},
			want:   "",
		},
		{
			name:   "read_only on",
			values: map[string]string{"read_only": "ON", "super_read_only": "OFF"},
			want:   "read_only",
		},
		{
			name:   "both on reports read_only first",
			values: map[string]string{"read_only": "ON", "super_read_only": "ON"},
			want:   "read_only",
		},
		{
			name:   "only super_read_only on",
			values: map[string]string{"read_only": "OFF", "super_read_only": "ON"},
			want:   "super_read_only",
		},
		{
			name:   "case-insensitive value",
			values: map[string]string{"read_only": "on", "super_read_only": "OFF"},
			want:   "read_only",
		},
		{
			name:   "empty map",
			values: map[string]string{},
			want:   "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstReadOnlyVariableOn(tc.values); got != tc.want {
				t.Errorf("firstReadOnlyVariableOn(%v) = %q, want %q", tc.values, got, tc.want)
			}
		})
	}
}
