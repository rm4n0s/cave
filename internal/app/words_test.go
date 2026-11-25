package app

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/history"
	"github.com/rm4n0s/cave/internal/core/server"
)

func TestResultText(t *testing.T) {
	tests := []struct {
		outcome attempts.Outcome
		want    string
	}{
		{attempts.Accepted, "Accepted"},
		{attempts.UnknownName, "Unknown name"},
		{attempts.WrongKey, "Wrong key"},
		{attempts.NoKey, "No key offered"},
		{attempts.NotProven, "Key not proven"},
		{attempts.Disabled, "Disabled person"},
		{attempts.HandshakeFailed, "Not SSH or broken handshake"},
		{attempts.Refused, "Too many connections"},
		{"somethingNew", "somethingNew"},
		{"bad\x1b[31m", "bad\uFFFD[31m"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := ResultText(tt.outcome); got != tt.want {
			t.Errorf("ResultText(%q) = %q, want %q", tt.outcome, got, tt.want)
		}
	}
}

func TestWho(t *testing.T) {
	if got := Who(attempts.Entry{PersonName: "alice", LoginName: "root"}); got != "alice" {
		t.Errorf("Who = %q", got)
	}
	if got := Who(attempts.Entry{LoginName: "root"}); got != "Unknown" {
		t.Errorf("Who = %q", got)
	}
}

var wordsNow = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

func TestAgo(t *testing.T) {
	ago := func(d time.Duration) time.Time { return wordsNow.Add(-d) }
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"a zero time", time.Time{}, "never"},
		{"now", wordsNow, "just now"},
		{"59 seconds", ago(59 * time.Second), "just now"},
		{"a time ahead", wordsNow.Add(time.Hour), "just now"},
		{"60 seconds", ago(60 * time.Second), "1 min ago"},
		{"3 minutes and a half", ago(3*time.Minute + 30*time.Second), "3 min ago"},
		{"59 minutes 59 seconds", ago(time.Hour - time.Second), "59 min ago"},
		{"1 hour", ago(time.Hour), "1 h ago"},
		{"23 hours 59 minutes", ago(24*time.Hour - time.Minute), "23 h ago"},
		{"24 hours", ago(24 * time.Hour), "1 d ago"},
		{"5 days", ago(5*24*time.Hour + 3*time.Hour), "5 d ago"},
		{"29 days", ago(29*24*time.Hour + 23*time.Hour), "29 d ago"},
		{"30 days", ago(30 * 24 * time.Hour), "2026-10-25"},
		{"a long time", time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC), "2025-01-02"},
	}
	for _, tt := range tests {
		if got := Ago(wordsNow, tt.t); got != tt.want {
			t.Errorf("%s: Ago = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestUntil(t *testing.T) {
	in := func(d time.Duration) time.Time { return wordsNow.Add(d) }
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"a zero time", time.Time{}, "now"},
		{"this moment", wordsNow, "now"},
		{"a time that passed", in(-time.Hour), "now"},
		{"a second", in(time.Second), "in 1 min"},
		{"59 seconds", in(59 * time.Second), "in 1 min"},
		{"61 seconds", in(61 * time.Second), "in 2 min"},
		{"12 minutes", in(12 * time.Minute), "in 12 min"},
		{"59 minutes 59 seconds", in(time.Hour - time.Second), "in 60 min"},
		{"1 hour", in(time.Hour), "in 1 h"},
		{"2 hours 10 minutes", in(2*time.Hour + 10*time.Minute), "in 3 h"},
		{"23 hours 59 minutes", in(24*time.Hour - time.Minute), "in 24 h"},
		{"24 hours", in(24 * time.Hour), "in 1 d"},
		{"3 days 1 hour", in(3*24*time.Hour + time.Hour), "in 4 d"},
		{"30 days", in(30 * 24 * time.Hour), "in 30 d"},
	}
	for _, tt := range tests {
		if got := Until(wordsNow, tt.t); got != tt.want {
			t.Errorf("%s: Until = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestActivitiesText(t *testing.T) {
	act := func(kind server.ActivityKind, detail string) server.Activity {
		return server.Activity{Kind: kind, Detail: detail}
	}
	tests := []struct {
		name string
		in   []server.Activity
		want string
	}{
		{"nothing", nil, "idle"},
		{"a shell", []server.Activity{act(server.ActivityShell, "")}, "shell"},
		{"an exec", []server.Activity{act(server.ActivityExec, "ls -l")}, "exec"},
		{"sftp", []server.Activity{act(server.ActivitySFTP, "")}, "sftp"},
		{"a local forward", []server.Activity{act(server.ActivityLocalForward, "127.0.0.1:5432")}, "forward to 127.0.0.1:5432"},
		{"a remote forward", []server.Activity{act(server.ActivityRemoteForward, "127.0.0.1:8080")}, "listening on 127.0.0.1:8080"},
		{"a forward without a detail", []server.Activity{act(server.ActivityLocalForward, "")}, "forward"},
		{"an agent", []server.Activity{act(server.ActivityAgent, "")}, "agent"},
		{"two shells", []server.Activity{act(server.ActivityShell, ""), act(server.ActivityShell, "")}, "2 shells"},
		{"three execs", []server.Activity{act(server.ActivityExec, "a"), act(server.ActivityExec, "b"), act(server.ActivityExec, "c")}, "3 execs"},
		{"two sftp sessions", []server.Activity{act(server.ActivitySFTP, ""), act(server.ActivitySFTP, "")}, "2 sftp sessions"},
		{"two shells and a forward, in the order of the kinds", []server.Activity{
			act(server.ActivityLocalForward, "127.0.0.1:1"), act(server.ActivityShell, ""), act(server.ActivityShell, ""),
		}, "2 shells, forward to 127.0.0.1:1"},
		{"every kind", []server.Activity{
			act(server.ActivityAgent, ""), act(server.ActivityRemoteForward, "127.0.0.1:2"), act(server.ActivityLocalForward, "10.0.0.1:3"),
			act(server.ActivitySFTP, ""), act(server.ActivityExec, "x"), act(server.ActivityShell, ""),
		}, "shell, exec, sftp, forward to 10.0.0.1:3, listening on 127.0.0.1:2, agent"},
		{"two forwards", []server.Activity{act(server.ActivityLocalForward, "a:1"), act(server.ActivityLocalForward, "b:2")}, "forward to a:1, forward to b:2"},
		{"a kind that is not known", []server.Activity{act("novel", ""), act("novel", "")}, "2 novel"},
		{"a detail that is not safe", []server.Activity{act(server.ActivityLocalForward, "a\x1b[31mb")}, "forward to a\uFFFD[31mb"},
	}
	for _, tt := range tests {
		if got := ActivitiesText(tt.in); got != tt.want {
			t.Errorf("%s: ActivitiesText = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestQuitQuestion(t *testing.T) {
	conn := func(person string) server.Connection { return server.Connection{PersonID: person} }
	tests := []struct {
		name string
		in   []server.Connection
		want string
	}{
		{"nobody", nil, ""},
		{"one person", []server.Connection{conn("p1")}, "1 person is connected. Quit and disconnect them?"},
		{"one person twice", []server.Connection{conn("p1"), conn("p1")}, "1 person is connected. Quit and disconnect them?"},
		{"three people", []server.Connection{conn("p1"), conn("p2"), conn("p3"), conn("p1")}, "3 people are connected. Quit and disconnect them?"},
	}
	for _, tt := range tests {
		if got := QuitQuestion(tt.in); got != tt.want {
			t.Errorf("%s: QuitQuestion = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestTheWordsHaveNoUnsafeText(t *testing.T) {
	// Every sentence the functions give for a result or a count is plain text.
	for _, o := range []attempts.Outcome{attempts.Accepted, attempts.UnknownName, attempts.WrongKey, attempts.NoKey, attempts.NotProven, attempts.Disabled, attempts.HandshakeFailed, attempts.Refused} {
		if text := ResultText(o); text == "" || strings.ContainsAny(text, "\x1b\n\t") {
			t.Errorf("ResultText(%q) = %q", o, text)
		}
	}
}

func TestHistoryWords(t *testing.T) {
	reasons := []struct {
		reason history.Reason
		want   string
	}{
		{"", "Connected now"},
		{history.ReasonClient, "Client left"},
		{history.ReasonDisconnected, "Disconnected by you"},
		{history.ReasonPersonChanged, "Person or key changed"},
		{history.ReasonKeepAlive, "Stopped answering"},
		{history.ReasonServerStopped, "Server stopped"},
		{history.ReasonUnknown, "Unknown: Cave did not close"},
		{"later\x1b[31m", "later�[31m"},
	}
	for _, tt := range reasons {
		if got := ReasonText(tt.reason); got != tt.want {
			t.Errorf("ReasonText(%q) = %q, want %q", tt.reason, got, tt.want)
		}
	}
	results := []struct {
		result history.Result
		want   string
	}{
		{history.ResultDone, "Done"}, {history.ResultFailed, "Failed"}, {history.ResultInterrupted, "Interrupted"}, {"odd", "odd"},
	}
	for _, tt := range results {
		if got := ResultWord(tt.result); got != tt.want {
			t.Errorf("ResultWord(%q) = %q, want %q", tt.result, got, tt.want)
		}
	}
	directions := []struct {
		direction history.Direction
		want      string
	}{{history.Upload, "Upload"}, {history.Download, "Download"}, {history.Both, "Both"}}
	for _, tt := range directions {
		if got := DirectionText(tt.direction); got != tt.want {
			t.Errorf("DirectionText(%v) = %q, want %q", tt.direction, got, tt.want)
		}
	}
}

func TestDurationText(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{-time.Second, "0 s"},
		{0, "0 s"},
		{45*time.Second + 900*time.Millisecond, "45 s"},
		{time.Minute, "1 min"},
		{12*time.Minute + 59*time.Second, "12 min"},
		{2 * time.Hour, "2 h"},
		{2*time.Hour + 5*time.Minute, "2 h 5 min"},
		{24 * time.Hour, "1 d"},
		{3*24*time.Hour + 4*time.Hour + 30*time.Minute, "3 d 4 h"},
		{400 * 24 * time.Hour, "400 d"},
	}
	for _, tt := range tests {
		if got := DurationText(tt.d); got != tt.want {
			t.Errorf("DurationText(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestSizeText(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{-5, "0 B"},
		{0, "0 B"},
		{512, "512 B"},
		{999, "999 B"},
		{1000, "1.0 KB"},
		{1_400, "1.4 KB"},
		{999_949, "999.9 KB"},
		{999_999, "1.0 MB"},
		{3_200_000, "3.2 MB"},
		{1_100_000_000, "1.1 GB"},
		{5_000_000_000_000, "5.0 TB"},
	}
	for _, tt := range tests {
		if got := SizeText(tt.n); got != tt.want {
			t.Errorf("SizeText(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
