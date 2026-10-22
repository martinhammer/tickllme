// SPDX-License-Identifier: GPL-3.0-or-later

// Command tickllme is TickLLMe, the MCP server that lets Claude read and
// record a Tickbuddy journal. Run without arguments, or as `tickllme serve`,
// it serves MCP over stdio; login, logout and status manage the sign-in it
// shares with Tickcli.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/term"

	"github.com/martinhammer/tickcli/tickbuddy"

	"github.com/martinhammer/tickllme/internal/server"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

// Environment variables for serve's flags; a flag wins.
const (
	envLevel   = "TICKLLME_LEVEL"
	envPrivate = "TICKLLME_PRIVATE"
)

const usage = `TickLLMe lets Claude read and record your Tickbuddy journal.

Usage:
  tickllme [serve] [--level 1|2|3] [--private]   serve MCP over stdio (what Claude runs)
  tickllme login <url> [--paste] [--insecure-http]
  tickllme logout [--local]
  tickllme status
  tickllme --version

Levels: 1 only reads (default), 2 also ticks, counts and writes notes,
3 also creates and renames tracks. Private tracks stay hidden without
--private. TICKLLME_LEVEL and TICKLLME_PRIVATE set the same; a flag wins.

Sign-in: TICKBUDDY_URL, TICKBUDDY_USER and TICKBUDDY_APP_PASSWORD (all
three), or else what ` + "`tickcli login` or `tickllme login`" + ` stored.
`

// app is one run; its fields are the outside world.
type app struct {
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer
	stdinTTY bool
	getenv   func(string) string
	store    *tickbuddy.Store
	hostname string
	readPass func() ([]byte, error)
}

func main() {
	host, _ := os.Hostname()
	a := &app{
		stdin:    os.Stdin,
		stdout:   os.Stdout,
		stderr:   os.Stderr,
		stdinTTY: term.IsTerminal(int(os.Stdin.Fd())),
		getenv:   os.Getenv,
		store:    tickbuddy.DefaultStore(),
		hostname: host,
		readPass: func() ([]byte, error) { return term.ReadPassword(int(os.Stdin.Fd())) },
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(a.run(ctx, os.Args[1:]))
}

// errUsage is bad usage: exit 2.
type errUsage struct{ msg string }

func (e *errUsage) Error() string { return e.msg }

func (a *app) run(ctx context.Context, args []string) int {
	cmd, rest := "serve", args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, rest = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		if len(args) > 0 && (args[0] == "--version" || args[0] == "-version" || args[0] == "version") {
			fmt.Fprintf(a.stdout, "tickllme %s\n", version)
			return 0
		}
		if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "-help") {
			fmt.Fprint(a.stdout, usage)
			return 0
		}
		err = a.serve(ctx, rest)
	case "version":
		fmt.Fprintf(a.stdout, "tickllme %s\n", version)
		return 0
	case "help":
		fmt.Fprint(a.stdout, usage)
		return 0
	case "login":
		err = a.login(ctx, rest)
	case "logout":
		err = a.logout(ctx, rest)
	case "status":
		err = a.status(ctx, rest)
	default:
		err = &errUsage{fmt.Sprintf("unknown command %q; see `tickllme help`", cmd)}
	}
	if err == nil {
		return 0
	}
	fmt.Fprintf(a.stderr, "tickllme: %s\n", err)
	var ue *errUsage
	if errors.As(err, &ue) {
		return 2
	}
	return 1
}

// flags returns a flag set whose errors are usage errors.
func (a *app) flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return &errUsage{err.Error()}
	}
	return nil
}

// unset reports whether a setting is empty, or a "${user_config.…}" the
// host left unsubstituted (spec §9 item 3).
func unset(v string) bool {
	v = strings.TrimSpace(v)
	return v == "" || strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}")
}

// serveSettings reads serve's flags over the TICKLLME_* variables.
func (a *app) serveSettings(args []string) (level int, private bool, err error) {
	fs := a.flags("serve")
	levelArg := fs.String("level", "", "access level, 1 to 3")
	privateArg := fs.String("private", "", "show private tracks")
	// --private alone means true, as a boolean flag.
	for i, s := range args {
		if s == "--private" || s == "-private" {
			args[i] = "--private=true"
		}
	}
	if err := parse(fs, args); err != nil {
		return 0, false, err
	}
	if fs.NArg() > 0 {
		return 0, false, &errUsage{fmt.Sprintf("serve takes no arguments, got %q", fs.Arg(0))}
	}
	lv, pv := *levelArg, *privateArg
	if unset(lv) {
		lv = a.getenv(envLevel)
	}
	if unset(pv) {
		pv = a.getenv(envPrivate)
	}
	level = server.LevelRead
	if !unset(lv) {
		f, err := strconv.ParseFloat(strings.TrimSpace(lv), 64)
		if err != nil || f != float64(int(f)) || f < 1 || f > 3 {
			return 0, false, &errUsage{fmt.Sprintf("the access level must be 1, 2 or 3, not %q", lv)}
		}
		level = int(f)
	}
	if !unset(pv) {
		if private, err = strconv.ParseBool(strings.TrimSpace(pv)); err != nil {
			return 0, false, &errUsage{fmt.Sprintf("--private must be true or false, not %q", pv)}
		}
	}
	return level, private, nil
}

func (a *app) serve(ctx context.Context, args []string) error {
	level, private, err := a.serveSettings(args)
	if err != nil {
		return err
	}
	logger := log.New(a.stderr, "tickllme: ", 0)
	creds := server.NewCredentials(a.getenv, a.store)
	if acct, origin, err := creds.Account(ctx); err != nil {
		logger.Printf("not signed in yet: %v", err)
	} else {
		logger.Printf("Signed in as %s at %s · from %s", acct.User, acct.URL, origin)
	}
	logger.Printf("access level %d, private tracks %s", level, map[bool]string{true: "shown", false: "hidden"}[private])

	client := tickbuddy.New(creds, tickbuddy.WithUserAgent("TickLLMe/"+version))
	m, err := server.New(server.Options{Level: level, Private: private, Client: client, Credentials: creds, Version: version})
	if err != nil {
		return err
	}
	err = m.Run(ctx, &mcp.StdioTransport{})
	if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
