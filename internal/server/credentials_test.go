// SPDX-License-Identifier: GPL-3.0-or-later

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/martinhammer/tickcli/tickbuddy"
	"github.com/martinhammer/tickcli/tickbuddy/tickbuddytest"
)

// fakeKeyring is an in-memory keychain.
type fakeKeyring map[string]string

func (k fakeKeyring) Get(service, user string) (string, error) {
	if v, ok := k[service+"/"+user]; ok {
		return v, nil
	}
	return "", tickbuddy.ErrKeyringNotFound
}

func (k fakeKeyring) Set(service, user, secret string) error {
	k[service+"/"+user] = secret
	return nil
}

func (k fakeKeyring) Delete(service, user string) error {
	delete(k, service+"/"+user)
	return nil
}

// store is a shared store holding acct in its keychain (none when acct is
// zero).
func store(t *testing.T, acct tickbuddy.Account) *tickbuddy.Store {
	k := fakeKeyring{}
	if acct.URL != "" {
		b, _ := json.Marshal(acct)
		k[tickbuddy.KeyringService+"/"+tickbuddy.KeyringUser] = string(b)
	}
	return &tickbuddy.Store{Getenv: func(string) string { return "" }, Keyring: k, Dir: t.TempDir(), GOOS: "linux"}
}

func vars(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestCredentialsFromSettings(t *testing.T) {
	stored := tickbuddy.Account{URL: "https://stored.example.com", User: "old", AppPassword: "old-pass"}
	c := NewCredentials(vars(map[string]string{
		tickbuddy.EnvURL:         "cloud.example.com/",
		tickbuddy.EnvUser:        "martin",
		tickbuddy.EnvAppPassword: "pw",
	}), store(t, stored))
	a, o, err := c.Account(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if a.URL != "https://cloud.example.com" || a.User != "martin" || a.AppPassword != "pw" || !o.Settings {
		t.Errorf("account = %+v, %+v", a, o)
	}
	if o.String() != "TickLLMe's settings" {
		t.Errorf("origin = %s", o)
	}
}

func TestCredentialsFallBackToTickcli(t *testing.T) {
	stored := tickbuddy.Account{URL: "https://stored.example.com", User: "martin", AppPassword: "pw"}
	for _, empty := range []map[string]string{
		{},
		{tickbuddy.EnvURL: "", tickbuddy.EnvUser: " ", tickbuddy.EnvAppPassword: ""},
		// What a host that doesn't substitute empty fields would pass.
		{tickbuddy.EnvURL: "${user_config.url}", tickbuddy.EnvUser: "${user_config.user}", tickbuddy.EnvAppPassword: "${user_config.app_password}"},
	} {
		c := NewCredentials(vars(empty), store(t, stored))
		a, o, err := c.Account(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if a != stored || o.Settings || o.String() != "your Tickcli sign-in (keychain)" {
			t.Errorf("%v: account = %+v, %s", empty, a, o)
		}
	}
}

func TestCredentialsPartial(t *testing.T) {
	for _, c := range []struct {
		set  map[string]string
		want string
	}{
		{map[string]string{tickbuddy.EnvURL: "https://x.example.com", tickbuddy.EnvUser: "m"},
			"TickLLMe's settings have a Nextcloud address and a user name but no app password. Fill in all three sign-in fields, or leave all three empty to use your Tickcli sign-in."},
		{map[string]string{tickbuddy.EnvURL: "https://x.example.com"},
			"TickLLMe's settings have a Nextcloud address but no user name or app password."},
		{map[string]string{tickbuddy.EnvAppPassword: "pw"},
			"have an app password but no Nextcloud address or user name"},
	} {
		_, _, err := NewCredentials(vars(c.set), store(t, tickbuddy.Account{})).Account(context.Background())
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: err = %v, want %q", c.set, err, c.want)
		}
		if !errors.Is(err, tickbuddy.ErrNoCredentials) {
			t.Errorf("%v: not ErrNoCredentials", c.set)
		}
	}
}

func TestCredentialsRetryAfterLogin(t *testing.T) {
	st := store(t, tickbuddy.Account{})
	c := NewCredentials(vars(nil), st)
	if _, _, err := c.Account(context.Background()); err != tickbuddy.ErrNoCredentials {
		t.Fatalf("err = %v", err)
	}
	acct := tickbuddy.Account{URL: "https://x.example.com", User: "m", AppPassword: "p"}
	if _, err := st.Save(context.Background(), acct); err != nil {
		t.Fatal(err)
	}
	if a, _, err := c.Account(context.Background()); err != nil || a != acct {
		t.Errorf("after login: %+v, %v", a, err)
	}
}

// errorEnv runs a level-2 server against fake with creds.
func errorEnv(t *testing.T, fake *tickbuddytest.Server, creds *Credentials) *env {
	client := tickbuddy.New(creds, tickbuddy.WithRetries(0), tickbuddy.WithTimeout(2*time.Second))
	m, err := New(Options{Level: 2, Client: client, Credentials: creds, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return connect(t, m, fake)
}

func TestErrorSentences(t *testing.T) {
	fake := tickbuddytest.New()
	t.Cleanup(fake.Close)
	journal(fake)
	settings := map[string]string{tickbuddy.EnvURL: fake.URL, tickbuddy.EnvUser: "martin", tickbuddy.EnvAppPassword: "wrong"}

	e := errorEnv(t, fake, NewCredentials(vars(nil), store(t, tickbuddy.Account{})))
	e.fails("list_tracks", nil, msgNotSignedIn)

	e = errorEnv(t, fake, NewCredentials(vars(map[string]string{tickbuddy.EnvURL: fake.URL}), store(t, tickbuddy.Account{})))
	e.fails("get_day", nil, "TickLLMe's settings have a Nextcloud address but no user name or app password")

	e = errorEnv(t, fake, NewCredentials(vars(settings), store(t, tickbuddy.Account{})))
	e.fails("list_tracks", nil, msgRefused)

	e = errorEnv(t, fake, NewCredentials(vars(nil), store(t, tickbuddy.Account{URL: fake.URL, User: "martin", AppPassword: "wrong"})))
	e.fails("list_tracks", nil, "sign in again with `tickcli login`")

	settings[tickbuddy.EnvAppPassword] = "app-pass"
	e = errorEnv(t, fake, NewCredentials(vars(settings), store(t, tickbuddy.Account{})))
	fake.Mu.Lock()
	fake.FailNext = 1
	fake.Mu.Unlock()
	e.fails("list_tracks", nil, "Can't reach "+fake.URL+": the server answered 502 Bad Gateway")

	fake.Mu.Lock()
	fake.NoTickbuddy = true
	fake.Mu.Unlock()
	e.fails("get_day", nil, "Tickbuddy isn't installed or enabled on this Nextcloud")
	fake.Mu.Lock()
	fake.NoTickbuddy = false
	fake.Mu.Unlock()

	down := tickbuddytest.New()
	downURL := down.URL
	down.Close()
	settings[tickbuddy.EnvURL] = downURL
	e = errorEnv(t, fake, NewCredentials(vars(settings), store(t, tickbuddy.Account{})))
	e.fails("list_tracks", nil, "Can't reach "+downURL+":")

	settings[tickbuddy.EnvURL] = "http://cloud.example.com"
	e = errorEnv(t, fake, NewCredentials(vars(settings), store(t, tickbuddy.Account{})))
	e.fails("list_tracks", nil, "uses plain http, where anyone on the way can read the app password. Use an https address, or if you must use http, sign in once in a terminal with `tickllme login http://cloud.example.com --insecure-http`")

	settings[tickbuddy.EnvURL] = "ftp://cloud.example.com"
	e = errorEnv(t, fake, NewCredentials(vars(settings), store(t, tickbuddy.Account{})))
	e.fails("list_tracks", nil, "The Nextcloud address in TickLLMe's settings won't do")
}

// TestDisabledWhileRunning disables Tickbuddy after the capabilities were
// read, as after an upgrade: Nextcloud's own 404 reads as not installed, not
// as a missing track.
func TestDisabledWhileRunning(t *testing.T) {
	e := start(t, 2, false)
	e.ok("list_tracks", nil)
	e.fake.Mu.Lock()
	e.fake.Disabled = true
	e.fake.Mu.Unlock()
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"set_tick", map[string]any{"track": "Exercise", "value": true}},
		{"get_stats", map[string]any{"track": "Exercise"}},
	} {
		_, msg := e.call(c.tool, c.args)
		if !strings.Contains(msg, "Tickbuddy isn't installed or enabled on this Nextcloud") || strings.Contains(strings.ToLower(msg), "no track named") {
			t.Errorf("%s with Tickbuddy disabled: error = %q, want the not-installed sentence", c.tool, msg)
		}
	}
}

func TestSentenceCase(t *testing.T) {
	for in, want := range map[string]string{
		"":                 "Something went wrong",
		"no track":         "No track.",
		"done.":            "Done.",
		"élan":             "Élan.",
		"Already a stop?":  "Already a stop?",
		" padded message ": "Padded message.",
	} {
		if got := sentenceCase(in); got != want {
			t.Errorf("sentenceCase(%q) = %q, want %q", in, got, want)
		}
	}
}

// fakeSystemdCreds "encrypts" by prefixing, so a file it wrote reads back.
type fakeSystemdCreds struct{}

func (fakeSystemdCreds) Encrypt(_ context.Context, plain []byte) ([]byte, error) {
	return append([]byte("sealed:"), plain...), nil
}

func (fakeSystemdCreds) Decrypt(_ context.Context, sealed []byte) ([]byte, error) {
	plain, ok := bytes.CutPrefix(sealed, []byte("sealed:"))
	if !ok {
		return nil, errors.New("not sealed here")
	}
	return plain, nil
}

func (fakeSystemdCreds) HasTPM(context.Context) bool { return true }

// A login stored through the package, as `tickcli login` stores it, is what
// TickLLMe falls back to with its settings empty, in each kind of store.
func TestFallbackFindsSharedLogin(t *testing.T) {
	fake := tickbuddytest.New()
	t.Cleanup(fake.Close)
	machineID := filepath.Join(t.TempDir(), "machine-id")
	if err := os.WriteFile(machineID, []byte("0123456789abcdef0123456789abcdef\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		store func() *tickbuddy.Store
		want  string
	}{
		{"keychain", func() *tickbuddy.Store { return store(t, tickbuddy.Account{}) }, "your Tickcli sign-in (keychain)"},
		{"file, machine id", func() *tickbuddy.Store {
			return &tickbuddy.Store{Getenv: func(string) string { return "" }, Dir: t.TempDir(), MachineIDPath: machineID, GOOS: "linux"}
		}, "your Tickcli sign-in (encrypted file)"},
		{"file, systemd-creds", func() *tickbuddy.Store {
			return &tickbuddy.Store{Getenv: func(string) string { return "" }, Dir: t.TempDir(), SystemdCreds: fakeSystemdCreds{}, GOOS: "linux"}
		}, "your Tickcli sign-in (encrypted file)"},
	} {
		st := c.store()
		acct := tickbuddy.Account{URL: fake.URL, User: "martin", AppPassword: "app-pass"}
		if _, src, err := st.SignIn(context.Background(), acct); err != nil {
			t.Fatalf("%s: sign in: %v", c.name, err)
		} else {
			t.Logf("%s: stored in %s", c.name, src)
		}
		for _, settings := range []map[string]string{
			nil,
			{tickbuddy.EnvURL: "${user_config.url}", tickbuddy.EnvUser: "${user_config.user}", tickbuddy.EnvAppPassword: "${user_config.app_password}"},
		} {
			// A fresh process: the store as DefaultStore would see it.
			creds := NewCredentials(vars(settings), st)
			got, o, err := creds.Account(context.Background())
			if err != nil || got != acct || o.String() != c.want {
				t.Errorf("%s, settings %v: %+v from %s, %v", c.name, settings, got, o, err)
			}
			e := errorEnv(t, fake, creds)
			e.ok("list_tracks", nil)
		}
	}
}
