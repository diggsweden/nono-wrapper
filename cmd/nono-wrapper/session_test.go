// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
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
	printSessionAudit(&output, "opencode", sessionAudit{SessionID: "0123456789abcdef", NetworkEvents: []networkEvent{event, event}})
	text := output.String()
	if strings.ContainsAny(text, "\x1b\r\a") || strings.Contains(text, "\nforged") ||
		!strings.Contains(text, `"deny\nforged" "host\x1b[2J:443"`) || !strings.Contains(text, "(2 events)") {
		t.Fatalf("unsafe or ungrouped audit output: %q", text)
	}
	if strings.Contains(text, "To allow") {
		t.Fatalf("suggested a grant for an agent-controlled target: %q", text)
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

func TestSessionAuditSuggestsOnlyPublicHostNames(t *testing.T) {
	var events []networkEvent
	for _, target := range []string{"api.example.com", "169.254.169.254", "10.0.0.5", "localhost", "API.EXAMPLE.COM"} {
		events = append(events, networkEvent{Decision: "deny", Target: target, Port: 443})
	}
	var output bytes.Buffer
	printSessionAudit(&output, "codex", sessionAudit{SessionID: "0123456789abcdef", NetworkEvents: events})
	text := output.String()
	if strings.Count(text, "To allow") != 1 || !strings.Contains(text, "To allow api.example.com in future codex sessions, run outside the sandbox:\n    nono-wrapper config codex allow-domain api.example.com\n  Then restart the agent.\n") {
		t.Fatalf("unexpected suggestions: %q", text)
	}
}

func TestSessionAuditDoesNotBroadenEndpointDenials(t *testing.T) {
	var output bytes.Buffer
	printSessionAudit(&output, "opencode", sessionAudit{SessionID: "0123456789abcdef", NetworkEvents: []networkEvent{{
		Decision: "deny", Target: "models.opencode.ai", Port: 443,
		Reason: "endpoint rules denied GET /experimental/v2.0/api.json?type=chat,decision,image: no rule matched on models.opencode.ai:443",
	}}})
	if strings.Contains(output.String(), "To allow") || !strings.Contains(output.String(), "endpoint rules denied GET") {
		t.Fatalf("endpoint denial must remain visible without a whole-host grant suggestion: %s", output.String())
	}
}

func TestSessionAuditDoesNotSuggestGrantForTransportFailure(t *testing.T) {
	var output bytes.Buffer
	printSessionAudit(&output, "opencode", sessionAudit{SessionID: "0123456789abcdef", NetworkEvents: []networkEvent{{
		Decision: "deny", Target: "models.opencode.ai", Port: 443,
		Reason: "I/O error: Connection reset by peer (os error 104)",
	}}})
	text := output.String()
	if strings.Contains(text, "To allow") || !strings.Contains(text, `"deny" "models.opencode.ai:443"`) ||
		!strings.Contains(text, "Connection reset by peer") || !strings.Contains(text, "not evidence of a missing host grant") {
		t.Fatalf("transport failure must remain visible without a grant suggestion: %s", text)
	}
}
