// SPDX-License-Identifier: GPL-3.0-or-later

// Package launchertest tests launcher/tickllme.sh against stub builds and a
// stub uname.
package launchertest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const launcher = "../../launcher/tickllme.sh"

// install lays out a plugin (builds/) or extension (server/) with stub
// builds that print their name and arguments, and a stub uname answering
// sys and machine. It returns the launcher's path and PATH.
func install(t *testing.T, dir, sys, machine string) (string, string) {
	t.Helper()
	root := t.TempDir()
	src, err := os.ReadFile(launcher)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "launcher", "tickllme.sh"), string(src), 0o755)
	for _, b := range []string{"linux-amd64", "linux-arm64", "darwin-universal"} {
		write(t, filepath.Join(root, dir, b, "tickllme"), "#!/bin/sh\necho "+b+" \"$@\"\n", 0o755)
	}
	bin := t.TempDir()
	write(t, filepath.Join(bin, "uname"), "#!/bin/sh\ncase $1 in -s) echo "+sys+";; -m) echo "+machine+";; esac\n", 0o755)
	return filepath.Join(root, "launcher", "tickllme.sh"), bin + string(os.PathListSeparator) + os.Getenv("PATH")
}

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func run(path, pathEnv string, args ...string) (string, error) {
	cmd := exec.Command(path, args...)
	cmd.Env = append(os.Environ(), "PATH="+pathEnv)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func TestLauncherPicksBuild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the launcher is a POSIX shell script")
	}
	for _, dir := range []string{"builds", "server"} {
		for _, c := range []struct{ sys, machine, want string }{
			{"Linux", "x86_64", "linux-amd64"},
			{"Linux", "aarch64", "linux-arm64"},
			{"Linux", "arm64", "linux-arm64"},
			{"Darwin", "x86_64", "darwin-universal"},
			{"Darwin", "arm64", "darwin-universal"},
		} {
			path, pathEnv := install(t, dir, c.sys, c.machine)
			out, err := run(path, pathEnv, "serve", "--level", "2", "--private=false")
			if err != nil || out != c.want+" serve --level 2 --private=false" {
				t.Errorf("%s %s %s: %q, %v", dir, c.sys, c.machine, out, err)
			}
		}
	}
}

func TestLauncherRefuses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the launcher is a POSIX shell script")
	}
	for _, c := range []struct{ sys, machine, want string }{
		{"FreeBSD", "x86_64", "no build for FreeBSD"},
		{"Linux", "armv7l", "no build for armv7l"},
	} {
		path, pathEnv := install(t, "builds", c.sys, c.machine)
		out, err := run(path, pathEnv)
		if err == nil || !strings.Contains(out, c.want) {
			t.Errorf("%s %s: %q, %v", c.sys, c.machine, out, err)
		}
	}
	// A missing build, and one that lost its exec bit.
	path, pathEnv := install(t, "builds", "Linux", "x86_64")
	root := filepath.Dir(filepath.Dir(path))
	bin := filepath.Join(root, "builds", "linux-amd64", "tickllme")
	if err := os.Chmod(bin, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := run(path, pathEnv); err != nil || out != "linux-amd64" {
		t.Errorf("without exec bit: %q, %v", out, err)
	}
	if err := os.Remove(bin); err != nil {
		t.Fatal(err)
	}
	if out, err := run(path, pathEnv); err == nil || !strings.Contains(out, "linux-amd64 build is missing") {
		t.Errorf("missing build: %q, %v", out, err)
	}
}

func TestLauncherIsExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no exec bits on Windows")
	}
	fi, err := os.Stat(launcher)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&0o111 == 0 {
		t.Errorf("%s isn't executable (mode %v)", launcher, fi.Mode())
	}
}
