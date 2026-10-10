"""set7: the browser's HTTP sequence of one cell, compact. usage: netseq.py NETWORK_JSONL [FROM_ISO] [TO_ISO]
One line per finished request: the time it finished, method, path without the query's values, status or the
browser's error text, and whether the service worker answered."""
import json
import re
import sys

path = sys.argv[1]
start = sys.argv[2] if len(sys.argv) > 2 else ""
end = sys.argv[3] if len(sys.argv) > 3 else "9999"
requested = {}
for line in open(path, encoding="utf-8"):
    e = json.loads(line)
    if e["ev"] == "request":
        requested[e["id"]] = e["at"]
        continue
    if not (start <= e["at"] <= end):
        continue
    url = re.sub(r"=([^&]*)", "=...", e.get("url", ""))
    if url.startswith("/assets/") or url.startswith("data:"):
        url = "/assets/..." if url.startswith("/assets/") else url[:20]
    if e["ev"] == "navigated":
        print(f'{e["at"][11:23]} NAVIGATED {e["url"]}')
    elif e["ev"] == "response":
        sent = requested.get(e.get("id"), "")[11:23]
        print(f'{e["at"][11:23]} {e["method"]:6} {url:60} {e["status"]}{" (sw)" if e.get("sw") else ""}  sent {sent}')
    elif e["ev"] == "failed":
        sent = requested.get(e.get("id"), "")[11:23]
        print(f'{e["at"][11:23]} {e["method"]:6} {url:60} FAILED {e.get("error")}  sent {sent}')
