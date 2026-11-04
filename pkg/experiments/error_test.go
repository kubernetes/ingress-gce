package experiments

import (
	"errors"
	"fmt"
	"testing"
)

func TestNilArgErrorMessage(t *testing.T) {
	testCases := []struct {
		argName string
		want    string
	}{
		{
			argName: "client",
			want:    "arg client cannot be nil",
		},
		{
			argName: "",
			want:    "arg  cannot be nil",
		},
		{
			argName: "abdefD",
			want:    "arg abdefD cannot be nil",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.argName, func(t *testing.T) {
			err := NewNilArgError(tc.argName)
			if err == nil {
				t.Fatalf("NewNilArgError(%q) = nil, want non-nil error", tc.argName)
			}
			if got := err.Error(); got != tc.want {
				t.Errorf("NewNilArgError(%q).Error() = %q, want %q", tc.argName, got, tc.want)
			}
		})
	}
}

func TestIsNilArgError(t *testing.T) {
	nilArgErr := NewNilArgError("client")

	testCases := []struct {
		desc string
		err  error
		want bool
	}{
		{
			desc: "nil error",
			err:  nil,
			want: false,
		},
		{
			desc: "NilArgError",
			err:  nilArgErr,
			want: true,
		},
		{
			desc: "NilArgError struct pointer",
			err:  &NilArgError{argName: "foo"},
			want: true,
		},
		{
			desc: "wrapped NilArgError",
			err:  fmt.Errorf("failed to create: %w", nilArgErr),
			want: true,
		},
		{
			desc: "double wrapped NilArgError",
			err:  fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", nilArgErr)),
			want: true,
		},
		{
			desc: "joined errors containing NilArgError",
			err:  errors.Join(errors.New("other"), nilArgErr),
			want: true,
		},
		{
			desc: "NilArgError formatted without wrapping",
			err:  fmt.Errorf("failed to create: %v", nilArgErr),
			want: false,
		},
		{
			desc: "unrelated error",
			err:  errors.New("arg client cannot be nil"),
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			if got := IsNilArgError(tc.err); got != tc.want {
				t.Errorf("IsNilArgError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
