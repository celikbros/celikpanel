import { useCallback, useEffect, useState } from 'react';
import { Network, Server, Check, AlertTriangle } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, inputClass, StatusDot } from './ui';
import { readApiError, apiErrorText } from '../lib/apiError';
import { HelpButton } from './HelpDrawer';

// The server's DNS identity: the two nameserver names it advertises, and
// whether it stands alone or is half of a pair.
//
// This screen exists because of one operator question (25 Jul): "can we use
// Frankfurt and Boston as the two nameservers, so they back each other up? or
// am I thinking nonsense?" It is not nonsense — it is what a hosting provider
// does, and the panel could not express it at all. Both names resolved to one
// machine, which makes the pair decoration: one reboot and every hosted domain
// goes dark.
//
// Every value here is entered by the operator. The panel applies it and then
// checks it; it does not decide it.
//
// Sunucunun DNS kimliği: ilan ettiği iki ad sunucusu adı ve tek başına mı
// durduğu yoksa bir çiftin yarısı mı olduğu.
//
// Bu ekran tek bir operatör sorusu yüzünden var (25 Tem): "Frankfurt'u ve
// Boston'u iki ad sunucusu olarak kullanıp birbirlerini yedekleyebilir miyiz?
// Yoksa saçma mı düşünüyorum?" Saçma değil — bir barındırma sağlayıcısının
// yaptığı şey bu ve panel bunu hiç ifade edemiyordu. İki ad da tek makineye
// çözülüyordu; bu da çifti süse çevirir: bir yeniden başlatma ve barındırılan
// her alan adı kararır.
//
// Buradaki her değeri operatör girer. Panel onu uygular ve sonra kontrol eder;
// karar vermez.
interface NSFact {
    host: string;
    ips: string[];
    points_here: boolean;
}
interface NSSettings {
    ns1: string;
    ns2: string;
    derived: boolean;
    server_ip: string;
    facts: NSFact[];
    usable: boolean;
}
interface Cluster {
    role: 'standalone' | 'primary' | 'secondary';
    peer_ip: string;
    peer_ns: string;
    peer_reachable: boolean;
    server_ip: string;
}

export function DNSServerSettings() {
    const { t } = useI18n();
    const [ns, setNs] = useState<NSSettings | null>(null);
    const [cl, setCl] = useState<Cluster | null>(null);
    const [ns1, setNs1] = useState('');
    const [ns2, setNs2] = useState('');
    const [busy, setBusy] = useState(false);

    const load = useCallback(async () => {
        const [a, b] = await Promise.all([
            fetch('/api/v1/settings/nameservers').then((r) => (r.ok ? r.json() : null)),
            fetch('/api/v1/settings/dns-cluster').then((r) => (r.ok ? r.json() : null)),
        ]);
        if (a) {
            setNs(a);
            setNs1(a.ns1);
            setNs2(a.ns2);
        }
        if (b) setCl(b);
    }, []);

    useEffect(() => {
        load();
    }, [load]);

    const saveNS = async () => {
        setBusy(true);
        try {
            const res = await fetch('/api/v1/settings/nameservers', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ns1, ns2 }),
            });
            if (!res.ok) {
                showToast('error', apiErrorText(await readApiError(res), t));
                return;
            }
            showToast('success', t('dnssrv.saved'));
            load();
        } finally {
            setBusy(false);
        }
    };

    const saveCluster = async (role: Cluster['role'], peer_ip: string, peer_ns: string) => {
        setBusy(true);
        try {
            const res = await fetch('/api/v1/settings/dns-cluster', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ role, peer_ip, peer_ns }),
            });
            if (!res.ok) {
                showToast('error', apiErrorText(await readApiError(res), t));
                return;
            }
            const d = await res.json();
            showToast('success', d.detail || t('dnssrv.saved'));
            load();
        } finally {
            setBusy(false);
        }
    };

    if (!ns || !cl) return null;

    return (
        <section className="rounded-xl border border-border bg-surface p-6">
            <div className="mb-4 flex flex-wrap items-center gap-2">
                <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/10 text-primary">
                    <Network className="h-4.5 w-4.5" />
                </span>
                <div className="min-w-0">
                    <h2 className="text-base font-semibold text-fg">{t('dnssrv.title')}</h2>
                    <p className="text-sm text-fg-muted">{t('dnssrv.subtitle')}</p>
                </div>
                <div className="ml-auto">
                    <HelpButton serviceId="dns-server-settings" name={t('dnssrv.title')} />
                </div>
            </div>

            {/* The names, and where they really point. / Adlar ve gerçekte nereyi gösterdikleri. */}
            <p className="mb-2 text-sm font-medium text-fg">{t('dnssrv.namesTitle')}</p>
            <p className="mb-3 text-xs leading-relaxed text-fg-muted">{t('dnssrv.namesHint', { ip: ns.server_ip })}</p>
            <div className="mb-3 grid gap-2 sm:grid-cols-2">
                <input className={inputClass} value={ns1} onChange={(e) => setNs1(e.target.value)} placeholder="ns1.example.com" />
                <input className={inputClass} value={ns2} onChange={(e) => setNs2(e.target.value)} placeholder="ns2.example.com" />
            </div>
            <ul className="mb-3 space-y-1">
                {ns.facts?.map((f) => (
                    <li key={f.host} className="flex items-center gap-2 font-mono text-xs">
                        <StatusDot ok={f.points_here} />
                        <span className="text-fg">{f.host}</span>
                        <span className="text-fg-muted">→ {f.ips.length ? f.ips.join(', ') : t('conn.none')}</span>
                        {!f.points_here && <span className="text-warning">{t('dnssrv.notThisServer')}</span>}
                    </li>
                ))}
            </ul>
            {!ns.usable && (
                <p className="mb-3 flex items-start gap-2 rounded-lg bg-warning/10 p-3 text-xs leading-relaxed text-fg-muted">
                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                    <span>{t('dnssrv.notUsable')}</span>
                </p>
            )}
            <Button onClick={saveNS} disabled={busy}>
                <Check className="h-4 w-4" /> {t('dnssrv.saveNames')}
            </Button>

            {/* The pair. / Çift. */}
            <hr className="my-6 border-border" />
            <p className="mb-2 text-sm font-medium text-fg">{t('dnssrv.roleTitle')}</p>
            <p className="mb-3 text-xs leading-relaxed text-fg-muted">{t('dnssrv.roleHint')}</p>

            <div className="mb-3 space-y-2">
                {(['standalone', 'primary', 'secondary'] as const).map((role) => (
                    <label
                        key={role}
                        className={`flex cursor-pointer items-start gap-2.5 rounded-lg border p-3 ${cl.role === role ? 'border-primary bg-primary/5' : 'border-border'}`}
                    >
                        <input
                            type="radio"
                            className="mt-0.5"
                            checked={cl.role === role}
                            onChange={() => setCl({ ...cl, role })}
                        />
                        <span className="min-w-0">
                            <span className="block text-sm font-medium text-fg">{t(`dnssrv.role.${role}` as Parameters<typeof t>[0])}</span>
                            <span className="block text-xs leading-relaxed text-fg-muted">
                                {t(`dnssrv.role.${role}.desc` as Parameters<typeof t>[0])}
                            </span>
                        </span>
                    </label>
                ))}
            </div>

            {cl.role !== 'standalone' && (
                <>
                    <div className="mb-2 grid gap-2 sm:grid-cols-2">
                        <input
                            className={inputClass}
                            value={cl.peer_ip}
                            onChange={(e) => setCl({ ...cl, peer_ip: e.target.value })}
                            placeholder={t('dnssrv.peerIpPlaceholder')}
                        />
                        <input
                            className={inputClass}
                            value={cl.peer_ns}
                            onChange={(e) => setCl({ ...cl, peer_ns: e.target.value })}
                            placeholder="ns2.example.com"
                        />
                    </div>
                    {cl.peer_ip && (
                        <p className="mb-3 flex items-center gap-1.5 text-xs">
                            <Server className="h-3.5 w-3.5 text-fg-muted" />
                            <StatusDot ok={cl.peer_reachable} />
                            <span className="text-fg-muted">
                                {cl.peer_reachable ? t('dnssrv.peerReachable') : t('dnssrv.peerUnreachable')}
                            </span>
                        </p>
                    )}
                </>
            )}

            <Button onClick={() => saveCluster(cl.role, cl.peer_ip, cl.peer_ns)} disabled={busy}>
                <Check className="h-4 w-4" /> {t('dnssrv.saveRole')}
            </Button>
        </section>
    );
}
