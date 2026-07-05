package app

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/emancipat3r/mullvad-pinger/internal/model"
)

// printCountries lists distinct country codes/names from the relay source.
func printCountries(w io.Writer, rels []model.Relay) {
	type cc struct{ code, name string }
	seen := map[string]cc{}
	for _, r := range rels {
		if r.CountryCode == "" {
			continue
		}
		if _, ok := seen[r.CountryCode]; !ok {
			seen[r.CountryCode] = cc{r.CountryCode, r.CountryName}
		}
	}
	list := make([]cc, 0, len(seen))
	for _, v := range seen {
		list = append(list, v)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].code < list[j].code })
	for _, c := range list {
		fmt.Fprintf(w, "%s\t%s\n", c.code, c.name)
	}
}

// printCities lists distinct cities, optionally scoped to the given country
// codes.
func printCities(w io.Writer, rels []model.Relay, countries []string) {
	scope := map[string]struct{}{}
	for _, c := range countries {
		scope[strings.ToLower(c)] = struct{}{}
	}
	type city struct{ code, name, country string }
	seen := map[string]city{}
	for _, r := range rels {
		if r.CityCode == "" {
			continue
		}
		if len(scope) > 0 {
			if _, ok := scope[strings.ToLower(r.CountryCode)]; !ok {
				continue
			}
		}
		key := r.CountryCode + "/" + r.CityCode
		if _, ok := seen[key]; !ok {
			seen[key] = city{r.CityCode, r.CityName, r.CountryCode}
		}
	}
	list := make([]city, 0, len(seen))
	for _, v := range seen {
		list = append(list, v)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].country != list[j].country {
			return list[i].country < list[j].country
		}
		return list[i].code < list[j].code
	})
	for _, c := range list {
		fmt.Fprintf(w, "%s\t%s\t%s\n", c.country, c.code, c.name)
	}
}

// printProviders lists distinct providers.
func printProviders(w io.Writer, rels []model.Relay) {
	seen := map[string]struct{}{}
	for _, r := range rels {
		if r.Provider != "" {
			seen[r.Provider] = struct{}{}
		}
	}
	list := make([]string, 0, len(seen))
	for p := range seen {
		list = append(list, p)
	}
	sort.Strings(list)
	for _, p := range list {
		fmt.Fprintln(w, p)
	}
}
