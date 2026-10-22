// SPDX-License-Identifier: GPL-3.0-or-later

package server

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/martinhammer/tickcli/tickbuddy"
)

// Credentials are TickLLMe's sign-in (spec §5): the three settings fields,
// which reach the server as the TICKBUDDY_* variables, or, with all three
// empty, what Tickcli's login stored in the keychain or encrypted file.
//
// A lookup that succeeds is kept for the rest of the run. One that fails is
// tried again on the next call, so signing in with `tickcli login` while
// Claude runs works without a restart.
type Credentials struct {
	getenv func(string) string
	store  *tickbuddy.Store

	mu     sync.Mutex
	acct   tickbuddy.Account
	origin Origin
	ok     bool
}

// Origin says where the credentials came from.
type Origin struct {
	// Settings is set when they came from TickLLMe's settings.
	Settings bool
	// Stored is where Tickcli's login keeps them, otherwise.
	Stored tickbuddy.Source
}

// String completes "Signed in as … at … · from …".
func (o Origin) String() string {
	switch {
	case o.Settings:
		return "TickLLMe's settings"
	case o.Stored.Kind == tickbuddy.SourceKeychain:
		return "your Tickcli sign-in (keychain)"
	default:
		return "your Tickcli sign-in (encrypted file)"
	}
}

// NewCredentials reads the settings with getenv and falls back to store.
func NewCredentials(getenv func(string) string, store *tickbuddy.Store) *Credentials {
	return &Credentials{getenv: getenv, store: store}
}

// Get implements tickbuddy.Credentials.
func (c *Credentials) Get(ctx context.Context) (string, string, string, error) {
	a, _, err := c.Account(ctx)
	return a.URL, a.User, a.AppPassword, err
}

// Account returns the credentials and where they came from.
func (c *Credentials) Account(ctx context.Context) (tickbuddy.Account, Origin, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ok {
		return c.acct, c.origin, nil
	}
	a, o, err := c.load(ctx)
	if err != nil {
		return tickbuddy.Account{}, Origin{}, err
	}
	c.acct, c.origin, c.ok = a, o, true
	return a, o, nil
}

// settingsFields are the three sign-in fields, in the order the settings
// show them.
var settingsFields = []struct{ env, has, missing string }{
	{tickbuddy.EnvURL, "a Nextcloud address", "Nextcloud address"},
	{tickbuddy.EnvUser, "a user name", "user name"},
	{tickbuddy.EnvAppPassword, "an app password", "app password"},
}

func (c *Credentials) load(ctx context.Context) (tickbuddy.Account, Origin, error) {
	vals := make([]string, len(settingsFields))
	var has, missing []string
	for i, f := range settingsFields {
		vals[i] = setting(c.getenv(f.env))
		if vals[i] == "" {
			missing = append(missing, f.missing)
		} else {
			has = append(has, f.has)
		}
	}
	switch len(has) {
	case 0:
		a, src, err := c.store.LoadStored(ctx)
		if err != nil {
			return tickbuddy.Account{}, Origin{}, err
		}
		return a, Origin{Stored: src}, nil
	case len(settingsFields):
	default:
		return tickbuddy.Account{}, Origin{}, &PartialSettingsError{Has: has, Missing: missing}
	}
	u, err := tickbuddy.NormalizeURL(vals[0], false)
	if err != nil {
		if _, insecureErr := tickbuddy.NormalizeURL(vals[0], true); insecureErr == nil {
			// The settings have no --insecure-http; signing in in a
			// terminal does.
			return tickbuddy.Account{}, Origin{}, fmt.Errorf("the Nextcloud address in TickLLMe's settings uses plain http, where anyone on the way can read the app password. Use an https address, or if you must use http, sign in once in a terminal with `tickllme login %s --insecure-http` and leave the three sign-in fields empty", vals[0])
		}
		return tickbuddy.Account{}, Origin{}, fmt.Errorf("the Nextcloud address in TickLLMe's settings won't do: %w", err)
	}
	return tickbuddy.Account{URL: u, User: vals[1], AppPassword: vals[2]}, Origin{Settings: true}, nil
}

// setting is a settings value, "" when the field is empty. A value still
// reading "${user_config.…}" is a field the host didn't fill in, which
// isn't documented not to happen (spec §9 item 3).
func setting(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
		return ""
	}
	return v
}

// PartialSettingsError is some but not all of the three sign-in fields
// filled in. It is a tickbuddy.ErrNoCredentials.
type PartialSettingsError struct {
	Has, Missing []string
}

func (e *PartialSettingsError) Error() string {
	return fmt.Sprintf("TickLLMe's settings have %s but no %s. Fill in all three sign-in fields, or leave all three empty to use your Tickcli sign-in.",
		strings.Join(e.Has, " and "), strings.Join(e.Missing, " or "))
}

func (e *PartialSettingsError) Unwrap() error { return tickbuddy.ErrNoCredentials }

// Messages of spec §5.1.
const (
	msgNotSignedIn = "TickLLMe isn't signed in to Nextcloud. Fill in the Nextcloud address, user name and app password in TickLLMe's settings, or sign in once in a terminal with `tickcli login` (or `tickllme login`) and leave those fields empty."
	msgRefused     = "Nextcloud refused the app password. It may have been revoked; create a new one under Settings → Security → Devices & sessions and enter it in TickLLMe's settings."
	msgRefusedTick = "Nextcloud refused the app password. It may have been revoked; create a new one under Settings → Security → Devices & sessions and sign in again with `tickcli login`."
)

// isNoCredentials reports whether err is "no credentials anywhere", as
// opposed to partial settings or a damaged store, which say more.
func isNoCredentials(err error) bool { return err == tickbuddy.ErrNoCredentials }
