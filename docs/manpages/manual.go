// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package manpages supplies the manual to the standalone executable.
package manpages

import _ "embed"

// Manual is also rendered by install-local for man(1).
//
//go:embed nono-wrapper.1.md
var Manual string
