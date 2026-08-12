import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const pageSource = readFileSync(
  new URL('../src/components/TeamMembersPage.tsx', import.meta.url),
  'utf8',
);
const apiSource = readFileSync(new URL('../src/lib/api.ts', import.meta.url), 'utf8');

test('team-member UI uses the typed customer API without an owner override', () => {
  assert.match(pageSource, /api\.getTeamMembers\(\)/);
  assert.match(pageSource, /api\.getTeamMemberSubscriptionScopes\(\)/);
  assert.match(pageSource, /api\.getTeamMemberDomainScopes\(\)/);
  assert.match(pageSource, /api\.createTeamMember\(/);
  assert.match(pageSource, /api\.updateTeamMember\(/);
  assert.match(pageSource, /api\.deleteTeamMember\(/);
  assert.doesNotMatch(pageSource, /\bfetch\s*\(/);
  assert.doesNotMatch(pageSource, /owner_id/);
});

test('team-member capability and permission mode sets stay closed', () => {
  const capabilityBlock = apiSource.match(/export const TEAM_CAPABILITIES = \[([\s\S]*?)\] as const;/);
  assert.ok(capabilityBlock, 'TEAM_CAPABILITIES must be declared as a closed tuple');
  const capabilities = [...capabilityBlock[1].matchAll(/'([^']+)'/g)].map((match) => match[1]);
  assert.deepEqual(capabilities, [
    'files',
    'databases',
    'mail',
    'dns',
    'ssl',
    'cron',
    'backups',
    'php',
    'statistics',
  ]);
  assert.match(apiSource, /export type TeamPermissionMode = 'view' \| 'manage';/);
  assert.match(apiSource, /value === 'view' \|\| value === 'manage'/);
  assert.match(pageSource, /choice === 'none'/);
});

test('team-member access updates always send both scope arrays', () => {
  assert.match(pageSource, /subscription_permissions: subscriptionPermissions/);
  assert.match(pageSource, /domain_permissions: domainPermissions/);
  assert.match(pageSource, /assertMemberScopes\(/);
  assert.match(pageSource, /team_member_scope_mismatch/);
});

test('password, suspension, and deletion flows preserve their safety contracts', () => {
  assert.match(pageSource, /draft\.password \? \{ password: draft\.password \} : \{\}/);
  assert.match(pageSource, /window\.confirm\(t\('team\.suspendConfirm'/);
  assert.match(pageSource, /window\.confirm\(t\('team\.deleteConfirm'/);
  assert.match(pageSource, /required=\{isNew\}/);
  assert.match(pageSource, /minLength=\{8\}/);
});
