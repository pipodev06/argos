package main

import (
	"reflect"
	"testing"
)

func TestSplitMustChange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in       []string
		wantRest []string
		wantFlag bool
	}{
		{nil, nil, false},
		{[]string{"admin"}, []string{"admin"}, false},
		{[]string{"--must-change", "admin"}, []string{"admin"}, true},
		{[]string{"admin", "--must-change"}, []string{"admin"}, true},
		{[]string{"--must-change"}, nil, true},
	}
	for _, tc := range tests {
		rest, flag := splitMustChange(tc.in)
		if !reflect.DeepEqual(rest, tc.wantRest) || flag != tc.wantFlag {
			t.Errorf("splitMustChange(%v) = %v, %v; want %v, %v", tc.in, rest, flag, tc.wantRest, tc.wantFlag)
		}
	}
}
