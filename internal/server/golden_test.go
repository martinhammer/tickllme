// SPDX-License-Identifier: GPL-3.0-or-later

package server

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/martinhammer/tickcli/tickbuddy/tickbuddytest"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/server -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

// TestEmptyDayJSON: a day without a value on a number, choice or newer
// type's track is "value": null in results (never left out, never the
// string "null"), on get_day and after set_tick clears a day.
func TestEmptyDayJSON(t *testing.T) {
	fake := tickbuddytest.New()
	t.Cleanup(fake.Close)
	journal(fake)
	run := fake.AddNumber("Run", "km", 1, false)
	mood := fake.AddChoice("Mum's mood", true, "Excited", "Happy", "Normal", "Sad")
	fake.AddTrack("Mood", "future")
	fake.SetTick(run, today, 5000)
	fake.SetTick(mood, today, 2)
	e := startWith(t, fake, 2, false)

	var out strings.Builder
	for _, c := range []struct {
		name string
		args map[string]any
	}{
		{"get_day", map[string]any{"date": "2026-10-10"}},
		{"set_tick", map[string]any{"track": "Run", "value": nil}},
		{"set_tick", map[string]any{"track": "Mum's mood", "value": nil}},
	} {
		text := e.text(c.name, c.args)
		if strings.Contains(text, `"value":"null"`) || !strings.Contains(text, `"value":null`) {
			t.Errorf("%s(%v): %s", c.name, c.args, text)
		}
		args, _ := json.Marshal(c.args)
		out.WriteString(c.name + " " + string(args) + "\n" + text + "\n")
	}
	golden(t, "empty-day-json", out.String())
}
