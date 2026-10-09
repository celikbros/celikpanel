# set4b: join the README parts and put the Debian 13 readings and the verification numbers into their places.
import io
import os

HERE = os.path.dirname(os.path.abspath(__file__))
NL = chr(10)


def read(name):
    return io.open(os.path.join(HERE, name), encoding='utf-8').read().replace(chr(13) + NL, NL)


values = {
    'CELL_D13': "| remeasure/set4b-debian13/run-a | s4b-d13-a | c | 15:42:04-15:55:03 | `complete-for-review` | M0, M10, R10, M2 passed |",
    'D13_ANSWER': "`200`, `success: true`, `outcome: verified`, `applied: stopped`, with `note`",
    'D13_NOTE': "`code: SERVICE_ACTION_NOTE`, `reason: unit_marked_failed_config`, `vars.failed_unit: postfix.service`, `result: exit-code`, `command: sudo systemctl reset-failed postfix.service`, the same `detail`; `error` is the documented sentence",
    'D13_UNIT': "`postfix.service` `failed`, `Result=exit-code`, after the answer and 8 seconds later (M10) and at the later reading of each R10 Stop; `postfix@-.service` is not a unit of this platform",
    'D13_RESET': "none, by the same two readings",
    'D13_START': "`200`, `applied: started`, the master runs, no unit `failed`, ports 25 and 587 answer (M10); `200` and the master runs after each R10 Stop",
    'D13_TRACE': ("**What the Agent did, Debian 13 (kernel trace of the three R10 Stops).** `systemctl stop postfix` ran for 1066 / 1083 / "
                  "1067 ms: there the unit that is stopped runs the daemon, and the command returns after the unit's whole stop, Postfix's "
                  "one-second pause included. One `postconf -h queue_directory` followed (the master had ended; no second look was "
                  "needed), then one `systemctl` 54 / 63 / 61 ms after the stop command had returned (by the order of the source, the "
                  "reading of `postfix.service`; `postfix@-.service` was not read as loaded before the stop and is not read after it), "
                  "then `postfix` for about one second (`postfix check`, for the note's line). The timeline's own fields are empty in this "
                  "cell: they are taken from `postfix@-.service`, which Debian does not have; the moments above are from "
                  "`runs[].timeline.agent_programs`. Debian GNU/Linux 13 (trixie), kernel 6.12.105+deb13-cloud-amd64, Postfix 3.10.13-0+deb13u1, systemd "
                  "257.13-1~deb13u1."),
    'D13_IMPORT': "the same answer: `imported: [domain, files, mail, forwarders, database:s4imp_app]`, `not_imported: []`, `left_out: [dns]`, `status: active`",
    'D13_DNS': "the same",
}
text = read('README.1.md') + read('README.2.md') + read('README.3.md') + read('README.4.md') + read('README.5.md')
for key, value in values.items():
    assert '{{' + key + '}}' in text, key
    text = text.replace('{{' + key + '}}', value)
for key, value in (line.split('=', 1) for line in read('README.values.txt').split(NL) if '=' in line):
    assert '{{' + key + '}}' in text, key
    text = text.replace('{{' + key + '}}', value)
assert '{{' not in text, text[text.index('{{'):text.index('{{') + 60]
io.open(os.path.join(HERE, 'README.md'), 'w', encoding='utf-8', newline=NL).write(text)
print('README.md', len(text.split(NL)), 'lines')
