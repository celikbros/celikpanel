import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import test from 'node:test';
import React from 'react';
import Renderer from 'react-test-renderer';
import ts from 'typescript';

// Real-browser inspection, 2026-10-08: the Panel's secure address was drawn with
// "break-all" and broke inside a word ("panel.examp / le.com") and, on a phone,
// inside the scheme ("http / s://"). An address is now one piece that moves to
// the next line whole; when it is wider than its line it breaks before a dot or
// the port (or at a hyphen, where a browser breaks by itself). What a line
// really does needs a browser (web/tools/browser-inspect, scenario "address");
// this holds the markup that gives the browser those break points.
//
// Gercek tarayici incelemesi, 8 Ekim 2026: panelin guvenli adresi sozcugun ve
// semanin icinden bolunuyordu. Adres artik tek parcadir; satirindan genisse
// noktadan ya da porttan once bolunur.
const require = createRequire(import.meta.url);
const reactURL = pathToFileURL(require.resolve('react')).href;
const read = path => readFileSync(new URL(path, import.meta.url), 'utf8');
const compiled = ts.transpileModule(read('../src/components/AddressLink.tsx'), { compilerOptions: { jsx: ts.JsxEmit.React, module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020 } }).outputText;
const { AddressLink } = await import('data:text/javascript;base64,' + Buffer.from(`import React from '${reactURL}';\n` + compiled.replace(/from ['"]react['"]/, `from '${reactURL}'`)).toString('base64'));

// The text of the link with "|" where the browser may break it.
function pieces(address) {
  const link = Renderer.create(React.createElement(AddressLink, { href: `${address}/setup`, address })).toJSON();
  assert.equal(link.type, 'a');
  assert.equal(link.props.href, `${address}/setup`);
  const text = node => typeof node === 'string' ? node : node.type === 'wbr' ? '|' : (node.children ?? []).map(text).join('');
  return { link, text: link.children.map(text).join('') };
}

test('the break points an address is given are before each dot and before the port', () => {
  assert.equal(pieces('https://panel.example.com:4812').text, 'https://panel|.example|.com|:4812');
  assert.equal(pieces('https://panel.example.com').text, 'https://panel|.example|.com');
  assert.equal(pieces('https://a.very-long-label.example.co.uk:2083/').text, 'https://a|.very-long-label|.example|.co|.uk|:2083|/');
  // Removing the break points gives back exactly the address.
  for (const address of ['https://panel.example.com:4812', 'http://203.0.113.10:2083', 'https://xn--bcher-kva.example']) {
    assert.equal(pieces(address).text.replaceAll('|', ''), address);
  }
});

test('the scheme is one unbreakable piece and the link moves to the next line whole', () => {
  const { link } = pieces('https://panel.example.com:4812');
  const scheme = link.children[0];
  assert.equal(scheme.type, 'span');
  assert.equal(scheme.props.className, 'whitespace-nowrap');
  assert.deepEqual(scheme.children, ['https://']);
  // One inline block: beside a sentence it wraps as a unit, and only an address
  // wider than its own line uses the break points above.
  assert.match(link.props.className, /\binline-block max-w-full\b/);
  assert.doesNotMatch(link.props.className, /break-all|break-words/);
  assert.match(link.props.className, /\[overflow-wrap:anywhere\]/, 'a single label wider than the line still has to wrap');
});

test('no screen that shows the secure address draws it with break-all', () => {
  for (const file of ['AccessHold.tsx', 'PanelAddressHint.tsx']) assert.doesNotMatch(read(`../src/components/${file}`), /break-all/, file);
  for (const [file, from] of [['RecoveryAccess.tsx', 'export function RecoveryAccess('], ['ServerSetup.tsx', 'function SetupHandoverAddress(']]) {
    const source = read(`../src/components/${file}`);
    const body = source.slice(source.indexOf(from), source.indexOf('\n}\n', source.indexOf(from)));
    assert.match(body, /<AddressLink /, file);
    assert.doesNotMatch(body, /break-all/, file);
  }
});
