// Package emlfile inspects saved email messages (.eml): their headers, the
// thread they quote, hidden text in their HTML, and their attachments.
package emlfile

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"regexp"
	"strconv"
	"strings"

	"github.com/xceg/sphinx/internal/aitext"
	"github.com/xceg/sphinx/internal/leak"
)

// Engine reaches the inspectors of attachments without an import cycle.
type Engine struct {
	Inspect func([]byte) ([]leak.Finding, error)
}

var errNotEmail = errors.New("not an email message")

// Looks reports whether b starts like a saved email: a header block with
// several of the fields every message has.
func Looks(b []byte) bool {
	head := b[:min(len(b), 8192)]
	if i := bytes.Index(head, []byte("\n\n")); i < 0 && !bytes.Contains(head, []byte("\r\n\r\n")) {
		return false
	}
	first := bytes.SplitN(head, []byte("\n"), 2)[0]
	if !headerLine.Match(first) {
		return false
	}
	n := 0
	for _, h := range []string{"\nFrom:", "\nDate:", "\nSubject:", "\nMessage-ID:", "\nMessage-Id:", "\nReceived:", "\nMIME-Version:", "\nTo:"} {
		if bytes.Contains(append([]byte("\n"), head...), []byte(h)) {
			n++
		}
	}
	return n >= 3
}

var headerLine = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*:\s?`)

type part struct {
	contentType string
	filename    string
	body        []byte
}

// parts flattens a MIME tree into its leaf parts, decoded.
func parts(h mail.Header, body io.Reader, depth int) []part {
	ct := h.Get("Content-Type")
	media, params, err := mime.ParseMediaType(ct)
	if err != nil {
		media = "text/plain"
	}
	if strings.HasPrefix(media, "multipart/") && params["boundary"] != "" && depth < 8 {
		var out []part
		mr := multipart.NewReader(body, params["boundary"])
		for {
			p, err := mr.NextRawPart()
			if err != nil {
				break
			}
			out = append(out, parts(mail.Header(p.Header), p, depth+1)...)
		}
		return out
	}
	raw, _ := io.ReadAll(io.LimitReader(body, 256<<20))
	switch strings.ToLower(h.Get("Content-Transfer-Encoding")) {
	case "base64":
		dec, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(raw)), ""))
		if err == nil {
			raw = dec
		}
	case "quoted-printable":
		if dec, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(raw))); err == nil {
			raw = dec
		}
	}
	name := ""
	if _, p, err := mime.ParseMediaType(h.Get("Content-Disposition")); err == nil {
		name = p["filename"]
	}
	if name == "" {
		name = params["name"]
	}
	if dec, err := new(mime.WordDecoder).DecodeHeader(name); err == nil {
		name = dec
	}
	return []part{{contentType: media, filename: name, body: raw}}
}

var (
	bracketIP   = regexp.MustCompile(`\[(\d{1,3}(?:\.\d{1,3}){3}|IPv6:[0-9a-fA-F:]+)\]`)
	fromHost    = regexp.MustCompile(`(?i)^from\s+(\S+)`)
	localHost   = regexp.MustCompile(`(?i)\.(local|lan|home|internal|corp|localdomain)\b|^[a-z0-9-]+$`)
	quotedStart = regexp.MustCompile(`(?im)^(?:>\s*)*(?:On .{5,120} wrote:|Le .{5,120} a écrit\s?:|-{2,}\s*Original Message\s*-{2,}|-{2,}\s*Forwarded message\s*-{2,}|Begin forwarded message:)\s*$`)
	quotedFrom  = regexp.MustCompile(`(?im)^(?:>\s*)*(?:From|De)\s?:\s*(.+)$`)
	hiddenHTML  = regexp.MustCompile(`(?is)<([a-z0-9]+)[^>]*style\s*=\s*["'][^"']*(?:display\s*:\s*none|visibility\s*:\s*hidden|font-size\s*:\s*0(?:px|pt|em)?\s*[;"']|opacity\s*:\s*0\s*[;"']|color\s*:\s*(?:#fff(?:fff)?|white)\b)[^"']*["'][^>]*>(.*?)</([a-z0-9]+)>`)
	tags        = regexp.MustCompile(`(?s)<[^>]+>`)
)

func Inspect(b []byte, e Engine) ([]leak.Finding, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(b))
	if err != nil {
		return nil, errNotEmail
	}
	h := msg.Header
	var out []leak.Finding
	if f, ok := network(h); ok {
		out = append(out, f)
	}
	if bcc := h.Get("Bcc"); bcc != "" {
		out = append(out, leak.PersonalNames.Find("The message lists blind-copied recipients, who were meant to stay hidden from the others.", decode(bcc)).Escalate(leak.High).At("Bcc header"))
	}
	var people []string
	for _, k := range []string{"From", "To", "Cc", "Reply-To"} {
		if v := h.Get(k); v != "" {
			people = append(people, strings.ToLower(k)+": "+decode(v))
		}
	}
	if len(people) > 0 {
		out = append(out, leak.PersonalNames.Find("The message names its sender and recipients.", strings.Join(people, "; ")).Lower(leak.Low).At("headers"))
	}
	var client []string
	for _, k := range []string{"X-Mailer", "User-Agent", "X-MimeOLE"} {
		if v := h.Get(k); v != "" {
			client = append(client, v)
		}
	}
	if len(client) > 0 {
		out = append(out, leak.SoftwareTrail.Find("The message names the mail program that sent it.", strings.Join(client, "; ")).At("headers"))
	}
	if d := h.Get("Date"); d != "" {
		out = append(out, leak.Timestamps.Find("The message records when it was sent, with the sender's time zone.", d).At("Date header"))
	}

	var text, hidden strings.Builder
	for _, p := range parts(mail.Header(h), msg.Body, 0) {
		switch {
		case p.filename == "" && p.contentType == "text/plain":
			text.Write(p.body)
		case p.filename == "" && p.contentType == "text/html":
			for _, m := range hiddenHTML.FindAllSubmatch(p.body, 64) {
				if t := strings.Join(strings.Fields(htmlText(m[2])), " "); t != "" {
					hidden.WriteString(t + "\n")
				}
			}
			if text.Len() == 0 {
				text.WriteString(htmlText(p.body))
			}
		case len(p.body) > 0:
			fs, _ := e.Inspect(p.body)
			name := p.filename
			if name == "" {
				name = p.contentType
			}
			for _, f := range fs {
				f.Message = "In the attachment " + name + ": " + f.Message
				loc := "attachment " + name
				if f.Location != "" {
					loc += ", " + f.Location
				}
				out = append(out, f.At(loc))
			}
		}
	}
	if f, ok := thread(text.String()); ok {
		out = append(out, f)
	}
	if hd := strings.TrimSpace(hidden.String()); hd != "" {
		if hit := aitext.Match(hd); hit != "" {
			out = append(out, leak.HiddenInstructions.Find("Text hidden in the message's HTML gives instructions to the AI tools that will read it.", strconv.Quote(hit)))
		}
		out = append(out, leak.InvisibleText.Find("The HTML body holds text that is styled to be invisible.", strconv.Quote(truncate(hd, 160))))
	}
	return out, nil
}

// network reports the first hops of the Received chain, which is where the
// sender's machine and network appear, plus X-Originating-IP.
func network(h mail.Header) (leak.Finding, bool) {
	received := h["Received"]
	var hops []string
	// Received headers are prepended by each server, so the sender's side
	// is at the end of the list.
	for i := len(received) - 1; i >= 0 && len(hops) < 4; i-- {
		r := strings.Join(strings.Fields(received[i]), " ")
		var bits []string
		if m := fromHost.FindStringSubmatch(r); m != nil && localHost.MatchString(strings.Trim(m[1], "[]")) && !bracketIP.MatchString(m[1]) {
			bits = append(bits, "machine "+m[1])
		}
		for _, ip := range bracketIP.FindAllStringSubmatch(r, -1) {
			bits = append(bits, describeIP(ip[1]))
		}
		if len(bits) > 0 {
			hops = append(hops, strings.Join(bits, ", "))
		}
	}
	if ip := strings.Trim(h.Get("X-Originating-IP"), "[] "); ip != "" {
		hops = append([]string{"originating " + describeIP(ip)}, hops...)
	}
	if id := h.Get("Message-ID"); id != "" {
		if i := strings.LastIndex(id, "@"); i >= 0 {
			host := strings.Trim(id[i+1:], "> ")
			if localHost.MatchString(host) && strings.Contains(host, ".") {
				hops = append(hops, "message ID names machine "+host)
			}
		}
	}
	hops = uniq(hops)
	if len(hops) == 0 {
		return leak.Finding{}, false
	}
	return leak.SenderNetwork.Find("The headers record the machines and network addresses the message came from.", strings.Join(hops, "; ")).At("Received headers"), true
}

func describeIP(s string) string {
	ip := net.ParseIP(strings.TrimPrefix(s, "IPv6:"))
	switch {
	case ip == nil:
		return s
	case ip.IsLoopback():
		return s + " (this machine)"
	case ip.IsPrivate():
		return s + " (private network)"
	}
	return s + " (public address)"
}

// thread counts quoted earlier messages and the senders they name.
func thread(text string) (leak.Finding, bool) {
	starts := quotedStart.FindAllStringIndex(text, -1)
	if len(starts) == 0 {
		return leak.Finding{}, false
	}
	var senders []string
	for _, m := range quotedFrom.FindAllStringSubmatch(text[starts[0][0]:], 32) {
		senders = append(senders, strings.TrimSpace(m[1]))
	}
	for _, s := range starts {
		line := strings.TrimSpace(text[s[0]:s[1]])
		if strings.HasPrefix(strings.TrimLeft(line, "> "), "On ") || strings.HasPrefix(strings.TrimLeft(line, "> "), "Le ") {
			senders = append(senders, strings.TrimLeft(line, "> "))
		}
	}
	n := len(starts)
	msg := fmt.Sprintf("The message quotes %d earlier email%s of the conversation.", n, plural(n))
	return leak.ThreadHistory.Find(msg, truncate(strings.Join(uniq(senders), "; "), 240)).At("message body"), true
}

func htmlText(b []byte) string {
	s := tags.ReplaceAllString(string(b), " ")
	r := strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'")
	return r.Replace(s)
}

func decode(s string) string {
	if d, err := new(mime.WordDecoder).DecodeHeader(s); err == nil {
		return d
	}
	return s
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func uniq(vs []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range vs {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// Reveal extracts the message text, with the thread it quotes, and the
// attachments as files.
func Reveal(b []byte) ([]leak.Artifact, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(b))
	if err != nil {
		return nil, errNotEmail
	}
	var out []leak.Artifact
	for i, p := range parts(msg.Header, msg.Body, 0) {
		switch {
		case p.filename == "" && p.contentType == "text/plain":
			out = append(out, leak.Artifact{Name: "message.txt", Note: "the message text, with the thread it quotes", Data: p.body})
		case p.filename != "" || !strings.HasPrefix(p.contentType, "text/"):
			name := p.filename
			if name == "" {
				name = fmt.Sprintf("part-%d", i+1)
			}
			name = strings.NewReplacer("/", "_", "\\", "_").Replace(name)
			out = append(out, leak.Artifact{Name: "attachment-" + name, Note: "attachment", Data: p.body})
		}
	}
	return out, nil
}
