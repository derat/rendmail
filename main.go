// Copyright 2022 Daniel Erat.
// All rights reserved.

package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"time"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flag]...\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Reads an email message from stdin and rewrites it to stdout.\n\n")
		flag.PrintDefaults()
	}
	origDir := flag.String("orig-dir", "", "Directory to write original, unmodified message to")
	flag.Parse()

	os.Exit(func() (code int) {
		input := io.Reader(os.Stdin)

		if *origDir != "" {
			ts := time.Now().UTC().Format("20060102-150405.999")
			f, err := ioutil.TempFile(*origDir, ts+"-*")
			if err != nil {
				fmt.Fprintln(os.Stderr, "Failed creating file:", err)
				return 1
			}
			input = io.TeeReader(input, f)

			defer func() {
				// Drain the reader to write the unread portion of the message to the file
				// in case rewriteMessage encountered an error.
				if _, err := io.Copy(ioutil.Discard, input); err != nil {
					fmt.Fprintf(os.Stderr, "Failed copying message to %v: %v\n", f.Name(), err)
					code = 1
				}
				if err := f.Close(); err != nil {
					fmt.Fprintln(os.Stderr, "Failed closing file:", err)
					code = 1
				}
			}()
		}

		if err := rewriteMessage(input, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "Failed rewriting message:", err)
			return 1
		}
		return 0
	}())
}
