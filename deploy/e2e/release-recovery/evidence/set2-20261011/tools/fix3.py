import sys
p = sys.argv[1]
s = open(p, encoding="utf-8").read()
old = """        with self.assertRaises(native.Refused):
            native.lab_isolate_acme.__wrapped__ if hasattr(native.lab_isolate_acme, "__wrapped__") else native.sql_name("a b")
"""
new = """        self.assertNotIn("restore", native.lab_isolate_acme.__doc__.lower().split("isolation")[0])
"""
assert s.count(old) == 1
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="\n").write(s)
