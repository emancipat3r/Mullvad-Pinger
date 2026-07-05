package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/emancipat3r/mullvad-pinger/internal/model"
)

func sampleResults() []model.Result {
	return []model.Result{
		{Rank: 1, Reachable: true, MedianMS: 10.123, JitterMS: 1.5, LossPct: 0, Sent: 5, Recv: 5,
			Relay: model.Relay{Hostname: "se-got-wg-001", CityName: "Gothenburg", CountryName: "Sweden", CountryCode: "se", CityCode: "got", Provider: "Mullvad", Owned: true, DAITA: true, Protocol: model.WireGuard}},
		{Rank: 2, Reachable: true, MedianMS: 22.5, JitterMS: 3.0, LossPct: 20, Sent: 5, Recv: 4,
			Relay: model.Relay{Hostname: "se-sto-wg-001", CityName: "Stockholm", CountryName: "Sweden", CountryCode: "se", CityCode: "sto", Provider: "31173", Owned: false, DAITA: false, Protocol: model.WireGuard}},
		{Rank: 0, Reachable: false, MedianMS: 0, JitterMS: 0, LossPct: 100, Sent: 5, Recv: 0,
			Relay: model.Relay{Hostname: "dead-wg-001", CityName: "Nowhere", CountryName: "Void", Provider: "ghost", Protocol: model.WireGuard}},
	}
}

func TestWriteJSONParsesCleanly(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, sampleResults()); err != nil {
		t.Fatal(err)
	}
	var got []JSONResult
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json did not parse: %v\n%s", err, buf.String())
	}
	if len(got) != 3 {
		t.Fatalf("json len = %d, want 3", len(got))
	}
	if !got[0].Winner || got[0].Rank != 1 {
		t.Errorf("first result should be winner rank 1: %+v", got[0])
	}
	if got[1].Winner {
		t.Error("second result should not be winner")
	}
	if got[2].Reachable || got[2].LossPct != 100 {
		t.Errorf("unreachable relay json = %+v", got[2])
	}
}

func TestWriteJSONNoStrayOutput(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, sampleResults()); err != nil {
		t.Fatal(err)
	}
	trimmed := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(trimmed, "[") || !strings.HasSuffix(trimmed, "]") {
		t.Errorf("json output has stray content:\n%s", buf.String())
	}
}

func TestTableAndJSONShareRanking(t *testing.T) {
	results := sampleResults()
	var jbuf, tbuf bytes.Buffer
	_ = WriteJSON(&jbuf, results)
	_ = WriteTable(&tbuf, results, false)

	var jr []JSONResult
	_ = json.Unmarshal(jbuf.Bytes(), &jr)

	table := tbuf.String()
	// The winner marker should be on the rank-1 host line.
	lines := strings.Split(table, "\n")
	var winnerLine string
	for _, l := range lines {
		if strings.Contains(l, "se-got-wg-001") {
			winnerLine = l
		}
	}
	if !strings.Contains(winnerLine, "*") {
		t.Errorf("winner line missing marker: %q", winnerLine)
	}
	if jr[0].Hostname != "se-got-wg-001" {
		t.Errorf("json winner = %s, want se-got-wg-001", jr[0].Hostname)
	}
}

func TestTableShowsUnreachable(t *testing.T) {
	var buf bytes.Buffer
	_ = WriteTable(&buf, sampleResults(), false)
	out := buf.String()
	if !strings.Contains(out, "dead-wg-001") {
		t.Error("unreachable relay should still appear in the table")
	}
	if !strings.Contains(out, "100.00") {
		t.Error("unreachable relay should show 100% loss")
	}
}

func TestTableVerifiedColumn(t *testing.T) {
	results := sampleResults()
	results[0].Verified = true
	results[0].HandshakeMS = 9.87
	var buf bytes.Buffer
	_ = WriteTable(&buf, results, true)
	out := buf.String()
	if !strings.Contains(out, "HANDSHAKE(ms)") {
		t.Error("verified table should include handshake column")
	}
	if !strings.Contains(out, "9.87") {
		t.Error("verified handshake value missing")
	}
}
