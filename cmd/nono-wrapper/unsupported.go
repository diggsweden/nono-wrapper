// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build !linux && !darwin

package main

// nono sandboxes agents only on Linux and macOS, so nono-wrapper builds only
// for those targets. This declaration makes other targets fail with this
// message instead of an undefined platform helper.
var _ int = "nono-wrapper supports only Linux and macOS"
