# Changelog

## [0.2.0] - 2026-11-22

Number and choice tracks (requires Tickbuddy 3.2.0 or later).

- `list_tracks` gives a number track's unit, decimal places and whether it allows negative values, and a choice track's options and whether they are an ordered scale.
- `get_day`, `get_ticks` and `set_tick` give a number's stored value exactly, with its `unit`, and a choice's place with its `label`; `null` is a day without a value.
- `set_tick` sets a number (`10.4` or `"10.4"`) or a choice (an option's label, `"#2"`, or the place as a number), and clears either with `null`.
- `get_stats` includes number and choice tracks, counting days with an entry; `maxValue` and `minValue` are a number's values, an ordered choice's places, or `null`.
- Level 3: `create_track` takes the new types with their settings, and `rename_option` renames a choice track's option.
- The instructions, the skill and `log_day` cover the new types: a number's 0 is a reading, a point is the decimal separator, adding to a number is reading the day and setting the total, and a choice track's options are shown for a yes before it is created.

## [0.1.0] - 2026-10-22

First release.

- Claude Code plugin (martinhammer/tickllme is its marketplace) and Claude Desktop extension for macOS, Windows and Linux.
- First version: the MCP server over stdio, with read tools at level 1 (`list_tracks`, `get_day`, `get_ticks`, `get_notes`, `get_stats`), writes at level 2 (`set_tick`, `add_to_counter`, `set_note` and the `log_day` prompt) and track tools at level 3 (`create_track`, `rename_track`).
- Sign-in from the settings, or from a Tickcli login. `tickllme login`, `logout` and `status`, sharing Tickcli's store.
