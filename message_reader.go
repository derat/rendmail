// Copyright 2022 Daniel Erat.
// All rights reserved.

package main

import (
	"bufio"
	"io"
)

// messageReader reads an email message line-by-line.
//
// Its functionality is similar to the ReadLine and ReadContinuedLine
// functions from Reader in the net/textproto, except it additionally returns
// the original data to callers.
type messageReader struct {
	r *bufio.Reader
}

func newMessageReader(r io.Reader) *messageReader {
	return &messageReader{r: bufio.NewReader(r)}
}

// readLine reads and returns a single newline-terminated line.
//
// The newline is included in the returned string.
//
// If one or more bytes are read but EOF is encountered before
// a newline, then the data and nil are returned. If EOF is
// encountered before reading any bytes, than io.EOF is returned.
func (mr *messageReader) readLine() (string, error) {
	ln, err := mr.r.ReadString('\n')
	if err == io.EOF && ln != "" {
		err = nil
	}
	return ln, err
}

// readFoldedLine reads and returns a possibly-folded line.
//
// See https://www.rfc-editor.org/rfc/rfc5322.html#section-2.2.3 for more
// details about folding. This function is similar to ReadContinuedLine
// from Reader in net/textproto.
//
// The folded return value contains all of the original lines, including
// terminating "\r\n" or "\n" suffixes if present.
//
// The unfolded return value contains the unfolded line, i.e. with all
// terminating suffixes removed.
func (mr *messageReader) readFoldedLine() (folded []string, unfolded string, err error) {
	first, err := mr.readLine()
	if err != nil {
		return nil, "", err
	}
	folded = append(folded, first)
	unfolded = trimCRLF(first)
	if len(unfolded) == 0 {
		return folded, unfolded, nil
	}

	for {
		if next, err := mr.r.Peek(1); err == io.EOF {
			return folded, unfolded, nil // input ends after newline
		} else if err != nil {
			return nil, "", err
		} else if next[0] != ' ' && next[0] != '\t' {
			return folded, unfolded, nil // next line isn't a continuation
		}

		ln, err := mr.readLine()
		if err != nil {
			return nil, "", err
		}
		folded = append(folded, ln)
		unfolded += trimCRLF(ln)
	}
}

// trimCRLF trims a trailing "\r\n" (or just "\n") from ln.
func trimCRLF(ln string) string {
	if len(ln) > 0 && ln[len(ln)-1] == '\n' {
		ln = ln[:len(ln)-1]
		if len(ln) > 0 && ln[len(ln)-1] == '\r' {
			ln = ln[:len(ln)-1]
		}
	}
	return ln
}
