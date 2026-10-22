#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Starts the TickLLMe build for this machine with the same arguments. The
# Claude Code plugin keeps the builds in builds/<os>-<arch>/, the Claude
# Desktop extension (on Linux) in server/<os>-<arch>/, both next to this
# launcher's directory.
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)

case $(uname -s) in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*)
		echo "tickllme: there is no build for $(uname -s); TickLLMe runs on Linux, macOS and Windows" >&2
		exit 1
		;;
esac

case $(uname -m) in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*)
		echo "tickllme: there is no build for $(uname -m) processors; TickLLMe runs on x86-64 and arm64" >&2
		exit 1
		;;
esac

# One universal binary serves both kinds of Mac.
if [ "$os" = darwin ]; then
	arch=universal
fi

for bin in "$root/builds/$os-$arch/tickllme" "$root/server/$os-$arch/tickllme"; do
	if [ -f "$bin" ]; then
		# An unpacked archive may have lost the exec bit.
		[ -x "$bin" ] || chmod +x "$bin" 2>/dev/null || true
		exec "$bin" "$@"
	fi
done

echo "tickllme: the $os-$arch build is missing from $root; reinstall TickLLMe" >&2
exit 1
