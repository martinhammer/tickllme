// SPDX-License-Identifier: GPL-3.0-or-later

package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/martinhammer/tickcli/tickbuddy"
)

type createTrackIn struct {
	Name string `json:"name" jsonschema:"The new track's name"`
	Type string `json:"type" jsonschema:"yes/no (ticked or not each day), counter (a whole number each day), number (a reading each day, such as 10.4 km) or choice (one of a fixed list of options each day)"`
	// A Number's settings.
	Unit          *string `json:"unit,omitempty" jsonschema:"number: the unit, such as km or °C. Default none"`
	DecimalPlaces *int    `json:"decimal_places,omitempty" jsonschema:"number: how many decimals to keep, 0 to 3. Default 1"`
	AllowNegative *bool   `json:"allow_negative,omitempty" jsonschema:"number: whether values below 0 are allowed. Default false"`
	// A Choice's settings.
	Options      []string `json:"options,omitempty" jsonschema:"choice: 2 to 99 option labels, in order. They can't be added, removed or reordered later, only renamed"`
	OrderedScale *bool    `json:"ordered_scale,omitempty" jsonschema:"choice: whether the options run from one end of a scale to the other (such as Excited to Sad), rather than being a plain list. Default false"`
}

type renameOptionIn struct {
	Track  string `json:"track" jsonschema:"The choice track's exact name (case doesn't matter) or #id"`
	Option any    `json:"option" jsonschema:"The option to rename: its current label, \"#N\" for the option in place N, or N as a number"`
	Label  string `json:"label" jsonschema:"The option's new label"`
}

type renameTrackIn struct {
	Track string `json:"track" jsonschema:"The track's exact name (case doesn't matter) or #id"`
	Name  string `json:"name" jsonschema:"The new name"`
}

type trackResultOut struct {
	Today string   `json:"today" jsonschema:"Today's date on the person's computer, YYYY-MM-DD"`
	Track trackOut `json:"track"`
}

func (s *srv) addTrackTools(m *mcp.Server) {
	mcp.AddTool(m, writeTool("create_track", "Create a track",
		"Adds a new track at the end of the journal.", false),
		s.createTrack)
	mcp.AddTool(m, writeTool("rename_track", "Rename a track",
		"Gives a track a new name; its entries and notes stay.", true),
		s.renameTrack)
	mcp.AddTool(m, writeTool("rename_option", "Rename a choice option",
		"Gives one option of a choice track a new label; the days that hold it keep it. Options can't be added, removed or reordered.", true),
		s.renameOption)
}

func (s *srv) createTrack(ctx context.Context, _ *mcp.CallToolRequest, in createTrackIn) (*mcp.CallToolResult, trackResultOut, error) {
	out := trackResultOut{Today: s.today().String()}
	var kind string
	switch strings.ToLower(strings.TrimSpace(in.Type)) {
	case "yes/no", "yesno", "boolean":
		kind = tickbuddy.TypeYesNo
	case "counter":
		kind = tickbuddy.TypeCounter
	case "number":
		kind = tickbuddy.TypeNumber
	case "choice":
		kind = tickbuddy.TypeChoice
	default:
		return nil, out, s.fail(fmt.Errorf("type must be yes/no, counter, number or choice, not %q", in.Type))
	}
	// Settings of another type go to the server, which refuses them.
	var settings *tickbuddy.TrackSettings
	if in.Unit != nil || in.DecimalPlaces != nil || in.AllowNegative != nil || in.Options != nil || in.OrderedScale != nil {
		settings = &tickbuddy.TrackSettings{Unit: in.Unit, DecimalPlaces: in.DecimalPlaces, AllowNegative: in.AllowNegative, Options: in.Options, OrderedScale: in.OrderedScale}
	}
	t, err := s.Client.CreateTrack(ctx, in.Name, kind, settings)
	if err != nil {
		return nil, out, s.fail(err)
	}
	out.Track = newTrackOut(t)
	return nil, out, nil
}

func (s *srv) renameTrack(ctx context.Context, _ *mcp.CallToolRequest, in renameTrackIn) (*mcp.CallToolResult, trackResultOut, error) {
	out := trackResultOut{Today: s.today().String()}
	t, err := s.resolve(ctx, in.Track)
	if err != nil {
		return nil, out, s.fail(err)
	}
	renamed, err := s.Client.RenameTrack(ctx, t.ID, in.Name)
	if err != nil {
		return nil, out, s.fail(readOnly(t, err))
	}
	out.Track = newTrackOut(renamed)
	return nil, out, nil
}

func (s *srv) renameOption(ctx context.Context, req *mcp.CallToolRequest, in renameOptionIn) (*mcp.CallToolResult, trackResultOut, error) {
	out := trackResultOut{Today: s.today().String()}
	raw, sent := rawArgument(req, "option")
	if !sent || isNull(raw) {
		return nil, out, s.fail(errors.New("option is missing: the option's label, \"#N\" or its place"))
	}
	t, err := s.resolve(ctx, in.Track)
	if err != nil {
		return nil, out, s.fail(err)
	}
	if !t.IsChoice() {
		return nil, out, s.fail(fmt.Errorf("%q is a %s track; only choice tracks have options", t.Name, typeName(t)))
	}
	place, err := choiceValue(t, raw)
	if err != nil {
		return nil, out, s.fail(err)
	}
	renamed, err := s.Client.RenameOption(ctx, t.ID, place, in.Label)
	if errors.Is(err, tickbuddy.ErrOptionNotFound) {
		return nil, out, s.fail(fmt.Errorf("%q has no option #%d", t.Name, place))
	}
	if err != nil {
		return nil, out, s.fail(readOnly(t, err))
	}
	out.Track = newTrackOut(renamed)
	return nil, out, nil
}
