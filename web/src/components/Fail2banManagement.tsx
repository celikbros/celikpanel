import { useState } from 'react';
import { Shield, Lock, Ban, Settings } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, KnownEmpty, RemoteGate, StatusDot } from './ui';
import { countText, decodeList, mapRemote, useRemote } from '../lib/remote';

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

// The settings are the four values or they are unknown; an answer without
// them is not "no limit set".
// Ayarlar ya dört değerdir ya da bilinmeyendir.
function decodeFail2banConfig(raw: unknown): Fail2banConfig {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('shape');
    const body = raw as Record<string, unknown>;
    if (typeof body.ban_time !== 'string' || typeof body.find_time !== 'string' || typeof body.max_retry !== 'number') throw new Error('field');
    if (body.ignore_ip !== null && body.ignore_ip !== undefined && !Array.isArray(body.ignore_ip)) throw new Error('list');
    return { ...(body as unknown as Fail2banConfig), ignore_ip: (body.ignore_ip as string[] | null | undefined) ?? [] };
}

// Fail2ban's jails, the addresses it has banned and its settings are three
// reads, and each is being read, could not be read, or known (9 Oct 2026).
// Before, a read that failed or had not answered was an empty list: the page
// said "No jails active" and "No banned addresses" on a server that had both,
// and the counts beside the tabs said 0.
//
// Fail2ban’in hapishaneleri, yasakladığı adresler ve ayarları üç okumadır ve her
// biri okunuyor, okunamadı ya da biliniyor durumundadır. Önceden başarısız ya
// da yanıtlanmamış okuma boş listeydi: sayfa, ikisi de olan bir sunucuda
// "Aktif hapishane yok" ve "Yasaklı adres yok" diyor, sekme sayıları 0 gösteriyordu.
export function Fail2banManagement({ onBack }: Fail2banManagementProps) {
    const { t } = useI18n();
    const [tab, setTab] = useState<'jails' | 'banned' | 'config'>('jails');
    const jails = useRemote('/api/v1/fail2ban/jails', decodeList<Fail2banJail>);
    const banned = useRemote('/api/v1/fail2ban/banned', decodeList<Fail2banBannedIP>);
    const config = useRemote('/api/v1/fail2ban/config', decodeFail2banConfig);
    const [unbanning, setUnbanning] = useState('');

    const unban = async (ip: string, jail: string) => {
        if (!confirm(t('f2b.confirmUnban', { ip, jail }))) return;
        setUnbanning(`${jail}/${ip}`);
        try {
            const r = await fetch('/api/v1/fail2ban/banned', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ip, jail }),
            });
            if (!r.ok) throw new Error();
            showToast('success', t('f2b.unbanned'));
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setUnbanning('');
            // Whatever the answer was, the lists are read again: they are what
            // says whether the address is still banned.
            // Yanıt ne olursa olsun listeler yeniden okunur.
            void banned.retry();
            void jails.retry();
        }
    };

    return (
        <ServiceShell serviceId="fail2ban" name="Fail2ban" icon={Shield} onBack={onBack}>
            <div className="mb-4 flex flex-wrap items-center gap-1 border-b border-border">
                <Tab active={tab === 'jails'} onClick={() => setTab('jails')} icon={Lock} label={t('f2b.tab.jails')} count={countText(mapRemote(jails.remote, (rows) => rows.length))} />
                <Tab active={tab === 'banned'} onClick={() => setTab('banned')} icon={Ban} label={t('f2b.tab.banned')} count={countText(mapRemote(banned.remote, (rows) => rows.length))} />
                <Tab active={tab === 'config'} onClick={() => setTab('config')} icon={Settings} label={t('f2b.tab.config')} />
            </div>

            {/* One least height for the three states of a tab. Nothing stands
                under the tabs on this page today; whatever is added there
                will not move when the answer arrives.
                Bir sekmenin üç durumu için tek en az yükseklik. Bugün bu
                sayfada sekmelerin altında bir şey yok; eklenecek olan, yanıt
                geldiğinde yer değiştirmez. */}
            <div className="min-h-[11rem]">
                {tab === 'jails' && (
                    <RemoteGate remote={jails.remote} checking={t('f2b.jails.checking')} failed={t('f2b.jails.unknown')} onRetry={() => void jails.retry()} busy={jails.reading} className="py-2">
                        {(shown) => (shown.value.length === 0 ? (
                            <KnownEmpty of={shown} icon={Lock} title={t('f2b.emptyJails')} />
                        ) : (
                            <TableWrap cols={[t('f2b.col.jail'), t('domains.col.status'), t('f2b.col.banned')]}>
                                {shown.value.map((j) => (
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
                    </RemoteGate>
                )}

                {tab === 'banned' && (
                    <RemoteGate remote={banned.remote} checking={t('f2b.banned.checking')} failed={t('f2b.banned.unknown')} onRetry={() => void banned.retry()} busy={banned.reading} className="py-2">
                        {(shown) => (shown.value.length === 0 ? (
                            <KnownEmpty of={shown} icon={Ban} title={t('f2b.emptyBanned')} />
                        ) : (
                            <TableWrap cols={[t('f2b.col.ip'), t('f2b.col.jail'), '']}>
                                {shown.value.map((b, i) => (
                                    <tr key={`${b.ip}-${i}`} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                        <td className="px-4 py-2.5 font-mono font-medium text-fg">{b.ip}</td>
                                        <td className="px-4 py-2.5 text-fg-muted">{b.jail}</td>
                                        <td className="row-actions px-4 py-2.5 text-right">
                                            {/* Off while the list is the earlier answer or is
                                                being read again: a ban is lifted only from a
                                                row the server has just listed.
                                                Liste önceki yanıtken ya da yeniden okunurken
                                                kapalıdır. */}
                                            <Button
                                                variant="secondary"
                                                onClick={() => void unban(b.ip, b.jail)}
                                                disabled={shown.stale || banned.reading || unbanning !== ''}
                                                loading={unbanning === `${b.jail}/${b.ip}`}
                                            >
                                                {t('f2b.unban')}
                                            </Button>
                                        </td>
                                    </tr>
                                ))}
                            </TableWrap>
                        ))}
                    </RemoteGate>
                )}

                {tab === 'config' && (
                    <RemoteGate remote={config.remote} checking={t('f2b.config.checking')} failed={t('f2b.config.unknown')} onRetry={() => void config.retry()} busy={config.reading} className="py-2">
                        {(shown) => (
                            <div className="rounded-xl border border-border bg-surface p-5">
                                <dl className="divide-y divide-border text-sm">
                                    <Row label={t('f2b.banTime')} value={shown.value.ban_time || '—'} />
                                    <Row label={t('f2b.findTime')} value={shown.value.find_time || '—'} />
                                    <Row label={t('f2b.maxRetry')} value={shown.value.max_retry ? String(shown.value.max_retry) : '—'} />
                                    <Row label={t('f2b.ignoreIp')} value={shown.value.ignore_ip.length ? shown.value.ignore_ip.join('  ') : '—'} mono />
                                </dl>
                                <p className="mt-4 text-xs text-fg-subtle">{t('f2b.configReadonly')}</p>
                            </div>
                        )}
                    </RemoteGate>
                )}
            </div>
        </ServiceShell>
    );
}

function Tab({ active, onClick, icon: Icon, label, count }: { active: boolean; onClick: () => void; icon: typeof Shield; label: string; count?: string }) {
    return (
        <button
            onClick={onClick}
            className={`-mb-px flex items-center gap-2 border-b-2 px-3 py-2.5 text-sm font-medium transition-colors ${
                active ? 'border-primary text-primary' : 'border-transparent text-fg-muted hover:text-fg'
            }`}
        >
            <Icon className="h-4 w-4" />
            {label}
            {count !== undefined && <span className="rounded-full bg-surface-2 px-1.5 py-0.5 text-xs text-fg-muted">{count}</span>}
        </button>
    );
}

function TableWrap({ cols, children }: { cols: string[]; children: React.ReactNode }) {
    return (
        <div className="overflow-x-auto rounded-xl border border-border-strong bg-surface">
            <table className="w-full text-sm">
                <thead>
                    <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                        {cols.map((c, i) => (
                            <th key={i} className={`px-4 py-2.5 ${i === cols.length - 1 ? 'text-right' : ''} ${c === '' ? 'row-actions' : ''}`}>
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
