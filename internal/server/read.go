// SPDX-License-Identifier: GPL-3.0-or-later

package server

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/martinhammer/tickcli/tickbuddy"
)

// trackOut is a track in a result. A Number's and a Choice's settings appear
// only on their own type.
type trackOut struct {
	ID            int         `json:"id"`
	Name          string      `json:"name"`
	Type          string      `json:"type" jsonschema:"yes/no, counter, number, choice, or another type from a newer Tickbuddy (read-only here)"`
	Archived      bool        `json:"archived"`
	Private       bool        `json:"private,omitempty"`
	Unit          string      `json:"unit,omitempty" jsonschema:"A number track's unit, such as km; left out when it has none"`
	DecimalPlaces *int        `json:"decimalPlaces,omitempty" jsonschema:"How many decimals a number track keeps, 0 to 3; Tickbuddy rounds values to them"`
	AllowNegative *bool       `json:"allowNegative,omitempty" jsonschema:"Whether a number track takes values below 0"`
	Options       []optionOut `json:"options,omitempty" jsonschema:"A choice track's options in order: pick one by its label"`
	OrderedScale  *bool       `json:"orderedScale,omitempty" jsonschema:"Whether a choice track's options run from one end of a scale to the other, rather than being a plain list"`
}

type optionOut struct {
	Value int    `json:"value" jsonschema:"The option's place, 1 to n: what a day stores"`
	Label string `json:"label"`
}

// typeName is t's type as Claude sees it: yes/no, counter, number, choice,
// or the server's own name for a type TickLLMe doesn't know (decision 11).
func typeName(t tickbuddy.Track) string {
	switch {
	case !t.Known():
		return t.Type
	case t.Type == tickbuddy.TypeYesNo:
		return "yes/no"
	}
	return t.Type
}

func newTrackOut(t tickbuddy.Track) trackOut {
	out := trackOut{ID: t.ID, Name: t.Name, Type: typeName(t), Archived: t.IsArchived(), Private: t.Private}
	switch {
	case t.IsNumber():
		out.Unit, out.DecimalPlaces, out.AllowNegative = t.Unit, &t.DecimalPlaces, &t.AllowNegative
	case t.IsChoice():
		out.OrderedScale = &t.OrderedScale
		for _, o := range t.Options {
			out.Options = append(out.Options, optionOut{Value: o.Value, Label: o.Label})
		}
	}
	return out
}

// value is v (a stored tick) as Claude sees it: true/false on a yes/no
// track, a number on a counter, the exact stored number on a Number, an
// option's place on a Choice, and on a track of a type TickLLMe doesn't
// know, the server's value unchanged.
func value(t tickbuddy.Track, v tickbuddy.Value) any { return tickbuddy.ValueFor(t, v) }

// extras are what a result adds next to a day's value on t: a Number's
// unit (when it has one, also without a value) and a Choice's label (with
// a value whose option exists).
func extras(t tickbuddy.Track, v tickbuddy.Value) (unit, label *string) {
	if t.IsNumber() && t.Unit != "" {
		unit = &t.Unit
	}
	if v = tickbuddy.ValueFor(t, v); t.IsChoice() && !v.IsNull() && v.Raw() == nil {
		if o, ok := t.Option(v.Int()); ok {
			label = &o.Label
		}
	}
	return unit, label
}

type listTracksIn struct {
	IncludeArchived bool `json:"include_archived,omitempty" jsonschema:"Include archived tracks too"`
}

type listTracksOut struct {
	Today  string     `json:"today" jsonschema:"Today's date on the person's computer, YYYY-MM-DD"`
	Tracks []trackOut `json:"tracks"`
}

type getDayIn struct {
	Date            string `json:"date,omitempty" jsonschema:"The day: YYYY-MM-DD, today, yesterday or -N for N days ago. Default today"`
	IncludeArchived bool   `json:"include_archived,omitempty" jsonschema:"Include archived tracks too"`
}

type dayTrack struct {
	ID       int     `json:"id"`
	Name     string  `json:"name"`
	Type     string  `json:"type" jsonschema:"yes/no, counter, number, choice, or another type from a newer Tickbuddy (read-only here)"`
	Value    any     `json:"value" jsonschema:"true/false on a yes/no track, a number on a counter; false or 0 means no entry. On a number track the reading (0 is a reading), on a choice track the option's place, null for no entry. On another type, the server's value as it is, null for no entry"`
	Unit     *string `json:"unit,omitempty" jsonschema:"A number track's unit"`
	Label    *string `json:"label,omitempty" jsonschema:"The label of a choice track's option"`
	Note     *string `json:"note,omitempty"`
	Archived bool    `json:"archived,omitempty"`
	Private  bool    `json:"private,omitempty"`
}

type getDayOut struct {
	Today  string     `json:"today" jsonschema:"Today's date on the person's computer, YYYY-MM-DD"`
	Date   string     `json:"date"`
	Tracks []dayTrack `json:"tracks"`
}

type rangeIn struct {
	From  string `json:"from" jsonschema:"The first day: YYYY-MM-DD, today, yesterday or -N for N days ago"`
	To    string `json:"to" jsonschema:"The last day, included, in the same forms"`
	Track string `json:"track,omitempty" jsonschema:"Only this track: its exact name or #id. One track allows up to 366 days, all tracks 31"`
}

type tickOut struct {
	Date    string  `json:"date"`
	Track   string  `json:"track"`
	TrackID int     `json:"trackId"`
	Value   any     `json:"value" jsonschema:"true on a yes/no track, a number on a counter or number track, the option's place on a choice track, the server's value as it is on another type"`
	Unit    *string `json:"unit,omitempty" jsonschema:"A number track's unit"`
	Label   *string `json:"label,omitempty" jsonschema:"The label of a choice track's option"`
}

type getTicksOut struct {
	Today string    `json:"today" jsonschema:"Today's date on the person's computer, YYYY-MM-DD"`
	From  string    `json:"from"`
	To    string    `json:"to"`
	Ticks []tickOut `json:"ticks" jsonschema:"Only days with an entry; a day not listed has none"`
}

type noteOut struct {
	Date    string `json:"date"`
	Track   string `json:"track"`
	TrackID int    `json:"trackId"`
	Text    string `json:"text"`
}

type getNotesOut struct {
	Today string    `json:"today" jsonschema:"Today's date on the person's computer, YYYY-MM-DD"`
	From  string    `json:"from"`
	To    string    `json:"to"`
	Notes []noteOut `json:"notes"`
}

type getStatsIn struct {
	Track       string `json:"track,omitempty" jsonschema:"Only this track: its exact name or #id. Default every track"`
	IncludeRuns bool   `json:"include_runs,omitempty" jsonschema:"Also list every streak and break (needs track)"`
	From        string `json:"from,omitempty" jsonschema:"Count only from this day: YYYY-MM-DD, today, yesterday or -N. Default the first tick"`
	To          string `json:"to,omitempty" jsonschema:"Count only up to this day, included. Default today"`
}

type runOut struct {
	IsStreak bool   `json:"isStreak" jsonschema:"true for a streak of entries, false for a break"`
	Length   int    `json:"length" jsonschema:"In days"`
	From     string `json:"from"`
	To       string `json:"to"`
}

type statsOut struct {
	Track         string   `json:"track"`
	TrackID       int      `json:"trackId"`
	Total         int      `json:"total" jsonschema:"Days ticked (yes/no), the sum of the counts (counter), or days with an entry (number, choice)"`
	WeeklyMean    float64  `json:"weeklyMean"`
	Last7         int      `json:"last7" jsonschema:"In the last 7 days, today included"`
	Previous7     int      `json:"previous7" jsonschema:"In the 7 days before those"`
	Peak7         int      `json:"peak7" jsonschema:"The most in any 7 days in a row"`
	Current       *runOut  `json:"current" jsonschema:"The streak or break running now"`
	UpToYesterday bool     `json:"upToYesterday" jsonschema:"The current streak runs to yesterday and today has no entry yet"`
	LongestStreak *runOut  `json:"longestStreak"`
	LongestBreak  *runOut  `json:"longestBreak"`
	FirstTick     string   `json:"firstTick,omitempty"`
	LastTick      string   `json:"lastTick,omitempty"`
	MaxValue      any      `json:"maxValue" jsonschema:"The highest count, entry (number) or place (choice on an ordered scale); null on a plain-list choice"`
	MinValue      any      `json:"minValue" jsonschema:"The lowest, as maxValue"`
	Runs          []runOut `json:"runs,omitempty"`
}

type getStatsOut struct {
	Today string     `json:"today" jsonschema:"Today's date on the person's computer, YYYY-MM-DD"`
	From  string     `json:"from,omitempty"`
	To    string     `json:"to,omitempty"`
	Stats []statsOut `json:"stats"`
}

// statsValue is a track's maxValue or minValue: a Number's exact Decimal,
// any other track's as the server sent it.
func statsValue(t tickbuddy.Track, v tickbuddy.Value) any {
	if t.IsNumber() {
		return tickbuddy.ValueFor(t, v)
	}
	return v
}

func newRunOut(r *tickbuddy.Run) *runOut {
	if r == nil {
		return nil
	}
	return &runOut{IsStreak: r.IsStreak, Length: r.Length, From: r.From.String(), To: r.To.String()}
}

func dateString(d *tickbuddy.Date) string {
	if d == nil {
		return ""
	}
	return d.String()
}

func (s *srv) addReadTools(m *mcp.Server) {
	mcp.AddTool(m, readTool("list_tracks", "List tracks",
		"Lists the person's Tickbuddy tracks in journal order: id, name, type (yes/no, counter, number, choice, or the name of a newer type TickLLMe can read but not tick) and whether archived. A number track also has its unit, decimal places and whether it allows negative values; a choice track its options in order, each with its place and label (pick an option by its label), and whether they are an ordered scale."),
		s.listTracks)
	mcp.AddTool(m, readTool("get_day", "Get a day",
		"Every track on one day with its value and note, unticked ones included (false or 0). The one call for \"how did I do yesterday?\"."),
		s.getDay)
	mcp.AddTool(m, readTool("get_ticks", "Get ticks",
		"The entries from one day to another, both included: only days with an entry. At most 31 days across all tracks, or 366 for one track, and 500 entries. For totals, means and streaks over a period use get_stats instead."),
		s.getTicks)
	mcp.AddTool(m, readTool("get_notes", "Get notes",
		"The day notes from one day to another, both included, newest first. Same limits as get_ticks."),
		s.getNotes)
	mcp.AddTool(m, readTool("get_stats", "Get stats",
		"The stats Tickbuddy shows on its Analytics cards, per track: total, weekly mean, last 7 days, current and longest streak and break. With from and/or to, only that period counts, so comparing two periods (\"2026 against 2025\") is two calls. On number and choice tracks the counts are of days with an entry; maxValue and minValue are the highest and lowest value (number) or place (choice on an ordered scale), null on a plain-list choice."),
		s.getStats)
}

func (s *srv) listTracks(ctx context.Context, _ *mcp.CallToolRequest, in listTracksIn) (*mcp.CallToolResult, listTracksOut, error) {
	out := listTracksOut{Today: s.today().String(), Tracks: []trackOut{}}
	tracks, err := s.tracks(ctx, in.IncludeArchived)
	if err != nil {
		return nil, out, s.fail(err)
	}
	for _, t := range tracks {
		out.Tracks = append(out.Tracks, newTrackOut(t))
	}
	return nil, out, nil
}

func (s *srv) getDay(ctx context.Context, _ *mcp.CallToolRequest, in getDayIn) (*mcp.CallToolResult, getDayOut, error) {
	out := getDayOut{Today: s.today().String(), Tracks: []dayTrack{}}
	day, err := s.day(in.Date)
	if err != nil {
		return nil, out, s.fail(err)
	}
	out.Date = day.String()
	if err := s.connect(ctx); err != nil {
		return nil, out, s.fail(err)
	}
	tracks, err := s.tracks(ctx, in.IncludeArchived)
	if err != nil {
		return nil, out, s.fail(err)
	}
	ticks, err := s.Client.Ticks(ctx, day, day)
	if err != nil {
		return nil, out, s.fail(err)
	}
	values := map[int]tickbuddy.Value{}
	for _, t := range ticks {
		if t.Date == day {
			values[t.TrackID] = t.Value
		}
	}
	notes := map[int]string{}
	if s.Client.Has(tickbuddy.FeatureNotes) {
		ns, err := s.Client.Notes(ctx, day, day)
		if err != nil {
			return nil, out, s.fail(err)
		}
		for _, n := range ns {
			if n.Date == day {
				notes[n.TrackID] = n.Text
			}
		}
	}
	for _, t := range tracks {
		v := dayValue(t, values)
		dt := dayTrack{ID: t.ID, Name: t.Name, Type: typeName(t), Value: v, Archived: t.IsArchived(), Private: t.Private}
		dt.Unit, dt.Label = extras(t, v)
		if n, ok := notes[t.ID]; ok {
			dt.Note = &n
		}
		out.Tracks = append(out.Tracks, dt)
	}
	return nil, out, nil
}

// dayValue is t's value in values, or its empty value (no, 0, or null on a
// number or choice track and a type TickLLMe doesn't know, where 0 can be a
// real value) without one.
func dayValue(t tickbuddy.Track, values map[int]tickbuddy.Value) tickbuddy.Value {
	if v, ok := values[t.ID]; ok {
		return tickbuddy.ValueFor(t, v)
	}
	return tickbuddy.EmptyValue(t)
}

// entry reports whether v is an entry: a tick or a count above 0, or on a
// number or choice track and a type TickLLMe doesn't know, any value but
// null (a number's 0 is a reading).
func entry(t tickbuddy.Track, v tickbuddy.Value) bool {
	if t.Type == tickbuddy.TypeYesNo || t.IsCounter() {
		return v.Int() > 0
	}
	return !tickbuddy.ValueFor(t, v).IsNull()
}

// span reads a range's from, to and track, and checks the day caps
// (decision 6). With a track, only it is in scope; otherwise the visible,
// active tracks are. byID maps the tracks in scope.
func (s *srv) span(ctx context.Context, in rangeIn) (from, to tickbuddy.Date, byID map[int]tickbuddy.Track, order map[int]int, err error) {
	today := s.today()
	if from, err = tickbuddy.ParseDate(in.From, today); err != nil {
		return
	}
	if to, err = tickbuddy.ParseDate(in.To, today); err != nil {
		return
	}
	if to.Before(from) {
		err = fmt.Errorf("from (%s) must be on or before to (%s)", from, to)
		return
	}
	days := int(to.Time(time.UTC).Sub(from.Time(time.UTC)).Hours()/24) + 1
	switch {
	case in.Track == "" && days > maxDaysAllTracks:
		err = fmt.Errorf("that's %d days, and across all tracks one call covers at most %d. Ask for fewer days, or for one track (up to %d days)", days, maxDaysAllTracks, maxDaysOneTrack)
		return
	case days > maxDaysOneTrack:
		err = fmt.Errorf("that's %d days, and for one track one call covers at most %d. Ask for fewer days, or use get_stats for totals over a longer period", days, maxDaysOneTrack)
		return
	}
	byID, order = map[int]tickbuddy.Track{}, map[int]int{}
	if in.Track != "" {
		var t tickbuddy.Track
		if t, err = s.resolve(ctx, in.Track); err != nil {
			return
		}
		byID[t.ID] = t
		return
	}
	tracks, err := s.tracks(ctx, false)
	if err != nil {
		return
	}
	for i, t := range tracks {
		byID[t.ID], order[t.ID] = t, i
	}
	return
}

func tooMany(n int, track string) error {
	if track != "" {
		return fmt.Errorf("that's %d entries, more than the %d one answer holds. Ask for fewer days, or use get_stats for totals", n, maxEntries)
	}
	return fmt.Errorf("that's %d entries, more than the %d one answer holds. Ask for fewer days or for one track", n, maxEntries)
}

func (s *srv) getTicks(ctx context.Context, _ *mcp.CallToolRequest, in rangeIn) (*mcp.CallToolResult, getTicksOut, error) {
	out := getTicksOut{Today: s.today().String(), Ticks: []tickOut{}}
	from, to, byID, order, err := s.span(ctx, in)
	if err != nil {
		return nil, out, s.fail(err)
	}
	out.From, out.To = from.String(), to.String()
	ticks, err := s.Client.Ticks(ctx, from, to)
	if err != nil {
		return nil, out, s.fail(err)
	}
	var kept []tickbuddy.Tick
	for _, t := range ticks {
		if tr, ok := byID[t.TrackID]; ok && entry(tr, t.Value) {
			kept = append(kept, t)
		}
	}
	if len(kept) > maxEntries {
		return nil, out, s.fail(tooMany(len(kept), in.Track))
	}
	slices.SortFunc(kept, func(a, b tickbuddy.Tick) int {
		if c := a.Date.Compare(b.Date); c != 0 {
			return c
		}
		return order[a.TrackID] - order[b.TrackID]
	})
	for _, t := range kept {
		tr := byID[t.TrackID]
		to := tickOut{Date: t.Date.String(), Track: tr.Name, TrackID: tr.ID, Value: value(tr, t.Value)}
		to.Unit, to.Label = extras(tr, t.Value)
		out.Ticks = append(out.Ticks, to)
	}
	return nil, out, nil
}

func (s *srv) getNotes(ctx context.Context, _ *mcp.CallToolRequest, in rangeIn) (*mcp.CallToolResult, getNotesOut, error) {
	out := getNotesOut{Today: s.today().String(), Notes: []noteOut{}}
	from, to, byID, _, err := s.span(ctx, in)
	if err != nil {
		return nil, out, s.fail(err)
	}
	out.From, out.To = from.String(), to.String()
	notes, err := s.Client.Notes(ctx, from, to)
	if err != nil {
		return nil, out, s.fail(err)
	}
	for _, n := range notes {
		if tr, ok := byID[n.TrackID]; ok {
			out.Notes = append(out.Notes, noteOut{Date: n.Date.String(), Track: tr.Name, TrackID: tr.ID, Text: n.Text})
		}
	}
	if len(out.Notes) > maxEntries {
		n := len(out.Notes)
		out.Notes = []noteOut{}
		return nil, out, s.fail(tooMany(n, in.Track))
	}
	return nil, out, nil
}

func (s *srv) getStats(ctx context.Context, _ *mcp.CallToolRequest, in getStatsIn) (*mcp.CallToolResult, getStatsOut, error) {
	today := s.today()
	out := getStatsOut{Today: today.String(), Stats: []statsOut{}}
	if in.IncludeRuns && in.Track == "" {
		return nil, out, s.fail(errors.New("listing every streak and break (include_runs) needs a track"))
	}
	var from, to tickbuddy.Date
	var err error
	if in.From != "" {
		if from, err = tickbuddy.ParseDate(in.From, today); err != nil {
			return nil, out, s.fail(err)
		}
	}
	if in.To != "" {
		if to, err = tickbuddy.ParseDate(in.To, today); err != nil {
			return nil, out, s.fail(err)
		}
	}
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		return nil, out, s.fail(fmt.Errorf("from (%s) must be on or before to (%s)", from, to))
	}
	out.From, out.To = from.String(), to.String()

	byID := map[int]tickbuddy.Track{}
	id := 0
	if in.Track != "" {
		t, err := s.resolve(ctx, in.Track)
		if err != nil {
			return nil, out, s.fail(err)
		}
		byID[t.ID], id = t, t.ID
	} else {
		tracks, err := s.tracks(ctx, false)
		if err != nil {
			return nil, out, s.fail(err)
		}
		for _, t := range tracks {
			byID[t.ID] = t
		}
	}

	var stats []tickbuddy.Stats
	if from.IsZero() && to.IsZero() {
		stats, err = s.Client.Stats(ctx, today, id, in.IncludeRuns, false)
	} else {
		stats, err = s.Client.StatsPeriod(ctx, today, from, to, id, in.IncludeRuns, false)
	}
	if err != nil {
		return nil, out, s.fail(err)
	}
	for _, st := range stats {
		t, ok := byID[st.TrackID]
		if !ok {
			continue
		}
		so := statsOut{
			Track: t.Name, TrackID: t.ID, Total: st.Total, WeeklyMean: st.WeeklyMean,
			Last7: st.Last7, Previous7: st.Previous7, Peak7: st.Peak7,
			Current: newRunOut(st.Current), UpToYesterday: st.UpToYesterday,
			LongestStreak: newRunOut(st.LongestStreak), LongestBreak: newRunOut(st.LongestBreak),
			FirstTick: dateString(st.FirstTick), LastTick: dateString(st.LastTick),
			MaxValue: statsValue(t, st.MaxValue), MinValue: statsValue(t, st.MinValue),
		}
		for _, r := range st.Runs {
			so.Runs = append(so.Runs, *newRunOut(&r))
		}
		out.Stats = append(out.Stats, so)
	}
	return nil, out, nil
}
