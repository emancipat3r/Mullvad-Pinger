package app

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/pflag"

	"github.com/emancipat3r/mullvad-pinger/internal/model"
)

// Options holds parsed CLI configuration.
type Options struct {
	Countries []string
	City      string
	Provider  string
	Owned     bool
	DAITA     bool
	Protocol  string

	MaxDistance float64
	TopN        int

	Count       int
	Concurrency int
	Timeout     time.Duration
	Deadline    time.Duration

	Verify  bool
	VerifyK int

	Connect bool
	Force   bool
	JSON    bool

	RelaysFile string
	Refresh    bool

	ListCountries bool
	ListCities    bool
	ListProviders bool

	Verbose int

	// ShowHelp is set when -h/--help was requested.
	ShowHelp bool
}

// parseFlags parses argv into Options. It returns the flag set (for usage) and
// any parse error. A -h/--help request sets ShowHelp and returns pflag.ErrHelp.
func parseFlags(args []string, stderr io.Writer) (*Options, *pflag.FlagSet, error) {
	fs := pflag.NewFlagSet("mullvad-pinger", pflag.ContinueOnError)
	fs.SetOutput(stderr)
	o := &Options{}

	fs.StringArrayVarP(&o.Countries, "country", "c", nil, "include only these country codes (repeatable)")
	fs.StringVar(&o.City, "city", "", "include only this city code")
	fs.StringVar(&o.Provider, "provider", "", "include only this provider")
	fs.BoolVar(&o.Owned, "owned", false, "only Mullvad-owned relays")
	fs.BoolVar(&o.DAITA, "daita", false, "only DAITA-eligible relays")
	fs.StringVar(&o.Protocol, "protocol", string(model.WireGuard), "wireguard or openvpn")

	fs.Float64Var(&o.MaxDistance, "max-distance", 2000, "prefilter radius in km (0 disables)")
	fs.IntVar(&o.TopN, "top-n", 50, "cap candidates after prefilter")

	fs.IntVarP(&o.Count, "count", "n", 5, "ICMP probes per host")
	fs.IntVar(&o.Concurrency, "concurrency", 25, "max concurrent probes")
	fs.DurationVar(&o.Timeout, "timeout", 2*time.Second, "per-probe timeout")
	fs.DurationVar(&o.Deadline, "deadline", 30*time.Second, "whole-run deadline")

	fs.BoolVar(&o.Verify, "verify", false, "handshake-verify the finalists")
	fs.IntVar(&o.VerifyK, "verify-k", 5, "finalists to verify")

	fs.BoolVar(&o.Connect, "connect", false, "connect to the winner via the daemon")
	fs.BoolVar(&o.Force, "force", false, "proceed even if a tunnel is active")
	fs.BoolVar(&o.JSON, "json", false, "JSON output")

	fs.StringVar(&o.RelaysFile, "relays-file", "", "override relays.json path")
	fs.BoolVar(&o.Refresh, "refresh", false, "force API fetch over local cache")

	fs.BoolVar(&o.ListCountries, "list-countries", false, "list available countries and exit")
	fs.BoolVar(&o.ListCities, "list-cities", false, "list available cities and exit")
	fs.BoolVar(&o.ListProviders, "list-providers", false, "list available providers and exit")

	fs.CountVarP(&o.Verbose, "verbose", "v", "increase logging verbosity (repeatable)")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "mullvad-pinger — find the lowest-latency Mullvad relay\n\nUsage:\n  mullvad-pinger [flags]\n\nFlags:\n%s", fs.FlagUsages())
	}

	if err := fs.Parse(args); err != nil {
		if err == pflag.ErrHelp {
			o.ShowHelp = true
		}
		return o, fs, err
	}
	return o, fs, nil
}

// validate checks option combinations that must hold before doing work.
func (o *Options) validate() error {
	switch model.Protocol(o.Protocol) {
	case model.WireGuard, model.OpenVPN:
	default:
		return fmt.Errorf("invalid --protocol %q (want wireguard or openvpn)", o.Protocol)
	}
	if o.Count < 1 {
		return fmt.Errorf("--count must be >= 1")
	}
	if o.Concurrency < 1 {
		return fmt.Errorf("--concurrency must be >= 1")
	}
	return nil
}

func (o *Options) anyList() bool {
	return o.ListCountries || o.ListCities || o.ListProviders
}
