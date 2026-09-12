package interactivelist

import (
	"errors"
	"strings"
	"testing"
)

func TestSelectOneFallback(t *testing.T) {
	options := []string{"alpha", "beta", "gamma"}

	tests := []struct {
		name    string
		input   string
		want    int
		wantErr error
	}{
		{"valid selection", "2\n", 1, nil},
		{"whitespace tolerated", "  1  \n", 0, nil},
		{"out of range", "9\n", 0, ErrCanceled},
		{"zero", "0\n", 0, ErrCanceled},
		{"non-numeric", "abc\n", 0, ErrCanceled},
		{"empty input", "\n", 0, ErrCanceled},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			idx, err := selectOneFallback(strings.NewReader(tc.input), "Pick one", options)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && idx != tc.want {
				t.Errorf("idx = %d, want %d", idx, tc.want)
			}
		})
	}
}

func TestSelectMultiFallback(t *testing.T) {
	options := []string{"alpha", "beta", "gamma", "delta"}

	tests := []struct {
		name  string
		input string
		want  []int
	}{
		{"empty input selects nothing (no preselection)", "\n", nil},
		{"single selection", "2\n", []int{1}},
		{"multiple selections sorted", "3,1\n", []int{0, 2}},
		{"whitespace tolerated", " 1 , 4 \n", []int{0, 3}},
		{"duplicates collapsed", "1,1,2\n", []int{0, 1}},
		{"invalid entries skipped", "0,5,abc,2\n", []int{1}},
		{"all invalid selects nothing", "0,5,abc\n", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selectMultiFallback(strings.NewReader(tc.input), "Pick some", options, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
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

func TestSelectMultiFallback_EmptyInputKeepsPreselected(t *testing.T) {
	options := []string{"alpha", "beta", "gamma", "delta"}
	preselected := func(i int) bool { return i == 1 || i == 3 } // beta, delta

	got, err := selectMultiFallback(strings.NewReader("\n"), "Pick some", options, preselected)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []int{1, 3}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
			break
		}
	}
}

func TestSelectMultiFallback_ExplicitInputOverridesPreselected(t *testing.T) {
	options := []string{"alpha", "beta", "gamma", "delta"}
	preselected := func(i int) bool { return i == 1 } // beta

	got, err := selectMultiFallback(strings.NewReader("3\n"), "Pick some", options, preselected)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []int{2} // gamma — the user's explicit choice replaces the preselection
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSelectOne_NoOptions(t *testing.T) {
	if _, err := SelectOne("Pick", nil); err == nil {
		t.Error("expected an error when there are no options")
	}
}

func TestSelectMulti_NoOptions(t *testing.T) {
	got, err := SelectMulti("Pick", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}
