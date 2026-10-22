# TickLLMe
TickLLMe (pronounced "tickle me") is the MCP server and Claude Desktop extension for [Tickbuddy](https://github.com/martinhammer/tickbuddy), the daily habit and event tracker for Nextcloud.

It lets Claude answer questions about your Tickbuddy journal ("how's my exercise streak?", "what did I tick yesterday?") and if you allow it record things ("I had three coffees", "tick the nightly backup and note it finished at 02:14") or even create a populate tracks ("check github.com/martinhammer and see on which days in 2026 I made a contribution, then create a new track in Tickbuddy called Coding and tick those days"). The level of access is controlled through settings. It runs on your own computer and talks only to your Nextcloud.

## What Claude may do

You choose an access level in TickLLMe's settings:

| Level | Claude can |
|---|---|
| 1 | Default. Only read your journal: tracks, days, ticks, notes and stats |
| 2 | Also tick and untick, change counters and write notes |
| 3 | Also add and rename tracks |

Claude can never delete, archive or reorder tracks and it can't import anything. Tracks marked private stay hidden from Claude unless you turn on "Show private tracks". Claude also asks before each tool call unless you allow it.

## Install

### Claude Desktop

Download `tickllme-vX.Y.Z.mcpb` from the [latest release](https://github.com/martinhammer/tickllme/releases/latest) and add it in Extensions in Claude Desktop. Then use the extension settings to sign in and configure TickLLMe.

### Claude Code

```
/plugin marketplace add martinhammer/tickllme
/plugin install tickllme@tickllme
```

Claude Code then asks for the settings as mentioned below.

Marketplaces other than Anthropic's don't update automatically by default, so turn on auto-update for this marketplace in `/plugin`, or update by hand there. Needs Claude Code 2.1.224 or later, on Linux (x64 or arm64), macOS or Windows.

## Signing in

There are two ways, and you only need one:

* Enter your **Nextcloud address**, **Nextcloud user name** and a **Nextcloud app password** in TickLLMe's settings. All three must be filled in.
* If you've already run `tickcli login` (or `tickllme login`) on this computer, leave all three fields empty. TickLLMe uses the same stored sign-in, from the keychain or Tickcli's encrypted file.
* If both are present, the settings win.

`tickllme status` shows which sign-in is used, for example:

```
Signed in as user6 at https://cloud.example.com · from TickLLMe's settings
Tickbuddy 2.3.0
```

## Settings

| Setting | What it does |
|---|---|
| Nextcloud address | The URL of your Nextcloud server where Tickbuddy is installed. Leave empty to use your Tickcli sign-in. The setting accepts only an `https://` address, or plain `http://` to `localhost` or a private IP address. For a Nextcloud you reach over plain http by name (such as `http://nextcloud.local`), sign in with `tickllme login <url> --insecure-http` instead and leave the fields empty. |
| Nextcloud user name | Your Nextcloud login name. Leave empty to use your Tickcli sign-in. |
| Nextcloud app password | A Nextcloud app password. Leave empty to use your Tickcli sign-in. |
| What Claude may do | The access level, 1 to 3. |
| Show private tracks | Off: Claude can't see or change private tracks. On: it can. |

## Tools

| Tool | Level | What it does |
|---|---|---|
| `list_tracks` | 1 | The tracks in journal order |
| `get_day` | 1 | Every track on one day, with its value and note |
| `get_ticks` | 1 | The entries over up to 31 days (366 for one track), at most 500 |
| `get_notes` | 1 | The day notes over the same ranges |
| `get_stats` | 1 | Total, weekly mean, last 7 days, streaks and breaks; for all time, or for a period with `from` and `to` |
| `set_tick` | 2 | Tick or untick a yes/no track, or set a counter, optionally with a note |
| `add_to_counter` | 2 | Add to or take from a counter |
| `set_note` | 2 | Write, replace, append to or delete a day's note |
| `create_track` | 3 | Add a yes/no or counter track |
| `rename_track` | 3 | Rename a track |

Dates are `YYYY-MM-DD`, `today`, `yesterday` or `-N` (N days ago), in your computer's time zone. Notes are plain text, up to 500 characters; appending a line that's already in the note changes nothing, so an agent logging several activities a day can append safely. Stats for a period need Tickbuddy with the `statsPeriod` feature, and appending notes the `notesAppend` feature; an older server says so.

In Claude Code, the `log_day` prompt is `/mcp__tickbuddy__log_day`: it goes through every track for a day, confirms what's already entered, asks about the rest, and saves the changes after one yes. Claude Desktop offers it among the extension's prompts.

## Running it yourself

```
tickllme [serve] [--level 1|2|3] [--private]   serve MCP over stdio
tickllme login <url> [--paste] [--insecure-http]
tickllme logout [--local]
tickllme status
tickllme --version
```

`TICKLLME_LEVEL` and `TICKLLME_PRIVATE` set the same as the flags; a flag wins. `TICKBUDDY_URL`, `TICKBUDDY_USER` and `TICKBUDDY_APP_PASSWORD` sign in for one run. Standard output carries only MCP messages; logs go to standard error.
