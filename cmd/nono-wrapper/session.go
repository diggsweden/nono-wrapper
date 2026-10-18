// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"
)

func runSession(ctx context.Context, nono string, args []string, agent, name string, output io.Writer) error {
	// Keep the actual terminal descriptors: piping stderr would make nono
	// disable its interactive PTY. Only the post-exit queries capture output.
	cmd := exec.Command(nono, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start nono: %w", err)
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	var err error
wait:
	for {
		select {
		case sig := <-signals:
			// Let nono stop its sandbox and finalize its audit, rather than
			// using CommandContext's immediate SIGKILL on cancellation.
			_ = cmd.Process.Signal(sig)
		case err = <-finished:
			break wait
		}
	}
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return fmt.Errorf("wait for nono: %w", err)
		}
		code = exit.ExitCode()
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			code = 128 + int(status.Signal())
		}
	}

	fmt.Fprintf(output, "\nnono-wrapper: %s session ended (exit %d)\n", agent, code)
	// An interrupt that ended the agent must not cancel its report. Bound the
	// local lookups so a reporting failure cannot leave the wrapper hanging.
	reportCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := reportSession(reportCtx, nono, agent, name, output); err != nil {
		fmt.Fprintf(output, "  Session report unavailable: %s\n", err)
		fmt.Fprintf(output, "  Find recorded sessions: nono audit list --command %s --recent 5\n", agent)
	}
	if code != 0 {
		return cli.Exit("", code)
	}
	return nil
}

type networkEvent struct {
	Decision string `json:"decision"`
	Target   string `json:"target"`
	Port     uint16 `json:"port"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	Status   int    `json:"status"`
	Reason   string `json:"reason"`
}

type sessionAudit struct {
	SessionID     string         `json:"session_id"`
	AuditEvents   int            `json:"audit_event_count"`
	NetworkEvents []networkEvent `json:"network_events"`
}

func reportSession(ctx context.Context, nono, agent, name string, output io.Writer) error {
	var session struct {
		Name    string `json:"name"`
		AuditID string `json:"rollback_session"`
	}
	// The unique run name is a supported nono selector. Never select the
	// newest audit: another agent may have finished concurrently.
	if err := querySessionJSON(ctx, nono, &session, "inspect", name, "--json"); err != nil {
		return err
	}
	if session.Name != name || !validAuditID(session.AuditID) {
		return errors.New("nono did not identify this run's audit")
	}
	var audit sessionAudit
	if err := querySessionJSON(ctx, nono, &audit, "audit", "show", session.AuditID, "--json"); err != nil {
		return err
	}
	if audit.SessionID != session.AuditID {
		return errors.New("nono returned a different session's audit")
	}
	printSessionAudit(output, agent, audit)
	return nil
}

func querySessionJSON(ctx context.Context, nono string, result any, args ...string) error {
	cmd := exec.CommandContext(ctx, nono, args...)
	cmd.WaitDelay = time.Second
	data, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("nono %s query failed: %w", args[0], err)
	}
	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("nono %s returned invalid JSON", args[0])
	}
	return nil
}

func validAuditID(id string) bool {
	if id == "" {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c == '-') {
			return false
		}
	}
	return true
}

func printSessionAudit(output io.Writer, agent string, audit sessionAudit) {
	fmt.Fprintf(output, "  Audit: %s (%d recorded events)\n", audit.SessionID, audit.AuditEvents)
	fmt.Fprintf(output, "  Network: %d recorded events\n", len(audit.NetworkEvents))
	// Count identical recorded events, not requests: the proxy can record
	// CONNECT, endpoint decisions and responses for one HTTPS request.
	counts := make(map[networkEvent]int)
	var events []networkEvent
	for _, event := range audit.NetworkEvents {
		if counts[event] == 0 {
			events = append(events, event)
		}
		counts[event]++
	}
	for _, event := range events {
		target := event.Target
		if event.Port != 0 {
			target += ":" + strconv.Itoa(int(event.Port))
		}
		// Audit strings can originate from agent-controlled requests. Quote
		// them so newlines and terminal escape sequences stay inert.
		fmt.Fprintf(output, "    %q %q", event.Decision, target)
		if event.Method != "" {
			fmt.Fprintf(output, " method=%q", event.Method)
		}
		if event.Path != "" {
			fmt.Fprintf(output, " path=%q", event.Path)
		}
		if event.Status != 0 {
			fmt.Fprintf(output, " status=%d", event.Status)
		}
		if event.Reason != "" {
			fmt.Fprintf(output, " reason=%q", event.Reason)
		}
		if counts[event] > 1 {
			fmt.Fprintf(output, " (%d events)", counts[event])
		}
		fmt.Fprintln(output)
	}
	// Suggest the grant for each refused host. Targets come from the agent's
	// requests, so only public-looking names are echoed into a command line:
	// never IP addresses, such as cloud metadata, or single-label names.
	suggested := map[string]bool{}
	for _, event := range events {
		host := strings.ToLower(event.Target)
		if event.Decision != "deny" || event.Path != "" || suggested[host] || !suggestableHost(host) {
			continue
		}
		suggested[host] = true
		fmt.Fprintf(output, "  To allow %s: nono-wrapper config %s add-host %s\n", host, agent, host)
	}
	fmt.Fprintf(output, "  Full audit: nono audit show %s\n", audit.SessionID)
	fmt.Fprintln(output, "  Check access: nono why --profile PROFILE --host HOST (or --path PATH --op read)")
}

func suggestableHost(host string) bool {
	tld := host[strings.LastIndex(host, ".")+1:]
	return validHostname(host) && strings.Contains(host, ".") && strings.Trim(tld, "0123456789") != ""
}
