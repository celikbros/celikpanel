import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const ui = readFileSync(new URL('../src/components/ui.tsx', import.meta.url), 'utf8');
const css = readFileSync(new URL('../src/index.css', import.meta.url), 'utf8');

// The tokens are space-separated RGB triplets, declared once per palette block.
// Reading them here rather than restating them is the point: the assertion below
// is about the palettes the product actually ships, in every skin and both
// themes, not about four numbers copied into a test.
function paletteBlocks(source) {
  const blocks = [];
  const selector = /(^|\n)([^\n{}]+)\{([^}]*)\}/g;
  let match;
  while ((match = selector.exec(source)) !== null) {
    const name = match[2].trim();
    const body = match[3];
    if (!body.includes('--')) continue;
    const tokens = {};
    for (const declaration of body.matchAll(/--([a-z0-9-]+):\s*(\d+)\s+(\d+)\s+(\d+)\s*;/g)) {
      tokens[declaration[1]] = [
        Number(declaration[2]), Number(declaration[3]), Number(declaration[4]),
      ];
    }
    if (Object.keys(tokens).length > 0) blocks.push({ name, tokens });
  }
  return blocks;
}

function channel(value) {
  const scaled = value / 255;
  return scaled <= 0.04045 ? scaled / 12.92 : ((scaled + 0.055) / 1.055) ** 2.4;
}

function luminance([r, g, b]) {
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

function contrast(a, b) {
  const [high, low] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (high + 0.05) / (low + 0.05);
}

const blocks = paletteBlocks(css);
const root = blocks.find((block) => block.name === ':root');
const dark = blocks.find((block) => block.name === '.dark');
assert.ok(root && dark, 'the light and dark token blocks must both be readable');

// Every palette a skin can produce, resolved the way the cascade resolves it: a
// skin block overrides only what it declares and inherits the rest.
function palettes() {
  const resolved = [
    { name: 'light', tokens: { ...root.tokens } },
    { name: 'dark', tokens: { ...root.tokens, ...dark.tokens } },
  ];
  for (const block of blocks) {
    if (block.name === ':root' || block.name === '.dark') continue;
    const isDark = block.name.startsWith('.dark');
    const base = isDark ? { ...root.tokens, ...dark.tokens } : { ...root.tokens };
    resolved.push({ name: block.name, tokens: { ...base, ...block.tokens } });
  }
  return resolved;
}

// R-047's leftover. A disabled primary was the filled block at half opacity, so
// its own label washed out against it: 2.1:1 in light and 2.4:1 in dark, on the
// control whose whole job in a refusal is to name the action being refused. The
// pairing it now uses has to clear AA in every palette the product ships,
// because a refusal an operator cannot read explains nothing.
//
// 9 Oct 2026: the recessed fill that replaced the wash was legible, and in the
// dark theme it was also a navy block with a light label - a disabled primary
// looked more like a button to press than the enabled one beside it. A control
// that cannot be used has no fill now, so its label stands on whatever the
// button stands on. The label therefore has to clear AA on every one of those
// surfaces (before: on surface-2 only). The dashed outline that says "not
// available" is drawn in the label's own colour (border-current), so the same
// measurement covers it: the border token of the imitation skins is as low as
// 1.1:1 on their dark surfaces and would have left a label with no outline.
const standsOn = ['bg', 'surface', 'surface-2', 'surface-subtle'];

test('a disabled button label is legible on every surface a button stands on, in every palette', () => {
  for (const palette of palettes()) {
    const fg = palette.tokens['fg-muted'];
    assert.ok(fg, `${palette.name} must resolve fg-muted`);
    for (const surface of standsOn) {
      const bg = palette.tokens[surface];
      assert.ok(bg, `${palette.name} must resolve ${surface}`);
      const ratio = contrast(fg, bg);
      assert.ok(
        ratio >= 4.5,
        `${palette.name}: disabled label on ${surface} is ${ratio.toFixed(2)}:1, want at least 4.5:1`,
      );
    }
  }
});

// A working button is the one place the recessed fill remains: it is busy, not
// unavailable. Its label stands on surface-2, as every disabled label did before.
test('a working button label is legible on its recessed fill in every palette', () => {
  for (const palette of palettes()) {
    const ratio = contrast(palette.tokens['fg-muted'], palette.tokens['surface-2']);
    assert.ok(ratio >= 4.5, `${palette.name}: working label is ${ratio.toFixed(2)}:1, want at least 4.5:1`);
  }
});

// What makes an enabled primary and a disabled one two different things is the
// fill: the enabled one is a block of the primary colour, the disabled one has
// none and shows the surface it stands on. That difference is only real if the
// primary colour itself can be told from that surface. Measured, not assumed:
// in the product's own light and dark themes the fill clears the 3:1 floor for
// a graphic that carries meaning (WCAG 1.4.11) on every surface a button
// stands on (lowest measured 9 Oct 2026: 7.8:1, dark, on surface-2). The
// imitation skins bring their own primary; the lowest is 2.87:1 (aapanel,
// light, on surface-2), so they are held to "no closer than today" - there the
// dashed outline, pinned below, is what carries the difference.
test('an enabled primary fill can be told from the unfilled disabled button, on every surface, in every palette', () => {
  for (const palette of palettes()) {
    const fill = palette.tokens.primary;
    assert.ok(fill, `${palette.name} must resolve primary`);
    const own = palette.name === 'light' || palette.name === 'dark';
    for (const surface of standsOn) {
      const ratio = contrast(fill, palette.tokens[surface]);
      const floor = own ? 3 : 2.8;
      assert.ok(
        ratio >= floor,
        `${palette.name}: the enabled primary fill is ${ratio.toFixed(2)}:1 against ${surface}, want at least ${floor}:1 - `
        + 'otherwise a filled and an unfilled button look alike',
      );
    }
  }
});

// The disabled treatment is one rule on the base, not a wash over each
// variant's own skin. One rule, so no variant can drift back to an unreadable
// disabled state, and pointer events are off so no variant's hover colour can
// repaint a control that does nothing.
test('the disabled state is one rule by shape and colour, not a wash over three skins', () => {
  const styles = ui.slice(ui.indexOf('const styles = {'), ui.indexOf('}[variant]'));
  assert.doesNotMatch(styles, /disabled:/,
    'a variant that owns its own disabled skin is a variant that can drift');

  const off = ui.slice(ui.indexOf('const off = loading'), ui.indexOf('return (', ui.indexOf('const off = loading')));
  assert.match(off, /loading\s*\?\s*'disabled:border-transparent disabled:bg-surface-2'\s*:\s*'disabled:border-dashed disabled:border-current disabled:bg-transparent'/,
    'unavailable has no fill and a dashed outline in the label colour; only a working button keeps the recessed fill');

  const base = ui.slice(ui.indexOf('inline-flex items-center gap-1.5'), ui.indexOf('${styles}'));
  assert.match(base, /disabled:text-fg-muted/);
  assert.match(base, /disabled:pointer-events-none/,
    'a disabled fill must not light up under the cursor');
  assert.match(base, /\$\{off\}/, 'the shape rule is applied to every variant');
  assert.doesNotMatch(base, /disabled:opacity-50/,
    'the wash is what put a disabled label below AA against its own fill');
  assert.doesNotMatch(base, /disabled:bg-surface-2/,
    'a fill on an unavailable control is what read as a call to action in the dark theme');
});
