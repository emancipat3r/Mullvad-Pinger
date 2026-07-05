package relays

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/emancipat3r/mullvad-pinger/internal/model"
)

func loadFixture(t *testing.T) []model.Relay {
	t.Helper()
	rels, err := FileSource{Path: "testdata/relays.json"}.Relays(context.Background())
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	return rels
}

func find(rels []model.Relay, host string) (model.Relay, bool) {
	for _, r := range rels {
		if r.Hostname == host {
			return r, true
		}
	}
	return model.Relay{}, false
}

func TestParseFixtureRoundTrip(t *testing.T) {
	rels := loadFixture(t)
	// 8 wireguard + 1 openvpn.
	if got, want := len(rels), 9; got != want {
		t.Fatalf("relay count = %d, want %d", got, want)
	}

	r, ok := find(rels, "se-got-wg-001")
	if !ok {
		t.Fatal("se-got-wg-001 not found")
	}
	if r.CountryCode != "se" || r.CountryName != "Sweden" {
		t.Errorf("country = %q/%q, want se/Sweden", r.CountryCode, r.CountryName)
	}
	if r.CityCode != "got" || r.CityName != "Gothenburg" {
		t.Errorf("city = %q/%q, want got/Gothenburg", r.CityCode, r.CityName)
	}
	if !r.Owned || !r.Active || !r.DAITA {
		t.Errorf("owned/active/daita = %v/%v/%v, want all true", r.Owned, r.Active, r.DAITA)
	}
	if r.Protocol != model.WireGuard {
		t.Errorf("protocol = %q, want wireguard", r.Protocol)
	}
	if r.PublicKey == "" {
		t.Error("wireguard relay missing public key")
	}
	if r.IPv4 != "185.213.154.68" {
		t.Errorf("ipv4 = %q", r.IPv4)
	}
	if !r.HasCoords || r.Latitude == 0 {
		t.Errorf("expected coordinates, got HasCoords=%v lat=%v", r.HasCoords, r.Latitude)
	}
}

func TestParseBooleansDerivedNotDefaulted(t *testing.T) {
	rels := loadFixture(t)
	inactive, ok := find(rels, "de-fra-wg-002")
	if !ok {
		t.Fatal("de-fra-wg-002 not found")
	}
	if inactive.Active {
		t.Error("de-fra-wg-002 should be inactive")
	}
	rented, _ := find(rels, "us-lax-wg-001")
	if rented.Owned {
		t.Error("us-lax-wg-001 should be rented (owned=false)")
	}
	if rented.DAITA {
		t.Error("us-lax-wg-001 should not be daita")
	}
}

func TestParseOpenVPNHasNoPublicKey(t *testing.T) {
	rels := loadFixture(t)
	ov, ok := find(rels, "se-got-ovpn-001")
	if !ok {
		t.Fatal("openvpn relay not found")
	}
	if ov.Protocol != model.OpenVPN {
		t.Errorf("protocol = %q, want openvpn", ov.Protocol)
	}
	if ov.PublicKey != "" {
		t.Error("openvpn relay should not carry a wireguard public key")
	}
}

func TestParseMissingCoordinates(t *testing.T) {
	rels := loadFixture(t)
	nul, ok := find(rels, "xx-nul-wg-001")
	if !ok {
		t.Fatal("xx-nul-wg-001 not found")
	}
	if nul.HasCoords {
		t.Error("relay with no coords should have HasCoords=false")
	}
	if nul.Latitude != 0 || nul.Longitude != 0 {
		t.Error("missing coords must be zero-valued, not fabricated")
	}
}

func TestParseCorruptJSON(t *testing.T) {
	if _, err := parse([]byte("{not json")); err == nil {
		t.Fatal("expected error on corrupt JSON, got nil")
	}
}

func TestLoadPrefersLocalFileOverride(t *testing.T) {
	rels, err := Load(context.Background(), LoadOptions{RelaysFile: "testdata/relays.json"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(rels) != 9 {
		t.Fatalf("relay count = %d, want 9", len(rels))
	}
}

func TestLoadMissingCacheFallsBackToAPI(t *testing.T) {
	fixture, err := os.ReadFile("testdata/relays.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(fixture)
	}))
	defer srv.Close()

	// Point RelaysFile at a nonexistent path but rely on Refresh -> API. Also
	// exercise the default-cache-missing branch by leaving RelaysFile empty and
	// forcing refresh.
	rels, err := Load(context.Background(), LoadOptions{
		Refresh: true,
		APIURL:  srv.URL,
		Client:  srv.Client(),
	})
	if err != nil {
		t.Fatalf("api fallback: %v", err)
	}
	if len(rels) != 9 {
		t.Fatalf("relay count via API = %d, want 9", len(rels))
	}
}

func TestFileSourceMissingFileErrors(t *testing.T) {
	_, err := FileSource{Path: filepath.Join(t.TempDir(), "nope.json")}.Relays(context.Background())
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSplitLocation(t *testing.T) {
	cases := []struct{ in, cc, city string }{
		{"us-qas", "us", "qas"},
		{"se-got", "se", "got"},
		{"nl-ams", "nl", "ams"},
		{"nocode", "nocode", ""},
	}
	for _, c := range cases {
		cc, city := splitLocation(c.in)
		if cc != c.cc || city != c.city {
			t.Errorf("splitLocation(%q) = %q/%q, want %q/%q", c.in, cc, city, c.cc, c.city)
		}
	}
}
