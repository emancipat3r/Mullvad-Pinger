package verify

import (
	"bytes"
	"encoding/base64"
	"os"
	"testing"
	"time"

	"golang.org/x/crypto/curve25519"
)

func TestBuildInitiationShape(t *testing.T) {
	priv := make([]byte, 32)
	priv[0] = 0x11
	serverPub, err := curve25519.X25519(mustKey(0x22), curve25519.Basepoint)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := buildInitiation(priv, serverPub)
	if err != nil {
		t.Fatalf("buildInitiation: %v", err)
	}
	if len(msg) != 148 {
		t.Fatalf("initiation length = %d, want 148", len(msg))
	}
	if msg[0] != 1 {
		t.Errorf("message type = %d, want 1", msg[0])
	}
	if msg[1] != 0 || msg[2] != 0 || msg[3] != 0 {
		t.Error("reserved bytes must be zero")
	}
	// mac1 (bytes 116:132) must be nonzero.
	if bytes.Equal(msg[116:132], make([]byte, 16)) {
		t.Error("mac1 should not be all zero")
	}
	// mac2 (bytes 132:148) must be zero (no cookie).
	if !bytes.Equal(msg[132:148], make([]byte, 16)) {
		t.Error("mac2 should be zero without a cookie")
	}
}

func TestBuildInitiationEphemeralRandomized(t *testing.T) {
	priv := mustKey(0x11)
	serverPub, _ := curve25519.X25519(mustKey(0x22), curve25519.Basepoint)
	a, _ := buildInitiation(priv, serverPub)
	b, _ := buildInitiation(priv, serverPub)
	if bytes.Equal(a[8:40], b[8:40]) {
		t.Error("ephemeral public key should differ between initiations")
	}
}

func TestMAC1Deterministic(t *testing.T) {
	// mac1 depends on the responder public key; the derived MAC key must be
	// stable for a fixed responder key.
	key := blakeHash(labelMAC1, mustKey(0x33))
	a := blakeMAC(key[:], []byte("hello"))
	b := blakeMAC(key[:], []byte("hello"))
	if a != b {
		t.Error("blakeMAC not deterministic")
	}
}

func TestTAI64NLayout(t *testing.T) {
	ts := tai64n(time.Unix(1_700_000_000, 12345))
	if len(ts) != 12 {
		t.Fatalf("tai64n length = %d, want 12", len(ts))
	}
	// High byte of the seconds label is 0x40.
	if ts[0] != 0x40 {
		t.Errorf("tai64n seconds label byte = %#x, want 0x40", ts[0])
	}
}

func TestLoadKeyUnavailable(t *testing.T) {
	old := os.Getenv("MULLVAD_WG_PRIVATE_KEY")
	os.Unsetenv("MULLVAD_WG_PRIVATE_KEY")
	defer os.Setenv("MULLVAD_WG_PRIVATE_KEY", old)
	if _, err := LoadKey(); err != ErrKeyUnavailable {
		t.Errorf("LoadKey() err = %v, want ErrKeyUnavailable", err)
	}
}

func TestLoadKeyValid(t *testing.T) {
	key := make([]byte, 32)
	key[3] = 0x09
	enc := base64.StdEncoding.EncodeToString(key)
	old := os.Getenv("MULLVAD_WG_PRIVATE_KEY")
	os.Setenv("MULLVAD_WG_PRIVATE_KEY", enc)
	defer os.Setenv("MULLVAD_WG_PRIVATE_KEY", old)
	got, err := LoadKey()
	if err != nil {
		t.Fatalf("LoadKey: %v", err)
	}
	if !bytes.Equal(got, key) {
		t.Error("LoadKey returned wrong bytes")
	}
}

func TestLoadKeyMalformed(t *testing.T) {
	old := os.Getenv("MULLVAD_WG_PRIVATE_KEY")
	os.Setenv("MULLVAD_WG_PRIVATE_KEY", "not-base64!!")
	defer os.Setenv("MULLVAD_WG_PRIVATE_KEY", old)
	if _, err := LoadKey(); err != ErrKeyUnavailable {
		t.Errorf("malformed key err = %v, want ErrKeyUnavailable", err)
	}
}

func mustKey(b byte) []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = b
	}
	// Clamp is not required for X25519 in x/crypto; return as-is.
	return k
}
