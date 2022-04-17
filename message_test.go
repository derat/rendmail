// Copyright 2022 Daniel Erat.
// All rights reserved.

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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
			in, err := ioutil.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}

			base := p[:len(p)-len(suf)]

			opts := rewriteOptions{silent: true}
			optsPath := base + ".opts.json"
			if _, err := os.Stat(optsPath); err == nil {
				if b, err := ioutil.ReadFile(optsPath); err != nil {
					t.Fatal(err)
				} else if err := json.Unmarshal(b, &opts); err != nil {
					t.Fatalf("Failed unmarshaling %v: %v", optsPath, err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}

			var b bytes.Buffer
			err = rewriteMessage(bytes.NewReader(in), &b, &opts)
			if opts.Strict {
				// Use the strict flag as a signal that we expect an error.
				if err == nil {
					t.Fatal("rewriteMessage unexpectedly succeeded in strict mode")
				}
				return
			}
			if err != nil {
				t.Fatal("rewriteMessage failed:", err)
			}
			got := b.String()

			outPath := base + ".out.txt"
			want, err := ioutil.ReadFile(outPath)
			if err != nil {
				t.Fatal(err)
			}

			if got != string(want) {
				cmd := exec.Command("diff", "-", outPath)
				cmd.Stdin = &b
				out, _ := cmd.Output()
				t.Error("rewriteMessage produced unexpected output (got vs. want):\n" + string(out))
			}

			// If the original message was valid, check that the rewritten one was too.
			if err := checkTestMessage(bytes.NewReader(in)); err == nil {
				if err := checkTestMessage(strings.NewReader(got)); err != nil {
					t.Error("rewriteMessage produced invalid message:", err)
				}
			}
		})
	}
}

// checkTestMesage uses the net/mail and mime/multipart packages to read an email message from r.
// An error is returned if the message is broken (in terms of RFC 5322/6532 and 2046).
func checkTestMessage(r io.Reader) error {
	var checkPart func(map[string][]string, io.Reader) error
	checkPart = func(header map[string][]string, body io.Reader) error {
		mtype, params, err := mime.ParseMediaType(textproto.MIMEHeader(header).Get("Content-Type"))
		if err != nil {
			mtype = defaultMediaType
			params = defaultContentParams
		}
		if !strings.HasPrefix(mtype, "multipart/") {
			return nil // non-multipart body, so we're done
		}
		mr := multipart.NewReader(body, params["boundary"])
		for {
			if part, err := mr.NextPart(); err == io.EOF {
				return nil // no more parts in the body
			} else if err != nil {
				return err
			} else if err := checkPart(part.Header, part); err != nil {
				return err
			}
		}
	}

	msg, err := mail.ReadMessage(r)
	if err != nil {
		return err
	}
	return checkPart(msg.Header, msg.Body)
}

func TestDecodeHeaderValue(t *testing.T) {
	for _, tc := range []struct {
		orig string
		dec  string
		ok   bool
	}{
		{"", "", true},
		{" ", " ", true},
		{"regular text", "regular text", true},
		{"¡confirmación, 再见, hi!", "confirmacion, , hi!", true}, // diacritic removed, non-ASCII dropped
		// Various examples from RFC 2047:
		{"=?iso-8859-1?q?this=20is=20some=20text?=", "this is some text", true},
		{"=?US-ASCII?Q?Keith_Moore?= <moore@cs.utk.edu>", "Keith Moore <moore@cs.utk.edu>", true},
		{"=?ISO-8859-1?Q?Keld_J=F8rn_Simonsen?= <keld@dkuug.dk>", "Keld Jrn Simonsen <keld@dkuug.dk>", true}, // ø isn't diacritical
		{"=?ISO-8859-1?Q?Andr=E9?= Pirard <PIRARD@vm1.ulg.ac.be>", "Andre Pirard <PIRARD@vm1.ulg.ac.be>", true},
		{"=?ISO-8859-1?Q?Olle_J=E4rnefors?= <ojarnef@admin.kth.se>", "Olle Jarnefors <ojarnef@admin.kth.se>", true},
		{"=?ISO-8859-1?Q?Patrik_F=E4ltstr=F6m?= <paf@nada.kth.se>", "Patrik Faltstrom <paf@nada.kth.se>", true},
		{"(=?ISO-8859-1?Q?a?=)", "(a)", true},
		{"(=?ISO-8859-1?Q?a?= b)", "(a b)", true},
		{"(=?ISO-8859-1?Q?a?= =?ISO-8859-1?Q?b?=)", "(ab)", true},
		{"(=?ISO-8859-1?Q?a?=  =?ISO-8859-1?Q?b?=)", "(ab)", true},
		{"(=?ISO-8859-1?Q?a?=\r\n    =?ISO-8859-1?Q?b?=)", "(ab)", true}, // we shouldn't actually pass line breaks
		{"(=?ISO-8859-1?Q?a_b?=)", "(a b)", true},
		{"(=?ISO-8859-1?Q?a?= =?ISO-8859-2?Q?_b?=)", "", false}, // unsupported charset
	} {
		if dec, ok := decodeHeaderValue(tc.orig); dec != tc.dec || ok != tc.ok {
			t.Errorf("decodeHeaderValue(%q) = (%q, %v); want (%q, %v)", tc.orig, dec, ok, tc.dec, tc.ok)
		}
	}
}

func TestFoldHeaderField(t *testing.T) {
	var (
		a38 = strings.Repeat("a", 38)
		a69 = strings.Repeat("a", 69) // 78 chars when preceded by "Subject: "
		a70 = strings.Repeat("a", 70) // 79 chars when preceded by "Subject: "
		a78 = strings.Repeat("a", 78) // always exceeds limit when preceded by "Subject: " or " "
	)

	for _, tc := range []struct {
		unfolded string
		term     string
		want     []string
	}{
		{"", "\n", nil},  // we shouldn't pass an empty string
		{" ", "\n", nil}, // we shouldn't pass just whitespace
		{"From: me", "\n", []string{"From: me\n"}},
		{"Subject: Some words", "\r\n", []string{"Subject: Some words\r\n"}},
		{"Subject: " + a69, "\n", []string{"Subject: " + a69 + "\n"}},
		{"Subject: " + a70, "\n", []string{"Subject:\n", " " + a70 + "\n"}},
		{"Subject: " + a69 + "\t" + a38 + " " + a38 + " " + a38, "\n",
			[]string{"Subject: " + a69 + "\n", "\t" + a38 + " " + a38 + "\n", " " + a38 + "\n"}},
		{"Subject: " + a78 + " " + a78, "\n", []string{"Subject:\n", " " + a78 + "\n", " " + a78 + "\n"}},
	} {
		if got := foldHeaderField(tc.unfolded, tc.term); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("foldHeaderField(%q, %q) = %q; want %q", tc.unfolded, tc.term, got, tc.want)
		}
	}
}

func TestShouldDelete(t *testing.T) {
	for _, tc := range []struct {
		mtype     string
		del, keep []string
		want      bool
	}{
		{"text/plain", nil, nil, false},
		{"text/plain", []string{"audio/*", "image/*"}, nil, false},
		{"image/jpeg", []string{"audio/*", "image/*"}, nil, true},
		{"image/jpeg", []string{"audio/*", "image/*"}, []string{"image/png"}, true},
		{"image/jpeg", []string{"audio/*", "image/*"}, []string{"image/png", "image/jpeg"}, false},
	} {
		if got, err := shouldDelete(tc.mtype, tc.del, tc.keep); err != nil {
			t.Errorf("shouldDelete(%q, %q, %q) failed: %v", tc.mtype, tc.del, tc.keep, err)
		} else if got != tc.want {
			t.Errorf("shouldDelete(%q, %q, %q) = %v; want %v", tc.mtype, tc.del, tc.keep, got, tc.want)
		}
	}
}
