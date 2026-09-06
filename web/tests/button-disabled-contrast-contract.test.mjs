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
test('a disabled primary button is legible in every palette the product ships', () => {
  for (const palette of palettes()) {
    const fg = palette.tokens['fg-muted'];
    const bg = palette.tokens['surface-2'];
    assert.ok(fg && bg, `${palette.name} must resolve fg-muted and surface-2`);
    const ratio = contrast(fg, bg);
    assert.ok(
      ratio >= 4.5,
      `${palette.name}: disabled primary label is ${ratio.toFixed(2)}:1, want at least 4.5:1`,
    );
  }
});

// The disabled treatment is a token pairing on the base, not a wash over each
// variant's own skin. One rule, so no variant can drift back to an unreadable
// disabled state, and pointer events are off so no variant's hover colour can
// repaint a control that does nothing.
test('the disabled state is one recessed rule, not a wash over three skins', () => {
  const styles = ui.slice(ui.indexOf('const styles = {'), ui.indexOf('}[variant]'));
  assert.doesNotMatch(styles, /disabled:/,
    'a variant that owns its own disabled skin is a variant that can drift');

  const base = ui.slice(ui.indexOf('inline-flex items-center gap-1.5'), ui.indexOf('${styles}'));
  assert.match(base, /disabled:bg-surface-2/);
  assert.match(base, /disabled:text-fg-muted/);
  assert.match(base, /disabled:pointer-events-none/,
    'a disabled fill must not light up under the cursor');
  assert.doesNotMatch(base, /disabled:opacity-50/,
    'the wash is what put a disabled label below AA against its own fill');
});
