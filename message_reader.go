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
	// RFC 5322 2.1.1 "Line Length Limits":
	//  There are two limits that this specification places on the number of
	//  characters in a line.  Each line of characters MUST be no more than
	//  998 characters, and SHOULD be no more than 78 characters, excluding
	//  the CRLF.

	// TODO: Add an upper bound on how long the line can be?
	ln, err := mr.r.ReadString('\n')
	if err == io.EOF && ln != "" {
		err = nil
	}
	return ln, err
}

// readFoldedLine reads and returns a possibly-folded line.
//
// See RFC 5322 2.2.3, "Long Header Fields", for more details about folding.
// This function is similar to ReadContinuedLine from Reader in net/textproto.
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

	// TODO: Limit how long the unfolded line can be? I don't see any hard
	// limits in the RFC, though.
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
//
// RFC 5322 2.3 says "CR and LF MUST only occur together as CRLF; they MUST NOT appear
// independently in the body.", but I think that all bets are off by the time that we're
// looking at e.g. a Maildir message file. On a Linux system, I always see only "\n"
// without a preceding "\r".
func trimCRLF(ln string) string {
	if len(ln) > 0 && ln[len(ln)-1] == '\n' {
		ln = ln[:len(ln)-1]
		if len(ln) > 0 && ln[len(ln)-1] == '\r' {
			ln = ln[:len(ln)-1]
		}
	}
	return ln
}
