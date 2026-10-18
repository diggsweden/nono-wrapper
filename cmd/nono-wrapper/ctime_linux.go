// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"io/fs"
	"syscall"
	"time"
)

// Match find -cnewer: metadata changes count even if mtime was restored.
func changeTime(info fs.FileInfo) time.Time {
	stat := info.Sys().(*syscall.Stat_t)
	return time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec)
}
