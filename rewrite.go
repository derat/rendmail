// Copyright 2022 Daniel Erat.
// All rights reserved.

package main

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/textproto"
	"strings"
)

// rewriteMessage reads an RFC 5233 (or RFC 2822, or RFC 822, sigh) message from
// r and writes it to w.
func rewriteMessage(r io.Reader, w io.Writer) error {
	_, err := copyMessagePart(newMessageReader(r), w, "")
	return err
}

// copyMessagePart reads a message part consisting of a header, a blank line,
// and a body from mr and writes it to w. The part can either be a full RFC 5233/2822/822
// message or an RFC 2045/2046 message body part terminated by delim.
func copyMessagePart(mr *messageReader, w io.Writer, delim string) (end bool, err error) {
	header, err := copyHeader(mr, w)
	if err != nil {
		return false, err
	}

	// TODO: We may need to check this copyHeader, since we need to rewrite headers
	// if we're deleting the body. Alternately, we could buffer the header lines
	// in-memory (they seem unlikely to be large) and then write them all at once.
	ctype := header["Content-Type"]
	if len(ctype) == 0 {
		// Use the default from RFC 2045 5.2, "Content-Type defaults".
		ctype = []string{"text/plain; charset=us-ascii"}
	}

	mtype, params, err := mime.ParseMediaType(ctype[0])
	if err != nil {
		// TODO: Decide how bad Content-Type headers should be handled.
		// For example, hard_ham/0188.7fc83c7dcf3fa40cb98e61a8e8661a03 in the
		// SpamAssassin corpus contains "text/plain; Windows-1252", and
		// spam_2/01359.deafa1d42658c6624c6809a446b7f369 has a "file"
		// parameter that includes an unquoted space.
		return false, fmt.Errorf("unparseable Content-Type %q: %v", ctype[0], err)
	}

	if strings.HasPrefix(mtype, "multipart/") {
		// RFC 2046 5.1.1:
		//  The only mandatory global parameter for the "multipart" media type is
		//  the boundary parameter, which consists of 1 to 70 characters from a
		//  set of characters known to be very robust through mail gateways, and
		//  NOT ending with white space. (If a boundary delimiter line appears to
		//  end with white space, the white space must be presumed to have been
		//  added by a gateway, and must be deleted.)
		bnd := params["boundary"]
		if len(bnd) < 1 || len(bnd) > 70 {
			return false, fmt.Errorf("invalid boundary %q", bnd)
		}
		subDelim := "--" + bnd

		// RFC 2046 5.1:
		//  In the case of multipart entities, in which one or more different
		//  sets of data are combined in a single body, a "multipart" media type
		//  field must appear in the entity's header.  The body must then contain
		//  one or more body parts, each preceded by a boundary delimiter line,
		//  and the last one followed by a closing boundary delimiter line.
		//  After its boundary delimiter line, each body part then consists of a
		//  header area, a blank line, and a body area.  Thus a body part is
		//  similar to an RFC 822 message in syntax, but different in meaning.

		// First, read the preamble (e.g. "This is a multi-part message in MIME format.").
		if end, err := copyBody(mr, w, subDelim); err != nil {
			return false, err
		} else if !end {
			// Next, copy the enclosed parts until we see the closing outer delimiter.
			// TODO: Is it valid for the preamble to be immediately followed by a
			// closing boundary delimiter?
			for {
				if end, err := copyMessagePart(mr, w, subDelim); err != nil {
					return false, err
				} else if end {
					break
				}
			}
		}
	}

	// Read the top-level body until we see the outer boundary.
	return copyBody(mr, w, delim)
}

// copyHeader reads the header portion of a message part from mr and writes it to w.
// The trailing blank line at the end of the header is written before returning.
func copyHeader(mr *messageReader, w io.Writer) (map[string][]string, error) {
	// The header consists of multiple (possibly repeated) header fields.
	header := make(map[string][]string)
	for {
		folded, unfolded, err := mr.readFoldedLine()
		if err == io.EOF {
			return nil, errors.New("missing body")
		} else if err != nil {
			return nil, err
		}

		// A blank line indicates the end of the header.
		if unfolded == "" {
			if len(folded) != 1 {
				return nil, errors.New("blank line is folded") // should never happen
			}
			if _, err := io.WriteString(w, folded[0]); err != nil {
				return nil, err
			}
			return header, nil // done
		}

		key, val, err := parseHeaderField(unfolded)
		if err != nil {
			return nil, fmt.Errorf("malformed header field %q: %v", unfolded, err)
		}
		header[key] = append(header[key], val)

		for _, ln := range folded {
			if _, err := io.WriteString(w, ln); err != nil {
				return nil, err
			}
		}
	}
}

// copyBody reads lines from mr and writes them to w until it finds delim
// at the beginning of a line. The delimiter line is written before returning.
//
// The returned end value is true if the delimiter was suffixed by "--" or if delim is empty and
// EOF was encountered. If delim is non-empty and EOF is encountered, an error is returned.
func copyBody(mr *messageReader, w io.Writer, delim string) (end bool, err error) {
	for {
		ln, err := mr.readLine()
		if err == io.EOF {
			if delim == "" {
				return true, nil // done
			} else {
				// TODO: Should we be lenient in some cases, e.g. the outermost parts?
				// For example, hard_ham/0142.0220f772ab37ba8d5899fc62f6878edf from the
				// SpamAssassin corpus appears to be a multipart/alternative Oracle
				// newsletter from 2002 that's missing an ending "--next_part_of_message--"
				// delimiter.
				return false, fmt.Errorf("EOF while looking for delimiter %q", delim)
			}
		} else if err != nil {
			return false, err
		}

		if _, err := io.WriteString(w, ln); err != nil {
			return false, err
		}
		if delim != "" && strings.HasPrefix(ln, delim) {
			end := strings.HasPrefix(ln[len(delim):], "--")
			return end, nil
		}
	}
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
