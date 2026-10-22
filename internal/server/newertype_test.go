// SPDX-License-Identifier: GPL-3.0-or-later

package server

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newer adds Mood (#5), a track of the fake's newer type "future", with
// the values 7, 7.5 and "red" on the three days before today and a note on
// yesterday's, and gives Exercise and Coffee yesterday's entries.
func newer(e *env) int {
	mood := e.fake.AddTrack("Mood", "future")
	e.fake.SetValue(mood, "2026-10-16", 7)
	e.fake.SetValue(mood, "2026-10-17", 7.5)
	e.fake.SetValue(mood, "2026-10-18", "red")
	e.fake.SetNote(mood, "2026-10-18", "Rainy")
	e.fake.SetTick(1, "2026-10-18", 1)
	e.fake.SetTick(2, "2026-10-18", 3)
	return mood
}

// writes counts the requests that weren't reads.
func (e *env) writes() int { return len(e.fake.Logged()) - e.fake.Count("GET") }

// TestNewerTypeReads: a track of a type TickLLMe doesn't know is read with
// the server's type and values, and the other tracks are as before.
func TestNewerTypeReads(t *testing.T) {
	e := start(t, 1, false)
	newer(e)

	tracks := e.ok("list_tracks", nil)["tracks"]
	want := []any{
		map[string]any{"id": 1.0, "name": "Exercise", "type": "yes/no", "archived": false},
		map[string]any{"id": 2.0, "name": "Coffee", "type": "counter", "archived": false},
		map[string]any{"id": 5.0, "name": "Mood", "type": "future", "archived": false},
	}
	if !reflect.DeepEqual(tracks, want) {
		t.Errorf("list_tracks = %v", tracks)
	}

	day := func(date string) []any {
		t.Helper()
		return e.ok("get_day", map[string]any{"date": date})["tracks"].([]any)
	}
	got := day("yesterday")
	want = []any{
		map[string]any{"id": 1.0, "name": "Exercise", "type": "yes/no", "value": true},
		map[string]any{"id": 2.0, "name": "Coffee", "type": "counter", "value": 3.0},
		map[string]any{"id": 5.0, "name": "Mood", "type": "future", "value": "red", "note": "Rainy"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("get_day yesterday = %v", got)
	}
	// A day without a value is null on Mood, and no or 0 on the others.
	got = day("today")
	want = []any{
		map[string]any{"id": 1.0, "name": "Exercise", "type": "yes/no", "value": false},
		map[string]any{"id": 2.0, "name": "Coffee", "type": "counter", "value": 0.0},
		map[string]any{"id": 5.0, "name": "Mood", "type": "future", "value": nil},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("get_day today = %v", got)
	}
	if v := day("-2")[2].(map[string]any)["value"]; v != 7.5 {
		t.Errorf("Mood on -2 = %v", v)
	}

	ticks := e.ok("get_ticks", map[string]any{"from": "-7", "to": today})["ticks"]
	want = []any{
		map[string]any{"date": "2026-10-16", "track": "Mood", "trackId": 5.0, "value": 7.0},
		map[string]any{"date": "2026-10-17", "track": "Mood", "trackId": 5.0, "value": 7.5},
		map[string]any{"date": "2026-10-18", "track": "Exercise", "trackId": 1.0, "value": true},
		map[string]any{"date": "2026-10-18", "track": "Coffee", "trackId": 2.0, "value": 3.0},
		map[string]any{"date": "2026-10-18", "track": "Mood", "trackId": 5.0, "value": "red"},
	}
	if !reflect.DeepEqual(ticks, want) {
		t.Errorf("get_ticks = %v", ticks)
	}
	if n := len(e.ok("get_ticks", map[string]any{"from": "-7", "to": today, "track": "mood"})["ticks"].([]any)); n != 3 {
		t.Errorf("get_ticks for Mood: %d entries", n)
	}

	notes := e.ok("get_notes", map[string]any{"from": "-7", "to": today})["notes"]
	want = []any{map[string]any{"date": "2026-10-18", "track": "Mood", "trackId": 5.0, "text": "Rainy"}}
	if !reflect.DeepEqual(notes, want) {
		t.Errorf("get_notes = %v", notes)
	}
}

// TestNewerTypeStats: get_stats shows what the server returns for the
// track, and leaves it out when that can't be read.
func TestNewerTypeStats(t *testing.T) {
	e := start(t, 1, false)
	mood := newer(e)
	e.fake.Stats[1] = map[string]any{"total": 40}
	e.fake.Stats[mood] = map[string]any{"total": 3}
	stats := e.ok("get_stats", nil)["stats"].([]any)
	if len(stats) != 3 || stats[2].(map[string]any)["track"] != "Mood" || stats[2].(map[string]any)["total"] != 3.0 {
		t.Errorf("stats = %v", stats)
	}
	e.fake.Stats[mood] = map[string]any{"total": 7.5}
	stats = e.ok("get_stats", nil)["stats"].([]any)
	if len(stats) != 2 || stats[0].(map[string]any)["total"] != 40.0 {
		t.Errorf("stats with Mood's unreadable = %v", stats)
	}
	if out := e.ok("get_stats", map[string]any{"track": "Mood"}); len(out["stats"].([]any)) != 0 {
		t.Errorf("Mood's unreadable stats = %v", out)
	}
}

// TestNewerTypeWrites: ticking the track is refused before anything is
// sent, a note with it too; a note alone is written.
func TestNewerTypeWrites(t *testing.T) {
	e := start(t, 2, false)
	mood := newer(e)
	const msg = `"Mood" is a "future" track; TickLLMe update required.`
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"set_tick", map[string]any{"track": "Mood", "value": true}},
		{"set_tick", map[string]any{"track": "#5", "value": 7}},
		{"set_tick", map[string]any{"track": "Mood", "value": 7, "note": "Sunny"}},
		{"set_tick", map[string]any{"track": "Mood", "value": 7, "append_note": "Sunny"}},
		{"add_to_counter", map[string]any{"track": "Mood", "delta": 1}},
		{"add_to_counter", map[string]any{"track": "Mood", "delta": 1, "append_note": "Sunny"}},
	} {
		if _, got := e.call(c.tool, c.args); got != msg {
			t.Errorf("%s(%v) = %q, want %q", c.tool, c.args, got, msg)
		}
	}
	if n := e.writes(); n != 0 {
		t.Errorf("%d writes sent: %v", n, e.fake.Logged())
	}

	out := e.ok("set_note", map[string]any{"track": "Mood", "text": "Sunny", "date": "yesterday", "append": true})
	if out["text"] != "Rainy\nSunny" {
		t.Errorf("set_note append = %v", out)
	}
	e.ok("set_note", map[string]any{"track": "Mood", "text": "Calm"})
	if n, _ := e.fake.Note(mood, today); n != "Calm" {
		t.Errorf("note = %q", n)
	}
	// The other tracks are written as before.
	if out := e.ok("set_tick", map[string]any{"track": "Exercise", "value": true}); out["value"] != true {
		t.Errorf("Exercise = %v", out)
	}
}

// TestNewerTypeGuidance: the instructions and log_day cover tracks of
// another type.
func TestNewerTypeGuidance(t *testing.T) {
	e := start(t, 2, false)
	if ins := e.cs.InitializeResult().Instructions; !strings.Contains(ins, "not ticked until TickLLMe is updated") {
		t.Errorf("instructions lack newer types:\n%s", ins)
	}
	res, err := e.cs.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "log_day"})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(text, "tracks of any other type than yes/no or counter: mention them together in one line as needing a TickLLMe update, and don't ask about them") {
		t.Errorf("log_day text lacks newer types:\n%s", text)
	}
}
