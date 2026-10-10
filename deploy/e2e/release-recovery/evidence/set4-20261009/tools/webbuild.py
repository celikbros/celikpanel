# set4: which of this run's archives hold the update card's new sentences in their web build (read on the QEMU host,
# from the archives the cells installed; nothing is extracted to disk). usage: webbuild.py ARTIFACTS_JSON...
import json
import sys
import tarfile

NEEDLES = ("This version was already tried on this server and rolled back",
           "Bu sürüm bu sunucuda daha önce denendi ve geri alındı",
           "The server recorded no more specific cause for that attempt.",
           "Starting it again runs the same update.",
           "This version already failed on this server")
out = {}
for path in sys.argv[1:]:
    document = json.load(open(path))
    for role in ("baseline", "good", "defective", "startcheck", "realstart"):
        if role not in document:
            continue
        item = document[role]
        counts, files = {n: 0 for n in NEEDLES}, 0
        with tarfile.open(item["archive"], "r:gz") as bundle:
            for member in bundle:
                if not member.isfile() or "/web/dist/" not in "/" + member.name or not member.name.endswith((".js", ".html", ".json", ".mjs")):
                    continue
                files += 1
                data = bundle.extractfile(member).read()
                for needle in NEEDLES:
                    if needle.encode() in data:
                        counts[needle] += 1
        out[path.split("/")[-2] + ":" + role] = {"version": item["version"], "commit": item["commit"], "archive_sha256": item["sha256"],
                                                  "web_dist_files_read": files, "files_containing": counts}
print(json.dumps(out, indent=1, sort_keys=True, ensure_ascii=False))
