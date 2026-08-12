import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const authSource = readFileSync(new URL('../src/auth/AuthContext.tsx', import.meta.url), 'utf8');
const apiSource = readFileSync(new URL('../src/lib/api.ts', import.meta.url), 'utf8');
const navSource = readFileSync(new URL('../src/nav.ts', import.meta.url), 'utf8');

test('unknown authenticated roles fail closed instead of becoming customers', () => {
  assert.match(authSource, /export function normalizeRole\(role: string\): Role/);
  assert.match(authSource, /throw new Error\('The authenticated user has an unsupported role\.'\)/);
  assert.match(apiSource, /export function parseCurrentUser\(raw: unknown\): CurrentUser/);
  assert.match(apiSource, /throw new Error\('unsupported_auth_identity'\)/);
  assert.match(authSource, /normalizeRole\(user\.effective_role\)/);
  assert.doesNotMatch(authSource, /:\s*'customer'\)\s+as Role/);
});

test('additional users receive only dashboard and domain navigation', () => {
  const accountRoles = navSource.match(/const ACCOUNT_ROLES: Role\[\] = \[([^\]]+)]/);
  const domainRoles = navSource.match(/const DOMAIN_ROLES: Role\[\] = \[\.\.\.ACCOUNT_ROLES, 'additional_user']/);

  assert.ok(accountRoles, 'baseline account role list must exist');
  assert.doesNotMatch(accountRoles[1], /additional_user/);
  assert.ok(domainRoles, 'additional users must be explicitly added to domain-only navigation');
  assert.match(navSource, /id: 'dashboard'.*roles: DOMAIN_ROLES/);
  assert.match(navSource, /id: 'domains'.*roles: DOMAIN_ROLES/);
  assert.doesNotMatch(navSource, /id: 'users'.*additional_user/);
});

test('route matching is segment-aware and unknown paths fail closed', () => {
  assert.match(navSource, /normalizedPath\.startsWith\(\x60\$\{item\.path}\/[\x60]\)/);
  assert.match(navSource, /return match \? isNavItemAllowed\(match, role, access\) : false;/);
});

test('customer team-member navigation requires the exact server feature contract', () => {
  assert.match(navSource, /item\.id === 'users' && role === 'customer'/);
  assert.match(navSource, /access\?\.accountType === 'account' && access\.teamMembers === true/);
});
