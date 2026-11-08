// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"
)

// Native socket subtree capabilities must exist before sandbox creation.
// Read their roots from resolved policy, not a second Go permission table.
func preparePrivateSocketDirectories(shown []byte, protected []pathRule) error {
	var profile struct {
		Filesystem struct {
			Directories []string `json:"unix_socket_subtree_bind"`
		} `json:"filesystem"`
	}
	if err := json.Unmarshal(shown, &profile); err != nil {
		return err
	}
	for _, raw := range profile.Filesystem.Directories {
		path, err := expandPolicyPath(raw)
		if err != nil {
			return err
		}
		actual, err := literalRuntimePath(path)
		if err != nil {
			return err
		}
		if err := checkProtectedWrites([]pathRule{{actual, pathRead | pathWrite, false}}, protected); err != nil {
			return err
		}
		// Reject an existing symlink before MkdirAll can follow its target.
		if info, err := os.Lstat(path); err == nil && !info.IsDir() {
			return invalidConfig{fmt.Errorf("private socket directory must not be a file or symlink: %q", path)}
		} else if err != nil && !os.IsNotExist(err) {
			return quotePathError(err)
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			return quotePathError(err)
		}
		directory, err := os.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return quotePathError(err)
		}
		info, err := directory.Stat()
		if err != nil {
			directory.Close()
			return quotePathError(err)
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !info.IsDir() || !ok || owner.Uid != uint32(os.Getuid()) {
			directory.Close()
			return invalidConfig{fmt.Errorf("private socket directory must be user-owned: %q", path)}
		}
		// Traversal alone can expose sockets to other users. Tighten only the
		// declared, owned root, never recursively or via a link, and preserve
		// owner permissions and special mode bits.
		if info.Mode().Perm()&0o077 != 0 {
			err = directory.Chmod(info.Mode() &^ 0o077)
		}
		directory.Close()
		if err != nil {
			return quotePathError(err)
		}
	}
	return nil
}
