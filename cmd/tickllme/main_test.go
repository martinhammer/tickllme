// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/martinhammer/tickcli/tickbuddy"
	"github.com/martinhammer/tickcli/tickbuddy/tickbuddytest"
)

func testApp(t *testing.T, env map[string]string) (*app, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	getenv := func(k string) string { return env[k] }
	dir := t.TempDir()
	machineID := filepath.Join(dir, "machine-id")
	if err := os.WriteFile(machineID, []byte("0123456789abcdef0123456789abcdef\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &app{
		stdin:  strings.NewReader(""),
		stdout: &out,
		stderr: &errOut,
		getenv: getenv,
		store:  &tickbuddy.Store{Getenv: getenv, Dir: filepath.Join(dir, "tickcli"), MachineIDPath: machineID, GOOS: "linux"},
	}, &out, &errOut
}

func TestServeSettings(t *testing.T) {
	for _, c := range []struct {
		args    []string
		env     map[string]string
		level   int
		private bool
		err     string
	}{
		{nil, nil, 1, false, ""},
		{[]string{"--level", "2"}, nil, 2, false, ""},
		{[]string{"--level", "3", "--private"}, nil, 3, true, ""},
		{[]string{"--private=false"}, nil, 1, false, ""},
		{[]string{"--level", "2", "--private=true"}, nil, 2, true, ""},
		{nil, map[string]string{envLevel: "2", envPrivate: "true"}, 2, true, ""},
		{[]string{"--level", "1", "--private=false"}, map[string]string{envLevel: "3", envPrivate: "true"}, 1, false, ""},
		// Unsubstituted ${user_config.…} fall back to the variables, then the defaults.
		{[]string{"--level", "${user_config.level}", "--private=${user_config.show_private}"}, nil, 1, false, ""},
		{[]string{"--level", "${user_config.level}"}, map[string]string{envLevel: "2"}, 2, false, ""},
		// MCPB and Claude Code may pass numbers as "2" or "2.0".
		{[]string{"--level", "2.0"}, nil, 2, false, ""},
		{[]string{"--level", "4"}, nil, 0, false, "must be 1, 2 or 3"},
		{[]string{"--level", "two"}, nil, 0, false, "must be 1, 2 or 3"},
		{[]string{"--private=maybe"}, nil, 0, false, "true or false"},
		{[]string{"--colour"}, nil, 0, false, "not defined"},
		{[]string{"extra"}, nil, 0, false, "no arguments"},
	} {
		a, _, _ := testApp(t, c.env)
		level, private, err := a.serveSettings(c.args)
		switch {
		case c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)):
			t.Errorf("%v %v: err = %v, want %q", c.args, c.env, err, c.err)
		case c.err == "" && err != nil:
			t.Errorf("%v %v: %v", c.args, c.env, err)
		case c.err == "" && (level != c.level || private != c.private):
			t.Errorf("%v %v = %d, %v; want %d, %v", c.args, c.env, level, private, c.level, c.private)
		}
	}
}

func TestVersionAndUsage(t *testing.T) {
	a, out, _ := testApp(t, nil)
	if code := a.run(context.Background(), []string{"--version"}); code != 0 || out.String() != "tickllme dev\n" {
		t.Errorf("--version: %d %q", code, out)
	}
	a, _, errOut := testApp(t, nil)
	if code := a.run(context.Background(), []string{"frobnicate"}); code != 2 || !strings.Contains(errOut.String(), "unknown command") {
		t.Errorf("unknown: %d %q", code, errOut)
	}
}

func TestStatus(t *testing.T) {
	fake := tickbuddytest.New()
	defer fake.Close()
	a, out, _ := testApp(t, map[string]string{tickbuddy.EnvURL: fake.URL, tickbuddy.EnvUser: "martin", tickbuddy.EnvAppPassword: "app-pass"})
	if code := a.run(context.Background(), []string{"status"}); code != 0 {
		t.Fatalf("status: %d", code)
	}
	want := "Signed in as martin at " + fake.URL + " · from TickLLMe's settings\nTickbuddy 2.3.0\n"
	if out.String() != want {
		t.Errorf("status = %q, want %q", out, want)
	}

	a, _, errOut := testApp(t, nil)
	if code := a.run(context.Background(), []string{"status"}); code != 1 || !strings.Contains(errOut.String(), "not signed in") {
		t.Errorf("status without sign-in: %d %q", code, errOut)
	}
}

func TestPasteLoginAndLogout(t *testing.T) {
	fake := tickbuddytest.New()
	defer fake.Close()
	a, out, _ := testApp(t, nil)
	a.stdin = strings.NewReader("martin\napp-pass\n")
	if code := a.run(context.Background(), []string{"login", fake.URL, "--paste"}); code != 0 {
		t.Fatalf("login: %d", code)
	}
	if !strings.HasPrefix(out.String(), "Signed in as martin · Tickbuddy 2.3.0") {
		t.Errorf("login said %q", out)
	}
	acct, src, err := a.store.LoadStored(context.Background())
	if err != nil || acct.User != "martin" || src.Kind != tickbuddy.SourceFile {
		t.Fatalf("stored %+v %+v %v", acct, src, err)
	}
	out.Reset()
	if code := a.run(context.Background(), []string{"status"}); code != 0 || !strings.Contains(out.String(), "· from your Tickcli sign-in (encrypted file)") {
		t.Errorf("status after login: %q", out)
	}
	out.Reset()
	if code := a.run(context.Background(), []string{"logout"}); code != 0 || !strings.Contains(out.String(), "Signed out") {
		t.Errorf("logout: %q", out)
	}
	if _, _, err := a.store.LoadStored(context.Background()); err != tickbuddy.ErrNoCredentials {
		t.Errorf("still stored: %v", err)
	}
}

func TestLinkLoginLogoutRevokes(t *testing.T) {
	fake := tickbuddytest.New()
	defer fake.Close()
	a, out, errOut := testApp(t, nil)
	a.hostname = "testbox"
	if code := a.run(context.Background(), []string{"login", fake.URL}); code != 0 {
		t.Fatalf("login: %d %s", code, errOut)
	}
	if !strings.Contains(errOut.String(), fake.URL+"/index.php/login/v2/flow/abc") {
		t.Errorf("no link shown: %q", errOut)
	}
	acct, _, err := a.store.LoadStored(context.Background())
	if err != nil || !acct.CreatedByLink || acct.AppPassword != "flow-pass" {
		t.Fatalf("stored %+v, %v", acct, err)
	}

	// Unexpanded settings count as empty: status reads the shared login.
	a.getenv = func(k string) string {
		return map[string]string{tickbuddy.EnvURL: "${user_config.url}", tickbuddy.EnvUser: "${user_config.user}", tickbuddy.EnvAppPassword: "${user_config.app_password}"}[k]
	}
	a.store.Getenv = a.getenv
	out.Reset()
	if code := a.run(context.Background(), []string{"status"}); code != 0 || out.String() != "Signed in as martin at "+fake.URL+" · from your Tickcli sign-in (encrypted file)\nTickbuddy 2.3.0\n" {
		t.Errorf("status: %q %s", out, errOut)
	}

	out.Reset()
	if code := a.run(context.Background(), []string{"logout"}); code != 0 || !strings.Contains(out.String(), "the app password was revoked on the server") || strings.Contains(out.String(), "still filled in") {
		t.Errorf("logout: %q", out)
	}
	if !fake.Revoked {
		t.Error("app password not revoked")
	}
}

func TestLogoutWithSettings(t *testing.T) {
	a, out, _ := testApp(t, map[string]string{tickbuddy.EnvURL: "https://x.example.com", tickbuddy.EnvUser: "m", tickbuddy.EnvAppPassword: "p"})
	if code := a.run(context.Background(), []string{"logout"}); code != 0 {
		t.Fatal(code)
	}
	want := "There was nothing stored to forget.\nTickLLMe's sign-in settings (the TICKBUDDY_* variables) are still filled in, so TickLLMe keeps using them.\n"
	if out.String() != want {
		t.Errorf("logout = %q", out)
	}
}

func TestStatusMissingFeatures(t *testing.T) {
	fake := tickbuddytest.New()
	defer fake.Close()
	fake.Old()
	a, out, _ := testApp(t, map[string]string{tickbuddy.EnvURL: fake.URL, tickbuddy.EnvUser: "martin", tickbuddy.EnvAppPassword: "app-pass"})
	if code := a.run(context.Background(), []string{"status"}); code != 0 || !strings.HasSuffix(out.String(), "Tickbuddy 2.2.0 · lacks booleanSet, counterIncrement, stats, statsPeriod, notes, notesAppend\n") {
		t.Errorf("status = %q", out)
	}
}
