#!/usr/bin/env python3

import requests
import subprocess
import threading
from queue import Queue
import argparse
from tqdm import tqdm

# Function to parse command-line arguments
def parse_args():
    parser = argparse.ArgumentParser(description='Ping Mullvad VPN servers to find the best one.')
    parser.add_argument('--max-concurrent-pings', type=int, default=10, help='Maximum number of concurrent pings. Default is 10.')
    parser.add_argument('--exclude-country', type=str, help='Exclude servers from this country.')
    parser.add_argument('--exclude-city', type=str, help='Exclude servers from this city.')
    parser.add_argument('--exclude-state', type=str, help='Exclude servers from this state abbreviation.')
    return parser.parse_args()

def fetch_mullvad_servers():
    try:
        response = requests.get('https://api-www.mullvad.net/www/relays/all/')
        response.raise_for_status()
        return response.json()
    except requests.RequestException as e:
        print(f"Error fetching server list: {e}")
        return []

def ping_server(server, results_queue, semaphore, pbar):
    hostname = f"{server['hostname']}.mullvad.net"
    with semaphore:
        try:
            output = subprocess.run(['ping', '-c', '1', hostname], capture_output=True, text=True, timeout=5)
            if output.returncode == 0:
                ping_time = float(output.stdout.splitlines()[-1].split('/')[4])
                results_queue.put((ping_time, server))
        except (subprocess.TimeoutExpired, subprocess.CalledProcessError):
            pass
        finally:
            pbar.update(1)

def find_best_mullvad_server(args):
    servers = fetch_mullvad_servers()
    if not servers:
        print("No servers fetched, exiting.")
        return

    # Filter out servers based on user input
    if args.exclude_country:
        servers = [s for s in servers if s.get('country_name', '').lower() != args.exclude_country.lower()]
    if args.exclude_city:
        servers = [s for s in servers if s.get('city_name', '').lower() != args.exclude_city.lower()]
    if args.exclude_state:
        servers = [s for s in servers if args.exclude_state.lower() not in s.get('city_name', '').lower()]

    if not servers:
        print("No servers available after filtering.")
        return

    results_queue = Queue()
    semaphore = threading.Semaphore(args.max_concurrent_pings)
    threads = []

    with tqdm(total=len(servers), desc="Pinging servers", unit="server") as pbar:
        for server in servers:
            t = threading.Thread(target=ping_server, args=(server, results_queue, semaphore, pbar))
            t.start()
            threads.append(t)
        for t in threads:
            t.join()

    if results_queue.empty():
        print("No ping results were obtained.")
        return

    best_ping, best_server = sorted([results_queue.get() for _ in range(results_queue.qsize())])[0]
    print(f"[*] Best server is {best_server['hostname']}.mullvad.net - time = {best_ping}ms")
    print(f"\tCountry: {best_server.get('country_name', 'Unknown')}")
    print(f"\tCity: {best_server.get('city_name', 'Unknown')}")

if __name__ == "__main__":
    args = parse_args()
    find_best_mullvad_server(args)
