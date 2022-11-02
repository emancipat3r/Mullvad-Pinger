#!/bin/python3

import requests
import json

html = requests.get('https://api-www.mullvad.net/www/relays/all/')
# Data var contains the blob of json from the Mullvad API.
# Python3 interprets the JSON array as a list because it has a leading
# and trailing bracket
data = html.json()
# Process blob into json_object var - interpretted by Python3 as a
# string type object. We tell it to indent with 4 spaces.
json_object = json.dumps(data, indent = 4)
# In order to work with the values we need to convert the JSON object
# to a python object using the json.loads() function.
python_object = json.loads(json_object)
# Drill down into list with nesting dicts
for dict in python_object:
	for key, value in dict.items():
		if key == 'hostname':
			print(value + ".mullvad.net")
