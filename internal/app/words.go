package app

import (
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/sanitize"
	"github.com/rm4n0s/cave/internal/core/server"
)

// The words both user interfaces show are written here once (docs/SPEC.md §10). Every function
// is pure: none reads the clock or a file; the time is an argument.

// ResultText is the wording of the result of an attempt (docs/SPEC.md §11.3). A result this
// program does not know is shown as it is, made safe.
func ResultText(o attempts.Outcome) string {
	switch o {
	case attempts.Accepted:
		return "Accepted"
	case attempts.UnknownName:
		return "Unknown name"
	case attempts.WrongKey:
		return "Wrong key"
	case attempts.NoKey:
		return "No key offered"
	case attempts.NotProven:
		return "Key not proven"
	case attempts.Disabled:
		return "Disabled person"
	case attempts.HandshakeFailed:
		return "Not SSH or broken handshake"
	case attempts.Refused:
		return "Too many connections"
	}
	return sanitize.Display(string(o), 32)
}

// unknownWho is what an entry of the attempts list is called when no person matched.
const unknownWho = "Unknown"

// Who says who an entry of the attempts list is about: the person's name, or "Unknown".
func Who(e attempts.Entry) string {
	if e.PersonName == "" {
		return unknownWho
	}
	return e.PersonName
}

// Ago says how long before now a time was: "just now", "3 min ago", "2 h ago", "5 d ago", and
// from 30 days on the date. A zero time is "never". Times round down.
func Ago(now, t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now" // also a time that is after now: clocks are not exact
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + " min ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + " h ago"
	case d < 30*24*time.Hour:
		return strconv.Itoa(int(d/(24*time.Hour))) + " d ago"
	}
	return t.Format("2006-01-02")
}

// Until says how long after now a time is: "in 12 min", "in 3 h", "in 4 d", or "now" when it is
// not after now. Times round up, so that a time still ahead never shows as "in 0 min".
func Until(now, t time.Time) string {
	d := t.Sub(now)
	switch {
	case d <= 0:
		return "now"
	case d < time.Hour:
		return "in " + strconv.Itoa(ceilDiv(d, time.Minute)) + " min"
	case d < 24*time.Hour:
		return "in " + strconv.Itoa(ceilDiv(d, time.Hour)) + " h"
	}
	return "in " + strconv.Itoa(ceilDiv(d, 24*time.Hour)) + " d"
}

// ceilDiv divides d by unit, rounding up.
func ceilDiv(d, unit time.Duration) int {
	return int((d + unit - 1) / unit)
}

// activityNames are the names of the activities that have none besides their kind, in the order
// ActivitiesText lists them, with the plural to use when there are several.
var activityNames = []struct {
	kind             server.ActivityKind
	singular, plural string
}{
	{server.ActivityShell, "shell", "shells"},
	{server.ActivityExec, "exec", "execs"},
	{server.ActivitySFTP, "sftp", "sftp sessions"},
}

// ActivitiesText says what a connection has open: "shell, sftp, forward to 127.0.0.1:5432".
// Several of a kind that has no detail are counted ("2 shells"); each forward is listed with
// its own detail. A connection with nothing open is "idle".
func ActivitiesText(activities []server.Activity) string {
	counts := map[server.ActivityKind]int{}
	var details = map[server.ActivityKind][]string{}
	for _, a := range activities {
		counts[a.Kind]++
		details[a.Kind] = append(details[a.Kind], sanitize.Display(a.Detail, server.MaxActivityDetail))
	}
	var parts []string
	for _, n := range activityNames {
		switch c := counts[n.kind]; {
		case c == 1:
			parts = append(parts, n.singular)
		case c > 1:
			parts = append(parts, strconv.Itoa(c)+" "+n.plural)
		}
	}
	for _, d := range details[server.ActivityLocalForward] {
		parts = append(parts, join("forward to", d, "forward"))
	}
	for _, d := range details[server.ActivityRemoteForward] {
		parts = append(parts, join("listening on", d, "listening"))
	}
	switch c := counts[server.ActivityAgent]; {
	case c == 1:
		parts = append(parts, "agent")
	case c > 1:
		parts = append(parts, strconv.Itoa(c)+" agents")
	}
	// A kind this program does not know is shown by its name, counted.
	known := map[server.ActivityKind]bool{
		server.ActivityShell: true, server.ActivityExec: true, server.ActivitySFTP: true,
		server.ActivityLocalForward: true, server.ActivityRemoteForward: true, server.ActivityAgent: true,
	}
	for _, a := range activities {
		if !known[a.Kind] {
			known[a.Kind] = true // once for each unknown kind
			name := sanitize.Display(string(a.Kind), 32)
			if c := counts[a.Kind]; c > 1 {
				name = strconv.Itoa(c) + " " + name
			}
			parts = append(parts, name)
		}
	}
	if len(parts) == 0 {
		return "idle"
	}
	return strings.Join(parts, ", ")
}

// join returns "<lead> <detail>", or alone when there is no detail.
func join(lead, detail, alone string) string {
	if detail == "" {
		return alone
	}
	return lead + " " + detail
}

// QuitQuestion is the question asked before Cave quits while people are connected
// (docs/SPEC.md §11.6). It is "" when nobody is connected.
func QuitQuestion(conns []server.Connection) string {
	people := map[string]bool{}
	for _, c := range conns {
		people[c.PersonID] = true
	}
	switch len(people) {
	case 0:
		return ""
	case 1:
		return "1 person is connected. Quit and disconnect them?"
	}
	return strconv.Itoa(len(people)) + " people are connected. Quit and disconnect them?"
}
