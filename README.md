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

```