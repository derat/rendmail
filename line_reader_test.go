// Copyright 2022 Daniel Erat.
// All rights reserved.

package main

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestLineReader_readLine(t *testing.T) {
	const eof = "EOF"
	for _, tc := range []struct {
		in   string
		want []string // or eof for empty line and io.EOF
	}{
		{"", []string{eof}},
		{"\n", []string{"\n", eof}},
		{"\r\n", []string{"\r\n", eof}},
		{"abc", []string{"abc", eof}},
		{"abc def\n", []string{"abc def\n", eof}},
		{"abc\r\ndef\r\n", []string{"abc\r\n", "def\r\n", eof}},
		{"abc\ndef\n", []string{"abc\n", "def\n", eof}},
		{"abc\ndef", []string{"abc\n", "def", eof}},
		{"abc\r\n\r\n", []string{"abc\r\n", "\r\n", eof}},
		{"abc\n\n\n", []string{"abc\n", "\n", "\n", eof}},
	} {
		t.Run(tc.in, func(t *testing.T) {
			lr := newLineReader(strings.NewReader(tc.in))
			var got []string
			for {
				if ln, err := lr.readLine(); err == nil {
					got = append(got, ln)
				} else if err == io.EOF {
					if ln != "" {
						t.Fatalf("readLine() returned both line %q and EOF", ln)
					}
					got = append(got, eof)
					break
				} else {
					t.Fatalf("readLine() failed: %v", err)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("readLine() produced %q; want %q", got, tc.want)
			}
		})
	}

}

func TestLineReader_readFoldedLine(t *testing.T) {
	const in = "A folded line\n\tusing a tab\n" +
		"A folded line \n  using two spaces\n" +
		"A line with a carriage return\r\n" +
		"A folded line with CRLF and \r\n a space\r\n" +
		"\n" +
		"A single line\n"

	res := func(folded []string, unfolded string, err error) string {
		return fmt.Sprintf("%q %q %v", folded, unfolded, err)
	}

	var got string
	lr := newLineReader(strings.NewReader(in))
	for {
		folded, unfolded, err := lr.readFoldedLine()
		if got != "" {
			got += "\n"
		}
		got += res(folded, unfolded, err)
		if err != nil {
			break
		}
	}

	if want := strings.Join([]string{
		res([]string{"A folded line\n", "\tusing a tab\n"}, "A folded line\tusing a tab", nil),
		res([]string{"A folded line \n", "  using two spaces\n"}, "A folded line   using two spaces", nil),
		res([]string{"A line with a carriage return\r\n"}, "A line with a carriage return", nil),
		res([]string{"A folded line with CRLF and \r\n", " a space\r\n"}, "A folded line with CRLF and  a space", nil),
		res([]string{"\n"}, "", nil),
		res([]string{"A single line\n"}, "A single line", nil),
		res(nil, "", io.EOF),
	}, "\n"); got != want {
		t.Errorf("readFoldedLine() produced:\n%s\nWant:\n%s", got, want)
	}

}
