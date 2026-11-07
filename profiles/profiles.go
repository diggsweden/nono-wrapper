// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

// Package profiles supplies the bundled nono profiles and the official pack
// pins to the executable, so a release binary can install them itself.
package profiles

import "embed"

// Files holds every bundled *.jsonc profile and the packs manifest.
//
//go:embed *.jsonc packs
var Files embed.FS
