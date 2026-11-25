package gui

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	m "github.com/rm4n0s/glutter/material"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/gui/testutil"
)

// threeConnections is three connections with every kind of activity.
func threeConnections() []server.Connection {
	return []server.Connection{
		{ID: "c_0000000000000001", PersonID: apptest.AliceID, PersonName: "alice", Address: "198.51.100.7",
			Since: apptest.Now.Add(-12 * time.Minute), Activities: []server.Activity{
				{Kind: server.ActivityShell}, {Kind: server.ActivityLocalForward, Detail: "127.0.0.1:5432"},
			}},
		{ID: "c_0000000000000002", PersonID: apptest.BobID, PersonName: "bob", Address: "2001:db8::77",
			Since: apptest.Now.Add(-3 * time.Hour), Activities: []server.Activity{{Kind: server.ActivitySFTP}, {Kind: server.ActivityExec}}},
		{ID: "c_0000000000000003", PersonID: apptest.CarolID, PersonName: "carol", Address: "192.0.2.50",
			Since: apptest.Now.Add(-40 * time.Second)},
	}
}

func TestGoldenConnections(t *testing.T) {
	t.Run("sample light wide", func(t *testing.T) {
		pump(t, light(apptest.Sample(t), t), testutil.Wide, "connections").MatchesGolden("connections-light-wide")
	})
	t.Run("sample dark narrow", func(t *testing.T) {
		f := apptest.Sample(t)
		f.SettingsSnapshot = apptest.SampleSettings(t, map[string]any{"ui.theme": "dark"})
		pump(t, f, testutil.Narrow, "connections").MatchesGolden("connections-dark-narrow")
	})
	t.Run("nobody", func(t *testing.T) {
		pump(t, light(apptest.Empty(t), t), testutil.Wide, "connections").MatchesGolden("connections-empty-light-wide")
	})
	t.Run("three connections", func(t *testing.T) {
		f := light(apptest.Sample(t), t)
		f.ConnectionList = threeConnections()
		pump(t, f, testutil.Wide, "connections").MatchesGolden("connections-three-light-wide")
	})
}

func TestDisconnect(t *testing.T) {
	t.Run("one, cancelled and then confirmed", func(t *testing.T) {
		f := apptest.Sample(t)
		tester := pump(t, f, testutil.Wide, "connections")
		tapKey(tester, disconnectKey("c_0000000000000001"))
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Disconnect alice (198.51.100.7)?")); err != nil {
			t.Fatal(err)
		}
		tapKey(tester, confirmCancelKey)
		eventually(t, tester, "the dialog closes", func() bool { return !tester.Exists(gluttertest.ByKey(confirmYesKey)) })
		if got := f.Calls(); len(got) != 0 {
			t.Fatalf("Cancel made the calls %q", got)
		}
		tapKey(tester, disconnectKey("c_0000000000000001"))
		tapKey(tester, confirmYesKey)
		eventually(t, tester, "the call", func() bool { return len(f.Calls()) == 1 })
		if got := f.Calls()[0]; got != "Disconnect(c_0000000000000001)" {
			t.Errorf("calls = %q", f.Calls())
		}
	})
	t.Run("all", func(t *testing.T) {
		f := apptest.Sample(t)
		f.ConnectionList = threeConnections()
		tester := pump(t, f, testutil.Wide, "connections")
		tapKey(tester, disconnectAllKey)
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Disconnect all 3 connections?")); err != nil {
			t.Fatal(err)
		}
		tapKey(tester, confirmYesKey)
		eventually(t, tester, "the call", func() bool { return len(f.Calls()) == 1 })
		if got := f.Calls()[0]; got != "DisconnectAll()" {
			t.Errorf("calls = %q", f.Calls())
		}
	})
	t.Run("all with one", func(t *testing.T) {
		tester := pump(t, apptest.Sample(t), testutil.Wide, "connections")
		tapKey(tester, disconnectAllKey)
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Disconnect the connection?")); err != nil {
			t.Error(err)
		}
	})
	t.Run("nobody: nothing to disconnect", func(t *testing.T) {
		tester := pump(t, apptest.Empty(t), testutil.Wide, "connections")
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText("Nobody is connected.")); err != nil {
			t.Error(err)
		}
		// The button does nothing: a tap opens no question.
		tapKey(tester, disconnectAllKey)
		if err := gluttertest.FindsNothing(tester, gluttertest.ByKey(confirmYesKey)); err != nil {
			t.Errorf("a question was asked with nobody connected: %v", err)
		}
	})
}

func TestTheTimesOfConnectionsMoveOn(t *testing.T) {
	f := apptest.Sample(t)
	tester := pump(t, f, testutil.Wide, "connections")
	since := gluttertest.ByKey(foundation.NewKey("since-c_0000000000000001"))
	if got := gluttertest.WidgetAs[*m.Text](tester, since).PlainText(); got != "198.51.100.7 · Since 12 min ago" {
		t.Fatalf("the line is %q", got)
	}
	for i := 0; i < 10; i++ { // five minutes of ticks
		f.Clock.Advance(tickEvery)
		tester.FlushPosted()
	}
	tester.PumpAndSettle()
	if got := gluttertest.WidgetAs[*m.Text](tester, since).PlainText(); got != "198.51.100.7 · Since 17 min ago" {
		t.Errorf("after five minutes the line is %q, want 17 min ago", got)
	}
}

func TestQuitAsksOnlyWithConnections(t *testing.T) {
	question := func() *apptest.Fake { return apptest.Sample(t) }
	t.Run("nobody connected: the exit is allowed", func(t *testing.T) {
		tester := pump(t, apptest.Empty(t), testutil.Wide, "overview")
		if !tester.RequestWindowClose(ui.ImplicitViewID) {
			t.Error("the exit was cancelled with nobody connected")
		}
		if err := gluttertest.FindsNothing(tester, gluttertest.ByKey(confirmYesKey)); err != nil {
			t.Errorf("a question was asked: %v", err)
		}
	})
	t.Run("one connection: asked, Cancel keeps the app, Quit exits", func(t *testing.T) {
		tester := pump(t, question(), testutil.Wide, "overview")
		if tester.RequestWindowClose(ui.ImplicitViewID) {
			t.Fatal("the exit was allowed with a connection")
		}
		tester.PumpAndSettle()
		if err := gluttertest.FindsOneWidget(tester, gluttertest.ByText(app.QuitQuestion(threeConnectionsOf(apptest.Sample(t))))); err != nil {
			t.Fatal(err)
		}
		tapKey(tester, confirmCancelKey)
		eventually(t, tester, "the question goes", func() bool { return !tester.Exists(gluttertest.ByKey(confirmYesKey)) })
		if got := tester.ExitRequests(); got != 0 {
			t.Fatalf("Cancel asked the platform to exit (%d)", got)
		}
		// Asked again, and this time yes.
		if tester.RequestWindowClose(ui.ImplicitViewID) {
			t.Fatal("the second request was allowed")
		}
		eventually(t, tester, "the question again", func() bool { return tester.Exists(gluttertest.ByKey(confirmYesKey)) })
		tapKey(tester, confirmYesKey)
		eventually(t, tester, "the exit", func() bool { return tester.ExitRequests() == 1 })
	})
	t.Run("two clicks make one question", func(t *testing.T) {
		tester := pump(t, question(), testutil.Wide, "overview")
		tester.RequestWindowClose(ui.ImplicitViewID)
		tester.RequestWindowClose(ui.ImplicitViewID)
		tester.PumpAndSettle()
		if got := tester.Count(gluttertest.ByKey(confirmYesKey)); got != 1 {
			t.Errorf("%d questions on the screen, want 1", got)
		}
	})
	t.Run("the end of the session is never asked about", func(t *testing.T) {
		tester := pump(t, question(), testutil.Wide, "overview")
		if !tester.RequestWindowCloseFor(ui.ImplicitViewID, ui.AppExitReasonSessionEnd) {
			t.Error("the end of the session was refused")
		}
		if err := gluttertest.FindsNothing(tester, gluttertest.ByKey(confirmYesKey)); err != nil {
			t.Errorf("a question was asked: %v", err)
		}
	})
}

// threeConnectionsOf is the connections of the fake.
func threeConnectionsOf(f *apptest.Fake) []server.Connection { return f.ConnectionList }

func TestConnectionsWithTheRealApplication(t *testing.T) {
	r := apptest.Open(t, nil)
	aliceKey := r.Person("alice")
	bobKey := r.Person("bob")
	first := r.Connect("alice", aliceKey)
	second := r.Connect("bob", bobKey)
	tester := pumpOptions(t, r.App, testutil.Wide, Options{Page: "connections", Clock: r.Clock}, syncRunner)
	sync := func(what string, cond func() bool) {
		t.Helper()
		eventually(t, tester, what, func() bool {
			r.Clock.Advance(app.CoalesceTime)
			return cond()
		})
	}
	sync("the page shows both", func() bool {
		return tester.Exists(gluttertest.ByText("alice")) && tester.Exists(gluttertest.ByText("bob"))
	})
	var firstID string
	for _, c := range r.App.Connections() {
		if c.PersonName == "alice" {
			firstID = c.ID
		}
	}
	tapKey(tester, disconnectKey(firstID))
	tapKey(tester, confirmYesKey)
	closedClient(t, first)
	sync("the page shows only bob", func() bool { return !tester.Exists(gluttertest.ByText("alice")) })
	// bob is still connected.
	if len(r.App.Connections()) != 1 || r.App.Connections()[0].PersonName != "bob" {
		t.Errorf("connections = %+v, want only bob", r.App.Connections())
	}
	if _, _, err := second.SendRequest("keepalive@openssh.com", true, nil); err != nil {
		t.Errorf("bob's connection is closed: %v", err)
	}
}
