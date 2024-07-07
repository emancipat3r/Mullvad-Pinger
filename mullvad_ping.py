#!/usr/bin/env python3

import requests
import subprocess
import threading
from queue import Queue
import argparse
from rich.console import Console, Group
from rich.live import Live
from rich.panel import Panel
from rich.progress import Progress, BarColumn, TextColumn, TimeElapsedColumn, TimeRemainingColumn
from rich.table import Table
from rich.logging import RichHandler
import logging
from io import StringIO

# Set up rich logging
console = Console()
log_stream = StringIO()
logging.basicConfig(level="NOTSET", format="%(message)s", datefmt="[%X]", handlers=[RichHandler(console=console, show_path=False)])
log = logging.getLogger("rich")

class CustomHandler(logging.StreamHandler):
    def __init__(self, stream=None):
        super().__init__(stream)
        self.log_capture_string = StringIO()

    def emit(self, record):
        super().emit(record)
        log_entry = self.format(record)
        self.log_capture_string.write(log_entry + "\n")
        self.log_capture_string.seek(0)

    def get_log_text(self):
        return self.log_capture_string.getvalue()

log_capture_handler = CustomHandler()
log.addHandler(log_capture_handler)

def parse_args():
    parser = argparse.ArgumentParser(description='Ping Mullvad VPN servers to find the best one.')
    parser.add_argument('--max-concurrent-pings', type=int, default=10, help='Maximum number of concurrent pings. Default is 10.')
    parser.add_argument('--exclude-country', type=str, help='Exclude servers from this country.')
    parser.add_argument('--exclude-city', type=str, help='Exclude servers from this city.')
    parser.add_argument('--exclude-state', type=str, help='Exclude servers from this state abbreviation.')
    parser.add_argument('--show-next-fastest', action='store_true', help='Show the next 10 fastest pinging servers.')
    return parser.parse_args()

def fetch_mullvad_servers():
    try:
        response = requests.get('https://api-www.mullvad.net/www/relays/all/')
        response.raise_for_status()
        return response.json()
    except requests.RequestException as e:
        log.error(f"Error fetching server list: {e}")
        return []

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
    if not servers:
        log.error("No servers fetched, exiting.")
        return

    # Filter out servers based on user input
    if args.exclude_country:
        servers = [s for s in servers if s.get('country_name', '').lower() != args.exclude_country.lower()]
    if args.exclude_city:
        servers = [s for s in servers if s.get('city_name', '').lower() != args.exclude_city.lower()]
    if args.exclude_state:
        servers = [s for s in servers if args.exclude_state.lower() not in s.get('city_name', '').lower()]

    if not servers:
        log.error("No servers available after filtering.")
        return

    results_queue = Queue()
    semaphore = threading.Semaphore(args.max_concurrent_pings)
    threads = []

    progress = Progress(
        TextColumn("{task.description}"),
        BarColumn(),
        TextColumn("{task.completed}/{task.total}"),
        TimeElapsedColumn(),
        TimeRemainingColumn(),
    )
    progress_task_id = progress.add_task("Pinging servers", total=len(servers))

    with Live(console=console, refresh_per_second=4) as live:
        for server in servers:
            t = threading.Thread(target=ping_server, args=(server, results_queue, semaphore, progress_task_id, progress))
            t.start()
            threads.append(t)
        
        for t in threads:
            t.join()

        if results_queue.empty():
            log.error("No ping results were obtained.")
            live.update(
                Group(
                    Panel(log_capture_handler.get_log_text()),
                    Panel(progress),
                    Panel("No results available.")
                )
            )
            return

        log.info("Sorting the results...")
        results = sorted([results_queue.get() for _ in range(results_queue.qsize())], key=lambda x: x[0])
        best_ping, best_server = results[0]

        fastest_server_table = Table()
        fastest_server_table.add_column("Hostname", style="cyan")
        fastest_server_table.add_column("Ping Time (ms)", justify="right", style="green")
        fastest_server_table.add_column("Country", style="magenta")
        fastest_server_table.add_column("City", style="yellow")
        fastest_server_table.add_row(
            f"{best_server['hostname']}.mullvad.net",
            f"{best_ping:.3f}",
            f"{best_server.get('country_name', 'Unknown')}",
            f"{best_server.get('city_name', 'Unknown')}"
        )

        results_group = [fastest_server_table]

        if args.show_next_fastest and len(results) > 1:
            log.info("Crunching numbers to find the next 10 fastest servers...")

            next_fastest_table = Table()
            next_fastest_table.add_column("Hostname", style="cyan")
            next_fastest_table.add_column("Ping Time (ms)", justify="right", style="green")
            next_fastest_table.add_column("Country", style="magenta")
            next_fastest_table.add_column("City", style="yellow")

            for ping, server in results[1:11]:
                next_fastest_table.add_row(
                    f"{server['hostname']}.mullvad.net",
                    f"{ping:.3f}",
                    f"{server.get('country_name', 'Unknown')}",
                    f"{server.get('city_name', 'Unknown')}"
                )

            results_group.append(next_fastest_table)

        live.update(
            Group(
                Panel(log_capture_handler.get_log_text()),
                Panel(progress),
                Panel(Group(*results_group))
            )
        )

if __name__ == "__main__":
    args = parse_args()
    find_best_mullvad_server(args)