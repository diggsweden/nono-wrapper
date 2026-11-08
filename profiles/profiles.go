// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package profiles supplies the bundled nono profiles and the official pack
// pins to the executable, so a release binary can install them itself.
package profiles

import "embed"

// Files holds every bundled *.jsonc profile and the packs manifest.
//
//go:embed *.jsonc packs
var Files embed.FS
