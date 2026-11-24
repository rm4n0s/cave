package pty

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/cave/internal/core/errtest"
)

func TestSizeCheck(t *testing.T) {
	tests := []struct {
		size Size
		ok   bool
	}{
		{Size{Cols: 80, Rows: 24}, true},
		{Size{Cols: 1, Rows: 1}, true},
		{Size{Cols: MaxCells, Rows: MaxCells, Width: 1 << 20, Height: 1 << 20}, true},
		// A dimension of 0 was not said: "ssh -tt" without a terminal of its own sends 0 by 0.
		{Size{Cols: 0, Rows: 24}, true},
		{Size{Cols: 80, Rows: 0}, true},
		{Size{}, true},
		{Size{Cols: MaxCells + 1, Rows: 24}, false},
		{Size{Cols: 80, Rows: MaxCells + 1}, false},
		{Size{Cols: 1 << 31, Rows: 1 << 31}, false},
	}
	for _, tt := range tests {
		err := tt.size.Check()
		if tt.ok {
			if err != nil {
				t.Errorf("%+v: %v", tt.size, err)
			}
			continue
		}
		meta := errtest.Route(t, err, "Size.Check.PtySizeInvalid")
		if meta["cols"] != tt.size.Cols || meta["rows"] != tt.size.Rows {
			t.Errorf("%+v: metadata = %v", tt.size, meta)
		}
	}
}

func TestParseModes(t *testing.T) {
	tests := []struct {
		name    string
		encoded []byte
		want    []Mode
		bad     bool
	}{
		{"empty", nil, nil, false},
		{"only the end", []byte{0}, nil, false},
		{"one mode and the end", []byte{53, 0, 0, 0, 1, 0}, []Mode{{53, 1}}, false},
		{"several modes", []byte{3, 0, 0, 0, 127, 53, 0, 0, 0, 0, 128, 0, 0, 150, 0, 0}, []Mode{{3, 127}, {53, 0}, {128, 38400}}, false},
		{"without the end", []byte{53, 0, 0, 0, 1}, []Mode{{53, 1}}, false},
		{"what follows the end is not read", []byte{53, 0, 0, 0, 1, 0, 99, 99}, []Mode{{53, 1}}, false},
		{"an undefined opcode ends the list", []byte{53, 0, 0, 0, 1, 160, 1, 2, 3, 4, 3, 0, 0, 0, 8}, []Mode{{53, 1}}, false},
		{"the last defined opcode", []byte{159, 0, 0, 0, 7}, []Mode{{159, 7}}, false},
		{"a large value", []byte{129, 255, 255, 255, 255}, []Mode{{129, 0xffffffff}}, false},
		{"a value cut short", []byte{53, 0, 0, 0}, nil, true},
		{"an opcode without a value", []byte{53}, nil, true},
		{"cut short after a good mode", []byte{53, 0, 0, 0, 1, 3, 0}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseModes(tt.encoded)
			if tt.bad {
				errtest.Route(t, err, "ParseModes.PtyModesInvalid")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseModes = %+v, want %+v", got, tt.want)
			}
		})
	}
}
