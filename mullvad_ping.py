#!/usr/bin/env python3

import requests
import subprocess
import threading
import json
from queue import Queue
import argparse
from rich.console import Console
from rich.progress import Progress, BarColumn, TextColumn, TimeElapsedColumn, TimeRemainingColumn
from rich.table import Table

console = Console()

def parse_args():
    parser = argparse.ArgumentParser(description='Ping Mullvad VPN servers to find the best one.')
    parser.add_argument('--max-concurrent-pings', type=int, default=10, help='Maximum number of concurrent pings. Default is 10.')
    parser.add_argument('--show-next-fastest', type=int, nargs='?', const=10, default=None, help='Show user specified amount of next fastest pinging servers.')
    parser.add_argument('--list-countries', action='store_true', help='List all available countries.')
    parser.add_argument('--list-cities', action='store_true', help='List all available cities.')
    parser.add_argument('--list-cities-in-country', type=str, help='List all cities in the specified country code.')
    parser.add_argument('--list-providers', action='store_true', help='List all server providers.')
    parser.add_argument('--list-providers-in-country', type=str, help='List all server providers in the specified country code.')
    parser.add_argument('--provider', type=str, help='Filter servers by provider to find the fastest one.')
    parser.add_argument('--country-code', type=str, help='Filter servers by country code. Can specify multiple.')
    parser.add_argument('--city-code', type=str, help='Filter servers by city code. Can specify multiple.')
    parser.add_argument('--vpn-type', type=str, choices=['wg', 'ovpn'], help='Filter servers by VPN type (WG or OVPN).')
    return parser.parse_args()

def fetch_mullvad_servers():
    try:
        response = requests.get('https://api-www.mullvad.net/www/relays/all/')
        response.raise_for_status()
        return response.json()
    except requests.RequestException as e:
        console.print(f"[red]Error fetching server list: {e}[/red]")
        return []

def count_server_types(servers, key, value):
    wg_count = sum(1 for server in servers if key in server and server[key] == value and "wireguard" in server['type'].lower())
    ovpn_count = sum(1 for server in servers if key in server and server[key] == value and "openvpn" in server['type'].lower())
    return wg_count, ovpn_count

def list_countries(servers):
    countries = sorted(set((server['country_name'], server['country_code']) for server in servers if 'country_name' in server and 'country_code' in server))
    table = Table(title="Available Countries", style="bold blue")
    table.add_column("No.", style="bold white")
    table.add_column("Country", style="magenta")
    table.add_column("Country Code", style="cyan")
    table.add_column("Wireguard Servers", style="green")
    table.add_column("OVPN Servers", style="green")
    
    for idx, (country_name, country_code) in enumerate(countries, start=1):
        wg_count, ovpn_count = count_server_types(servers, 'country_code', country_code)
        table.add_row(str(idx), country_name, country_code, str(wg_count), str(ovpn_count))
    console.print(table)

def list_cities(servers):
    cities = sorted(set((server['city_name'], server['city_code'], server['country_name'], server['country_code']) for server in servers if 'city_name' in server and 'city_code' in server))
    table = Table(title="Available Cities", style="bold blue")
    table.add_column("No.", style="bold white")
    table.add_column("City", style="magenta")
    table.add_column("City Code", style="cyan")
    table.add_column("Country", style="green")
    table.add_column("Country Code", style="green")
    table.add_column("Wireguard Servers", style="green")
    table.add_column("OVPN Servers", style="green")
    
    for idx, (city_name, city_code, country_name, country_code) in enumerate(cities, start=1):
        wg_count, ovpn_count = count_server_types(servers, 'city_code', city_code)
        table.add_row(str(idx), city_name, city_code, country_name, country_code, str(wg_count), str(ovpn_count))
    console.print(table)

def list_cities_in_country(servers, country_code):
    cities = sorted(set((server['city_name'], server['city_code'], server['country_name']) for server in servers if 'city_name' in server and 'city_code' in server and server['country_code'].lower() == country_code.lower()))
    table = Table(title=f"Available Cities in Country {country_code.upper()}", style="bold blue")
    table.add_column("No.", style="bold white")
    table.add_column("City", style="magenta")
    table.add_column("City Code", style="cyan")
    table.add_column("Country", style="green")
    table.add_column("Wireguard Servers", style="green")
    table.add_column("OVPN Servers", style="green")
    
    for idx, (city_name, city_code, country_name) in enumerate(cities, start=1):
        wg_count, ovpn_count = count_server_types(servers, 'city_code', city_code)
        table.add_row(str(idx), city_name, city_code, country_name, str(wg_count), str(ovpn_count))
    console.print(table)

def list_providers(servers):
    providers = []
    for server in servers:
        if ('provider' and 'country_name') in server:
            provider, country = server['provider'], server['country_name']

            existing_provider = None
            for p in providers:
                if p['provider'] == provider:
                    existing_provider = p

            if existing_provider:
                if country not in existing_provider['countries']:
                    existing_provider['countries'].append(country)

            else:
                providers.append({'provider': provider, 'countries': [country]})


    table = Table(title="Available Providers", style="bold blue")
    table.add_column("No.", style="bold white")
    table.add_column("Provider", style="magenta")
    table.add_column("Serviced Countries", style="cyan")

    for idx, provider_info in enumerate(providers, start=1):
        provider_name = provider_info['provider']
        countries = ", ".join(provider_info['countries'])
        table.add_row(str(idx), provider_name, countries)
    console.print(table)

def list_providers_in_country(servers, country_code):
    providers = []
    for server in servers:
        if ('provider' and 'country_name') in server and country_code == server['country_code']:
            provider, country = server['provider'], server['country_name']

            existing_provider = None
            for p in providers:
                if p['provider'] == provider:
                    existing_provider = p

            if existing_provider:
                if country not in existing_provider['countries']:
                    existing_provider['countries'].append(country)

            else:
                providers.append({'provider': provider, 'countries': [country]})


    table = Table(title="Available Providers", style="bold blue")
    table.add_column("No.", style="bold white")
    table.add_column("Provider", style="magenta")
    table.add_column("Serviced Countries", style="cyan")

    for idx, provider_info in enumerate(providers, start=1):
        provider_name = provider_info['provider']
        countries = ", ".join(provider_info['countries'])
        table.add_row(str(idx), provider_name, countries)
    console.print(table)

def ping_server(server, results_queue, semaphore, progress_task_id, progress):
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
            progress.update(progress_task_id, advance=1)

def find_best_mullvad_server(args):

    servers = fetch_mullvad_servers()
    
    # If empty
    if not servers:
        console.print("[red]No servers fetched, exiting.[/red]")
        return

    # If '--list-countries'
    if args.list_countries:
        list_countries(servers)
        return

    # If '--list-cities'
    if args.list_cities:
        list_cities(servers)
        return

    # Filtered list of cities based on '--list-cities-in-country {COUNTRY_CODE}'
    if args.list_cities_in_country:
        list_cities_in_country(servers, args.list_cities_in_country)
        return

    # If '--list-providers
    if args.list_providers:
        list_providers(servers)
        return

    # Filtered list of cities based on '--list-providers-in-country {COUNTRY_CODE}'
    if args.list_providers_in_country:
        list_providers_in_country(servers, args.list_providers_in_country)
        return

    # Filter out servers based on user input
    if args.provider:
        servers = [s for s in servers if s['provider'].lower() == args.provider]
        console.print(f"[bold][green][+][/bold][/green] Filtering for provider: [magenta]{args.provider}[/magenta] [[green]{len(servers)}[/green]]")

    if args.country_code:
        #servers = [s for s in servers if s.get('country_code', '').lower() in [code.lower() for code in args.country_code]]
        servers = [s for s in servers if s['country_code'] == args.country_code]
        console.print(f"[bold][green][+][/bold][/green] Filtering for country code: [magenta]{args.country_code}[/magenta] [[green]{len(servers)}[/green]]")

    if args.city_code:
        servers = [s for s in servers if s.get('city_code', '').lower() in [code.lower() for code in args.city_code]]

    if args.vpn_type:
        vpn_type = args.vpn_type.lower()
        if vpn_type == 'wg':
            vpn_type = "wireguard"
        elif vpn_type == 'ovpn':
            vpn_type = "openvpn"
        servers = [s for s in servers if s['type'] == vpn_type]
        console.print(f"[bold][green][+][/bold][/green] Filtering for VPN type: [magenta]{args.vpn_type.lower()}[/magenta] [[green]{len(servers)}[/green]]")

    if not servers:
        console.print("[red][bold][!][/red][/bold] No servers available after filtering. Exiting...")
        return

    results_queue = Queue()
    semaphore = threading.Semaphore(args.max_concurrent_pings)
    threads = []

    with Progress(
        TextColumn("{task.description}"),
        BarColumn(),
        TextColumn("{task.completed}/{task.total}"),
        TimeElapsedColumn(),
        TimeRemainingColumn(),
    ) as progress:
        progress_task_id = progress.add_task("Pinging servers", total=len(servers))
        
        for server in servers:
            t = threading.Thread(target=ping_server, args=(server, results_queue, semaphore, progress_task_id, progress))
            t.start()
            threads.append(t)
        for t in threads:
            t.join()

    if results_queue.empty():
        console.print("[red]No ping results were obtained.[/red]")
        return

    results = sorted([results_queue.get() for _ in range(results_queue.qsize())], key=lambda x: x[0])
    best_ping, best_server = results[0]

    console.print("\n" + "=" * 80 + "\n", style="bold green")

    table = Table(title="Fastest Mullvad Server", style="bold blue")
    table.add_column("Hostname", style="cyan")
    table.add_column("Ping Time (ms)", justify="right", style="green")
    table.add_column("Country", style="magenta")
    table.add_column("City", style="yellow")
    table.add_column("Provider", style="red")

    table.add_row(
        f"{best_server['hostname']}.mullvad.net",
        f"{best_ping:.3f}",
        f"{best_server.get('country_name', 'Unknown')}",
        f"{best_server.get('city_name', 'Unknown')}",
        f"{best_server.get('provider', 'Unknown')}"
    )

    console.print(table)

    if args.show_next_fastest is not None:
        console.print("\n" + "=" * 80 + "\n", style="bold green")
        table_next_fastest = Table(title=f"Next {args.show_next_fastest} Fastest Mullvad Servers", style="bold blue")
        table_next_fastest.add_column("Hostname", style="cyan")
        table_next_fastest.add_column("Ping Time (ms)", justify="right", style="green")
        table_next_fastest.add_column("Country", style="magenta")
        table_next_fastest.add_column("City", style="yellow")
        table_next_fastest.add_column("Provider", style="red")

        for ping, server in results[1:int(f"{(args.show_next_fastest + 1)}")]:
            table_next_fastest.add_row(
                f"{server['hostname']}.mullvad.net",
                f"{ping:.3f}",
                f"{server.get('country_name', 'Unknown')}",
                f"{server.get('city_name', 'Unknown')}",
                f"{server.get('provider', 'Unknown')}"
            )

        console.print(table_next_fastest)

    console.print("\n" + "=" * 80 + "\n", style="bold green")
    console.print("[bold][green][+][/bold][/green] All tasks complete. Exiting...\n")

if __name__ == "__main__":
    args = parse_args()
    find_best_mullvad_server(args)
