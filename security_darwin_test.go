package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNamesCannotBreakLines(t *testing.T) {
	evil := "render\r\nBcc: attacker@example.com \x00x"
	got := oneLine(evil)
	if strings.ContainsAny(got, "\r\n\x00 ") {
		t.Fatalf("control characters survived: %q", got)
	}
	if oneLine("Google Chrome Helper (Renderer)") != "Google Chrome Helper (Renderer)" {
		t.Fatal("ordinary names must not change")
	}
}

func TestAlertEmailHeadersStayIntact(t *testing.T) {
	subject := "[TaskLens] evil\r\nBcc: attacker@example.com is using 40 GB"
	if line := "Subject: " + mimeHeader(oneLine(subject)); strings.ContainsAny(line, "\r\n") {
		t.Fatalf("header injection still possible: %q", line)
	}
}

func TestSpecialFilesInReportFolderAreSkipped(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "trap-2026-11-12-150000.ips"), 0o600); err != nil {
		t.Skip(err)
	}
	os.Symlink("/etc/passwd", filepath.Join(dir, "link-2026-11-12-150000.ips"))
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if e.Type().IsRegular() {
			t.Fatalf("%s should not count as an ordinary file", e.Name())
		}
	}
	done := make(chan struct{})
	go func() { readReportsIn(dir, time.Now().AddDate(0, 0, -1), &signals{}); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reading the report folder hung on a special file")
	}
}

func TestChangingMailServerForgetsPassword(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var forgot []string
	forgetSecret = func(_, user string) { forgot = append(forgot, user) }
	defer func() { forgetSecret = secretDelete }()
	st := &settingsStore{s: defaultSettings()}
	st.s.Email.Username = "me@gmail.com"
	st.s.Email.FallbackPassword = "secret"
	next := st.get()
	next.Email.FallbackPassword = ""
	if err := st.save(next); err != nil {
		t.Fatal(err)
	}
	if st.s.Email.FallbackPassword != "secret" {
		t.Fatal("saving unrelated settings must keep the password")
	}
	next.Email.Host, next.Email.Port = "smtp.attacker.example", 465
	if err := st.save(next); err != nil {
		t.Fatal(err)
	}
	if st.s.Email.FallbackPassword != "" || len(forgot) != 1 {
		t.Fatal("a new mail server must not receive the old password")
	}
}
