package emlfile

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/xceg/sphinx/internal/leak"
)

var attachments = Engine{Inspect: func(b []byte) ([]leak.Finding, error) {
	if bytes.HasPrefix(b, []byte{0xFF, 0xD8}) {
		return []leak.Finding{leak.GPSLocation.Find("The photo records the exact place it was taken.", "map")}, nil
	}
	return nil, nil
}}

// testdata/forwarded.eml was built with Python's email package: two
// Received hops from a home network, a Bcc, a forwarded thread, hidden
// HTML and a JPEG attachment.
func TestForwardedMessage(t *testing.T) {
	b, err := os.ReadFile("testdata/forwarded.eml")
	if err != nil {
		t.Fatal(err)
	}
	if !Looks(b) {
		t.Fatal("not recognised as an email")
	}
	fs, err := Inspect(b, attachments)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range fs {
		got[f.Rule] += f.Message + " | " + f.Evidence + "\n"
	}
	checks := map[*leak.Rule][]string{
		leak.SenderNetwork:      {"192.168.1.23 (private network)", "81.56.12.7 (public address)", "tbecker-macbook.local"},
		leak.ThreadHistory:      {"2 earlier emails", "Hugo Lenz"},
		leak.PersonalNames:      {"blind-copied", "Ines Morel"},
		leak.HiddenInstructions: {"ignore previous instructions"},
		leak.GPSLocation:        {"In the attachment team-offsite.jpg"},
		leak.SoftwareTrail:      {"Apple Mail"},
	}
	for rule, wants := range checks {
		for _, w := range wants {
			if !strings.Contains(got[rule.ID], w) {
				t.Errorf("%s lacks %q:\n%s", rule.Name, w, got[rule.ID])
			}
		}
	}
	arts, _ := Reveal(b)
	names := ""
	for _, a := range arts {
		names += a.Name + " "
	}
	if !strings.Contains(names, "message.txt") || !strings.Contains(names, "attachment-team-offsite.jpg") {
		t.Fatalf("reveal: %s", names)
	}
}

func TestPlainTextIsNotEmail(t *testing.T) {
	for _, s := range []string{
		"Notes:\nFrom: the meeting\nDate: tomorrow\n\nbody",
		"package main\n\nfunc main() {}\n",
		"Subject line only\n\n",
	} {
		if Looks([]byte(s)) {
			t.Errorf("%q taken for an email", s)
		}
	}
}

func TestQuietMessage(t *testing.T) {
	msg := "From: a@example.com\r\nTo: b@example.com\r\nSubject: hi\r\nDate: Mon, 13 May 2024 09:14:55 +0000\r\n\r\nLunch at noon?\r\n"
	fs, err := Inspect([]byte(msg), attachments)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if f.Severity >= leak.Medium {
			t.Fatalf("a plain message is not a leak: %+v", f)
		}
	}
}
