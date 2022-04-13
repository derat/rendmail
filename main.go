// Copyright 2022 Daniel Erat.
// All rights reserved.

package main

import (
	"fmt"
	"os"
)

func main() {
	os.Exit(func() int {
		if err := rewriteMessage(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "Failed rewriting message:", err)
			return 1
		}
		return 0
	}())
}
