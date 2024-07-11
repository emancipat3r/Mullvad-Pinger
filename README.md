## Overview
This tool helps you find the fastest Mullvad VPN server based on your location. It begins by collecting available VPN servers through a GET request to Mullvad's API. User-defined exclusions (by country, city, or state) are then applied to filter the servers. Each server is subsequently pinged, and the fastest one is determined based on the ping response time in milliseconds.

While ping response times are not a comprehensive measure of VPN tunnel speed, they can serve as a useful indicator of potential performance.

## Setup
```bash
python3 -m venv myenv
pip install -r requirements.txt
```

## Usage
```bash
usage: mullvad_ping.py [-h] [--max-concurrent-pings MAX_CONCURRENT_PINGS] [--exclude-country-code EXCLUDE_COUNTRY_CODE] [--exclude-city-code EXCLUDE_CITY_CODE] [--exclude-state EXCLUDE_STATE]
                       [--show-next-fastest] [--list-countries] [--list-cities] [--list-cities-in-country LIST_CITIES_IN_COUNTRY] [--list-providers] [--provider PROVIDER]

Ping Mullvad VPN servers to find the best one.

options:
  -h, --help            show this help message and exit
  --max-concurrent-pings MAX_CONCURRENT_PINGS
                        Maximum number of concurrent pings. Default is 10.
  --exclude-country-code EXCLUDE_COUNTRY_CODE
                        Exclude servers from this country code.
  --exclude-city-code EXCLUDE_CITY_CODE
                        Exclude servers from this city code.
  --exclude-state EXCLUDE_STATE
                        Exclude servers from this state abbreviation.
  --show-next-fastest   Show the next 10 fastest pinging servers.
  --list-countries      List all available countries.
  --list-cities         List all available cities.
  --list-cities-in-country LIST_CITIES_IN_COUNTRY
                        List all cities in the specified country code.
  --list-providers      List all server providers.
  --provider PROVIDER   Filter servers by provider to find the fastest one.
```