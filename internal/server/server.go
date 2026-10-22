// SPDX-License-Identifier: GPL-3.0-or-later

// Package server is TickLLMe's MCP server: its tools, the log_day prompt,
// the instructions Claude gets, and the shapes of its results. The handlers
// don't know their transport; cmd/tickllme runs them over stdio.
package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/martinhammer/tickcli/tickbuddy"
)

// Access levels (D7). Only the tools a level allows are registered.
const (
	LevelRead   = 1 // read the journal
	LevelTicks  = 2 // also tick, change counters and write notes
	LevelTracks = 3 // also create and rename tracks
)

// Caps on one read (decision 6).
const (
	maxDaysAllTracks = 31
	maxDaysOneTrack  = 366
	maxEntries       = 500
	maxNoteRunes     = 500
)

// Options configure a server.
type Options struct {
	// Level is LevelRead, LevelTicks or LevelTracks.
	Level int
	// Private shows private tracks (D8); without it they are hidden.
	Private bool
	// Client talks to Tickbuddy.
	Client *tickbuddy.Client
	// Credentials, when they are TickLLMe's own, let error messages say
	// where to fix a refused app password. May be nil.
	Credentials *Credentials
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Version is TickLLMe's version, as the server reports it.
	Version string
}

type srv struct {
	Options
}

// New returns the MCP server for opts, with the tools and prompt its level
// allows.
func New(opts Options) (*mcp.Server, error) {
	if opts.Level < LevelRead || opts.Level > LevelTracks {
		return nil, fmt.Errorf("the access level must be 1, 2 or 3, not %d", opts.Level)
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	s := &srv{opts}
	m := mcp.NewServer(&mcp.Implementation{Name: "tickllme", Title: "TickLLMe", Version: opts.Version},
		&mcp.ServerOptions{Instructions: instructions(opts.Level, opts.Private)})
	s.addReadTools(m)
	if opts.Level >= LevelTicks {
		s.addWriteTools(m)
		s.addPrompt(m)
	}
	if opts.Level >= LevelTracks {
		s.addTrackTools(m)
	}
	return m, nil
}

// today is the machine's today (decision 9).
func (s *srv) today() tickbuddy.Date { return tickbuddy.DateOf(s.Now()) }

// day reads a date argument: "" is today.
func (s *srv) day(arg string) (tickbuddy.Date, error) {
	if strings.TrimSpace(arg) == "" {
		return s.today(), nil
	}
	return tickbuddy.ParseDate(arg, s.today())
}

// connect reads the server's capabilities, so Has works afterwards.
func (s *srv) connect(ctx context.Context) error {
	_, err := s.Client.Capabilities(ctx)
	return err
}

// tracks lists the tracks Claude may see: private ones only with Private.
func (s *srv) tracks(ctx context.Context, includeArchived bool) ([]tickbuddy.Track, error) {
	all, err := s.Client.Tracks(ctx, includeArchived)
	if err != nil {
		return nil, err
	}
	out := make([]tickbuddy.Track, 0, len(all))
	for _, t := range all {
		if t.Private && !s.Private {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// resolve finds the track query names among the visible tracks, archived
// ones included: exact name ignoring case, or #id, never a prefix (D13). A
// hidden private track reads as not found.
func (s *srv) resolve(ctx context.Context, query string) (tickbuddy.Track, error) {
	if strings.TrimSpace(query) == "" {
		return tickbuddy.Track{}, errors.New("name a track: its name or #id")
	}
	visible, err := s.tracks(ctx, true)
	if err != nil {
		return tickbuddy.Track{}, err
	}
	t, err := tickbuddy.Resolve(visible, query, false)
	var me *tickbuddy.MatchError
	if errors.As(err, &me) {
		return tickbuddy.Track{}, matchError(me, visible)
	}
	return t, err
}

// matchError lists the candidates with their ids, so Claude can ask which
// one was meant. Names are quoted: they are the person's data.
func matchError(me *tickbuddy.MatchError, visible []tickbuddy.Track) error {
	if me.Kind == tickbuddy.ErrAmbiguous {
		return fmt.Errorf("%q matches several tracks: %s. Name one by its #id", me.Query, trackList(me.Candidates))
	}
	var active []tickbuddy.Track
	for _, t := range visible {
		if !t.IsArchived() {
			active = append(active, t)
		}
	}
	if len(active) == 0 {
		return fmt.Errorf("no track named %q, and there are no tracks", me.Query)
	}
	return fmt.Errorf("no track named %q. Tracks: %s", me.Query, trackList(active))
}

func trackList(tracks []tickbuddy.Track) string {
	parts := make([]string, len(tracks))
	for i, t := range tracks {
		parts[i] = fmt.Sprintf("%q (#%d)", t.Name, t.ID)
	}
	return strings.Join(parts, ", ")
}

// archivedError refuses a write to an archived track (decision 7).
func archivedError(t tickbuddy.Track) error {
	return fmt.Errorf("track %q is archived, so nothing was changed", t.Name)
}

// readOnly reads a write's error: the server's 423 for an archived track
// becomes the same refusal as before sending; a 423 for another reason keeps
// the server's message (decision 7).
func readOnly(t tickbuddy.Track, err error) error {
	var e *tickbuddy.ReadOnlyError
	if errors.As(err, &e) && e.Reason == tickbuddy.ReasonArchived {
		return archivedError(t)
	}
	return err
}

// fail turns an error into the one plain sentence Claude gets.
func (s *srv) fail(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(s.sentence(err))
}

func (s *srv) sentence(err error) string {
	var e *tickbuddy.Error
	var te *tickbuddy.TypeError
	switch {
	case errors.As(err, &te):
		return fmt.Sprintf("%q is a %q track; TickLLMe update required.", te.Track, te.Type)
	case isNoCredentials(err):
		return msgNotSignedIn
	case errors.Is(err, tickbuddy.ErrUnauthorized):
		if s.Credentials != nil {
			if _, o, err := s.Credentials.Account(context.Background()); err == nil && !o.Settings {
				return msgRefusedTick
			}
		}
		return msgRefused
	case errors.Is(err, tickbuddy.ErrUnreachable) && errors.As(err, &e):
		where := "the server"
		if s.Credentials != nil {
			if a, _, err := s.Credentials.Account(context.Background()); err == nil {
				where = a.URL
			}
		}
		reason := strings.TrimPrefix(e.Message, "can't reach the server: ")
		return sentenceCase(fmt.Sprintf("Can't reach %s: %s", where, reason))
	case errors.Is(err, context.Canceled):
		return "The request was cancelled"
	}
	return sentenceCase(err.Error())
}

// sentenceCase starts msg with a capital letter and ends it with a stop.
func sentenceCase(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "Something went wrong"
	}
	r, n := utf8.DecodeRuneInString(msg)
	msg = string(unicode.ToUpper(r)) + msg[n:]
	if !strings.HasSuffix(msg, ".") && !strings.HasSuffix(msg, "?") && !strings.HasSuffix(msg, "!") {
		msg += "."
	}
	return msg
}

func ptr[T any](v T) *T { return &v }

// Tool annotations (spec §4.1).
func readTool(name, title, desc string) *mcp.Tool {
	return &mcp.Tool{Name: name, Title: title, Description: desc, Annotations: &mcp.ToolAnnotations{
		Title: title, ReadOnlyHint: true, OpenWorldHint: ptr(false),
	}}
}

func writeTool(name, title, desc string, idempotent bool) *mcp.Tool {
	return &mcp.Tool{Name: name, Title: title, Description: desc, Annotations: &mcp.ToolAnnotations{
		Title: title, DestructiveHint: ptr(false), IdempotentHint: idempotent, OpenWorldHint: ptr(false),
	}}
}
