// SPDX-License-Identifier: GPL-3.0-or-later

package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/martinhammer/tickcli/tickbuddy"
)

type createTrackIn struct {
	Name string `json:"name" jsonschema:"The new track's name"`
	Type string `json:"type" jsonschema:"yes/no (ticked or not each day) or counter (a number each day)"`
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
}

func (s *srv) createTrack(ctx context.Context, _ *mcp.CallToolRequest, in createTrackIn) (*mcp.CallToolResult, trackResultOut, error) {
	out := trackResultOut{Today: s.today().String()}
	var kind string
	switch strings.ToLower(strings.TrimSpace(in.Type)) {
	case "yes/no", "yesno", "boolean":
		kind = tickbuddy.TypeYesNo
	case "counter":
		kind = tickbuddy.TypeCounter
	default:
		return nil, out, s.fail(fmt.Errorf("type must be yes/no or counter, not %q", in.Type))
	}
	t, err := s.Client.CreateTrack(ctx, in.Name, kind)
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
