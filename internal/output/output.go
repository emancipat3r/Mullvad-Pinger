// Package output renders the ranked results as a human table or stable JSON.
// The result payload goes to stdout; diagnostics belong on stderr (handled by
// the caller). In JSON mode nothing but the JSON array reaches stdout.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/emancipat3r/mullvad-pinger/internal/model"
)

// JSONResult is the documented, stable JSON shape for one measured relay.
type JSONResult struct {
	Rank        int     `json:"rank"`
	Hostname    string  `json:"hostname"`
	CityCode    string  `json:"city_code"`
	City        string  `json:"city"`
	CountryCode string  `json:"country_code"`
	Country     string  `json:"country"`
	Provider    string  `json:"provider"`
	Owned       bool    `json:"owned"`
	DAITA       bool    `json:"daita"`
	Protocol    string  `json:"protocol"`
	MedianMS    float64 `json:"median_ms"`
	JitterMS    float64 `json:"jitter_ms"`
	LossPct     float64 `json:"loss_pct"`
	Sent        int     `json:"probes_sent"`
	Recv        int     `json:"probes_recv"`
	Reachable   bool    `json:"reachable"`
	Winner      bool    `json:"winner"`
	Verified    bool    `json:"verified,omitempty"`
	HandshakeMS float64 `json:"handshake_ms,omitempty"`
}

func toJSON(results []model.Result) []JSONResult {
	out := make([]JSONResult, 0, len(results))
	for _, r := range results {
		out = append(out, JSONResult{
			Rank:        r.Rank,
			Hostname:    r.Relay.Hostname,
			CityCode:    r.Relay.CityCode,
			City:        r.Relay.CityName,
			CountryCode: r.Relay.CountryCode,
			Country:     r.Relay.CountryName,
			Provider:    r.Relay.Provider,
			Owned:       r.Relay.Owned,
			DAITA:       r.Relay.DAITA,
			Protocol:    string(r.Relay.Protocol),
			MedianMS:    round2(r.MedianMS),
			JitterMS:    round2(r.JitterMS),
			LossPct:     round2(r.LossPct),
			Sent:        r.Sent,
			Recv:        r.Recv,
			Reachable:   r.Reachable,
			Winner:      r.Reachable && r.Rank == 1,
			Verified:    r.Verified,
			HandshakeMS: round2(r.HandshakeMS),
		})
	}
	return out
}

// WriteJSON emits the results as a JSON array. Only this payload is written.
func WriteJSON(w io.Writer, results []model.Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(toJSON(results))
}

// WriteTable renders a human-readable ranked table. Unreachable hosts are shown
// after the ranked winners with 100% loss. The winner (rank 1) is marked.
func WriteTable(w io.Writer, results []model.Result, verified bool) error {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	header := "RANK\tHOSTNAME\tCITY\tCOUNTRY\tMEDIAN(ms)\tLOSS(%)\tJITTER(ms)\tOWNED\tDAITA\tPROVIDER"
	if verified {
		header += "\tHANDSHAKE(ms)"
	}
	fmt.Fprintln(tw, header)

	for _, r := range results {
		rankCell := "-"
		marker := ""
		if r.Reachable {
			rankCell = strconv.Itoa(r.Rank)
			if r.Rank == 1 {
				marker = " *"
			}
		}
		median, jitter := "-", "-"
		if r.Reachable {
			median = fmtF(r.MedianMS)
			jitter = fmtF(r.JitterMS)
		}
		row := strings.Join([]string{
			rankCell + marker,
			r.Relay.Hostname,
			nonEmpty(r.Relay.CityName),
			nonEmpty(r.Relay.CountryName),
			median,
			fmtF(r.LossPct),
			jitter,
			yesNo(r.Relay.Owned),
			yesNo(r.Relay.DAITA),
			nonEmpty(r.Relay.Provider),
		}, "\t")
		if verified {
			hs := "-"
			if r.Verified {
				hs = fmtF(r.HandshakeMS)
			}
			row += "\t" + hs
		}
		fmt.Fprintln(tw, row)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if n := countReachable(results); n > 0 {
		fmt.Fprintln(w, "\n* fastest eligible relay (lowest median RTT)")
	}
	return nil
}

func countReachable(rs []model.Result) int {
	n := 0
	for _, r := range rs {
		if r.Reachable {
			n++
		}
	}
	return n
}

func fmtF(v float64) string { return strconv.FormatFloat(round2(v), 'f', 2, 64) }

func round2(v float64) float64 {
	// Fixed precision, no locale dependence.
	return float64(int64(v*100+0.5)) / 100
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func nonEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
