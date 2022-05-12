import requests
import json

apiKey = "eyJhbGciOiJIUzI1NiJ9.eyJ0aWQiOjE2MDE2OTYwMywidWlkIjoyOTk1NzIwMCwiaWFkIjoiMjAyMi0wNS0xMlQwODoxMjoyMi4yODhaIiwicGVyIjoibWU6d3JpdGUiLCJhY3RpZCI6MTE4NzU5MjIsInJnbiI6InVzZTEifQ.YmQdT2lnqe6HJOMA_PXthXPaziqoYlsbyZ5Bi52pkj4"
apiUrl = "https://api.monday.com/v2"
headers = {"Authorization" : apiKey}

query2 = '{boards(limit:1) { name id description items { name column_values{title id type text } } } }'
data = {'query' : query2}

r = requests.post(url=apiUrl, json=data, headers=headers) # make request
print(r.json())