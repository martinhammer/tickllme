# Changelog

## [0.1.0] - 2026-10-22

First release.

- Claude Code plugin (martinhammer/tickllme is its marketplace) and Claude Desktop extension for macOS, Windows and Linux.
- First version: the MCP server over stdio, with read tools at level 1 (`list_tracks`, `get_day`, `get_ticks`, `get_notes`, `get_stats`), writes at level 2 (`set_tick`, `add_to_counter`, `set_note` and the `log_day` prompt) and track tools at level 3 (`create_track`, `rename_track`).
- Sign-in from the settings, or from a Tickcli login. `tickllme login`, `logout` and `status`, sharing Tickcli's store.
