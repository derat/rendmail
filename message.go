// Copyright 2022 Daniel Erat.
// All rights reserved.

package main

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// rewriteOptions contains options used to control rewriteMessage's behavior.
type rewriteOptions struct {
	DeleteMediaTypes []string  `json:"deleteMediaTypes"` // globs for attachment media types to delete
	KeepMediaTypes   []string  `json:"keepMediaTypes"`   // globs that override deleteMediaTypes
	Now              time.Time `json:"now"`              // current time
	DecodeSubject    bool      `json:"decodeSubject"`    // decode Subject header field to X-Rendmail-Subject
	Strict           bool      `json:"strict"`           // fail for bad messages

	verbose bool // write noisy messages to stderr
	silent  bool // set during testing
}

// rewriteMessage reads an RFC 5322 (or RFC 2822, or RFC 822, sigh) message from
// r and writes it to w.
func rewriteMessage(r io.Reader, w io.Writer, opts *rewriteOptions) error {
	lr := newLineReader(r)
	_, err := copyMessagePart(lr, w, "", opts)

	// If we encountered a message error in non-strict mode, try to copy the rest of the message.
	if _, ok := err.(*msgError); ok && !opts.Strict {
		if !opts.silent {
			fmt.Fprintln(os.Stderr, "Ignoring error:", err)
		}
		if _, err := io.Copy(w, lr.r); err != nil {
			return err
		}
		return nil
	}
	return err
}

// copyMessagePart reads a message part consisting of a header, a blank line,
// and a body from lr and writes it to w. The part can either be a full RFC 5322/2822/822
// message or an RFC 2045/2046 message body part terminated by delim.
func copyMessagePart(lr *lineReader, w io.Writer, delim string,
	opts *rewriteOptions) (end bool, err error) {
	hdata, err := copyHeader(lr, w, opts)
	if err != nil {
		return false, err
	}

	if strings.HasPrefix(hdata.mediaType, "multipart/") && !hdata.deletePart {
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
			return false, &msgError{fmt.Sprintf("invalid boundary %q", bnd)}
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
		if end, err := copyBody(lr, w, subDelim, false); err != nil {
			return false, err
		} else if !end {
			// Next, copy the enclosed parts until we see the closing outer delimiter.
			// TODO: Is it valid for the preamble to be immediately followed by a
			// closing boundary delimiter?
			for {
				if end, err := copyMessagePart(lr, w, subDelim, opts); err != nil {
					return false, err
				} else if end {
					break
				}
			}
		}
	}

	// Read the top-level body until we see the outer boundary.
	return copyBody(lr, w, delim, hdata.deletePart)
}

// headerData contains information parsed by copyHeader from a message part.
type headerData struct {
	mediaType     string            // media type from Content-Type , e.g. "text/plain" or "multipart/mixed"
	contentParams map[string]string // additional parameters from Content-Type
	deletePart    bool              // true if the message part should be deleted
}

// Defaults from RFC 2045 5.2, "Content-Type defaults".
var defaultMediaType, defaultContentParams, _ = mime.ParseMediaType("text/plain; charset=us-ascii")

// copyHeader reads the header portion of a message part from lr and writes it to w.
// The trailing blank line at the end of the header is written before returning.
func copyHeader(lr *lineReader, w io.Writer, opts *rewriteOptions) (data headerData, err error) {
	var term string // message's line terminator (either "\r\n" or "\n")

	data.mediaType = defaultMediaType
	data.contentParams = defaultContentParams
	gotContentType := false

	for {
		folded, unfolded, err := lr.readFoldedLine()
		if err == io.EOF {
			return data, &msgError{"missing body"}
		} else if err != nil {
			return data, err
		}

		// Use the first line to determine whether the message is using CRLF or just LF.
		if term == "" {
			if strings.HasSuffix(folded[0], "\r\n") {
				term = "\r\n"
			} else {
				term = "\n"
			}
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

		var newLines []string // new lines to write after this one

		var msgErr *msgError // returned later after writing the folded lines
		if key, val, err := parseHeaderField(unfolded); err != nil {
			// This can happen if the blank line between the header and body is missing, resulting
			// in us trying to parse a line from the body as a header. The only place that I've seen
			// this is in some pre-2009 messages where I'd deleted attachments using mutt (did
			// mutt's MIME implementation have a bug?). It also appears to be mentioned in
			// https://bugzilla.mozilla.org/show_bug.cgi?id=335189.
			msgErr = &msgError{fmt.Sprintf("malformed header field %q: %v", unfolded, err)}
		} else if key == "Content-Type" && !gotContentType {
			mtype, params, err := mime.ParseMediaType(val)
			if err != nil {
				if opts.verbose {
					fmt.Fprintf(os.Stderr, "Ignoring invalid Content-Type %q: %v\n", val, err)
				}
				// RFC 2045 5.2:
				//  It is also recommend that this default be assumed when a
				//  syntactically invalid Content-Type header field is encountered.
				mtype = defaultMediaType
				params = defaultContentParams
			}

			data.mediaType = mtype
			data.contentParams = params
			gotContentType = true

			if data.deletePart, err = shouldDelete(data.mediaType, opts.DeleteMediaTypes,
				opts.KeepMediaTypes); err != nil {
				return data, err
			} else if data.deletePart {
				if opts.verbose {
					fmt.Fprintln(os.Stderr, "Deleting "+data.mediaType)
				}

				// This is patterned after what mutt does when deleting an attachment.
				// It adds a header field like the following, followed by a blank line
				// (to end the header and start the body) and the rest of the original headers:
				//
				//  Content-Type: message/external-body; access-type=x-mutt-deleted;
				//          expiration="Mon, 6 Jan 2020 16:51:39 -0400"; length=340416
				//
				// message/external-body is described in RFC 1521 7.3.3 (replacing RFC 1341 7.3.3).
				if _, err := io.WriteString(
					w, "Content-Type: message/external-body; access-type=x-rendmail-deleted;"+term+
						"\texpiration=\""+opts.Now.Format(time.RFC1123Z)+"\""+term+
						term); err != nil {
					return data, err
				}
			}
		} else if key == "Subject" && opts.DecodeSubject {
			if dec, ok := decodeHeaderValue(val); ok && dec != "" && dec != val {
				// Just to mention it, RFC 6648 advocates avoiding "X-" headers, and they were
				// actually removed for email in RFC 2822 (after being described by RFC 822).
				newLines = append(newLines, foldHeaderField("X-Rendmail-Subject: "+dec, term)...)
			}
		}

		for _, ln := range folded {
			if _, err := io.WriteString(w, ln); err != nil {
				return data, err
			}
		}
		for _, ln := range newLines {
			if _, err := io.WriteString(w, ln); err != nil {
				return data, err
			}
		}

		// So that we'll still write the message in non-strict mode, only return an earlier
		// message error after we've written the folded lines.
		if msgErr != nil {
			return data, msgErr
		}
	}
}

// copyBody reads lines from lr and writes them to w until it finds delim
// at the beginning of a line. The delimiter line is written before returning.
// If deletePart is true, all lines up to but not including the delimiter are
// dropped instead of being written to w.
//
// The returned end value is true if the delimiter was suffixed by "--" or if delim is empty and
// EOF was encountered. If delim is non-empty and EOF is encountered, an error is returned.
func copyBody(lr *lineReader, w io.Writer, delim string, deletePart bool) (end bool, err error) {
	for {
		ln, err := lr.readLine()
		if err == io.EOF {
			if delim != "" {
				// This happens if a multipart message is truncated or the final delimiter is
				// missing for some reason.
				//
				// For example, hard_ham/0142.0220f772ab37ba8d5899fc62f6878edf from the SpamAssassin
				// corpus appears to be a multipart/alternative Oracle newsletter from 2002 that's
				// missing an ending "--next_part_of_message--" delimiter.
				return false, &msgError{fmt.Sprintf("EOF while looking for delimiter %q", delim)}
			}
			return true, nil // done
		} else if err != nil {
			return false, err
		}

		isDelim := delim != "" && strings.HasPrefix(ln, delim)
		if !deletePart || isDelim {
			if _, err := io.WriteString(w, ln); err != nil {
				return false, err
			}
		}
		if isDelim {
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

// decodeHeaderValue attempts to convert an RFC 2047 header value to 7-bit ASCII.
// The returned bool is false if the conversion failed (e.g. the original value
// used an unsupported charset). Any non-ASCII characters left after decoding and
// conversion are dropped.
func decodeHeaderValue(unfolded string) (string, bool) {
	// First, try to decode from the RFC 2047 form (i.e. Quoted-Printable or base64).
	dec, err := headerDecoder.DecodeHeader(unfolded)
	if err != nil {
		return "", false
	}
	// Next, remove accents and then drop anything that's not 7-bit ASCII.
	res, _, err := transform.String(headerTransformChain, dec)
	return res, err == nil
}

// These are used by decodeHeaderValue.
var headerDecoder = mime.WordDecoder{
	// By default, WordDecoder only supports the utf-8, iso-8859-1 and us-ascii charsets.
	CharsetReader: func(charset string, input io.Reader) (io.Reader, error) {
		switch {
		case strings.EqualFold("windows-1252", charset):
			return charmap.Windows1252.NewDecoder().Reader(input), nil
		default:
			return nil, fmt.Errorf("unhandled charset %q", charset)
		}
	},
}
var headerTransformChain = transform.Chain(
	norm.NFD, // decompose by canonical equivalence
	runes.Remove(runes.In(unicode.Mn)), // remove "Mark, nonspacing"
	norm.NFC, // recompose by canonical equivalence
	runes.Remove(runes.Predicate(func(r rune) bool { // remove non-printable ASCII
		// From RFC 5322 2.2:
		//  A field name MUST be composed of printable US-ASCII characters (i.e., characters
		//  that have values between 33 and 126, inclusive), except colon.  A field body may be
		//  composed of printable US-ASCII characters as well as the space (SP, ASCII value 32)
		//  and horizontal tab (HTAB, ASCII value 9) characters (together known as the white
		//  space characters, WSP).
		return (r < 32 || r > 126) && r != 9
	})),
)

// foldHeaderField wraps unfolded across multiple lines, each of which will be terminated
// with term ("\r\n" or "\n"). See RFC 5322 2.2.3.
func foldHeaderField(unfolded, term string) []string {
	var folded []string
	for _, p := range foldRegexp.FindAllString(unfolded, -1) {
		if len(folded) == 0 {
			folded = append(folded, p)
		} else if len(folded[len(folded)-1])+len(p) <= 78 {
			folded[len(folded)-1] += p
		} else {
			folded[len(folded)-1] += term
			folded = append(folded, p)
		}
	}
	if len(folded) > 0 {
		folded[len(folded)-1] += term
	}
	return folded
}

// foldRegexp matches any number of space or tab characters followed by one or more
// non-space/tab characters.
var foldRegexp = regexp.MustCompile(`[ \t]*[^ \t]+`)

// shouldDelete returns true if attachments of type mtype should be deleted.
// del and keep correspond to deleteMediaTypes and keepMediaTypes in rewriteOptions.
// An error is only returned if an invalid glob is encountered.
func shouldDelete(mtype string, del, keep []string) (bool, error) {
	for _, dp := range del {
		if dm, err := filepath.Match(dp, mtype); err != nil {
			return false, err
		} else if dm {
			for _, kp := range keep {
				if km, err := filepath.Match(kp, mtype); err != nil {
					return false, err
				} else if km {
					return false, nil // in keep
				}
			}
			return true, nil // matched by del and not by keep
		}
	}
	return false, nil // not matched by del
}

// msgError describes an error encountered within a message.
// Regular error objects are used for errors encountered while reading or writing.
type msgError struct{ text string }

func (err *msgError) Error() string { return err.text }
