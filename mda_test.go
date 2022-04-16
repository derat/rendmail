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
	mdaMsg  = "testdata/sa_easy_ham_2_00869.0fbb783356f6875063681dc49cfcb1eb-delete"
	mdaDate = "2021-02-18T21:54:42.123Z" // matches .opts.json file
)

// runMDATest uses a mail delivery agent to perform end-to-end testing.
func runMDATest(t *testing.T, cfgTmpl string, cmdFunc func(cfg string) *exec.Cmd) {
	rp, err := exec.LookPath("rendmail")
	if err != nil {
		t.Fatal(err)
	}

	td := t.TempDir()

	// Create directories for the MDA and rendmail to write to.
	bdir := filepath.Join(td, "backup")
	if err := os.Mkdir(bdir, 0755); err != nil {
		t.Fatal(err)
	}
	inbox := filepath.Join(td, "inbox")
	if err := os.Mkdir(inbox, 0755); err != nil {
		t.Fatal(err)
	}

	// Write the MDA's config file.
	tmpl := template.Must(template.New("cfg").Parse(strings.TrimLeft(cfgTmpl, "\n")))
	cp := filepath.Join(td, "config")
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
		LogFile:      filepath.Join(td, "log"),
		RendmailPath: rp,
		FakeNow:      mdaDate,
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
	mp := mdaMsg + ".in.txt"
	mf, err := os.Open(mp)
	if err != nil {
		t.Fatal(err)
	}
	defer mf.Close()

	// Run the MDA.
	cmd := cmdFunc(cp)
	cmd.Stdin = mf
	if err := cmd.Run(); err != nil {
		t.Fatalf("%q failed: %v", strings.Join(cmd.Args, " "), err)
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
	compare(filepath.Join(inbox, "new"), mdaMsg+".out.txt") // modified message
	compare(bdir, mp)                                       // original, backed-up message
}

func TestProcmail(t *testing.T) {
	runMDATest(t, procmailrcTemplate, func(cfg string) *exec.Cmd {
		return exec.Command("procmail", "-m", cfg)
	})
}

const procmailrcTemplate = `
VERBOSE=on
LOGFILE={{.LogFile}}

:0 hbfw
| {{.RendmailPath}} -delete-binary -fake-now={{.FakeNow}} -backup-dir={{.BackupDir}} -verbose

:0
{{.Inbox}}/
`

func TestFDM(t *testing.T) {
	runMDATest(t, fdmConfTemplate, func(cfg string) *exec.Cmd {
		return exec.Command("fdm", "-vv", "-m", "-f", cfg, "fetch")
	})
}

const fdmConfTemplate = `
set no-received
account "stdin" stdin
match all
      action rewrite "{{.RendmailPath}} -delete-binary -fake-now={{.FakeNow}} -backup-dir={{.BackupDir}} -verbose"
      continue
match all action maildir "{{.Inbox}}"
`
