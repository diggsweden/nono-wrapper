// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestSessionAuditEscapesTerminalControls(t *testing.T) {
	event := networkEvent{
		Decision: "deny\nforged", Target: "host\x1b[2J", Port: 443,
		Method: "GET\r", Path: "/path\nforged", Reason: "denied\x1b]0;title\a",
	}
	var output bytes.Buffer
	printSessionAudit(&output, sessionAudit{SessionID: "0123456789abcdef", NetworkEvents: []networkEvent{event, event}})
	text := output.String()
	if strings.ContainsAny(text, "\x1b\r\a") || strings.Contains(text, "\nforged") ||
		!strings.Contains(text, `"deny\nforged" "host\x1b[2J:443"`) || !strings.Contains(text, "(2 events)") {
		t.Fatalf("unsafe or ungrouped audit output: %q", text)
	}
}

func TestAuditIDsRejectUnsafeHints(t *testing.T) {
	for _, id := range []string{"", "--help", "$(touch file)", "abc\nforged", "abc;command"} {
		if validAuditID(id) {
			t.Errorf("accepted unsafe session ID %q", id)
		}
	}
	for _, id := range []string{"0123456789abcdef", "20260214-143022-12345"} {
		if !validAuditID(id) {
			t.Errorf("rejected nono session ID %q", id)
		}
	}
}
