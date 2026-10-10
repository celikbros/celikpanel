# set4b: append the entries of 2026-10-09 to the four documents. Idempotent: each document is taken from HEAD
# (`git show HEAD:<path>`; nobody else has it modified in the working tree) and the entry is appended to that.
# The sentences are read from the sources, never typed here. Run from the repository root:
#   python docs_gen.py [--final]     (REMEASURE.txt in the script's folder holds the native re-measurement paragraphs)
import io
import os
import re
import subprocess
import sys
import textwrap

HERE = os.path.dirname(os.path.abspath(__file__))
NL = chr(10)


def read(path):
    return io.open(path, encoding='utf-8').read().replace(chr(13) + NL, NL)


def catalogue(path, key):
    found = re.search(r"^ {4}'" + re.escape(key) + r"': (\"(?:[^\"\\]|\\.)*\"|'(?:[^'\\]|\\.)*'),$", read(path), re.M)
    assert found, (path, key)
    text = found.group(1)
    value = text[1:-1].replace(chr(92) + "'", "'").replace(chr(92) + '"', '"')
    assert chr(92) not in value, value
    return value


def go_note(reason_constant):
    source = read('cmd/panel/service_action_outcome.go')
    body = source[source.index('func serviceActionNoteMessage(reason string) string {'):]
    block = body[body.index('case ' + reason_constant + ':'):]
    marks = (NL + chr(9) + 'case ', NL + chr(9) + '}')
    block = block[:min(block.index(mark, 5) for mark in marks if mark in block[5:])]
    parts = re.findall(r'"([^"]*)"', block)
    assert parts and chr(92) not in ''.join(parts), reason_constant
    return ''.join(parts)


SERVER = {'en': 'web/src/i18n/screens/server/en.ts', 'tr': 'web/src/i18n/screens/server/tr.ts'}
SCREENS = {'en': 'web/src/i18n/screens/en.ts', 'tr': 'web/src/i18n/screens/tr.ts'}
S = {
    'api_not_settled': go_note('serviceActionNoteUnitNotSettled'),
    'api_not_read': go_note('serviceActionNoteUnitNotRead'),
}
for lang in ('en', 'tr'):
    S['settled_' + lang] = catalogue(SERVER[lang], 'services.action.note.unit_not_settled')
    S['unread_' + lang] = catalogue(SERVER[lang], 'services.action.note.unit_state_not_read')
    S['recovered_' + lang] = catalogue(SERVER[lang], 'panelUpdate.previousAttempt.recovered')
    S['nothing_' + lang] = catalogue(SCREENS[lang], 'import.step.nothing')

REMEASURE = {}
current = None
for line in read(os.path.join(HERE, 'REMEASURE.txt')).split(NL):
    if line.startswith('== '):
        current = line[3:].strip()
        REMEASURE[current] = []
    elif current:
        REMEASURE[current].append(line)
REMEASURE = {k: NL.join(v).strip() for k, v in REMEASURE.items()}


def wrap(text, first='', rest='', width=79):
    return NL.join(textwrap.wrap(text, width=width, initial_indent=first, subsequent_indent=rest,
                                 break_long_words=False, break_on_hyphens=False))


def render(template):
    """Blocks are separated by empty lines. `- ` and `  - ` open a bullet; a block that opens with `~ ` is a list of
    recorded sentences (`~ name` then `LABEL=key` lines); a heading or a table is kept as it is. Bullets that follow
    each other are not separated by an empty line, as in the documents."""
    out = []
    for block in template.strip(NL).split(NL + NL):
        lines = block.split(NL)
        head = lines[0]
        if head.startswith('#') or head.startswith('|'):
            out.append(('raw', block))
            continue
        if head.startswith('~ '):
            rendered = []
            for line in lines:
                if line.startswith('~ '):
                    rendered.append(wrap(line[2:], '- ', '  '))
                else:
                    label, key = line.split('=', 1)
                    rendered.append(wrap(label + ': "' + S[key] + '"', '  ', '  '))
            out.append(('sentences', NL.join(rendered)))
            continue
        text = ' '.join(line.strip() for line in lines)
        if head.startswith('  - '):
            out.append(('bullet', wrap(text[2:], '  - ', '    ')))
        elif head.startswith('- '):
            out.append(('bullet', wrap(text[2:], '- ', '  ')))
        else:
            out.append(('paragraph', wrap(text)))
    text = ''
    for index, (kind, block) in enumerate(out):
        if index:
            text += NL if kind == 'bullet' and out[index - 1][0] == 'bullet' else NL + NL
        text += block
    return text + NL


def fill(text):
    for key, value in REMEASURE.items():
        text = text.replace('{{' + key + '}}', value)
    assert '{{' not in text, re.findall(r'\{\{[^}]*\}\}', text)
    return text


def append(path, template_path):
    shown = subprocess.run(['git', 'show', 'HEAD:' + path], capture_output=True, check=True).stdout
    head = shown.decode('utf-8').replace(chr(13) + NL, NL)
    entry = render(fill(read(os.path.join(HERE, template_path))))
    long = [line for line in entry.split(NL) if len(line) > 79 and not line.startswith('#') and ' ' in line.strip()[4:]
            and not re.match(r'^\*\*.*\*\*$', line)]
    assert not long, long[:3]
    io.open(path, 'w', encoding='utf-8', newline=NL).write(head.rstrip(NL) + NL + NL + entry)
    print(path, len(entry.split(NL)), 'lines appended')


append('docs/OPERATION-GUIDANCE.md', 'guidance.en.txt')
append('docs/OPERATION-GUIDANCE.tr.md', 'guidance.tr.txt')
append('docs/RESILIENCE-CONTRACT.md', 'contract.en.txt')
append('docs/RESILIENCE-CONTRACT.tr.md', 'contract.tr.txt')
if '--final' in sys.argv:
    for path in ('docs/OPERATION-GUIDANCE.md', 'docs/OPERATION-GUIDANCE.tr.md', 'docs/RESILIENCE-CONTRACT.md',
                 'docs/RESILIENCE-CONTRACT.tr.md'):
        assert 'PENDING' not in read(path)[-60000:], path
    print('final: no pending paragraph')
