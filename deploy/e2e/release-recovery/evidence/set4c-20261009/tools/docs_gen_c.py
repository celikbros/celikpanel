# set4c: append the entry of 2026-10-09 about postconf value reads to the four documents. Idempotent: each document
# is taken from HEAD (`git show HEAD:<path>`) and the entry is appended to that. The Agent's sentence is read from
# the source (cmd/agent/postconf_value.go), never typed here. Run from the repository root.
import io
import os
import re
import subprocess
import textwrap

HERE = os.path.dirname(os.path.abspath(__file__))
NL = chr(10)


def read(path):
    return io.open(path, encoding='utf-8').read().replace(chr(13) + NL, NL)


def unread_sentence():
    source = read('cmd/agent/postconf_value.go')
    body = source[source.index('func (e *postconfUnreadError) Error() string {'):]
    body = body[:body.index(NL + '}')]
    parts = []
    for literal, field in re.findall(r'"([^"]*)"|e\.(setting|command)', body):
        parts.append('{' + field + '}' if field else literal)
    text = ''.join(parts)
    assert chr(92) not in text and '{setting}' in text and '{command}' in text, text
    return text


S = {'unread': unread_sentence()}


def wrap(text, first='', rest='', width=79):
    return NL.join(textwrap.wrap(text, width=width, initial_indent=first, subsequent_indent=rest,
                                 break_long_words=False, break_on_hyphens=False))


def render(template):
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


def append(path, *templates):
    shown = subprocess.run(['git', 'show', 'HEAD:' + path], capture_output=True, check=True).stdout
    head = shown.decode('utf-8').replace(chr(13) + NL, NL)
    entry = render((NL + NL).join(read(os.path.join(HERE, name)).strip(NL) for name in templates))
    long = [line for line in entry.split(NL) if len(line) > 79 and not line.startswith('#') and ' ' in line.strip()[4:]]
    assert not long, long[:3]
    io.open(path, 'w', encoding='utf-8', newline=NL).write(head.rstrip(NL) + NL + NL + entry)
    print(path, len(entry.split(NL)), 'lines appended')


append('docs/OPERATION-GUIDANCE.md', 'guidance.en.txt')
append('docs/OPERATION-GUIDANCE.tr.md', 'guidance.tr.txt')
append('docs/RESILIENCE-CONTRACT.md', 'contract.en.1.txt', 'contract.en.2.txt')
append('docs/RESILIENCE-CONTRACT.tr.md', 'contract.tr.1.txt', 'contract.tr.2.txt')
print('sentence:', S['unread'])
