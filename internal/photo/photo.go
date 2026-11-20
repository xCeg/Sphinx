// Package photo turns EXIF and XMP metadata into findings. It is shared by
// every image format, since PNG and JPEG carry the same metadata blocks.
package photo

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/xceg/sphinx/internal/exif"
	"github.com/xceg/sphinx/internal/leak"
	"github.com/xceg/sphinx/internal/xmp"
)

func Findings(m *exif.Meta, x xmp.Packet) []leak.Finding {
	if m == nil {
		m = &exif.Meta{}
	}
	var out []leak.Finding
	add := func(f leak.Finding, loc string) { out = append(out, f.At(loc)) }

	if g := m.GPS; g != nil {
		add(leak.GPSLocation.Find("The photo records the exact place it was taken: "+g.String()+".", g.MapURL()), "EXIF GPS")
	} else if g := xmpGPS(x); g != nil {
		add(leak.GPSLocation.Find("The photo records the exact place it was taken: "+g.String()+".", g.MapURL()), "XMP")
	}
	if place := join(x.First("Iptc4xmpCore:Location"), x.First("photoshop:City"), x.First("photoshop:State"), x.First("photoshop:Country")); place != "" {
		add(leak.GPSLocation.Find("The photo is labelled with a place name.", place).Lower(leak.Medium), "XMP")
	}

	serials := uniq(
		labelled("camera body", m.BodySerial), labelled("lens", m.LensSerial),
		labelled("camera body", x.First("aux:SerialNumber")), labelled("lens", x.First("aux:LensSerialNumber")),
	)
	if len(serials) > 0 {
		add(leak.DeviceSerial.Find("The photo carries the serial number of the device that took it, which ties it to every other photo from that device.", strings.Join(serials, "; ")), "EXIF")
	}

	names := uniq(m.Artist, m.Owner, m.XPAuthor)
	names = uniq(append(names, x["dc:creator"]...)...)
	if len(names) > 0 {
		add(leak.PersonalNames.Find("The photo names the person who took or owns it.", strings.Join(names, "; ")), "EXIF/XMP")
	}
	if c := m.Copyright; c != "" && !containsAny(names, c) {
		add(leak.PersonalNames.Find("The photo carries a copyright notice.", c).Lower(leak.Low), "EXIF")
	}

	if when := firstOf(m.DateTimeOriginal, m.DateTimeDigitized, m.DateTime, x.First("xmp:CreateDate"), x.First("exif:DateTimeOriginal")); when != "" {
		ev := when
		if m.OffsetTime != "" {
			ev += " (UTC" + m.OffsetTime + ")"
		}
		add(leak.Timestamps.Find("The photo records the date and time it was taken.", ev), "EXIF")
	}

	device := join(m.Make, strings.TrimPrefix(m.Model, m.Make+" "))
	trail := uniq(labelled("device", device), labelled("lens", m.LensModel), labelled("software", m.Software),
		labelled("computer", m.HostComputer), labelled("tool", x.First("xmp:CreatorTool")))
	if agents := uniq(x["stEvt:softwareAgent"]...); len(agents) > 0 {
		trail = append(trail, fmt.Sprintf("edit history: %d steps (%s)", len(x["stEvt:action"]), strings.Join(agents, ", ")))
	}
	if len(trail) > 0 {
		add(leak.SoftwareTrail.Find("The photo names the device and software that produced it.", strings.Join(trail, "; ")), "EXIF/XMP")
	}

	if paths := localPaths(x["stRef:filePath"], x["stEvt:changed"], x["photoshop:DocumentAncestors"]); len(paths) > 0 {
		add(leak.LocalPaths.Find("The editing history keeps file paths from the computer it was edited on.", strings.Join(paths, "; ")), "XMP")
	}

	notes := uniq(m.Description, m.UserComment, m.XPComment, m.XPSubject, x.First("dc:description"), x.First("dc:title"))
	if len(notes) > 0 {
		add(leak.EmbeddedText.Find("The photo carries written notes or a caption.", quote(notes)), "EXIF/XMP")
	}
	return out
}

func labelled(label, v string) string {
	if v == "" {
		return ""
	}
	return label + " " + v
}

func join(parts ...string) string {
	return strings.Join(uniq(parts...), ", ")
}

func firstOf(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func uniq(vs ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range vs {
		v = strings.TrimSpace(v)
		if v == "" || seen[strings.ToLower(v)] {
			continue
		}
		seen[strings.ToLower(v)] = true
		out = append(out, v)
	}
	return out
}

func containsAny(names []string, s string) bool {
	for _, n := range names {
		if strings.Contains(strings.ToLower(s), strings.ToLower(n)) {
			return true
		}
	}
	return false
}

func quote(vs []string) string {
	q := make([]string, len(vs))
	for i, v := range vs {
		q[i] = strconv.Quote(Truncate(v, 200))
	}
	return strings.Join(q, "; ")
}

// Truncate shortens s to at most n runes, marking the cut.
func Truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

var pathLike = regexp.MustCompile(`(?i)^(?:file://|[a-z]:[\\/]|/(?:Users|home|Volumes|mnt)/|\\\\)`)

// localPaths keeps values that look like paths on someone's machine.
func localPaths(groups ...[]string) []string {
	var all []string
	for _, g := range groups {
		for _, v := range g {
			if pathLike.MatchString(v) {
				all = append(all, v)
			}
		}
	}
	return uniq(all...)
}

var xmpCoord = regexp.MustCompile(`^(\d{1,3}),(\d{1,2}(?:\.\d+)?)(?:,(\d{1,2}(?:\.\d+)?))?([NSEW])$`)

func parseCoord(s string) (float64, bool) {
	m := xmpCoord.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	deg, _ := strconv.ParseFloat(m[1], 64)
	mins, _ := strconv.ParseFloat(m[2], 64)
	secs, _ := strconv.ParseFloat(m[3], 64)
	v := deg + mins/60 + secs/3600
	if m[4] == "S" || m[4] == "W" {
		v = -v
	}
	return v, true
}

func xmpGPS(x xmp.Packet) *exif.GPS {
	lat, ok1 := parseCoord(x.First("exif:GPSLatitude"))
	lon, ok2 := parseCoord(x.First("exif:GPSLongitude"))
	if !ok1 || !ok2 || (lat == 0 && lon == 0) {
		return nil
	}
	return &exif.GPS{Lat: lat, Lon: lon}
}
