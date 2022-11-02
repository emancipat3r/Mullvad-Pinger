#!/usr/bin/python3

import os
import subprocess
import re
import json

domains = []
time = []
domains_time = []
i = 0

# GET ALL MULLVAD VPN DOMAINS
html = requests.get('https://api-www.mullvad.net/www/relays/all/')
data = html.json()
json_object = json.dumps(data, indent = 4)
python_object = json.loads(json_object)
for dict in python_object:
	for key, value in dict.items():
		if key == 'hostname':
			print(value + ".mullvad.net")


print('[*] Pinging all the VPN domains')
for i in range(1,201):
    output = subprocess.Popen(['timeout', '1', 'ping', '-c', '1', 
                               "us{}-wireguard.mullvad.net".format(i)], 
                              stdout=subprocess.PIPE)
    text = output.communicate()[0].decode('utf-8')
    if i == 50:
        print('[*] Pinging progress - 25%')
    if i == 100:
        print('[*] Pinging progress - 50%')
    if i == 150:
        print('[*] Pinging progress - 75%')
    if i == 200:
        print('[*] Pinging complete - calculating...')

    for line in text.splitlines():
        if line.startswith('-'):
            for word in line.split(' '):
                if word.startswith('us'):
                    domains.append(word)
        if line.startswith('64'):
            for word in line.split(' '):
                if word.startswith('time'):
                    for section in word.split('='):
                        if section != 'time':
                            time.append(float(section))

for i in range(len(time)):
    a = []
    a.append(time[i])
    a.append(domains[i])
    domains_time.append(a)

domains_time.sort()
print(f'[*] Best server is {domains_time[0][1]} - time = {domains_time[0][0]}ms')
