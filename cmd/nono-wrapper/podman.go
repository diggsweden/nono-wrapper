// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"os"
	"runtime"
	"syscall"
)

func podmanSessionEnvironment(profile string, defaults runtimeDefaults) []string {
	if defaults.Podman.Platform != runtime.GOOS || os.Getuid() == 0 || !bundledProfile(profile) {
		return nil
	}
	for _, key := range defaults.Podman.OverrideVariables {
		if _, set := os.LookupEnv(key); set {
			return nil // Explicit transport choices, including empty values, win.
		}
	}
	path, err := expandPolicyPath(defaults.Podman.Socket)
	if err != nil {
		return nil
	}
	return podmanEnvironmentForSocket(path, defaults.Podman.TransportVariable)
}

func podmanEnvironmentForSocket(path, variable string) []string {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return nil
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Getuid()) {
		return nil
	}
	return []string{variable + "=unix://" + path}
}
