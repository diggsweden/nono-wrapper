// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/crypto/ssh"
)

// Grant existing public files, not a glob that could later match private data.
// The location is declarative; content validation prevents a renamed private
// key from receiving an automatic exception to the upstream SSH-directory deny.
func configuredPublicKeyPaths(defaults runtimeDefaults) ([]string, error) {
	var paths []string
	for _, rule := range defaults.PublicKeyFiles {
		pattern, err := expandPolicyPath(rule.Path)
		if err != nil {
			return nil, err
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		for _, path := range matches {
			actual, err := literalRuntimePath(path)
			if err != nil || actual != filepath.Clean(path) {
				continue
			}
			if isPublicKeyFile(path) {
				paths = append(paths, path)
			}
		}
	}
	return paths, nil
}

func isPublicKeyFile(path string) bool {
	info, err := os.Lstat(path)
	const maxPublicKeyBytes = 1 << 20
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxPublicKeyBytes {
		return false
	}
	// Never follow a final symlink or block on a replaced FIFO. Verify the
	// descriptor still names the regular file inspected above before reading.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return false
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPublicKeyBytes+1))
	if err != nil || len(data) > maxPublicKeyBytes {
		return false
	}
	line := bytes.TrimSpace(data)
	if bytes.ContainsAny(line, "\r\n") {
		return false
	}
	_, _, options, rest, err := ssh.ParseAuthorizedKey(line)
	return err == nil && len(options) == 0 && len(bytes.TrimSpace(rest)) == 0
}
