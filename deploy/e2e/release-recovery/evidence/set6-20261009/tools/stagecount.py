# set6: one line from a `set6_redact.py count` report (read-only). usage: stagecount.py COUNT_REPORT_JSON
import json
import sys

d = json.load(open(sys.argv[1]))
print(len(d["files_that_would_change"]), "file(s) would change,", len(d["files_that_would_change_inside_base64_text"]),
      "inside base64 text, values known:", d["distinct_values_known"])
