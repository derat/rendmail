// Copyright 2022 Daniel Erat.
// All rights reserved.

package main

import (
	"bufio"
	"io"
)

func rewriteMessage(w io.Writer, r io.Reader) error {
	lr := newLineReader(r)
	for {
		folded, _, err := lr.readFolded()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		for _, ln := range folded {
			if _, err := w.Write(ln); err != nil {
				return err
			}
		}
	}
	return nil
}

type lineReader struct {
	r *bufio.Reader
}

func newLineReader(r io.Reader) *lineReader {
	return &lineReader{r: bufio.NewReader(r)}
}

// readFolded reads and returns a possibly-folded line.
//
// See https://www.rfc-editor.org/rfc/rfc5322.html#section-2.2.3 for more
// details about folding.
//
// The folded return value contains all of the original lines, including
// terminating "\r\n" or "\n" suffixes if present.
//
// The unfolded return value contains the unfolded line, i.e. with all
// terminating suffixes removed.
func (lr *lineReader) readFolded() (folded [][]byte, unfolded []byte, err error) {
	first, err := lr.r.ReadBytes('\n')
	if err != nil && (err != io.EOF || len(first) == 0) {
		return nil, nil, err
	}
	folded = append(folded, first)
	unfolded = trimCRLF(first)
	if len(unfolded) == 0 {
		return folded, unfolded, nil
	}

	for {
		next, err := lr.r.Peek(1)
		if err == io.EOF {
			return folded, unfolded, nil
		} else if err != nil {
			return nil, nil, err
		}
		if next[0] == ' ' || next[0] == '\t' {
			ln, err := lr.r.ReadBytes('\n')
			if err != nil && (err != io.EOF || len(ln) == 0) {
				return nil, nil, err
			}
			folded = append(folded, ln)
			unfolded = append(unfolded, trimCRLF(ln)...)
		} else {
			return folded, unfolded, nil
		}
	}
	return folded, unfolded, nil
}

// trimCRLF trims a trailing "\r\n" (or just "\n") from ln.
func trimCRLF(ln []byte) []byte {
	if len(ln) > 0 && ln[len(ln)-1] == '\n' {
		ln = ln[:len(ln)-1]
		if len(ln) > 0 && ln[len(ln)-1] == '\r' {
			ln = ln[:len(ln)-1]
		}
	}
	return ln
}
