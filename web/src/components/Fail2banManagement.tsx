import { useState, useEffect } from 'react';
import { Shield, Lock, Ban, Settings } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, EmptyState, StatusDot } from './ui';

interface Fail2banManagementProps {
    onBack: () => void;
}

interface Fail2banJail {
    name: string;
    enabled: boolean;
    active: boolean;
    banned: number;
}

interface Fail2banBannedIP {
    ip: string;
    jail: string;
    time: string;
    country: string;
}

interface Fail2banConfig {
    ban_time: string;
    find_time: string;
    max_retry: number;
    ignore_ip: string[];
}

export function Fail2banManagement({ onBack }: Fail2banManagementProps) {
    const { t } = useI18n();
    const [tab, setTab] = useState<'jails' | 'banned' | 'config'>('jails');
    const [jails, setJails] = useState<Fail2banJail[]>([]);
    const [banned, setBanned] = useState<Fail2banBannedIP[]>([]);
    const [config, setConfig] = useState<Fail2banConfig | null>(null);

    const load = async () => {
        try {
            const [j, b, c] = await Promise.all([
                fetch('/api/v1/fail2ban/jails').then((r) => (r.ok ? r.json() : [])),
                fetch('/api/v1/fail2ban/banned').then((r) => (r.ok ? r.json() : [])),
                fetch('/api/v1/fail2ban/config').then((r) => (r.ok ? r.json() : null)),
            ]);
            setJails(j || []);
            setBanned(b || []);
            setConfig(c);
        } catch {
            /* silent */
        }
    };

    useEffect(() => {
        load();
    }, []);

    const unban = async (ip: string, jail: string) => {
        if (!confirm(t('f2b.confirmUnban', { ip, jail }))) return;
        try {
            const r = await fetch('/api/v1/fail2ban/banned', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ip, jail }),
            });
            if (!r.ok) throw new Error();
            showToast('success', t('f2b.unbanned'));
            load();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    return (
        <ServiceShell serviceId="fail2ban" name="Fail2ban" icon={Shield} onBack={onBack}>
            <div className="mb-4 flex items-center gap-1 border-b border-border">
                <Tab active={tab === 'jails'} onClick={() => setTab('jails')} icon={Lock} label={t('f2b.tab.jails')} count={jails.length} />
                <Tab active={tab === 'banned'} onClick={() => setTab('banned')} icon={Ban} label={t('f2b.tab.banned')} count={banned.length} />
                <Tab active={tab === 'config'} onClick={() => setTab('config')} icon={Settings} label={t('f2b.tab.config')} />
            </div>

            {tab === 'jails' &&
                (jails.length === 0 ? (
                    <EmptyState icon={Lock} title={t('f2b.emptyJails')} />
                ) : (
                    <TableWrap cols={[t('f2b.col.jail'), t('domains.col.status'), t('f2b.col.banned')]}>
                        {jails.map((j) => (
                            <tr key={j.name} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                <td className="px-4 py-2.5 font-medium text-fg">{j.name}</td>
                                <td className="px-4 py-2.5">
                                    <span className="inline-flex items-center gap-1.5 text-fg-muted">
                                        <StatusDot ok={j.active} />
                                        {j.active ? t('services.running') : t('services.stopped')}
                                    </span>
                                </td>
                                <td className="px-4 py-2.5 text-right font-semibold text-fg">{j.banned}</td>
                            </tr>
                        ))}
                    </TableWrap>
                ))}

            {tab === 'banned' &&
                (banned.length === 0 ? (
                    <EmptyState icon={Ban} title={t('f2b.emptyBanned')} />
                ) : (
                    <TableWrap cols={[t('f2b.col.ip'), t('f2b.col.jail'), '']}>
                        {banned.map((b, i) => (
                            <tr key={`${b.ip}-${i}`} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                <td className="px-4 py-2.5 font-mono font-medium text-fg">{b.ip}</td>
                                <td className="px-4 py-2.5 text-fg-muted">{b.jail}</td>
                                <td className="px-4 py-2.5 text-right">
                                    <Button variant="secondary" onClick={() => unban(b.ip, b.jail)}>
                                        {t('f2b.unban')}
                                    </Button>
                                </td>
                            </tr>
                        ))}
                    </TableWrap>
                ))}

            {tab === 'config' && config && (
                <div className="rounded-xl border border-border bg-surface p-5 shadow-card">
                    <dl className="divide-y divide-border text-sm">
                        <Row label={t('f2b.banTime')} value={config.ban_time || '—'} />
                        <Row label={t('f2b.findTime')} value={config.find_time || '—'} />
                        <Row label={t('f2b.maxRetry')} value={config.max_retry ? String(config.max_retry) : '—'} />
                        <Row label={t('f2b.ignoreIp')} value={config.ignore_ip?.length ? config.ignore_ip.join('  ') : '—'} mono />
                    </dl>
                    <p className="mt-4 text-xs text-fg-subtle">{t('f2b.configReadonly')}</p>
                </div>
            )}
        </ServiceShell>
    );
}

function Tab({ active, onClick, icon: Icon, label, count }: { active: boolean; onClick: () => void; icon: typeof Shield; label: string; count?: number }) {
    return (
        <button
            onClick={onClick}
            className={`-mb-px flex items-center gap-2 border-b-2 px-3 py-2.5 text-sm font-medium transition-colors ${
                active ? 'border-primary text-primary' : 'border-transparent text-fg-muted hover:text-fg'
            }`}
        >
            <Icon className="h-4 w-4" />
            {label}
            {count !== undefined && <span className="rounded-full bg-surface-2 px-1.5 py-0.5 text-[11px] text-fg-muted">{count}</span>}
        </button>
    );
}

function TableWrap({ cols, children }: { cols: string[]; children: React.ReactNode }) {
    return (
        <div className="overflow-x-auto rounded-xl border border-border bg-surface shadow-card">
            <table className="w-full text-sm">
                <thead>
                    <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                        {cols.map((c, i) => (
                            <th key={i} className={`px-4 py-2.5 ${i === cols.length - 1 ? 'text-right' : ''}`}>
                                {c}
                            </th>
                        ))}
                    </tr>
                </thead>
                <tbody>{children}</tbody>
            </table>
        </div>
    );
}

function Row({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
    return (
        <div className="flex items-center justify-between gap-4 py-2.5 first:pt-0 last:pb-0">
            <dt className="text-fg-subtle">{label}</dt>
            <dd className={`font-medium text-fg ${mono ? 'font-mono text-xs' : ''}`}>{value}</dd>
        </div>
    );
}
