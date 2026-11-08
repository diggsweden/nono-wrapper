// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

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
