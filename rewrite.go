// Copyright 2022 Daniel Erat.
// All rights reserved.

package main

import (
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"strings"
)

func rewriteMessage(w io.Writer, r io.Reader) error {
	mr := newMessageReader(r)

	// Read the top-level header (consisting of multiple header fields).
	header := make(map[string][]string)
	for {
		folded, unfolded, err := mr.readFoldedLine()
		if err == io.EOF {
			return errors.New("missing body")
		} else if err != nil {
			return err
		}

		// A blank line indicates the end of the header.
		if unfolded == "" {
			if len(folded) != 1 {
				return errors.New("blank line is folded") // should never happen
			}
			if _, err := io.WriteString(w, folded[0]); err != nil {
				return err
			}
			break
		}

		key, val, err := parseHeaderField(unfolded)
		if err != nil {
			return fmt.Errorf("malformed header field %q: %v", unfolded, err)
		}
		header[key] = append(header[key], val)

		for _, ln := range folded {
			if _, err := io.WriteString(w, ln); err != nil {
				return err
			}
		}
	}

	// TODO: Use the Content-Type header to parse the body.

	// Read the top-level body.
	for {
		ln, err := mr.readLine()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		if _, err := io.WriteString(w, ln); err != nil {
			return err
		}
	}

	return nil
}

// parseHeaderField splits ln, e.g. "from: \"Bob\" <user@example.org>", into
// a canonicalized key and value, e.g. "From" and "\"Bob\" <user@example.org>".
func parseHeaderField(ln string) (key, val string, err error) {
	// TODO: Check that the line doesn't start with whitespace?
	// https://cs.opensource.google/go/go/+/refs/tags/go1.18:src/net/textproto/reader.go;l=497
	// checks this for the first line.

	// This is basically strings.Cut, but that wasn't introduced until Go 1.18.
	idx := strings.IndexByte(ln, ':')
	if idx < 0 {
		return "", "", errors.New("missing colon")
	}

	key = textproto.CanonicalMIMEHeaderKey(ln[:idx])

	// TODO: Is this right?
	// https://cs.opensource.google/go/go/+/refs/tags/go1.18:src/net/textproto/reader.go;l=526
	val = strings.TrimLeft(ln[idx+1:], " \t")

	return key, val, nil
}
