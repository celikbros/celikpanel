import { useState } from 'react';
import { Waypoints, Settings, Shield, Gauge } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { useI18n } from '../i18n';
import { KnownEmpty, RemoteGate } from './ui';
import { decodeList, useRemote } from '../lib/remote';

interface NginxManagementProps {
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
// A parsed block of the running configuration is a record of strings or it is
// unknown. A value Nginx does not set is an empty string in a known record and
// is drawn as "—"; a record that could not be read is not a list of dashes.
// Çalışan yapılandırmanın ayrıştırılmış bir bloğu ya dize kaydıdır ya da
// bilinmeyendir. Nginx'in ayarlamadığı değer, bilinen kayıtta boş dizedir ve
// "—" çizilir; okunamayan kayıt çizgi listesi değildir.
function decodeBlock<T>(raw: unknown): T {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('shape');
    if (Object.values(raw).some((value) => typeof value !== 'string')) throw new Error('field');
    return raw as T;
}

export function NginxManagement({ onBack }: NginxManagementProps) {
    const { t } = useI18n();
    const [tab, setTab] = useState<'global' | 'ssl' | 'rate'>('global');
    // Three reads, each being read, could not be read, or known (9 Oct 2026).
    // Before, a failed or unfinished read drew every value as "—" and the rate
    // limits as "none defined".
    // Üç okuma; her biri okunuyor, okunamadı ya da biliniyor. Önceden başarısız
    // ya da bitmemiş okuma her değeri "—", hız sınırlarını "tanımlı değil" çiziyordu.
    const global = useRemote('/api/v1/nginx/global', decodeBlock<NginxGlobalConfig>);
    const ssl = useRemote('/api/v1/nginx/ssl', decodeBlock<NginxSSLConfig>);
    const rate = useRemote('/api/v1/nginx/ratelimits', decodeList<NginxRateLimit>);

    return (
        <ServiceShell serviceId="nginx" name="Nginx" icon={Waypoints} onBack={onBack}>
            <div className="mb-4 flex flex-wrap items-center gap-1 border-b border-border">
                <Tab active={tab === 'global'} onClick={() => setTab('global')} icon={Settings} label={t('nginx.tab.global')} />
                <Tab active={tab === 'ssl'} onClick={() => setTab('ssl')} icon={Shield} label={t('nginx.tab.ssl')} />
                <Tab active={tab === 'rate'} onClick={() => setTab('rate')} icon={Gauge} label={t('nginx.tab.rateLimits')} />
            </div>

            {/* One least height for the three states of a tab. Nothing stands
                under the tabs on this page today; whatever is added there
                will not move when the answer arrives.
                Bir sekmenin üç durumu için tek en az yükseklik. Bugün bu
                sayfada sekmelerin altında bir şey yok; eklenecek olan, yanıt
                geldiğinde yer değiştirmez. */}
            <div className="min-h-[11rem]">
                {tab === 'global' && (
                    <RemoteGate remote={global.remote} checking={t('nginx.global.checking')} failed={t('nginx.global.unknown')} onRetry={() => void global.retry()} busy={global.reading} className="py-2">
                        {(shown) => (
                            <Panel note={t('nginx.readonly')}>
                                <Row label={t('nginx.workerProcesses')} value={shown.value.worker_processes} />
                                <Row label={t('nginx.workerConnections')} value={shown.value.worker_connections} />
                                <Row label={t('nginx.keepalive')} value={shown.value.keepalive_timeout} />
                                <Row label={t('nginx.maxBodySize')} value={shown.value.client_max_body_size} />
                                <Row label={t('nginx.serverTokens')} value={shown.value.server_tokens} />
                                <Row label={t('nginx.gzip')} value={shown.value.gzip} />
                            </Panel>
                        )}
                    </RemoteGate>
                )}

                {tab === 'ssl' && (
                    <RemoteGate remote={ssl.remote} checking={t('nginx.ssl.checking')} failed={t('nginx.ssl.unknown')} onRetry={() => void ssl.retry()} busy={ssl.reading} className="py-2">
                        {(shown) => (
                            <Panel note={t('nginx.readonly')}>
                                <Row label={t('nginx.sslProtocols')} value={shown.value.ssl_protocols} mono />
                                <Row label={t('nginx.sslCiphers')} value={shown.value.ssl_ciphers} mono />
                                <Row label={t('nginx.preferServerCiphers')} value={shown.value.ssl_prefer_server_ciphers} />
                            </Panel>
                        )}
                    </RemoteGate>
                )}

                {tab === 'rate' && (
                    <RemoteGate remote={rate.remote} checking={t('nginx.rate.checking')} failed={t('nginx.rate.unknown')} onRetry={() => void rate.retry()} busy={rate.reading} className="py-2">
                        {(shown) => (shown.value.length === 0 ? (
                            <KnownEmpty of={shown} icon={Gauge} title={t('nginx.emptyRateLimits')} />
                        ) : (
                            <div className="overflow-x-auto rounded-xl border border-border-strong bg-surface">
                                <table className="w-full text-sm">
                                    <thead>
                                        <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                                            <th className="px-4 py-2.5">{t('nginx.rl.name')}</th>
                                            <th className="px-4 py-2.5">{t('nginx.rl.zone')}</th>
                                            <th className="px-4 py-2.5">{t('nginx.rl.size')}</th>
                                            <th className="px-4 py-2.5">{t('nginx.rl.rate')}</th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        {shown.value.map((r, i) => (
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
                    </RemoteGate>
                )}
            </div>
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
        <div className="rounded-xl border border-border bg-surface p-5">
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
