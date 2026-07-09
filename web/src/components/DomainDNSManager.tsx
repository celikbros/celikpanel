import { useState, useEffect } from 'react';
import { Globe, Plus, Trash2, ShieldCheck, Copy, AlertTriangle } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, EmptyState, inputClass } from './ui';

interface DNSRecord {
    id: number;
    name: string;
    type: string;
    content: string;
    ttl: number;
    prio?: number;
    disabled: boolean;
}

interface DomainDNSManagerProps {
    domainId: number;
    domainName: string;
}

const recordTypes = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV'];

// Record-type pill colours. Categorical, readable in both themes; kept small
// and intentional rather than one hue per type.
// Kayıt-türü rozet renkleri. Kategorik, iki temada okunur; tür başına bir
// renk yerine küçük ve bilinçli tutuldu.
const typeColor: Record<string, string> = {
    A: 'bg-primary/10 text-primary',
    AAAA: 'bg-primary/10 text-primary',
    CNAME: 'bg-success/10 text-success',
    MX: 'bg-warning/15 text-warning',
    SRV: 'bg-warning/15 text-warning',
};

export function DomainDNSManager({ domainId, domainName }: DomainDNSManagerProps) {
    const { t } = useI18n();
    const [records, setRecords] = useState<DNSRecord[]>([]);
    const [loading, setLoading] = useState(true);
    const [zoneExists, setZoneExists] = useState<boolean | null>(null);
    const [showAddForm, setShowAddForm] = useState(false);
    const [newType, setNewType] = useState('A');
    const [newName, setNewName] = useState('@');
    const [newContent, setNewContent] = useState('');
    const [newTTL, setNewTTL] = useState(3600);
    const [newPrio, setNewPrio] = useState(0);

    // Whether anything actually serves this zone: '' = no DNS server installed
    // (records are saved but not published), otherwise "pdns"/"bind". null
    // while loading — the banner only appears once we know for sure.
    // Bu zone'u fiilen bir şeyin sunup sunmadığı: '' = DNS sunucusu kurulu
    // değil (kayıtlar kayıtlı ama yayınlanmıyor), aksi halde "pdns"/"bind".
    // Yüklenirken null — bant ancak kesin bilince görünür.
    const [dnsServer, setDnsServer] = useState<string | null>(null);

    useEffect(() => {
        checkZone();
        fetch('/api/v1/hosting/capabilities')
            .then((r) => (r.ok ? r.json() : null))
            .then((c) => setDnsServer(c ? (c.dns_server ?? '') : null))
            .catch(() => setDnsServer(null));
    }, [domainId]);

    const checkZone = async () => {
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/zone`);
            if (res.ok) {
                setZoneExists(true);
                loadRecords();
            } else {
                setZoneExists(false);
                setLoading(false);
            }
        } catch {
            setZoneExists(false);
            setLoading(false);
        }
    };

    const loadRecords = async () => {
        setLoading(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/records`);
            if (res.ok) {
                const data = await res.json();
                setRecords(data.records || []);
            }
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setLoading(false);
        }
    };

    const createZone = async () => {
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/zone`, { method: 'POST' });
            if (!res.ok) throw new Error();
            showToast('success', t('dns.zoneCreated'));
            setZoneExists(true);
            loadRecords();
        } catch {
            showToast('error', t('dns.zoneCreateFailed'));
        }
    };

    const addRecord = async () => {
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/records`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: newName, type: newType, content: newContent, ttl: newTTL, prio: newPrio }),
            });
            if (!res.ok) throw new Error();
            showToast('success', t('dns.recordAdded'));
            setShowAddForm(false);
            setNewContent('');
            loadRecords();
        } catch {
            showToast('error', t('dns.recordAddFailed'));
        }
    };

    const deleteRecord = async (id: number) => {
        if (!confirm(t('dns.confirmDelete'))) return;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/records?id=${id}`, { method: 'DELETE' });
            if (!res.ok) throw new Error();
            showToast('success', t('dns.recordDeleted'));
            loadRecords();
        } catch {
            showToast('error', t('dns.recordDeleteFailed'));
        }
    };

    if (loading && zoneExists === null) {
        return <p className="text-sm text-fg-muted">{t('dns.checking')}</p>;
    }

    if (zoneExists === false) {
        return (
            <EmptyState
                icon={Globe}
                title={t('dns.zoneMissing')}
                hint={t('dns.zoneMissingHint', { name: domainName })}
                action={
                    <Button variant="primary" icon={Plus} onClick={createZone}>
                        {t('dns.enableZone')}
                    </Button>
                }
            />
        );
    }

    const needsPrio = newType === 'MX' || newType === 'SRV';

    return (
        <div>
            {/* Honesty first: the records below are real, editable panel data,
                but with no DNS server installed NOTHING serves them — say so
                loudly instead of letting 13 rows look live. DNSSEC signing
                needs the DNS server's tooling, so that card only exists when
                one is installed.
                Önce dürüstlük: aşağıdaki kayıtlar gerçek, düzenlenebilir panel
                verisidir ama DNS sunucusu kurulu değilken onları HİÇBİR ŞEY
                yayınlamaz — 13 satırı canlı gibi bırakmak yerine bunu açıkça
                söyle. DNSSEC imzalama DNS sunucusunun aracını ister; o kart
                yalnız biri kuruluyken var olur. */}
            {dnsServer === '' && (
                <div className="mb-4 flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/10 p-3 text-sm text-fg">
                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                    <span>{t('dns.notServed')}</span>
                </div>
            )}
            {dnsServer !== '' && <DNSSECSection domainId={domainId} domainName={domainName} />}

            <div className="mb-3 flex items-center justify-between">
                <span className="text-xs text-fg-subtle">{t('common.itemsTotal', { n: records.length })}</span>
                <Button variant="primary" icon={Plus} onClick={() => setShowAddForm((s) => !s)}>
                    {t('dns.addRecord')}
                </Button>
            </div>

            {showAddForm && (
                <div className="mb-4 rounded-lg border border-border bg-surface-2/50 p-4">
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-12">
                        <label className="sm:col-span-2">
                            <span className="mb-1 block text-xs text-fg-muted">{t('dns.type')}</span>
                            <select value={newType} onChange={(e) => setNewType(e.target.value)} className={inputClass}>
                                {recordTypes.map((rt) => (
                                    <option key={rt} value={rt}>
                                        {rt}
                                    </option>
                                ))}
                            </select>
                        </label>
                        <label className="sm:col-span-3">
                            <span className="mb-1 block text-xs text-fg-muted">{t('dns.name')}</span>
                            <input value={newName} onChange={(e) => setNewName(e.target.value)} placeholder={t('dns.nameHint')} className={inputClass} />
                        </label>
                        <label className={needsPrio ? 'sm:col-span-4' : 'sm:col-span-5'}>
                            <span className="mb-1 block text-xs text-fg-muted">{t('dns.content')}</span>
                            <input value={newContent} onChange={(e) => setNewContent(e.target.value)} placeholder="192.168.1.1" className={inputClass} />
                        </label>
                        <label className="sm:col-span-2">
                            <span className="mb-1 block text-xs text-fg-muted">{t('dns.ttl')}</span>
                            <input type="number" value={newTTL} onChange={(e) => setNewTTL(parseInt(e.target.value))} className={inputClass} />
                        </label>
                        {needsPrio && (
                            <label className="sm:col-span-1">
                                <span className="mb-1 block text-xs text-fg-muted">{t('dns.priority')}</span>
                                <input type="number" value={newPrio} onChange={(e) => setNewPrio(parseInt(e.target.value))} className={inputClass} />
                            </label>
                        )}
                    </div>
                    <div className="mt-3 flex justify-end gap-2">
                        <Button variant="secondary" onClick={() => setShowAddForm(false)}>
                            {t('dns.cancel')}
                        </Button>
                        <Button variant="primary" icon={Plus} onClick={addRecord}>
                            {t('dns.save')}
                        </Button>
                    </div>
                </div>
            )}

            {records.length === 0 ? (
                <EmptyState icon={Globe} title={t('dns.noRecords')} />
            ) : (
                <div className="overflow-x-auto rounded-lg border border-border">
                    <table className="w-full text-sm">
                        <thead>
                            <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                                <th className="px-4 py-2.5">{t('dns.type')}</th>
                                <th className="px-4 py-2.5">{t('dns.name')}</th>
                                <th className="px-4 py-2.5">{t('dns.content')}</th>
                                <th className="px-4 py-2.5">{t('dns.ttl')}</th>
                                <th className="px-4 py-2.5" />
                            </tr>
                        </thead>
                        <tbody>
                            {records.map((rec) => (
                                <tr key={rec.id} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                    <td className="px-4 py-2.5">
                                        <span className={`rounded px-1.5 py-0.5 text-xs font-semibold ${typeColor[rec.type] ?? 'bg-surface-2 text-fg-muted'}`}>
                                            {rec.type}
                                        </span>
                                    </td>
                                    <td className="px-4 py-2.5 font-medium text-fg">{rec.name}</td>
                                    <td className="break-all px-4 py-2.5 font-mono text-fg-muted">
                                        {rec.prio ? <span className="mr-1.5 text-warning">[{rec.prio}]</span> : null}
                                        {rec.content}
                                    </td>
                                    <td className="px-4 py-2.5 text-fg-muted">{rec.ttl}</td>
                                    <td className="px-4 py-2.5 text-right">
                                        <button
                                            onClick={() => deleteRecord(rec.id)}
                                            className="rounded-md p-1.5 text-fg-subtle transition-colors hover:bg-surface-2 hover:text-danger"
                                        >
                                            <Trash2 className="h-4 w-4" />
                                        </button>
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}
        </div>
    );
}


// DNSSEC: sign the zone in one click and hand the operator the DS records to
// enter at the registrar. Without that DS, validators treat the zone (and
// its DANE/TLSA records) as insecure — so both live together here.
// DNSSEC: zone'u tek tıkla imzala ve operatöre registrar'a girilecek DS
// kayıtlarını ver. O DS olmadan doğrulayıcılar zone'u (ve DANE/TLSA
// kayıtlarını) güvensiz sayar — o yüzden ikisi burada birlikte yaşar.
function DNSSECSection({ domainId }: { domainId: number; domainName: string }) {
    const { t } = useI18n();
    const [secured, setSecured] = useState(false);
    const [ds, setDs] = useState<string[]>([]);
    const [busy, setBusy] = useState(false);
    const [loaded, setLoaded] = useState(false);

    useEffect(() => {
        fetch(`/api/v1/domains/${domainId}/dnssec`)
            .then((r) => (r.ok ? r.json() : null))
            .then((d) => {
                if (d) {
                    setSecured(d.secured === true);
                    setDs(d.ds || []);
                }
            })
            .catch(() => {})
            .finally(() => setLoaded(true));
    }, [domainId]);

    const sign = async () => {
        setBusy(true);
        try {
            const r = await fetch(`/api/v1/domains/${domainId}/dnssec`, { method: 'POST' });
            const d = await r.json();
            if (!r.ok) throw new Error(d.error);
            setSecured(true);
            setDs(d.ds || []);
            showToast('success', t('dnssec.signed'));
        } catch (e) {
            showToast('error', e instanceof Error && e.message ? e.message : t('common.error'));
        } finally {
            setBusy(false);
        }
    };

    if (!loaded) return null;

    return (
        <section className="mb-5 rounded-xl border border-border bg-surface p-5">
            <div className="mb-1 flex items-center gap-2">
                <ShieldCheck className={`h-4 w-4 ${secured ? 'text-success' : 'text-fg-muted'}`} />
                <h3 className="text-sm font-semibold text-fg">DNSSEC</h3>
                {secured && (
                    <span className="rounded-md bg-success/10 px-2 py-0.5 text-xs font-medium text-success">
                        {t('dnssec.on')}
                    </span>
                )}
            </div>
            {secured ? (
                <>
                    <p className="mb-3 text-sm text-fg-muted">{t('dnssec.dsHint')}</p>
                    <div className="space-y-2">
                        {ds.map((rec) => (
                            <div key={rec} className="flex items-center gap-2">
                                <code className="min-w-0 flex-1 overflow-x-auto rounded-lg bg-surface-2 px-3 py-2 text-xs text-fg">
                                    {rec}
                                </code>
                                <button
                                    onClick={() => navigator.clipboard.writeText(rec).then(() => showToast('success', t('vpn.copied')))}
                                    title={t('vpn.copy')}
                                    className="rounded-md p-1.5 text-fg-muted hover:bg-surface-2 hover:text-fg"
                                >
                                    <Copy className="h-4 w-4" />
                                </button>
                            </div>
                        ))}
                    </div>
                </>
            ) : (
                <div className="flex flex-wrap items-center justify-between gap-3">
                    <p className="text-sm text-fg-muted">{t('dnssec.offHint')}</p>
                    <Button variant="primary" disabled={busy} onClick={sign}>
                        {t('dnssec.sign')}
                    </Button>
                </div>
            )}
        </section>
    );
}
