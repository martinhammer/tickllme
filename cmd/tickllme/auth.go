// SPDX-License-Identifier: GPL-3.0-or-later

package main

// login, logout and status are the tickbuddy package's shared sign-in
// (Store.SignIn, Store.Logout, ReadStatus) with TickLLMe's wording, so a
// login made here or with `tickcli login` serves both.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/martinhammer/tickcli/tickbuddy"

	"github.com/martinhammer/tickllme/internal/server"
)

func (a *app) userAgent() string { return "TickLLMe/" + version }

func (a *app) clientOptions() []tickbuddy.Option {
	return []tickbuddy.Option{tickbuddy.WithUserAgent(a.userAgent())}
}

func (a *app) login(ctx context.Context, args []string) error {
	fs := a.flags("login")
	paste := fs.Bool("paste", false, "type a user and app password instead of opening a link")
	insecure := fs.Bool("insecure-http", false, "allow plain http to a public address")
	// Flags may come before or after the URL.
	var pos, flagArgs []string
	for _, s := range args {
		if strings.HasPrefix(s, "-") {
			flagArgs = append(flagArgs, s)
		} else {
			pos = append(pos, s)
		}
	}
	if err := parse(fs, flagArgs); err != nil {
		return err
	}
	if len(pos) != 1 {
		return &errUsage{"usage: tickllme login <url> [--paste] [--insecure-http]"}
	}
	url, err := tickbuddy.NormalizeURL(pos[0], *insecure)
	if err != nil {
		return &errUsage{err.Error()}
	}
	var acct tickbuddy.Account
	if *paste {
		acct, err = a.pasteLogin(url)
	} else {
		acct, err = a.linkLogin(ctx, url)
	}
	if err != nil {
		return err
	}

	caps, src, err := a.store.SignIn(ctx, acct, a.clientOptions()...)
	if errors.Is(err, tickbuddy.ErrUnauthorized) {
		return errors.New("the server refused this user and app password; nothing was stored")
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Signed in as %s · Tickbuddy %s\n", acct.User, caps.Version)
	where := "the keychain"
	if src.Kind == tickbuddy.SourceFile {
		where = fmt.Sprintf("%s, encrypted with %s", a.store.Path(), strings.TrimSuffix(strings.TrimPrefix(src.String(), "file ("), ")"))
	}
	fmt.Fprintf(a.stderr, "Credentials stored in %s, shared with Tickcli. Leave the sign-in fields in TickLLMe's settings empty to use them.\n", where)
	if a.settingsFilled() {
		fmt.Fprintln(a.stderr, "Note: TickLLMe's sign-in settings (the TICKBUDDY_* variables) are set here, and they come first.")
	}
	return nil
}

func (a *app) linkLogin(ctx context.Context, url string) (tickbuddy.Account, error) {
	host := a.hostname
	if host == "" {
		host = "this computer"
	}
	flow, err := tickbuddy.StartLogin(ctx, url, "TickLLMe on "+host, nil)
	if err != nil {
		return tickbuddy.Account{}, err
	}
	fmt.Fprintf(a.stderr, "Open this link in a browser, sign in and grant access:\n\n  %s\n\n", flow.LoginURL)
	fmt.Fprintf(a.stderr, "Waiting for you to grant access (Ctrl-C to stop)…\n")
	acct, err := flow.Wait(ctx, 0)
	if errors.Is(err, tickbuddy.ErrLoginExpired) {
		return tickbuddy.Account{}, fmt.Errorf("%w; run `tickllme login` again", err)
	}
	return acct, err
}

func (a *app) pasteLogin(url string) (tickbuddy.Account, error) {
	in := bufio.NewReader(a.stdin)
	readLine := func() (string, error) {
		s, err := in.ReadString('\n')
		if err != nil && s == "" {
			return "", err
		}
		return strings.TrimRight(s, "\r\n"), nil
	}
	var user, password string
	var err error
	if a.stdinTTY {
		fmt.Fprintf(a.stderr, "User: ")
		if user, err = readLine(); err != nil {
			return tickbuddy.Account{}, err
		}
		fmt.Fprintf(a.stderr, "App password: ")
		pw, err := a.readPass()
		fmt.Fprintln(a.stderr)
		if err != nil {
			return tickbuddy.Account{}, err
		}
		password = string(pw)
	} else {
		user, err = readLine()
		if err == nil {
			password, err = readLine()
		}
		if err != nil {
			return tickbuddy.Account{}, &errUsage{"expected the user and the app password as two lines on standard input"}
		}
	}
	user, password = strings.TrimSpace(user), strings.TrimSpace(password)
	if user == "" || password == "" {
		return tickbuddy.Account{}, &errUsage{"both a user and an app password are needed"}
	}
	return tickbuddy.Account{URL: url, User: user, AppPassword: password}, nil
}

func (a *app) logout(ctx context.Context, args []string) error {
	fs := a.flags("logout")
	local := fs.Bool("local", false, "only forget the credentials; don't revoke the app password")
	if err := parse(fs, args); err != nil {
		return err
	}
	inSettings := a.settingsFilled()
	res, err := a.store.Logout(ctx, *local, a.clientOptions()...)
	if errors.Is(err, tickbuddy.ErrNotRevoked) {
		return fmt.Errorf("%w (run `tickllme logout --local` to only forget it, then remove it under Settings → Security → Devices & sessions)", err)
	}
	if err != nil {
		return err
	}
	switch {
	case !res.SignedIn && inSettings:
		fmt.Fprintln(a.stdout, "There was nothing stored to forget.")
	case !res.SignedIn:
		fmt.Fprintln(a.stdout, "Not signed in; there is nothing to forget.")
	case res.Revoked:
		fmt.Fprintln(a.stdout, "Signed out of Tickcli and TickLLMe; the app password was revoked on the server.")
	case res.Pasted:
		fmt.Fprintln(a.stdout, "Signed out of Tickcli and TickLLMe. The app password you pasted still works on the server; remove it under Settings → Security → Devices & sessions if nothing else uses it.")
	default:
		fmt.Fprintln(a.stdout, "Signed out of Tickcli and TickLLMe.")
	}
	if inSettings {
		fmt.Fprintln(a.stdout, "TickLLMe's sign-in settings (the TICKBUDDY_* variables) are still filled in, so TickLLMe keeps using them.")
	}
	return nil
}

// settingsFilled reports whether any of TickLLMe's sign-in settings is
// filled in; an unexpanded ${user_config.…} counts as empty.
func (a *app) settingsFilled() bool {
	for _, k := range []string{tickbuddy.EnvURL, tickbuddy.EnvUser, tickbuddy.EnvAppPassword} {
		if !unset(a.getenv(k)) {
			return true
		}
	}
	return false
}

func (a *app) status(ctx context.Context, args []string) error {
	if err := parse(a.flags("status"), args); err != nil {
		return err
	}
	var acct tickbuddy.Account
	var origin server.Origin
	var version string
	var missing []string
	if a.settingsFilled() {
		// TickLLMe's own settings, which the package doesn't know.
		creds := server.NewCredentials(a.getenv, a.store)
		var err error
		if acct, origin, err = creds.Account(ctx); err != nil {
			return err
		}
		caps, err := tickbuddy.New(creds, a.clientOptions()...).Capabilities(ctx)
		if err != nil {
			return a.statusError(err)
		}
		version, missing = caps.Version, caps.Missing()
	} else {
		// The shared login. The variables are empty or unexpanded here,
		// so the store must not read them.
		store := *a.store
		store.Getenv = func(string) string { return "" }
		st, err := tickbuddy.ReadStatus(ctx, tickbuddy.NewStoredCredentials(&store), a.clientOptions()...)
		if err != nil {
			return a.statusError(err)
		}
		acct, origin, version, missing = st.Account, server.Origin{Stored: st.Source}, st.Version, st.Missing
	}
	fmt.Fprintf(a.stdout, "Signed in as %s at %s · from %s\n", acct.User, acct.URL, origin)
	fmt.Fprintf(a.stdout, "Tickbuddy %s", version)
	if len(missing) > 0 {
		fmt.Fprintf(a.stdout, " · lacks %s", strings.Join(missing, ", "))
	}
	fmt.Fprintln(a.stdout)
	return nil
}

func (a *app) statusError(err error) error {
	switch {
	case err == tickbuddy.ErrNoCredentials:
		return errors.New("not signed in; run `tickllme login <url>` (or `tickcli login <url>`), or fill in the sign-in fields in TickLLMe's settings")
	case errors.Is(err, tickbuddy.ErrUnauthorized):
		return errors.New("the server refused the app password; it may have been revoked")
	}
	return err
}
