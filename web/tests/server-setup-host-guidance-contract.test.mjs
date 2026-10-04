import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const setup = readFileSync(new URL('../src/components/ServerSetup.tsx', import.meta.url), 'utf8');
const en = readFileSync(new URL('../src/i18n/screens/en.ts', import.meta.url), 'utf8');
const tr = readFileSync(new URL('../src/i18n/screens/tr.ts', import.meta.url), 'utf8');

const mapped = (code) => {
  const match = setup.match(new RegExp(String.raw`\n\s*${code}: '([^']+)'`));
  return match ? match[1] : null;
};

// Decision B/C (2026-09-30): a rolled-back DNS step and a firewall that cannot
// be checked each get their own text; neither falls back to the nameserver
// identity or an internal error.
test('setup maps rolled-back DNS and firewall host conditions to their own guidance', () => {
  const expected = {
    server_setup_dns_rolled_back: 'setup.guide.dnsRolledBack',
    host_restart_required: 'setup.blocker.hostRestart',
    firewall_kernel_unavailable: 'setup.blocker.firewallKernel',
    firewall_engine_unavailable: 'setup.blocker.firewallEngine',
    firewall_busy: 'setup.blocker.firewallBusy',
    firewall_status_unknown: 'setup.blocker.firewallUnknown',
  };
  for (const [code, key] of Object.entries(expected)) {
    assert.equal(mapped(code), key, code);
    assert.notEqual(mapped(code), 'setup.blocker.dnsIdentity', code);
    assert.match(en, new RegExp(`"${key.replaceAll('.', String.raw`\.`)}": "`), `EN ${key}`);
    assert.match(tr, new RegExp(`"${key.replaceAll('.', String.raw`\.`)}": "`), `TR ${key}`);
  }
  assert.match(en, /"setup\.guide\.dnsRolledBack": "The DNS engine installation did not complete and was undone: no DNS engine is running, and the installed packages were kept stopped\. Nothing else was changed\. Review a new plan and start this DNS step again\."/);
  assert.match(tr, /"setup\.guide\.dnsRolledBack": "DNS motoru kurulumu tamamlanmadı ve geri alındı: çalışan bir DNS motoru yok, kurulan paketler durdurulmuş olarak saklandı\. Başka hiçbir şey değiştirilmedi\. Yeni planı gözden geçirip bu DNS adımını yeniden başlatın\."/);
  assert.match(en, /"setup\.blocker\.hostRestart": "This server was updated and must be restarted before its firewall can be checked\. Restart the server, then open setup again; your draft continues and nothing was changed\."/);
});
