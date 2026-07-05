import { useState, useEffect } from 'react';
import { Waypoints, Settings, Shield, Gauge } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { useI18n } from '../i18n';
import { EmptyState } from './ui';

interface NginxManagementProps {
    initialVersion: string;
    onBack: () => void;
}

interface NginxGlobalConfig {
    worker_processes: string;
    worker_connections: string;
    keepalive_timeout: string;
    client_max_body_size: string;
    server_tokens: string;
    gzip: string;
}

interface NginxSSLConfig {
    ssl_ciphers: string;
    ssl_protocols: string;
    ssl_prefer_server_ciphers: string;
}

interface NginxRateLimit {
    name: string;
    zone: string;
    size: string;
    rate: string;
}

// Nginx config is shown read-only, parsed live from `nginx -T`. In-panel
// editing isn't wired yet, so we don't offer a Save button that would lie.
// Nginx config'i salt-okunur gösterilir, `nginx -T`'den canlı ayrıştırılır.
// Panel içi düzenleme henüz bağlı değil; yalan söyleyecek bir Kaydet butonu
// sunmuyoruz.
export function NginxManagement({ onBack }: NginxManagementProps) {
    const { t } = useI18n();
    const [tab, setTab] = useState<'global' | 'ssl' | 'rate'>('global');
    const [global, setGlobal] = useState<NginxGlobalConfig | null>(null);
    const [ssl, setSSL] = useState<NginxSSLConfig | null>(null);
    const [rate, setRate] = useState<NginxRateLimit[]>([]);

    useEffect(() => {
        Promise.all([
            fetch('/api/v1/nginx/global').then((r) => (r.ok ? r.json() : null)),
            fetch('/api/v1/nginx/ssl').then((r) => (r.ok ? r.json() : null)),
            fetch('/api/v1/nginx/ratelimits').then((r) => (r.ok ? r.json() : [])),
        ])
            .then(([g, s, rl]) => {
                setGlobal(g);
                setSSL(s);
                setRate(rl || []);
            })
            .catch(() => {});
    }, []);

    return (
        <ServiceShell serviceId="nginx" name="Nginx" icon={Waypoints} onBack={onBack}>
            <div className="mb-4 flex items-center gap-1 border-b border-border">
                <Tab active={tab === 'global'} onClick={() => setTab('global')} icon={Settings} label={t('nginx.tab.global')} />
                <Tab active={tab === 'ssl'} onClick={() => setTab('ssl')} icon={Shield} label={t('nginx.tab.ssl')} />
                <Tab active={tab === 'rate'} onClick={() => setTab('rate')} icon={Gauge} label={t('nginx.tab.rateLimits')} />
            </div>

            {tab === 'global' && (
                <Panel note={t('nginx.readonly')}>
                    <Row label={t('nginx.workerProcesses')} value={global?.worker_processes} />
                    <Row label={t('nginx.workerConnections')} value={global?.worker_connections} />
                    <Row label={t('nginx.keepalive')} value={global?.keepalive_timeout} />
                    <Row label={t('nginx.maxBodySize')} value={global?.client_max_body_size} />
                    <Row label={t('nginx.serverTokens')} value={global?.server_tokens} />
                    <Row label={t('nginx.gzip')} value={global?.gzip} />
                </Panel>
            )}

            {tab === 'ssl' && (
                <Panel note={t('nginx.readonly')}>
                    <Row label={t('nginx.sslProtocols')} value={ssl?.ssl_protocols} mono />
                    <Row label={t('nginx.sslCiphers')} value={ssl?.ssl_ciphers} mono />
                    <Row label={t('nginx.preferServerCiphers')} value={ssl?.ssl_prefer_server_ciphers} />
                </Panel>
            )}

            {tab === 'rate' &&
                (rate.length === 0 ? (
                    <EmptyState icon={Gauge} title={t('nginx.emptyRateLimits')} />
                ) : (
                    <div className="overflow-x-auto rounded-xl border border-border bg-surface shadow-card">
                        <table className="w-full text-sm">
                            <thead>
                                <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                                    <th className="px-4 py-2.5">Name</th>
                                    <th className="px-4 py-2.5">{t('nginx.rl.zone')}</th>
                                    <th className="px-4 py-2.5">{t('nginx.rl.size')}</th>
                                    <th className="px-4 py-2.5">{t('nginx.rl.rate')}</th>
                                </tr>
                            </thead>
                            <tbody>
                                {rate.map((r, i) => (
                                    <tr key={i} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                        <td className="px-4 py-2.5 font-medium text-fg">{r.name}</td>
                                        <td className="px-4 py-2.5 font-mono text-fg-muted">{r.zone || '—'}</td>
                                        <td className="px-4 py-2.5 text-fg-muted">{r.size || '—'}</td>
                                        <td className="px-4 py-2.5 text-fg-muted">{r.rate || '—'}</td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                ))}
        </ServiceShell>
    );
}

function Tab({ active, onClick, icon: Icon, label }: { active: boolean; onClick: () => void; icon: typeof Settings; label: string }) {
    return (
        <button
            onClick={onClick}
            className={`-mb-px flex items-center gap-2 border-b-2 px-3 py-2.5 text-sm font-medium transition-colors ${
                active ? 'border-primary text-primary' : 'border-transparent text-fg-muted hover:text-fg'
            }`}
        >
            <Icon className="h-4 w-4" />
            {label}
        </button>
    );
}

function Panel({ children, note }: { children: React.ReactNode; note: string }) {
    return (
        <div className="rounded-xl border border-border bg-surface p-5 shadow-card">
            <dl className="divide-y divide-border text-sm">{children}</dl>
            <p className="mt-4 text-xs text-fg-subtle">{note}</p>
        </div>
    );
}

function Row({ label, value, mono }: { label: string; value?: string; mono?: boolean }) {
    return (
        <div className="flex items-start justify-between gap-4 py-2.5 first:pt-0">
            <dt className="shrink-0 text-fg-subtle">{label}</dt>
            <dd className={`break-all text-right font-medium text-fg ${mono ? 'font-mono text-xs' : ''}`}>{value || '—'}</dd>
        </div>
    );
}
