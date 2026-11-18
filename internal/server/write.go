// SPDX-License-Identifier: GPL-3.0-or-later

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/martinhammer/tickcli/tickbuddy"
)

type setTickIn struct {
	Track      string  `json:"track" jsonschema:"The track's exact name (case doesn't matter) or #id"`
	Value      any     `json:"value" jsonschema:"yes/no track: true or false. Counter: a whole number, 0 or more (0 removes the entry). Number track: a number with a point and no grouping, such as 10.4 or \"10.4\"; null clears the day. Choice track: an option's label from list_tracks, \"#N\" for the option in place N, or N as a number; null clears the day"`
	Date       string  `json:"date,omitempty" jsonschema:"The day: YYYY-MM-DD, today, yesterday or -N for N days ago. Default today"`
	Note       *string `json:"note,omitempty" jsonschema:"Replace the day's note with this (\"\" deletes it). Leave out to keep the note"`
	AppendNote *string `json:"append_note,omitempty" jsonschema:"Add this to the day's note on a new line; a line already there is skipped. Not with note"`
}

type addIn struct {
	Track      string  `json:"track" jsonschema:"The counter's exact name (case doesn't matter) or #id"`
	Delta      int     `json:"delta" jsonschema:"How much to add; negative takes away (never below 0). Not 0"`
	Date       string  `json:"date,omitempty" jsonschema:"The day: YYYY-MM-DD, today, yesterday or -N for N days ago. Default today"`
	Note       *string `json:"note,omitempty" jsonschema:"Replace the day's note with this (\"\" deletes it). Leave out to keep the note"`
	AppendNote *string `json:"append_note,omitempty" jsonschema:"Add this to the day's note on a new line; a line already there is skipped. Not with note"`
}

type setNoteIn struct {
	Track  string `json:"track" jsonschema:"The track's exact name (case doesn't matter) or #id"`
	Text   string `json:"text" jsonschema:"The note, plain text, at most 500 characters. \"\" deletes the note (unless append)"`
	Date   string `json:"date,omitempty" jsonschema:"The day: YYYY-MM-DD, today, yesterday or -N for N days ago. Default today"`
	Append bool   `json:"append,omitempty" jsonschema:"Add the text to the day's note on a new line instead of replacing it; a line already there is skipped"`
}

type writeOut struct {
	Today   string  `json:"today" jsonschema:"Today's date on the person's computer, YYYY-MM-DD"`
	Track   string  `json:"track"`
	TrackID int     `json:"trackId"`
	Date    string  `json:"date"`
	Value   any     `json:"value" jsonschema:"The day's value afterwards: true/false, a number (on a number track as stored, rounded to its decimal places), a choice's place, or null when cleared"`
	Unit    *string `json:"unit,omitempty" jsonschema:"A number track's unit"`
	Label   *string `json:"label,omitempty" jsonschema:"The label of a choice track's option"`
	Note    *string `json:"note,omitempty" jsonschema:"The whole note afterwards, when the call changed it"`
}

type noteWriteOut struct {
	Today   string `json:"today" jsonschema:"Today's date on the person's computer, YYYY-MM-DD"`
	Track   string `json:"track"`
	TrackID int    `json:"trackId"`
	Date    string `json:"date"`
	Text    string `json:"text" jsonschema:"The whole note afterwards (\"\" when there is none)"`
}

// setTickSchema is set_tick's input schema: as inferred, with value
// limited to the JSON kinds some track type takes.
func setTickSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[setTickIn](nil)
	if err != nil {
		panic(err)
	}
	schema.Properties["value"].Types = []string{"boolean", "number", "string", "null"}
	return schema
}

func (s *srv) addWriteTools(m *mcp.Server) {
	setTick := writeTool("set_tick", "Set a day's value",
		"Ticks or unticks a yes/no track, sets a counter to a number, a number track to a reading, or a choice track to one of its options, on one day, optionally with a note. null clears a number or choice day. Use add_to_counter for \"two more\" on a counter.", true)
	setTick.InputSchema = setTickSchema()
	mcp.AddTool(m, setTick, s.setTick)
	mcp.AddTool(m, writeTool("add_to_counter", "Add to a counter",
		"Adds to (or with a negative delta takes from) a counter on one day, optionally with a note. Not safe to repeat: each call counts again.", false),
		s.addToCounter)
	mcp.AddTool(m, writeTool("set_note", "Write a day's note",
		"Sets, replaces, deletes (empty text) or, with append, adds to the note of a track on one day.", true),
		s.setNote)
}

// noteEdit reads note and append_note: nil when neither is sent.
func noteEdit(note, appendNote *string) (*tickbuddy.NoteEdit, error) {
	switch {
	case note != nil && appendNote != nil:
		return nil, errors.New("send note or append_note, not both")
	case appendNote != nil:
		return tickbuddy.AppendNote(*appendNote), checkNoteLength(*appendNote)
	case note != nil:
		return tickbuddy.ReplaceNote(*note), checkNoteLength(*note)
	}
	return nil, nil
}

func checkNoteLength(text string) error {
	if len([]rune(tickbuddy.NormalizeNote(text))) > maxNoteRunes {
		return fmt.Errorf("notes can be at most %d characters", maxNoteRunes)
	}
	return nil
}

// target finds the track and day a write goes to, refusing an archived
// track before anything is sent. A write of a value (tick) is refused on a
// track of a type TickLLMe doesn't know, note and all (decision 11); a
// note alone is allowed.
func (s *srv) target(ctx context.Context, query, date string, tick bool) (tickbuddy.Track, tickbuddy.Date, error) {
	day, err := s.day(date)
	if err != nil {
		return tickbuddy.Track{}, day, err
	}
	t, err := s.resolve(ctx, query)
	if err != nil {
		return t, day, err
	}
	if tick && !t.Known() {
		return t, day, tickbuddy.NeedsNewerClient(t)
	}
	if t.IsArchived() {
		return t, day, archivedError(t)
	}
	return t, day, nil
}

// yesNo reads a yes/no value: true/false, or 1/0 since models send either.
func yesNo(t tickbuddy.Track, v any) (bool, error) {
	switch v := v.(type) {
	case bool:
		return v, nil
	case float64:
		if v == 0 || v == 1 {
			return v == 1, nil
		}
	}
	return false, fmt.Errorf("%q is a yes/no track, so value must be true or false", t.Name)
}

// countValue reads a counter value: a whole number, 0 or more.
func countValue(t tickbuddy.Track, v any) (int, error) {
	if f, ok := v.(float64); ok && f >= 0 && f == math.Trunc(f) && f <= math.MaxInt32 {
		return int(f), nil
	}
	return 0, fmt.Errorf("%q is a counter, so value must be a whole number, 0 or more", t.Name)
}

// rawArgument is the tool argument name exactly as it was sent, and whether
// it was. The SDK reads arguments into a map before the typed input, so a
// JSON number reaches that as a float64: a Number's value is read from here
// instead, and never passes through a float.
func rawArgument(req *mcp.CallToolRequest, name string) (json.RawMessage, bool) {
	if req == nil || req.Params == nil {
		return nil, false
	}
	var args map[string]json.RawMessage
	if json.Unmarshal(req.Params.Arguments, &args) != nil {
		return nil, false
	}
	v, ok := args[name]
	return v, ok
}

// isNull reports whether raw is JSON null.
func isNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }

// numberValue reads a Number track's value: a JSON number or a string in
// the API's form, with a point and no grouping or exponent.
func numberValue(t tickbuddy.Track, raw json.RawMessage) (string, error) {
	text := string(bytes.TrimSpace(raw))
	var str string
	if json.Unmarshal(raw, &str) == nil {
		text = str
	}
	if !tickbuddy.ValidNumber(text) {
		return "", fmt.Errorf("%q takes a number with a point, such as 10.4, or null to clear the day", t.Name)
	}
	return strings.TrimSpace(text), nil
}

// choiceValue reads a Choice track's value: a label (exactly, ignoring case
// and surrounding spaces, never a prefix) or "#N" as a string, or the place
// as a JSON integer. A string of digits is a label, never a place.
func choiceValue(t tickbuddy.Track, raw json.RawMessage) (int, error) {
	var label string
	if json.Unmarshal(raw, &label) == nil {
		o, err := tickbuddy.ResolveOption(t, label, false)
		if err != nil {
			return 0, optionError(err)
		}
		return o.Value, nil
	}
	place, err := strconv.Atoi(string(bytes.TrimSpace(raw)))
	if err != nil {
		return 0, fmt.Errorf("%q takes an option's label, \"#N\" or its place. Options: %s", t.Name, optionList(t.Options))
	}
	if _, ok := t.Option(place); !ok {
		return 0, fmt.Errorf("%q has no option #%d. Options: %s", t.Name, place, optionList(t.Options))
	}
	return place, nil
}

// optionError words ResolveOption's error for Claude: names quoted, since
// they are the person's data.
func optionError(err error) error {
	var oe *tickbuddy.OptionError
	if !errors.As(err, &oe) {
		return err
	}
	if oe.Kind == tickbuddy.ErrOptionAmbiguous {
		return fmt.Errorf("%q matches several options of %q: %s. Name one by its #N", oe.Query, oe.Track, optionList(oe.Candidates))
	}
	return fmt.Errorf("%q has no option %q. Options: %s", oe.Track, oe.Query, optionList(oe.Candidates))
}

func optionList(options []tickbuddy.ChoiceOption) string {
	parts := make([]string, len(options))
	for i, o := range options {
		parts[i] = fmt.Sprintf("%q (#%d)", o.Label, o.Value)
	}
	return strings.Join(parts, ", ")
}

func (s *srv) wrote(t tickbuddy.Track, day tickbuddy.Date, r tickbuddy.Result, note *tickbuddy.NoteEdit) writeOut {
	out := writeOut{Today: s.today().String(), Track: t.Name, TrackID: t.ID, Date: day.String(), Value: value(t, r.Value)}
	out.Unit, out.Label = extras(t, r.Value)
	if note != nil {
		out.Note = r.Note
	}
	return out
}

func (s *srv) setTick(ctx context.Context, req *mcp.CallToolRequest, in setTickIn) (*mcp.CallToolResult, writeOut, error) {
	out := writeOut{Today: s.today().String()}
	note, err := noteEdit(in.Note, in.AppendNote)
	if err != nil {
		return nil, out, s.fail(err)
	}
	raw, sent := rawArgument(req, "value")
	if !sent {
		if in.Value == nil {
			return nil, out, s.fail(errors.New("value is missing: true/false on a yes/no track, a number on a counter or number track, an option's label on a choice track"))
		}
		raw, _ = json.Marshal(in.Value)
	}
	t, day, err := s.target(ctx, in.Track, in.Date, true)
	if err != nil {
		return nil, out, s.fail(err)
	}
	var send func() (tickbuddy.Result, error)
	switch {
	case t.IsCounter():
		n, err := countValue(t, in.Value)
		if err != nil {
			return nil, out, s.fail(err)
		}
		send = func() (tickbuddy.Result, error) { return s.Client.SetCount(ctx, t.ID, day, n, note) }
	case (t.IsNumber() || t.IsChoice()) && isNull(raw):
		send = func() (tickbuddy.Result, error) { return s.Client.ClearValue(ctx, t.ID, day, note) }
	case t.IsNumber():
		v, err := numberValue(t, raw)
		if err != nil {
			return nil, out, s.fail(err)
		}
		send = func() (tickbuddy.Result, error) { return s.Client.SetNumber(ctx, t.ID, day, v, note) }
	case t.IsChoice():
		place, err := choiceValue(t, raw)
		if err != nil {
			return nil, out, s.fail(err)
		}
		send = func() (tickbuddy.Result, error) { return s.Client.SetChoice(ctx, t.ID, day, place, note) }
	default:
		ticked, err := yesNo(t, in.Value)
		if err != nil {
			return nil, out, s.fail(err)
		}
		send = func() (tickbuddy.Result, error) { return s.Client.SetYesNo(ctx, t.ID, day, ticked, note) }
	}
	r, err := send()
	if err != nil {
		return nil, out, s.fail(readOnly(t, err))
	}
	if r.Archived {
		return nil, out, s.fail(archivedError(t))
	}
	return nil, s.wrote(t, day, r, note), nil
}

func (s *srv) addToCounter(ctx context.Context, _ *mcp.CallToolRequest, in addIn) (*mcp.CallToolResult, writeOut, error) {
	out := writeOut{Today: s.today().String()}
	if in.Delta == 0 {
		return nil, out, s.fail(errors.New("delta must not be 0"))
	}
	note, err := noteEdit(in.Note, in.AppendNote)
	if err != nil {
		return nil, out, s.fail(err)
	}
	t, day, err := s.target(ctx, in.Track, in.Date, true)
	if err != nil {
		return nil, out, s.fail(err)
	}
	switch {
	case t.IsNumber():
		return nil, out, s.fail(fmt.Errorf("%q is a number track, and add_to_counter is only for counters. Read the day with get_day, then set the new total with set_tick", t.Name))
	case t.IsChoice():
		return nil, out, s.fail(fmt.Errorf("%q is a choice track; use set_tick", t.Name))
	case !t.IsCounter():
		return nil, out, s.fail(fmt.Errorf("%q is a yes/no track, not a counter; use set_tick", t.Name))
	}
	r, err := s.Client.Add(ctx, t.ID, day, in.Delta, note)
	if errors.Is(err, tickbuddy.ErrUnreachable) {
		return nil, out, errors.New(s.sentence(err) + " The server may or may not have counted this; check with get_day before adding again.")
	}
	if err != nil {
		return nil, out, s.fail(readOnly(t, err))
	}
	if r.Archived {
		return nil, out, s.fail(archivedError(t))
	}
	return nil, s.wrote(t, day, r, note), nil
}

func (s *srv) setNote(ctx context.Context, _ *mcp.CallToolRequest, in setNoteIn) (*mcp.CallToolResult, noteWriteOut, error) {
	out := noteWriteOut{Today: s.today().String()}
	if err := checkNoteLength(in.Text); err != nil {
		return nil, out, s.fail(err)
	}
	t, day, err := s.target(ctx, in.Track, in.Date, false)
	if err != nil {
		return nil, out, s.fail(err)
	}
	var stored string
	if in.Append {
		stored, err = s.Client.AppendNote(ctx, t.ID, day, in.Text, false)
	} else {
		stored, err = s.Client.SetNote(ctx, t.ID, day, in.Text)
	}
	if err != nil {
		return nil, out, s.fail(readOnly(t, err))
	}
	if in.Append && !tickbuddy.NoteContains(stored, in.Text) || !in.Append && stored != tickbuddy.NormalizeNote(in.Text) {
		return nil, out, s.fail(archivedError(t))
	}
	return nil, noteWriteOut{Today: out.Today, Track: t.Name, TrackID: t.ID, Date: day.String(), Text: stored}, nil
}
