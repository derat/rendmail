// Copyright 2022 Daniel Erat.
// All rights reserved.

package main

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRewriteMessage(t *testing.T) {
	const suf = ".in.txt"
	inPaths, err := filepath.Glob("testdata/*" + suf)
	if err != nil {
		t.Fatal("Failed getting input files:", err)
	}

	for _, p := range inPaths {
		t.Run(p, func(t *testing.T) {
			in, err := os.Open(p)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()

			op := p[:len(p)-len(suf)] + ".out.txt"
			want, err := ioutil.ReadFile(op)
			if err != nil {
				t.Fatal(err)
			}

			var b bytes.Buffer
			if err := rewriteMessage(in, &b); err != nil {
				t.Fatal("rewriteMessage failed:", err)
			}
			if got := b.String(); got != string(want) {
				cmd := exec.Command("diff", "-", op)
				cmd.Stdin = &b
				out, _ := cmd.Output()
				t.Error("rewriteMessage produced bad output (got vs. want):\n" + string(out))
			}
		})
	}
}
