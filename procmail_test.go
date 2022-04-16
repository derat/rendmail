// Copyright 2022 Daniel Erat.
// All rights reserved.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
)

const (
	procmailMsg  = "testdata/sa_easy_ham_2_00869.0fbb783356f6875063681dc49cfcb1eb-delete"
	procmailDate = "2021-02-18T21:54:42.123Z" // matches .opts.json file
)

func TestProcmail(t *testing.T) {
	rp, err := exec.LookPath("rendmail")
	if err != nil {
		t.Fatal(err)
	}

	// Create directories for procmail.
	td := t.TempDir()
	bdir := filepath.Join(td, "backup")
	if err := os.Mkdir(bdir, 0755); err != nil {
		t.Fatal(err)
	}
	inbox := filepath.Join(td, "inbox")
	if err := os.Mkdir(inbox, 0755); err != nil {
		t.Fatal(err)
	}

	// Write the .procmailrc file.
	tmpl := template.Must(template.New("rc").Parse(strings.TrimLeft(procmailrcTemplate, "\n")))
	cp := filepath.Join(td, ".procmailrc")
	cf, err := os.Create(cp)
	if err != nil {
		t.Fatal(err)
	}
	if err := tmpl.Execute(cf, struct {
		LogFile      string
		RendmailPath string
		FakeNow      string
		BackupDir    string
		Inbox        string
	}{
		LogFile:      filepath.Join(td, "procmail.log"),
		RendmailPath: rp,
		FakeNow:      procmailDate,
		BackupDir:    bdir,
		Inbox:        inbox,
	}); err != nil {
		cf.Close()
		t.Fatal("Executing template failed:", err)
	}
	if err := cf.Close(); err != nil {
		t.Fatal(err)
	}

	// Open the source message file.
	mp := procmailMsg + ".in.txt"
	mf, err := os.Open(mp)
	if err != nil {
		t.Fatal(err)
	}
	defer mf.Close()

	// Run procmail.
	cmd := exec.Command("procmail", "-m", cp)
	cmd.Stdin = mf
	if err := cmd.Run(); err != nil {
		t.Fatal("procmail failed:", err)
	}

	// Compares a single file in gotDir against wantPath.
	compare := func(gotDir, wantPath string) {
		var gotPath string
		if paths, err := filepath.Glob(gotDir + "/*"); err != nil {
			t.Fatal(err)
		} else if len(paths) != 1 {
			t.Errorf("%v contains %q; want 1 file", gotDir, paths)
			return
		} else {
			gotPath = paths[0]
		}
		if out, err := exec.Command("diff", gotPath, wantPath).Output(); err != nil {
			t.Errorf("%v doesn't match %v:\n%s", gotPath, wantPath, out)
		}
	}
	compare(filepath.Join(inbox, "new"), procmailMsg+".out.txt") // modified message
	compare(bdir, mp)                                            // original, backed-up message
}

const procmailrcTemplate = `
VERBOSE=on
LOGFILE={{.LogFile}}

:0 hbfw
| {{.RendmailPath}} -delete-binary -fake-now={{.FakeNow}} -backup-dir={{.BackupDir}} -verbose

:0
{{.Inbox}}/
`
