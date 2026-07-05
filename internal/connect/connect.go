// Package connect wires the optional connect action to the Mullvad daemon.
//
// Connection happens ONLY through `mullvad` CLI subcommands. This package never
// edits wg-quick, WireGuard configs, or any daemon state file.
package connect

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/emancipat3r/mullvad-pinger/internal/model"
)

// Connector connects to a relay and reports tunnel status through the daemon.
type Connector interface {
	Connect(ctx context.Context, r model.Relay) error
	Status(ctx context.Context) (model.TunnelStatus, error)
}

// ClientName is the daemon CLI binary.
const ClientName = "mullvad"

// ErrClientNotFound is returned when the mullvad CLI is absent.
var ErrClientNotFound = fmt.Errorf("%s client not found in PATH", ClientName)

// MullvadConnector drives the real mullvad CLI.
type MullvadConnector struct {
	// Bin overrides the binary name/path (for tests); defaults to ClientName.
	Bin string
}

func (c MullvadConnector) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return ClientName
}

// Available reports whether the mullvad client is present in PATH.
func (c MullvadConnector) Available() bool {
	_, err := exec.LookPath(c.bin())
	return err == nil
}

// Connect pins the daemon to the given relay and connects. It sets the location
// to the specific hostname, then issues connect — never by editing config.
func (c MullvadConnector) Connect(ctx context.Context, r model.Relay) error {
	if !c.Available() {
		return ErrClientNotFound
	}
	if err := run(ctx, c.bin(), "relay", "set", "location",
		r.CountryCode, r.CityCode, r.Hostname); err != nil {
		return fmt.Errorf("set relay location: %w", err)
	}
	if err := run(ctx, c.bin(), "connect"); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	return nil
}

// Status queries the daemon for the current tunnel state.
func (c MullvadConnector) Status(ctx context.Context) (model.TunnelStatus, error) {
	if !c.Available() {
		return model.TunnelStatus{}, ErrClientNotFound
	}
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, c.bin(), "status")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return model.TunnelStatus{}, fmt.Errorf("query status: %w", err)
	}
	return parseStatus(out.String()), nil
}

// parseStatus reads the first line of `mullvad status`. A line beginning with
// "Connected" or "Connecting" means traffic is (or is becoming) tunneled.
func parseStatus(s string) model.TunnelStatus {
	line := strings.TrimSpace(s)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	lower := strings.ToLower(line)
	st := model.TunnelStatus{}
	if strings.HasPrefix(lower, "connected") || strings.HasPrefix(lower, "connecting") {
		st.Connected = true
		// e.g. "Connected to se-got-wg-001 in Gothenburg, Sweden"
		if fields := strings.Fields(line); len(fields) >= 3 && strings.EqualFold(fields[1], "to") {
			st.Relay = fields[2]
		}
	}
	return st
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), msg, err)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}
