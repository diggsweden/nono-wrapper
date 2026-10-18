// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

//go:build !linux && !darwin

package main

// nono sandboxes agents only on Linux and macOS, so nono-wrapper builds only
// for those targets. This declaration makes other targets fail with this
// message instead of an undefined platform helper.
var _ int = "nono-wrapper supports only Linux and macOS"
