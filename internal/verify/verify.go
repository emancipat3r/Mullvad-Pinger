// Package verify implements the optional FR-6 handshake tier: re-measure the
// finalists by timing an actual WireGuard handshake, since ICMP can be routed
// or prioritized differently from data-plane traffic.
//
// This tier requires the user's registered WireGuard private key. When it is
// unavailable, callers degrade to ICMP ranking rather than failing the run.
package verify

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"net"
	"os"
	"time"

	"golang.org/x/crypto/blake2s"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"

	"github.com/emancipat3r/mullvad-pinger/internal/model"
)

// DefaultPort is the WireGuard UDP port Mullvad relays listen on.
const DefaultPort = 51820

// ErrKeyUnavailable signals that no usable WireGuard private key was found, so
// the caller should fall back to ICMP ranking with a warning.
var ErrKeyUnavailable = errors.New("no usable WireGuard private key (set MULLVAD_WG_PRIVATE_KEY)")

// Verifier times a data-plane handshake to a relay.
type Verifier interface {
	// Verify returns the handshake round-trip time to the relay.
	Verify(ctx context.Context, r model.Relay) (time.Duration, error)
}

// WireGuardVerifier performs a real WireGuard handshake initiation and times the
// response. It needs the user's registered private key (base64) to be accepted
// by the relay.
type WireGuardVerifier struct {
	// PrivateKey is the 32-byte Curve25519 static private key. When nil, the
	// verifier looks it up from the MULLVAD_WG_PRIVATE_KEY environment variable.
	PrivateKey []byte
	// Port overrides DefaultPort.
	Port int
	// Timeout bounds the wait for a handshake response.
	Timeout time.Duration
}

// LoadKey resolves the private key from the environment, returning
// ErrKeyUnavailable when absent or malformed.
func LoadKey() ([]byte, error) {
	raw := os.Getenv("MULLVAD_WG_PRIVATE_KEY")
	if raw == "" {
		return nil, ErrKeyUnavailable
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil, ErrKeyUnavailable
	}
	return key, nil
}

func (v WireGuardVerifier) key() ([]byte, error) {
	if len(v.PrivateKey) == 32 {
		return v.PrivateKey, nil
	}
	return LoadKey()
}

func (v WireGuardVerifier) Verify(ctx context.Context, r model.Relay) (time.Duration, error) {
	priv, err := v.key()
	if err != nil {
		return 0, err
	}
	if r.PublicKey == "" || r.IPv4 == "" {
		return 0, fmt.Errorf("relay %s missing public key or address", r.Hostname)
	}
	serverPub, err := base64.StdEncoding.DecodeString(r.PublicKey)
	if err != nil || len(serverPub) != 32 {
		return 0, fmt.Errorf("relay %s has invalid public key", r.Hostname)
	}
	port := v.Port
	if port == 0 {
		port = DefaultPort
	}
	timeout := v.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	msg, err := buildInitiation(priv, serverPub)
	if err != nil {
		return 0, err
	}

	raddr := &net.UDPAddr{IP: net.ParseIP(r.IPv4), Port: port}
	conn, err := net.DialUDP("udp4", nil, raddr)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}

	start := time.Now()
	if _, err := conn.Write(msg); err != nil {
		return 0, err
	}
	buf := make([]byte, 256)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return 0, err
		}
		// Message type 2 is a handshake response.
		if n >= 1 && buf[0] == 2 {
			return time.Since(start), nil
		}
	}
}

// --- WireGuard Noise_IKpsk2_25519_ChaChaPoly_BLAKE2s handshake initiation ---

var (
	construction = []byte("Noise_IKpsk2_25519_ChaChaPoly_BLAKE2s")
	identifier   = []byte("WireGuard v1 zx2c4 Jason@zx2c4.com")
	labelMAC1    = []byte("mac1----")
)

// buildInitiation constructs a 148-byte WireGuard handshake initiation message
// from our static private key to the responder's static public key.
func buildInitiation(staticPriv, serverPub []byte) ([]byte, error) {
	staticPub, err := curve25519.X25519(staticPriv, curve25519.Basepoint)
	if err != nil {
		return nil, err
	}

	ci := blakeHash(construction)
	hi := blakeHash(ci[:], identifier, serverPub)

	// Ephemeral keypair.
	ephPriv := make([]byte, 32)
	if _, err := rand.Read(ephPriv); err != nil {
		return nil, err
	}
	ephPub, err := curve25519.X25519(ephPriv, curve25519.Basepoint)
	if err != nil {
		return nil, err
	}

	ci = kdf1(ci[:], ephPub)
	hi = blakeHash(hi[:], ephPub)

	// Encrypt static key.
	es, err := curve25519.X25519(ephPriv, serverPub)
	if err != nil {
		return nil, err
	}
	c1, k1 := kdf2(ci[:], es)
	ci = c1
	encStatic, err := aeadSeal(k1[:], staticPub, hi[:])
	if err != nil {
		return nil, err
	}
	hi = blakeHash(hi[:], encStatic)

	// Encrypt timestamp.
	ss, err := curve25519.X25519(staticPriv, serverPub)
	if err != nil {
		return nil, err
	}
	_, k2 := kdf2(ci[:], ss)
	encTimestamp, err := aeadSeal(k2[:], tai64n(time.Now()), hi[:])
	if err != nil {
		return nil, err
	}

	// Assemble the message.
	msg := make([]byte, 148)
	msg[0] = 1 // type: initiation
	// bytes 1..3 reserved zero
	senderIndex := make([]byte, 4)
	if _, err := rand.Read(senderIndex); err != nil {
		return nil, err
	}
	copy(msg[4:8], senderIndex)
	copy(msg[8:40], ephPub)
	copy(msg[40:88], encStatic)     // 32 + 16
	copy(msg[88:116], encTimestamp) // 12 + 16

	// mac1 over everything preceding it.
	macKey := blakeHash(labelMAC1, serverPub)
	m1 := blakeMAC(macKey[:], msg[:116])
	copy(msg[116:132], m1[:])
	// mac2 left zero (no cookie).

	return msg, nil
}

// tai64n encodes t as a 12-byte TAI64N timestamp.
func tai64n(t time.Time) []byte {
	out := make([]byte, 12)
	secs := uint64(t.Unix()) + 0x400000000000000a
	binary.BigEndian.PutUint64(out[0:8], secs)
	binary.BigEndian.PutUint32(out[8:12], uint32(t.Nanosecond()))
	return out
}

func blakeHash(parts ...[]byte) [32]byte {
	h, _ := blake2s.New256(nil)
	for _, p := range parts {
		h.Write(p)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func blakeMAC(key, data []byte) [16]byte {
	h, _ := blake2s.New128(key)
	h.Write(data)
	var out [16]byte
	copy(out[:], h.Sum(nil))
	return out
}

func hmacBlake(key, data []byte) [32]byte {
	m := hmac.New(func() hash.Hash { h, _ := blake2s.New256(nil); return h }, key)
	m.Write(data)
	var out [32]byte
	copy(out[:], m.Sum(nil))
	return out
}

// kdf1 returns τ1 = HMAC(HMAC(key, input), 0x1).
func kdf1(key, input []byte) [32]byte {
	t0 := hmacBlake(key, input)
	return hmacBlake(t0[:], []byte{0x1})
}

// kdf2 returns (τ1, τ2) where τ2 = HMAC(τ0, τ1 || 0x2).
func kdf2(key, input []byte) ([32]byte, [32]byte) {
	t0 := hmacBlake(key, input)
	t1 := hmacBlake(t0[:], []byte{0x1})
	t2 := hmacBlake(t0[:], append(append([]byte{}, t1[:]...), 0x2))
	return t1, t2
}

func aeadSeal(key, plaintext, aad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, chacha20poly1305.NonceSize) // counter 0
	return aead.Seal(nil, nonce, plaintext, aad), nil
}
