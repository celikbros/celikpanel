import { Check, ArrowRight } from 'lucide-react';
import { useI18n } from '../i18n';
import { Link } from '../router';
import { Button } from './ui';

export function StartGuide({ dnsReady, scanFresh, scanning, onScan, panelSecured, firewallReady, onFirewall, firewallBusy }: {
    dnsReady: boolean; scanFresh: boolean; scanning: boolean; onScan: () => void;
    panelSecured: boolean | null; firewallReady: boolean | null; onFirewall?: () => void; firewallBusy: boolean;
}) {
    const { t } = useI18n();
    return (
        <section className="mt-6 overflow-hidden rounded-xl border border-border-strong bg-surface" aria-labelledby="start-guide-title">
            <div className="p-5 sm:p-6">
                <h2 id="start-guide-title" className="text-xl font-semibold text-fg">{t('start.admin.title')}</h2>
                <p className="mt-2 max-w-3xl text-sm text-fg-muted">{t('start.admin.hint')}</p>
                <div className="mt-5 flex flex-wrap items-start justify-between gap-4">
                    <div className="min-w-0 flex-1 basis-64">
                        <h3 className="flex items-center gap-2 font-semibold text-fg">
                            {dnsReady && <Check aria-hidden="true" className="h-4 w-4 text-success" />}
                            {t(dnsReady ? 'start.dns.ready' : 'start.dns.title')}
                        </h3>
                        <p className="mt-1 max-w-3xl text-sm text-fg-muted">{t('start.dns.hint')}</p>
                    </div>
                    <Link to="/settings?section=dns" className="inline-flex items-center gap-2 rounded-lg bg-primary px-4 py-2 text-sm font-semibold text-primary-fg hover:bg-primary/90">
                        {t(dnsReady ? 'start.dns.review' : 'start.dns.action')}<ArrowRight aria-hidden="true" className="h-4 w-4" />
                    </Link>
                </div>
                {!scanFresh && <div className="mt-4 flex flex-wrap items-center gap-3 text-sm text-fg-muted" role="status">
                    <span>{t('start.scan')}</span>
                    <Button variant="secondary" className="text-sm" onClick={onScan} disabled={scanning}>{t(scanning ? 'common.loading' : 'start.scan.action')}</Button>
                </div>}
                <details className="mt-5 border-t border-border pt-4">
                    <summary className="cursor-pointer text-sm font-semibold text-primary">{t('start.dns.sequence')}</summary>
                    <ol className="mt-3 list-decimal space-y-2 pl-5 text-sm text-fg-muted">
                        <li>{t('start.dns.plan')}</li><li>{t('start.dns.engine')}</li><li>{t('start.dns.verify')}</li>
                    </ol>
                </details>
                {dnsReady && <div className="mt-5 border-t border-border pt-4">
                    <h3 className="font-semibold text-fg">{t('start.next.title')}</h3>
                    <p className="mt-1 text-sm text-fg-muted">{t('start.next.hint')}</p>
                    <div className="mt-3 flex flex-wrap gap-4 text-sm font-semibold text-primary">
                        <Link to="/domains">{t('start.next.web')}</Link>
                        <Link to="/services#mail-stacks">{t('start.next.mail')}</Link>
                    </div>
                </div>}
            </div>
            <details open className="border-t border-border bg-surface-2/40 p-5 sm:p-6">
                <summary className="cursor-pointer font-semibold text-fg">{t('start.security.title')}</summary>
                <p className="mt-2 text-sm text-fg-muted">{t('start.security.hint')}</p>
                <ul className="mt-4 divide-y divide-border text-sm">
                    <li className="flex flex-wrap items-center justify-between gap-3 py-3">
                        <span>{t('dashboard.step.firewall')} · {t(firewallReady === null ? 'dashboard.statusUnknown' : firewallReady ? 'dashboard.stepDone' : 'start.review')}</span>
                        {onFirewall ? <Button className="text-sm" onClick={onFirewall} disabled={firewallBusy}>{t('firewall.turnOn')}</Button>
                            : <Link to="/services" className="font-semibold text-primary">{t('start.review')}</Link>}
                    </li>
                    <li className="flex flex-wrap items-center justify-between gap-3 py-3">
                        <span>{t('start.panel.title')} · {t(panelSecured === null ? 'dashboard.statusUnknown' : panelSecured ? 'dashboard.stepDone' : 'start.review')}</span>
                        <Link to="/settings?section=panel" className="font-semibold text-primary">{t('start.panel.action')}</Link>
                    </li>
                    <li className="flex flex-wrap items-center justify-between gap-3 py-3">
                        <span>{t('start.account.title')}</span><Link to="/settings?section=account" className="font-semibold text-primary">{t('start.account.action')}</Link>
                    </li>
                    <li className="flex flex-wrap items-center justify-between gap-3 py-3">
                        <span>{t('start.updates.title')}</span><Link to="/settings?section=updates" className="font-semibold text-primary">{t('start.updates.action')}</Link>
                    </li>
                    <li className="flex flex-wrap items-center justify-between gap-3 py-3">
                        <span>{t('start.audit.title')}</span><Link to="/settings?section=security" className="font-semibold text-primary">{t('start.audit.action')}</Link>
                    </li>
                </ul>
            </details>
        </section>
    );
}
