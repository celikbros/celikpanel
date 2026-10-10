import { readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { join, relative } from 'node:path';
import ts from 'typescript';

// The remote-state ratchet: a source scan for the ways a screen turns "the
// read failed" or "the read has not answered yet" into something it then shows
// as a fact. Each of them is how a negative state reached the screen without
// being known (owner report, 8 Oct 2026):
//
//   valueFromFailure      `r.ok ? r.json() : null` (or [], {}, false). The
//                         failure becomes a value; the screen then draws
//                         "none", "off" or "missing" from it.
//   swallowedFailure      an empty `catch {}` around a read, or
//                         `.catch(() => {})` / `.catch(() => setX(null))`.
//   ignoredFailure        `if (res.ok) { ... }` on a read with no else and no
//                         early exit, or `if (!res.ok) return;`. The state
//                         keeps whatever default it started with.
//   rawRead               a read `fetch(` outside src/lib. Not wrong by
//                         itself: it is the measure of what has not moved to
//                         lib/remote.ts yet.
//   unprovenEmptyState    `<EmptyState>` used directly. `KnownEmpty` takes the
//                         answer that proves there is nothing.
//
// The counts per file are in remote-state-ratchet.json, and they may only go
// down. A screen that is migrated reads through useRemote / readRemote and
// leaves the list.
//
// Uzak-durum mandalı: bir ekranın "okuma başarısız oldu" ya da "okuma henüz
// yanıt vermedi" durumunu, sonra olgu diye gösterdiği bir şeye çevirme
// yollarını kaynakta sayar. Dosya başına sayılar remote-state-ratchet.json
// içindedir ve yalnız azalabilir.
export const patterns = {
  valueFromFailure: 'a failed read becomes a value: `x.ok ? … : null | [] | {}`',
  swallowedFailure: 'a failed read is swallowed: empty `catch {}` around a read, or `.catch(() => {})`',
  ignoredFailure: 'a failed read is ignored: `if (res.ok) {…}` with no else, or `if (!res.ok) return;`',
  rawRead: 'a read `fetch(` outside src/lib (not yet on lib/remote.ts)',
  unprovenEmptyState: '`<EmptyState>` without the answer that proves it (use `KnownEmpty of={…}`)',
};
export const patternNames = Object.keys(patterns);

const zero = () => Object.fromEntries(patternNames.map((name) => [name, 0]));

function isFetch(node) {
  if (!ts.isCallExpression(node)) return false;
  const callee = node.expression;
  if (ts.isIdentifier(callee)) return callee.text === 'fetch';
  return ts.isPropertyAccessExpression(callee) && callee.name.text === 'fetch'
    && ts.isIdentifier(callee.expression) && ['window', 'globalThis', 'self'].includes(callee.expression.text);
}

// 'read' when the call provably sends no body-changing method, 'write' when it
// provably does, 'unclear' when the options are built somewhere else.
function fetchKind(call) {
  const init = call.arguments[1];
  if (!init) return 'read';
  if (!ts.isObjectLiteralExpression(init)) return 'unclear';
  for (const property of init.properties) {
    if (ts.isSpreadAssignment(property)) return 'unclear';
    const name = property.name && (ts.isIdentifier(property.name) || ts.isStringLiteral(property.name)) ? property.name.text : '';
    if (name !== 'method') continue;
    if (ts.isPropertyAssignment(property) && ts.isStringLiteralLike(property.initializer)) {
      return ['GET', 'HEAD'].includes(property.initializer.text.toUpperCase()) ? 'read' : 'write';
    }
    return 'unclear';
  }
  return 'read';
}

const contains = (node, test) => {
  let found = false;
  const visit = (child) => {
    if (found) return;
    if (test(child)) { found = true; return; }
    ts.forEachChild(child, visit);
  };
  visit(node);
  return found;
};

// A value that stands in for an answer the server did not give.
function madeUp(node) {
  if (!node) return true;
  if (ts.isParenthesizedExpression(node)) return madeUp(node.expression);
  if (node.kind === ts.SyntaxKind.NullKeyword || node.kind === ts.SyntaxKind.FalseKeyword) return true;
  if (ts.isIdentifier(node) && node.text === 'undefined') return true;
  if (ts.isArrayLiteralExpression(node) && node.elements.length === 0) return true;
  if (ts.isObjectLiteralExpression(node) && node.properties.length === 0) return true;
  if (ts.isStringLiteralLike(node) && node.text === '') return true;
  return false;
}

const isOk = (node) => ts.isPropertyAccessExpression(node) && node.name.text === 'ok';
const okSubject = (node) => (isOk(node) && ts.isIdentifier(node.expression) ? node.expression.text : null);

// The body of the function a node sits in, or the file.
function scopeOf(node) {
  for (let at = node.parent; at; at = at.parent) {
    if (ts.isFunctionLike(at) && at.body) return at.body;
    if (ts.isSourceFile(at)) return at;
  }
  return node.getSourceFile();
}

// How `name` got its response inside `scope`: the kind of the fetch that
// initialises or is assigned to it. 'unclear' when it cannot be seen.
function responseKind(name, scope) {
  let kind = null;
  const visit = (node) => {
    let initializer = null;
    if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && node.name.text === name) initializer = node.initializer;
    if (ts.isBinaryExpression(node) && node.operatorToken.kind === ts.SyntaxKind.EqualsToken
      && ts.isIdentifier(node.left) && node.left.text === name) initializer = node.right;
    if (ts.isParameter(node) && ts.isIdentifier(node.name) && node.name.text === name && kind === null) kind = 'param';
    if (initializer) {
      let found = 'unclear';
      const look = (child) => {
        if (isFetch(child)) { found = fetchKind(child); return; }
        ts.forEachChild(child, look);
      };
      look(initializer);
      kind = kind === 'read' ? 'read' : found;
    }
    ts.forEachChild(node, visit);
  };
  visit(scope);
  return kind ?? 'unclear';
}

function endsControl(statement) {
  if (!statement) return false;
  if (ts.isReturnStatement(statement) || ts.isThrowStatement(statement)
    || ts.isContinueStatement(statement) || ts.isBreakStatement(statement)) return true;
  if (ts.isBlock(statement)) return endsControl(statement.statements[statement.statements.length - 1]);
  return false;
}

// `return;`, `return null;`, `return [];`, `continue;` and nothing else.
function silentExit(statement) {
  if (ts.isBlock(statement)) return statement.statements.length === 1 && silentExit(statement.statements[0]);
  if (ts.isContinueStatement(statement)) return true;
  return ts.isReturnStatement(statement) && madeUp(statement.expression);
}

// A `.catch(handler)` whose handler does nothing, returns a made-up value, or
// only stores one.
function quietHandler(handler) {
  if (!handler || !(ts.isArrowFunction(handler) || ts.isFunctionExpression(handler))) return false;
  const body = handler.body;
  const storesMadeUp = (expression) => ts.isCallExpression(expression)
    && expression.arguments.length === 1 && madeUp(expression.arguments[0]);
  if (!ts.isBlock(body)) return madeUp(body) || storesMadeUp(body);
  if (body.statements.length === 0) return true;
  return body.statements.every((statement) => (
    (ts.isExpressionStatement(statement) && storesMadeUp(statement.expression))
    || (ts.isReturnStatement(statement) && madeUp(statement.expression))
  ));
}

// scanSource counts the patterns in one file. `path` is relative to web/, with
// forward slashes; it decides whether a raw read is inside src/lib.
export function scanSource(path, text) {
  const counts = zero();
  const kind = path.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
  const file = ts.createSourceFile(path, text, ts.ScriptTarget.ES2022, true, kind);
  const inLib = path.startsWith('src/lib/');
  const isPrimitives = path === 'src/components/ui.tsx';

  const visit = (node) => {
    if (isFetch(node) && !inLib && fetchKind(node) === 'read') counts.rawRead++;

    if (ts.isConditionalExpression(node) && isOk(node.condition) && madeUp(node.whenFalse)) counts.valueFromFailure++;

    if (ts.isCatchClause(node) && node.block.statements.length === 0) {
      const attempt = node.parent.tryBlock;
      if (contains(attempt, (child) => isFetch(child) || ts.isAwaitExpression(child))) counts.swallowedFailure++;
    }
    if (ts.isCallExpression(node) && ts.isPropertyAccessExpression(node.expression)
      && node.expression.name.text === 'catch' && quietHandler(node.arguments[0])) counts.swallowedFailure++;

    if (ts.isIfStatement(node)) {
      const positive = okSubject(node.expression);
      const negated = ts.isPrefixUnaryExpression(node.expression) && node.expression.operator === ts.SyntaxKind.ExclamationToken
        ? okSubject(node.expression.operand) : null;
      const subject = positive ?? negated;
      if (subject && responseKind(subject, scopeOf(node)) !== 'write') {
        if (positive && !node.elseStatement && !endsControl(node.thenStatement)) counts.ignoredFailure++;
        if (negated && !node.elseStatement && silentExit(node.thenStatement)) counts.ignoredFailure++;
      }
    }

    if (!isPrimitives && (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node))
      && ts.isIdentifier(node.tagName) && node.tagName.text === 'EmptyState') counts.unprovenEmptyState++;

    ts.forEachChild(node, visit);
  };
  visit(file);
  return counts;
}

// scanTree scans every .ts and .tsx file under `<webDir>/src`.
export function scanTree(webDir) {
  const result = {};
  const walk = (dir) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const full = join(dir, entry.name);
      if (entry.isDirectory()) { walk(full); continue; }
      if (!/\.tsx?$/.test(entry.name) || entry.name.endsWith('.d.ts')) continue;
      const path = relative(webDir, full).replaceAll('\\', '/');
      const counts = scanSource(path, readFileSync(full, 'utf8'));
      if (patternNames.some((name) => counts[name] > 0)) {
        result[path] = Object.fromEntries(patternNames.filter((name) => counts[name] > 0).map((name) => [name, counts[name]]));
      }
    }
  };
  walk(join(webDir, 'src'));
  return Object.fromEntries(Object.keys(result).sort().map((path) => [path, result[path]]));
}

export const totals = (files) => {
  const sum = zero();
  for (const counts of Object.values(files)) for (const name of patternNames) sum[name] += counts[name] ?? 0;
  return sum;
};

export const allowListURL = new URL('./remote-state-ratchet.json', import.meta.url);
export const readAllowList = () => JSON.parse(readFileSync(allowListURL, 'utf8'));

// `node tests/remote-state-ratchet.mjs` prints the totals of the working tree.
// `--tighten` lowers the allow-list to what the tree now has and drops files
// that reached zero. It never raises a number and never adds a file: that is a
// regression, and the answer to it is lib/remote.ts.
// `--report <web dir>` prints the scan of another tree as JSON.
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const webDir = fileURLToPath(new URL('../', import.meta.url));
  if (process.argv[2] === '--report') {
    const files = scanTree(process.argv[3] ?? webDir);
    console.log(JSON.stringify({ totals: totals(files), fileCount: Object.keys(files).length, files }, null, 2));
  } else {
    const actual = scanTree(webDir);
    if (process.argv[2] === '--tighten') {
      const list = readAllowList();
      const next = {};
      const refused = [];
      for (const [path, counts] of Object.entries(actual)) {
        const allowed = list.files[path];
        if (!allowed) { refused.push(`${path}: not on the allow-list`); continue; }
        next[path] = {};
        for (const [name, count] of Object.entries(counts)) {
          if (count > (allowed[name] ?? 0)) refused.push(`${path}: ${name} is ${count}, allowed ${allowed[name] ?? 0}`);
          next[path][name] = Math.min(count, allowed[name] ?? 0);
        }
      }
      if (refused.length > 0) {
        console.error('Not tightened. These are regressions, not allowances:\n- ' + refused.join('\n- '));
        process.exitCode = 1;
      } else {
        writeFileSync(allowListURL, JSON.stringify({ ...list, files: next }, null, 2) + '\n');
        console.log(`Allow-list tightened: ${Object.keys(next).length} files remain.`);
      }
    }
    console.log(JSON.stringify({ totals: totals(actual), fileCount: Object.keys(actual).length }));
  }
}
