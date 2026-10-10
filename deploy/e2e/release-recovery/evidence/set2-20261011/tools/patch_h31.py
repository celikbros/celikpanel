"""set2 H31 (set2-ubuntu run-a): a service action can be refused before anything is sent because the server setup
(which waits for DNS for the whole life of the isolated guest) is momentarily at work: `409 server_setup_busy`. That
refusal changed nothing; the owner waits and presses again. The refusals are recorded, the repeated action is judged."""
import sys
p = sys.argv[1] + "/settings_writes_trial.py"
s = open(p, encoding="utf-8", newline="").read()
old = '''        answer = self.call(label, "POST", "/api/v1/service/action", {"name": service, "action": action}, timeout=240,
                           kind="service")
'''
new = '''        busy = []
        for attempt in range(1, 10):
            answer = self.call(label + ("" if attempt == 1 else f", pressed again ({attempt})"), "POST", "/api/v1/service/action",
                               {"name": service, "action": action}, timeout=240, kind="service")
            refused = answer["_parsed"] if isinstance(answer["_parsed"], dict) else {}
            if answer["status"] != 409 or refused.get("code") not in BUSY_CODES:
                break
            # H31: refused before anything was sent; the owner waits and presses again.
            busy.append({"at": answer["at"], "code": refused.get("code"), "error": refused.get("error")})
            time.sleep(6)
'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''                  "said": said, "truth_action_took_effect": bool(truth), "matches": matches,
'''
new = '''                  "said": said, "truth_action_took_effect": bool(truth), "matches": matches, "refused_while_busy": busy,
'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''PG_REFUSED_VALUE = "eight-megabytes"'''
new = '''PG_REFUSED_VALUE = "eight-megabytes"
# Refusals of a service action that are sent before anything is done (the setup or another package task is at work).
BUSY_CODES = ("server_setup_busy", "HOST_MUTATION_BUSY", "SERVICE_OPERATION_BUSY")'''
assert s.count(old) == 1
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="").write(s)
print("H31 patched")
