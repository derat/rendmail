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

// Defaults from RFC 2045 5.2, "Content-Type defaults".
var defaultMediaType, defaultContentParams, _ = mime.ParseMediaType("text/plain; charset=us-ascii")

// rewriteMessage reads an RFC 5322 (or RFC 2822, or RFC 822, sigh) message from
// r and writes it to w.
func rewriteMessage(r io.Reader, w io.Writer) error {
	_, err := copyMessagePart(newLineReader(r), w, "")
	return err
}

// copyMessagePart reads a message part consisting of a header, a blank line,
// and a body from lr and writes it to w. The part can either be a full RFC 5322/2822/822
// message or an RFC 2045/2046 message body part terminated by delim.
func copyMessagePart(lr *lineReader, w io.Writer, delim string) (end bool, err error) {
	hdata, err := copyHeader(lr, w)
	if err != nil {
		return false, err
	}

	if strings.HasPrefix(hdata.mediaType, "multipart/") {
		// RFC 2046 5.1.1:
		//  The only mandatory global parameter for the "multipart" media type is
		//  the boundary parameter, which consists of 1 to 70 characters from a
		//  set of characters known to be very robust through mail gateways, and
		//  NOT ending with white space. (If a boundary delimiter line appears to
		//  end with white space, the white space must be presumed to have been
		//  added by a gateway, and must be deleted.)
		//
		// I've seen invalid 71-character boundaries being used in the wild, e.g.
		// "--=_NextPart_5213_0a55_d6217661_9281_11d9_a2b8_0040529d55d7_alternative",
		// so I'm choosing to not check the length here.
		bnd := hdata.contentParams["boundary"]
		if bnd == "" {
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
		if end, err := copyBody(lr, w, subDelim); err != nil {
			return false, err
		} else if !end {
			// Next, copy the enclosed parts until we see the closing outer delimiter.
			// TODO: Is it valid for the preamble to be immediately followed by a
			// closing boundary delimiter?
			for {
				if end, err := copyMessagePart(lr, w, subDelim); err != nil {
					return false, err
				} else if end {
					break
				}
			}
		}
	}

	// Read the top-level body until we see the outer boundary.
	return copyBody(lr, w, delim)
}

// headerData contains information parsed by copyHeader from a message part.
type headerData struct {
	mediaType     string            // media type from Content-Type , e.g. "text-plain" or "multipart/mixed"
	contentParams map[string]string // additional parameters from Content-Type
}

// copyHeader reads the header portion of a message part from lr and writes it to w.
// The trailing blank line at the end of the header is written before returning.
func copyHeader(lr *lineReader, w io.Writer) (data headerData, err error) {
	data.mediaType = defaultMediaType
	data.contentParams = defaultContentParams
	gotContentType := false

	for {
		folded, unfolded, err := lr.readFoldedLine()
		if err == io.EOF {
			return data, errors.New("missing body")
		} else if err != nil {
			return data, err
		}

		// A blank line indicates the end of the header.
		if unfolded == "" {
			if len(folded) != 1 {
				return data, errors.New("blank line is folded") // should never happen
			}
			if _, err := io.WriteString(w, folded[0]); err != nil {
				return data, err
			}
			return data, nil // done
		}

		if key, val, err := parseHeaderField(unfolded); err != nil {
			return data, fmt.Errorf("malformed header field %q: %v", unfolded, err)
		} else if key == "Content-Type" && !gotContentType {
			mtype, params, err := mime.ParseMediaType(val)
			if err != nil {
				// RFC 2045 5.2:
				//  It is also recommend that this default be assumed when a
				//  syntactically invalid Content-Type header field is encountered.
				mtype = defaultMediaType
				params = defaultContentParams

				// TODO: Maybe still return an error for multipart?
				//return data, fmt.Errorf("unparseable Content-Type %q: %v", val, err)
			}

			data.mediaType = mtype
			data.contentParams = params
			gotContentType = true

			// TODO: If we see a media type indicating that the part should be dropped,
			// replace it with something like this (copying what mutt does when deleting
			// an attachment):
			//
			//  Content-Type: message/external-body; access-type=x-mutt-deleted;
			//          expiration="Mon, 6 Jan 2020 16:51:39 -0400"; length=340416
			//
			// Then add a blank line and write the rest of the headers as the part's body,
			// skip the original body, but still write the terminating delimiter.
			//
			// message/external-body is described in RFC 1341 7.3.3.
		}

		for _, ln := range folded {
			if _, err := io.WriteString(w, ln); err != nil {
				return data, err
			}
		}
	}
}

// copyBody reads lines from lr and writes them to w until it finds delim
// at the beginning of a line. The delimiter line is written before returning.
//
// The returned end value is true if the delimiter was suffixed by "--" or if delim is empty and
// EOF was encountered. If delim is non-empty and EOF is encountered, an error is returned.
func copyBody(lr *lineReader, w io.Writer, delim string) (end bool, err error) {
	for {
		ln, err := lr.readLine()
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
