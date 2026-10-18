// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"
)

const socketMediationNotice = "WSL2 cannot run the seccomp listener that Unix-socket mediation needs (microsoft/WSL#9548), so the sandbox starts without it: the agent can connect to any Unix socket your user can, such as Docker's. Use native Linux or a virtual machine for the full policy."

// nono refuses linux.af_unix_mediation = "pathname" on WSL2 instead of
// starting without it, so the wrapper drops the setting from its own profiles
// there. Profiles from elsewhere keep nono's refusal.
func socketMediationUnavailable(ctx context.Context, nono, profile string, shown []byte) bool {
	return bundledProfile(profile) && mediatesSockets(shown) && wsl2Host(ctx, nono)
}

func mediatesSockets(shown []byte) bool {
	var resolved struct {
		Linux struct {
			Mode string `json:"af_unix_mediation"`
		} `json:"linux"`
	}
	return json.Unmarshal(shown, &resolved) == nil && resolved.Linux.Mode == "pathname"
}

// nono's own detection decides, so mediation is never dropped on a host where
// nono would enforce it. The local hints only spare other hosts the extra call;
// a failed or unrecognised check keeps mediation and nono's own error.
func wsl2Host(ctx context.Context, nono string) bool {
	if !wsl2Hint() {
		return false
	}
	check := exec.CommandContext(ctx, nono, "setup", "--check-only")
	check.WaitDelay = time.Second
	report, err := check.Output()
	return err == nil && strings.Contains(string(report), "WSL2 environment detected")
}

func wsl2Hint() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	if _, err := os.Stat("/proc/sys/fs/binfmt_misc/WSLInterop"); err == nil {
		return true
	}
	version, _ := os.ReadFile("/proc/version")
	return strings.Contains(strings.ToLower(string(version)), "microsoft")
}
