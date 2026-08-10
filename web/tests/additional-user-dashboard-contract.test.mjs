import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const dashboardSource = readFileSync(new URL('../src/components/Dashboard.tsx', import.meta.url), 'utf8');

function functionBody(name, nextName) {
  const start = dashboardSource.indexOf(`function ${name}()`);
  const end = dashboardSource.indexOf(`function ${nextName}()`, start + 1);
  assert.notEqual(start, -1, `${name} must exist`);
  assert.notEqual(end, -1, `${nextName} must follow ${name}`);
  return dashboardSource.slice(start, end);
}

test('additional users are routed to their own fail-closed dashboard', () => {
  assert.match(dashboardSource, /role === 'additional_user'\) return <AdditionalUserDashboard \/>/);
});

test('additional-user dashboard requests only the authorization-filtered domains collection', () => {
  const source = functionBody('AdditionalUserDashboard', 'CustomerDashboard');

  assert.match(source, /fetch\('\/api\/v1\/domains'/);
  assert.doesNotMatch(source, /\bapi\s*\./);
  assert.doesNotMatch(source, /\/api\/v1\/(?:system|managed-services|firewall|audit-logs|users|dashboard|hosting|panel)/);
  assert.doesNotMatch(source, /SystemStats|cpu_|mem_|disk_|uptime|hostname|audit|setup|QuickAction|CountCard|GaugeCard|InfoRow|services/i);
  assert.match(source, /setDomains\(Array\.isArray\(value\) \? value : \[\]\)/);
});

test('additional-user dashboard exposes only granted domain cards or a safe empty state', () => {
  const source = functionBody('AdditionalUserDashboard', 'CustomerDashboard');

  assert.match(source, /dashboard\.welcome/);
  assert.match(source, /domains\.map\(\(domain\)/);
  assert.match(source, /domain\.domain_name/);
  assert.match(source, /navigate\(`\/domains\/\$\{encodeURIComponent\(domain\.domain_name\)\}`\)/);
  assert.match(source, /domains\.empty/);
  assert.match(source, /navigate\('\/domains'\)/);
  assert.match(source, /t\('nav\.domains'\)/);
  assert.doesNotMatch(source, /navigate\('\/(?:services|users|settings|import|databases|vpn)'\)/);
});
