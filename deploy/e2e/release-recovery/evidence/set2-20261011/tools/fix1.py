import sys
p = sys.argv[1]
s = open(p, encoding="utf-8").read()
a = s.index("rep('''        self.current[\"queue_typo\"]")
b = s.index("rep('''            if failed:")
s = s[:a] + s[b:]
old = """                self.current.setdefault("cron_texts", {})[kind] = self.catalogue(
                    None, 0, {}, None) or {
                    key: {language: self.translator.text(key, {"detail": said, "user": user}, language=language)
                          for language in ("en", "tr")}
                    for key in ("cron.unknown", "cron.unknown." + str(record["answer_cause"] or "none"), "cron.unknown.said")
                    if key in self.translator.catalog["en"]}
"""
new = """                self.current.setdefault("cron_texts", {})[kind] = {
                    key: {language: self.translator.text(key, {"detail": said, "user": user}, language=language)
                          for language in ("en", "tr")}
                    for key in ("cron.unknown", "cron.unknown." + str(record["answer_cause"] or "none"), "cron.unknown.said")
                    if key in self.translator.catalog["en"]}
"""
assert s.count(old) == 1
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="\n").write(s)
print("fixed")
