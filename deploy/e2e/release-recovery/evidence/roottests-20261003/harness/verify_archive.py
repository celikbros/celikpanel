#!/usr/bin/env python3
"""Compare every regular file of a tar against the committed blob ids (git ls-tree -r)."""
import hashlib, sys, tarfile
tree = {}
for line in open(sys.argv[1], encoding="utf-8"):
    oid, path = line.rstrip("\n").split("\t", 1)
    tree[path] = oid
seen = 0; bad = []
with tarfile.open(sys.argv[2]) as tf:
    for m in tf:
        if not m.isfile():
            continue
        data = tf.extractfile(m).read()
        oid = hashlib.sha1(b"blob %d\0" % len(data) + data).hexdigest()
        seen += 1
        if tree.get(m.name) != oid:
            bad.append(m.name)
missing = set(tree) - {n for n in tree}
print(f"archive={sys.argv[2]} files={seen} tree_blobs={len(tree)} mismatches={len(bad)}")
for b in bad[:10]:
    print("MISMATCH", b)
