// Copyright 2022 Daniel Erat.
// All rights reserved.

package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"time"
)

func main() {
	opts := rewriteOptions{Now: time.Now()}

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flag]...\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Reads an email message from stdin and rewrites it to stdout.\n\n")
		flag.PrintDefaults()
	}
	backupDir := flag.String("backup-dir", "", "Directory to which original, unmodified message will be saved")
	deleteBinary := flag.Bool("delete-binary", false, "Delete common binary attachments from message")
	deleteTypes := flag.String("delete-types", "", "Comma-separated globs of attachment media types to delete")
	fakeNow := flag.String("fake-now", "", "Hardcoded RFC 3339 time (only used for testing)")
	keepTypes := flag.String("keep-types", "", "Comma-separated glob overrides for -delete-types")
	flag.BoolVar(&opts.Verbose, "verbose", false, "Write informative messages to stderr")

	flag.Parse()

	os.Exit(func() (code int) {
		if *fakeNow != "" {
			var err error
			if opts.Now, err = time.Parse(time.RFC3339, *fakeNow); err != nil {
				fmt.Fprintln(os.Stderr, "Bad -fake-now time:", err)
				return 2
			}
		}

		if *deleteBinary {
			if *deleteTypes != "" || *keepTypes != "" {
				fmt.Fprintln(os.Stderr, "-delete-binary is incompatible with -delete-types and -keep-types")
				return 2
			}
			opts.DeleteMediaTypes = binaryDeleteTypes
			opts.KeepMediaTypes = binaryKeepTypes
		} else {
			opts.DeleteMediaTypes = splitList(*deleteTypes)
			opts.KeepMediaTypes = splitList(*keepTypes)
		}

		input := io.Reader(os.Stdin)
		if *backupDir != "" {
			if err := os.MkdirAll(*backupDir, 0700); err != nil {
				fmt.Fprintln(os.Stderr, "Failed creating backup dir:", err)
				return 1
			}
			f, err := ioutil.TempFile(*backupDir, opts.Now.UTC().Format("20060102-150405.999")+"-*")
			if err != nil {
				fmt.Fprintln(os.Stderr, "Failed creating backup file:", err)
				return 1
			}
			input = io.TeeReader(input, f)

			defer func() {
				// Drain the reader to write the unread portion of the message to the file
				// in case rewriteMessage encountered an error.
				if _, err := io.Copy(ioutil.Discard, input); err != nil {
					fmt.Fprintf(os.Stderr, "Failed writing message to %v: %v\n", f.Name(), err)
					code = 1
				}
				if err := f.Close(); err != nil {
					fmt.Fprintln(os.Stderr, "Failed closing file:", err)
					code = 1
				}
			}()
		}

		if err := rewriteMessage(input, os.Stdout, &opts); err != nil {
			fmt.Fprintln(os.Stderr, "Failed rewriting message:", err)
			return 1
		}
		return 0
	}())
}

// Binary media type patterns used for -delete-binary.
var binaryDeleteTypes = []string{
	"application/*",
	"audio/*",
	"image/*",
	"video/*",
}

// application/ includes various non-binary types, so we explicitly keep some.
// This list is probably woefully incomplete; it's based on my own corpus,
// https://developer.mozilla.org/en-US/docs/Web/HTTP/Basics_of_HTTP/MIME_types/Common_types,
// and https://www.iana.org/assignments/media-types/media-types.xhtml.
var binaryKeepTypes = []string{
	"application/ecmascript",
	"application/ics",
	"application/javascript",
	"application/json",
	"application/pgp-*", // signature, encrypted, keys
	"application/pkcs7-signature",
	"application/rtf", // may include embedded images
	"application/xml",

	"application/*+json",
	"application/*+xml",

	"application/x-csh",
	"application/x-dia-diagram",
	"application/x-ecmascript",
	"application/x-httpd-php",
	"application/x-javascript",
	"application/x-perl",
	"application/x-ruby",
	"application/x-sh",
}

// splitList returns items from the supplied comma-separated list.
// Whitespace around items is trimmed and empty items are omitted.
func splitList(list string) []string {
	var items []string
	for _, s := range strings.Split(list, ",") {
		if s = strings.TrimSpace(s); s != "" {
			items = append(items, s)
		}
	}
	return items
}
