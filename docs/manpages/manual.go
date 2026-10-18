// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

// Package manpages supplies the manual to the standalone executable.
package manpages

import _ "embed"

// Manual is also rendered by install-local for man(1).
//
//go:embed nono-wrapper.1.md
var Manual string
