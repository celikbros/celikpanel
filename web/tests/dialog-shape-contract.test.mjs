import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import test from 'node:test';

// Register R-047 found the mail install dialogue with its actions below its own
// fold and fixed it there. R-059 found the identical defect in the DNS review
// dialogue: 994 tall inside a box of 808 at 1440x900, 1608 inside 758 at
// 390x844, opening scrolled to the top — so the operator saw a refusal and no
// way to dismiss it. The two shared nothing; they were hand-built overlays, and
// eleven more stood beside them.
//
// So this file pins the fix where it now lives — one Dialog — and pins that it
// is the only one. A test per dialogue would have to be written again for the
// fourteenth, which is exactly the failure mode the register named.
//
// R-047 posta kurulum diyaloğunu kendi katlamasının altında kalmış eylemleriyle
// buldu ve orada düzeltti. R-059 aynı kusuru DNS inceleme diyaloğunda buldu.
// İkisi hiçbir şey paylaşmıyordu. Bu dosya düzeltmeyi artık yaşadığı yerde —
// tek bir Dialog'da — sabitler ve tek olduğunu sabitler.
const componentsDir = new URL('../src/components/', import.meta.url);
const ui = readFileSync(new URL('ui.tsx', componentsDir), 'utf8');

function dialogPrimitive() {
  const start = ui.indexOf('export function Dialog({');
  assert.ok(start > 0, 'the shared Dialog primitive is gone');
  const end = ui.indexOf('\nexport function ', start + 1);
  assert.ok(end > start, 'the shared Dialog primitive has no end');
  return ui.slice(start, end);
}

// The whole of R-059 in one assertion: the box is bounded, the body scrolls,
// and the decision is not inside the thing that scrolls.
test('the shared dialogue is a bounded column whose actions are outside the scroller', () => {
  const dialog = dialogPrimitive();

  const panelAt = dialog.indexOf('className:');
  assert.ok(panelAt > 0, 'the panel no longer has a class of its own');
  const panel = dialog.slice(panelAt, dialog.indexOf('} as const;', panelAt));
  assert.match(panel, /flex max-h-\[90vh\] w-full \$\{dialogWidths\[width\]\} flex-col/);
  assert.doesNotMatch(panel, /overflow-y-auto/, 'the panel itself must not be the scroller');

  const bodyAt = dialog.indexOf('min-h-0 flex-1 overflow-y-auto');
  assert.ok(bodyAt > 0, 'the body is not a scroller of its own');
  assert.ok(
    dialog.indexOf('overflow-y-auto', bodyAt + 'min-h-0 flex-1 overflow-y-auto'.length) < 0,
    'the body must be the only thing that scrolls',
  );

  // The footer opens after the body element has closed, and the actions are
  // inside it — so no amount of body content can push them off screen.
  const bodyCloses = dialog.indexOf('{children}</div>', bodyAt);
  assert.ok(bodyCloses > bodyAt, 'the body no longer closes around the caller content');
  const footerAt = dialog.indexOf('shrink-0 border-t border-border', bodyCloses);
  assert.ok(footerAt > bodyCloses, 'the actions are not in a footer outside the body');
  assert.ok(dialog.indexOf('{actions}', footerAt) > footerAt, 'the actions left the footer');
  assert.ok(
    dialog.indexOf('{footerLead}', footerAt) > footerAt,
    'the acknowledgement that gates a primary must sit beside it, not above the fold',
  );

  // The header is pinned too, so a scrolled body never loses what it is about.
  const headerAt = dialog.indexOf('shrink-0 border-b border-border');
  assert.ok(headerAt > 0 && headerAt < bodyAt, 'the header is not pinned above the body');
});

// Below sm the row becomes a reversed column, so the caller's DOM order decides
// which control leads at both widths at once — the rule R-047's leftover
// settled for the DNS dialogue, now true for every dialogue.
test('the action row reverses on a phone and end-aligns once there is room', () => {
  const dialog = dialogPrimitive();
  assert.match(dialog, /flex flex-col-reverse gap-2 sm:flex-row sm:justify-end/);
});

test('the shared dialogue carries the accessibility every dialogue needs', () => {
  const dialog = dialogPrimitive();
  assert.match(dialog, /role: 'dialog'/);
  assert.match(dialog, /'aria-modal': true/);
  assert.match(dialog, /'aria-labelledby': `\$\{id\}-title`/);
  assert.match(dialog, /'aria-describedby'/);
  assert.match(dialog, /'aria-busy': busy \|\| undefined/);
  assert.match(dialog, /id=\{`\$\{id\}-title`\}/);
  assert.match(dialog, /id=\{`\$\{id\}-description`\}/);
});

// A dialogue that vanishes without a trace reads exactly like a button that did
// not work — the reason the destructive confirmations refuse backdrop
// dismissal. Now that a dialogue has more than one silent exit, they have to
// leave together or the property is a half-truth.
test('dismissal is one property: no silent exit survives dismissible={false}', () => {
  const dialog = dialogPrimitive();
  assert.match(dialog, /const canDismiss = dismissible && !busy && onDismiss !== undefined;/);
  // Escape is gated on it...
  assert.match(dialog, /if \(!canDismiss\) return;[\s\S]{0,400}event\.key !== 'Escape'/);
  // ...and so is the backdrop.
  assert.match(dialog, /if \(canDismiss && event\.currentTarget === event\.target\) onDismiss!\(\);/);
});

// The install dialogue asks a second time before enabling a vendor repository,
// so two dialogues are on screen. One keypress must not close both.
test('Escape belongs to the dialogue on top, not to every dialogue open', () => {
  const dialog = dialogPrimitive();
  assert.match(ui, /const openDialogs: symbol\[\] = \[\];/);
  assert.match(dialog, /openDialogs\.push\(token\)/);
  assert.match(dialog, /openDialogs\[openDialogs\.length - 1\] !== tokenRef\.current/);
  assert.match(dialog, /openDialogs\.splice\(at, 1\)/, 'a closed dialogue must leave the stack');
});

// The point of a shared shape is that there is no second one. These five are
// full-screen states rather than dialogues: a loading scrim, a drawer, the
// mobile navigation, and the two operation locks, which deliberately have no
// action row, no backdrop dismissal and — for the update lock — a trapped
// Escape, because leaving them is not a thing the operator may do.
//
// Paylasilan bir bicimin anlami, ikincisinin olmamasidir. Asagidaki bes yer
// diyalog degil, tam ekran durumlardir.
const fullScreenStates = new Map([
  ['ComponentOperation.tsx', 'the scrim shown while the operation overlay chunk loads'],
  ['HelpDrawer.tsx', 'a drawer, not a dialogue'],
  ['Layout.tsx', 'the mobile navigation rail'],
  ['OperationOverlay.tsx', 'the operation lock: no actions, deliberately not dismissible'],
  ['SystemUpdateOperation.tsx', 'the panel-update lock: Escape is trapped on purpose'],
]);

test('no screen builds a modal dialogue of its own any more', () => {
  const offenders = [];
  for (const name of readdirSync(componentsDir).filter((n) => n.endsWith('.tsx'))) {
    if (name === 'ui.tsx' || fullScreenStates.has(name)) continue;
    const source = readFileSync(new URL(name, componentsDir), 'utf8');
    if (source.includes('fixed inset-0')) offenders.push(name);
  }
  assert.deepEqual(
    offenders,
    [],
    `these built their own overlay instead of using Dialog: ${offenders.join(', ')}`,
  );
});

// Every dialogue in the product, named. A dialogue that stops calling Dialog
// drops out of this list and the count fails, which is the whole point: the
// fourteenth dialogue cannot be hand-built quietly.
test('every dialogue in the product is the shared one', () => {
  const users = new Map();
  for (const name of readdirSync(componentsDir).filter((n) => n.endsWith('.tsx'))) {
    if (name === 'ui.tsx') continue;
    const source = readFileSync(new URL(name, componentsDir), 'utf8');
    const count = (source.match(/<Dialog\b/g) ?? []).length;
    if (count > 0) {
      assert.match(
        source,
        /import \{[^}]*\bDialog\b[^}]*\} from '\.\/ui';/,
        `${name} renders a Dialog it did not import from the shared primitives`,
      );
      users.set(name, count);
    }
  }

  assert.deepEqual(Object.fromEntries([...users].sort()), {
    'AddDatabaseModalV2.tsx': 1,
    'AddDomainModal.tsx': 1,
    'AddUserModalV2.tsx': 1,
    'ChangePasswordModal.tsx': 1,
    'DNSEngineCard.tsx': 1,
    'Dashboard.tsx': 1,
    'DatabaseAccountStrip.tsx': 1,
    'DomainMailManager.tsx': 1,
    'ServiceList.tsx': 7,
    'ServiceShell.tsx': 1,
  });
});

// A dialogue with no id has no title to point an assistive reader at, and one
// with no actions is the defect R-059 is about.
test('every dialogue names itself and offers a way out', () => {
  for (const name of readdirSync(componentsDir).filter((n) => n.endsWith('.tsx'))) {
    if (name === 'ui.tsx') continue;
    const source = readFileSync(new URL(name, componentsDir), 'utf8');
    let at = source.indexOf('<Dialog');
    while (at >= 0) {
      const next = source.indexOf('<Dialog', at + 1);
      const region = source.slice(at, next < 0 ? source.length : next);
      assert.match(region, /\sid=['"{]/, `a Dialog in ${name} does not name itself`);
      assert.match(region, /\sactions=\{/, `a Dialog in ${name} offers no action`);
      assert.match(region, /\stitle=/, `a Dialog in ${name} has no title`);
      at = next;
    }
  }
});
