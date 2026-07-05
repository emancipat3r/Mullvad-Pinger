# mullvad-pinger

Find the lowest-latency [Mullvad](https://mullvad.net) relay for your current
location — correctly and fast — and optionally connect to it through the Mullvad
daemon.

This is a Go rewrite. It fixes the three structural faults of the original
Python tool: it **narrows before it measures** (inclusion filters + a distance
prefilter), it **measures natively** over ICMP instead of shelling out to
`/bin/ping` and string-parsing its output, and it **filters by inclusion**
(country, city, provider, ownership, DAITA, protocol) rather than by exclusion.

> [!WARNING]
> **Speed, not unlinkability.** This tool optimizes for the lowest-latency
> relay. That is the *opposite* of unlinkability: pinning yourself to a
> measured-optimal relay makes you more linkable across sessions, not less. Use
> this for **speed**. If your goal is anti-fingerprinting, do not reach for this
> tool — **DAITA** is the lever for that. This caveat is echoed at the top of
> the ranking/connect code paths (`internal/rank`, `internal/app`, `main.go`).

## Install / build

Requires Go 1.22+ (the module currently pins a newer toolchain via a
dependency). Produces a single statically linkable binary; `CGO_ENABLED=0`
builds:

```bash
CGO_ENABLED=0 go build -o mullvad-pinger .
```

## Quick start

```bash
# Rank the fastest WireGuard relays near you and print a table.
./mullvad-pinger

# Only US relays, 3 probes each, top 5 nearest.
./mullvad-pinger -c us --top-n 5 --count 3

# Machine-readable output (clean stdout; diagnostics go to stderr).
./mullvad-pinger -c se --json | jq '.[0]'

# Rank, then connect to the winner through the daemon.
./mullvad-pinger -c se --connect
```

Example:

```
RANK  HOSTNAME       CITY         COUNTRY  MEDIAN(ms)  LOSS(%)  JITTER(ms)  OWNED  DAITA  PROVIDER
1 *   us-qas-wg-103  Ashburn, VA  USA      8.98        0.00     0.10        no     no     DataPacket
2     us-qas-wg-102  Ashburn, VA  USA      9.62        0.00     0.28        no     no     DataPacket
-     us-qas-wg-101  Ashburn, VA  USA      -           100.00   -           no     no     DataPacket

* fastest eligible relay (lowest median RTT)
```

Ranking is on **median** RTT. **Packet loss** and **jitter** are reported
alongside every result so a relay with a low median but high jitter or loss is
visibly distinguishable, not silently ranked first. Unreachable hosts are shown
with 100% loss, never dropped silently.

## How it works

1. **Relay source — local first, API fallback.** The relay list is read from the
   Mullvad daemon's local cache when present:
   - Linux: `/var/cache/mullvad-vpn/relays.json`
   - macOS: `/Library/Caches/mullvad-vpn/relays.json`

   If the cache is missing, or you pass `--refresh`, the list is fetched from the
   Mullvad app API (`https://api.mullvad.net/app/v1/relays`). Override the file
   with `--relays-file`. The daemon cache and the API share the same JSON shape.

2. **Geolocation + tunnel detection.** Your coordinates come from
   `https://am.i.mullvad.net/json`, which also reports whether you are already
   exiting through Mullvad; the daemon's `mullvad status` is consulted too. If a
   tunnel is active, probes would traverse the tunnel and be meaningless, so the
   tool **refuses to measure and exits 4** unless you pass `--force`.

3. **Stage one — prefilter.** Inclusion filters are applied, then haversine
   distance from you to each relay. Relays beyond `--max-distance` km (default
   2000; `0` disables) are dropped, and the nearest `--top-n` (default 50) are
   kept. Distance is only a cheap heuristic to shrink the candidate set — not the
   answer.

4. **Stage two — measurement.** Each candidate is probed with native ICMP via
   [`pro-bing`](https://github.com/prometheus-community/pro-bing), `--count`
   probes per host (default 5), under bounded concurrency (`errgroup` +
   `SetLimit`, default 25), a per-probe `--timeout` (2s) and a whole-run
   `--deadline` (30s). Unprivileged UDP datagram sockets are preferred; if the
   kernel disallows them (`net.ipv4.ping_group_range`), the tool falls back to
   privileged raw sockets when run as root, otherwise prints actionable guidance.
   It **never** shells out to `ping`.

5. **Stage three — optional handshake verify.** `--verify` re-measures the top
   `--verify-k` finalists (default 5) by timing an actual WireGuard handshake to
   each relay's public key and UDP port, since ICMP can be routed or prioritized
   differently from data-plane traffic. This needs your registered WireGuard
   private key (set `MULLVAD_WG_PRIVATE_KEY` to its base64 form). If it is
   unavailable, `--verify` prints an explanation and falls back to ICMP ranking
   rather than failing the run.

6. **Connect (opt-in).** `--connect` connects to the winner by invoking the
   daemon: `mullvad relay set location <country> <city> <hostname>` then
   `mullvad connect`. It never edits `wg-quick`, WireGuard configs, or daemon
   state. If the `mullvad` binary is missing, it exits 5.

## List operations

These print available values from the relay source and exit without measuring or
geolocating:

```bash
./mullvad-pinger --list-countries
./mullvad-pinger --list-cities -c se     # optionally scoped by country
./mullvad-pinger --list-providers
```

## Flags

| Flag | Default | Meaning |
|------|---------|---------|
| `--country, -c` (repeatable) | none | Include only these country codes |
| `--city` | none | Include only this city code |
| `--provider` | none | Include only this provider |
| `--owned` | false | Only Mullvad-owned relays |
| `--daita` | false | Only DAITA-eligible relays |
| `--protocol` | wireguard | `wireguard` or `openvpn` |
| `--max-distance` | 2000 | Prefilter radius in km; `0` disables |
| `--top-n` | 50 | Cap candidates after prefilter |
| `--count, -n` | 5 | ICMP probes per host |
| `--concurrency` | 25 | Max concurrent probes |
| `--timeout` | 2s | Per-probe timeout |
| `--deadline` | 30s | Whole-run deadline |
| `--verify` | false | Handshake-verify the finalists |
| `--verify-k` | 5 | Finalists to verify |
| `--connect` | false | Connect to the winner via the daemon |
| `--force` | false | Proceed even if a tunnel is active |
| `--json` | false | JSON output (clean stdout) |
| `--relays-file` | auto | Override relays.json path |
| `--refresh` | false | Force API fetch over local cache |
| `--list-countries` / `--list-cities` / `--list-providers` | | List and exit |
| `--verbose, -v` (repeatable) | | Increase logging verbosity |

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Generic error |
| 2 | No candidates after filtering |
| 3 | No successful probes |
| 4 | Active tunnel without `--force` |
| 5 | `mullvad` client not found for `--connect` |

## Development

The network, geolocation, relay-source, and connect actions all sit behind
interfaces (`relays.Source`, `geo.Geolocator`, `probe.Pinger`,
`connect.Connector`, `verify.Verifier`), and the pure core (haversine, filtering,
stats, ranking) is data-in/data-out. The whole pipeline is exercised on fakes —
no test touches the real network or the real `mullvad` binary.

```bash
go build ./...          # CGO_ENABLED=0 builds
go vet ./...
go test -race ./...
```

## Non-goals

Not a replacement for the Mullvad app: it does not manage keys, accounts, or
settings, and it changes connection state only through `mullvad` subcommands. No
GUI/TUI, no daemon mode — a one-shot CLI.
