import { useState, useEffect } from 'react';
import { Globe, Plus, Trash2, ShieldCheck, Copy, AlertTriangle, RefreshCw } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, EmptyState, inputClass } from './ui';
import { apiErrorText, readApiError } from '../lib/apiError';

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
    readOnly?: boolean;
    isAdditionalUser?: boolean;
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

export function DomainDNSManager({
    domainId,
    domainName,
    readOnly = false,
    isAdditionalUser = false,
}: DomainDNSManagerProps) {
    const { t } = useI18n();
    const [records, setRecords] = useState<DNSRecord[]>([]);
    const [loading, setLoading] = useState(true);
    const [zoneExists, setZoneExists] = useState<boolean | null>(null);
	const [zoneError, setZoneError] = useState('');
	const [recordsError, setRecordsError] = useState('');
    const [showAddForm, setShowAddForm] = useState(false);
    const [newType, setNewType] = useState('A');
    const [newName, setNewName] = useState('@');
    const [newContent, setNewContent] = useState('');
    const [newTTL, setNewTTL] = useState(3600);
    const [newPrio, setNewPrio] = useState(0);
    const [publishing, setPublishing] = useState(false);
    const [mutatingRecord, setMutatingRecord] = useState(false);

    // Whether anything actually serves this zone: '' = no DNS server installed
    // (records are saved but not published), otherwise "pdns"/"bind". null
    // while loading — the banner only appears once we know for sure.
    // Bu zone'u fiilen bir şeyin sunup sunmadığı: '' = DNS sunucusu kurulu
    // değil (kayıtlar kayıtlı ama yayınlanmıyor), aksi halde "pdns"/"bind".
    // Yüklenirken null — bant ancak kesin bilince görünür.
    const [dnsServer, setDnsServer] = useState<string | null>(null);
	const [dnsServerError, setDnsServerError] = useState('');

    useEffect(() => {
		void checkZone();
		setDnsServer(null);
		setDnsServerError('');
        if (isAdditionalUser) {
            // Team members deliberately cannot inspect server-global service
            // inventory. Domain records, zone state and DNSSEC remain available
            // through their tenant-scoped endpoints below.
            return;
        }
        fetch('/api/v1/hosting/capabilities')
			.then(async (response) => {
				if (!response.ok) {
					throw new Error(apiErrorText(await readApiError(response), t, 'dns.statusUnavailable'));
				}
				return response.json();
			})
			.then((capabilities) => {
				setDnsServer(capabilities.dns_server ?? '');
				setDnsServerError('');
			})
			.catch((error) => {
				setDnsServer(null);
				setDnsServerError(error instanceof Error && error.message ? error.message : t('dns.statusUnavailable'));
			});
    }, [domainId, isAdditionalUser, t]);

    const checkZone = async () => {
		setLoading(true);
		setZoneError('');
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/zone`);
            if (res.ok) {
                setZoneExists(true);
				await loadRecords();
			} else if (res.status === 404) {
                setZoneExists(false);
                setLoading(false);
			} else {
				throw new Error(apiErrorText(await readApiError(res), t, 'dns.zoneStatusUnavailable'));
            }
		} catch (error) {
			setZoneExists(null);
			setZoneError(error instanceof Error && error.message ? error.message : t('dns.zoneStatusUnavailable'));
            setLoading(false);
        }
    };

    const loadRecords = async () => {
        setLoading(true);
		setRecordsError('');
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/records`);
			if (!res.ok) {
				throw new Error(apiErrorText(await readApiError(res), t, 'dns.recordsLoadFailed'));
            }
			const data = await res.json();
			setRecords(Array.isArray(data.records) ? data.records : []);
		} catch (error) {
			const message = error instanceof Error && error.message ? error.message : t('dns.recordsLoadFailed');
			setRecordsError(message);
			showToast('error', message);
        } finally {
            setLoading(false);
        }
    };

    const publishZone = async () => {
        if (readOnly) return;
        setPublishing(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/zone`, { method: 'POST' });
            if (!res.ok) {
                throw new Error(apiErrorText(await readApiError(res), t, 'dns.zonePublishFailed'));
            }
            const result: { created?: boolean } = await res.json();
            showToast('success', t(result.created ? 'dns.zoneCreated' : 'dns.zonePublished'));
            setZoneExists(true);
            await loadRecords();
        } catch (error) {
            showToast('error', error instanceof Error && error.message ? error.message : t('dns.zonePublishFailed'));
        } finally {
            setPublishing(false);
        }
    };

    const addRecord = async () => {
        if (readOnly) return;
        setMutatingRecord(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/records`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: newName, type: newType, content: newContent, ttl: newTTL, prio: newPrio }),
            });
            if (!res.ok) {
                throw new Error(apiErrorText(await readApiError(res), t, 'dns.recordAddFailed'));
            }
            showToast('success', t('dns.recordAdded'));
            setShowAddForm(false);
            setNewContent('');
            await loadRecords();
        } catch (error) {
            showToast('error', error instanceof Error && error.message ? error.message : t('dns.recordAddFailed'));
        } finally {
            setMutatingRecord(false);
        }
    };

    const deleteRecord = async (id: number) => {
        if (readOnly) return;
        if (!confirm(t('dns.confirmDelete'))) return;
        setMutatingRecord(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/records?id=${id}`, { method: 'DELETE' });
            if (!res.ok) {
                throw new Error(apiErrorText(await readApiError(res), t, 'dns.recordDeleteFailed'));
            }
            showToast('success', t('dns.recordDeleted'));
            await loadRecords();
        } catch (error) {
            showToast('error', error instanceof Error && error.message ? error.message : t('dns.recordDeleteFailed'));
        } finally {
            setMutatingRecord(false);
        }
    };

	if (zoneError) {
		return (
			<EmptyState
				icon={AlertTriangle}
				title={t('dns.zoneStatusUnavailable')}
				hint={zoneError}
				action={
					<Button variant="secondary" icon={RefreshCw} disabled={loading} onClick={() => void checkZone()}>
						{t('common.retry')}
					</Button>
				}
			/>
		);
	}

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
                    readOnly ? undefined : (
                        <Button variant="primary" icon={Plus} disabled={publishing} onClick={publishZone}>
                            {publishing ? t('dns.publishing') : t('dns.enableZone')}
                        </Button>
                    )
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
			{isAdditionalUser && (
				<div className="mb-4 flex items-start gap-2 rounded-lg border border-info/30 bg-info/10 p-3 text-sm text-fg">
					<AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-info" />
					<span>{t('dns.teamServerStatusUnavailable')}</span>
				</div>
			)}
			{dnsServerError && (
				<div className="mb-4 flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/10 p-3 text-sm text-fg">
					<AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
					<span>{dnsServerError}</span>
				</div>
			)}
            {(isAdditionalUser || (dnsServer !== null && dnsServer !== '')) && (
                <DNSSECSection domainId={domainId} domainName={domainName} readOnly={readOnly} />
            )}

            <div className="mb-3 flex items-center justify-between">
                <span className="text-xs text-fg-subtle">{t('common.itemsTotal', { n: records.length })}</span>
                <div className="flex flex-wrap items-center gap-2">
                    {readOnly ? (
                        <Button variant="secondary" icon={RefreshCw} disabled={loading} onClick={() => void loadRecords()}>
                            {t('dns.refresh')}
                        </Button>
                    ) : (
                        <>
                            <Button
                                variant="secondary"
                                icon={RefreshCw}
                                disabled={publishing || dnsServer === ''}
                                onClick={publishZone}
                            >
                                {publishing ? t('dns.publishing') : t('dns.republish')}
                            </Button>
                            <Button variant="primary" icon={Plus} onClick={() => setShowAddForm((s) => !s)}>
                                {t('dns.addRecord')}
                            </Button>
                        </>
                    )}
                </div>
            </div>

			{recordsError && (
				<div className="mb-4 flex items-start justify-between gap-3 rounded-lg border border-danger/30 bg-danger/10 p-3 text-sm text-fg">
					<span>{recordsError}</span>
					<Button variant="secondary" icon={RefreshCw} disabled={loading} onClick={() => void loadRecords()}>
						{t('common.retry')}
					</Button>
				</div>
			)}

            {!readOnly && showAddForm && (
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
                        <Button variant="primary" icon={Plus} disabled={mutatingRecord} onClick={addRecord}>
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
                                {!readOnly && <th className="px-4 py-2.5" />}
                            </tr>
                        </thead>
                        <tbody>
                            {records.map((rec) => {
                                const managedRecord = rec.type === 'SOA' || (rec.type === 'NS' && rec.name === domainName);
                                return (
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
                                    {!readOnly && (
                                        <td className="px-4 py-2.5 text-right">
                                            <button
                                                onClick={() => deleteRecord(rec.id)}
                                                disabled={managedRecord || mutatingRecord}
                                                aria-label={t('dns.confirmDelete')}
                                                title={managedRecord ? t('dns.managedRecord') : t('dns.confirmDelete')}
                                                className="rounded-md p-1.5 text-fg-subtle transition-colors hover:bg-surface-2 hover:text-danger disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent disabled:hover:text-fg-subtle"
                                            >
                                                <Trash2 className="h-4 w-4" />
                                            </button>
                                        </td>
                                    )}
                                </tr>
                                );
                            })}
                        </tbody>
                    </table>
                </div>
            )}
        </div>
    );
}


// A DS record is "KeyTag Algorithm DigestType Digest ; ( comment )". Registrars
// almost always want those four as separate form fields, so we split them.
// DS kaydı "KeyTag Algorithm DigestType Digest ; ( yorum )" biçimindedir. Kayıt
// operatörleri neredeyse her zaman bu dördünü ayrı alan olarak ister; ayırırız.
function parseDS(rec: string): { keyTag: string; algo: string; digestType: string; digest: string } {
    const main = rec.split(';')[0].trim();
    const parts = main.split(/\s+/);
    return {
        keyTag: parts[0] ?? '',
        algo: parts[1] ?? '',
        digestType: parts[2] ?? '',
        digest: parts.slice(3).join(''),
    };
}

// Human-readable names for the numeric codes, so the operator recognises the
// dropdown option at the registrar.
// Sayısal kodların insan-okur adları; operatör kayıt operatöründeki açılır
// seçeneği tanısın diye.
const algoLabel: Record<string, string> = {
    '8': 'RSA/SHA-256 (8)',
    '10': 'RSA/SHA-512 (10)',
    '13': 'ECDSA P-256/SHA-256 (13)',
    '14': 'ECDSA P-384/SHA-384 (14)',
    '15': 'Ed25519 (15)',
};
const digestLabel: Record<string, string> = {
    '1': 'SHA-1 (1)',
    '2': 'SHA-256 (2)',
    '4': 'SHA-384 (4)',
};

// One labelled, individually-copyable DS field.
// Etiketli, tek tek kopyalanabilir bir DS alanı.
function DSField({ label, value, note, mono }: { label: string; value: string; note?: string; mono?: boolean }) {
    const { t } = useI18n();
    return (
        <div className="min-w-0">
            <div className="mb-0.5 text-xs font-medium text-fg-subtle">{label}</div>
            <div className="flex items-center gap-1.5">
                <span className={`min-w-0 flex-1 truncate rounded bg-surface px-2 py-1 text-sm text-fg ${mono ? 'font-mono text-xs' : ''}`} title={value}>
                    {value}
                </span>
                <button
                    onClick={() => navigator.clipboard.writeText(value).then(() => showToast('success', t('vpn.copied')))}
                    title={t('vpn.copy')}
                    className="shrink-0 rounded-md p-1 text-fg-muted hover:bg-surface-2 hover:text-fg"
                >
                    <Copy className="h-3.5 w-3.5" />
                </button>
            </div>
            {note && <div className="mt-0.5 text-[11px] text-fg-subtle">{note}</div>}
        </div>
    );
}

// DNSSEC: sign the zone in one click and hand the operator the DS records to
// enter at the registrar. Without that DS, validators treat the zone (and
// its DANE/TLSA records) as insecure — so both live together here.
// DNSSEC: zone'u tek tıkla imzala ve operatöre registrar'a girilecek DS
// kayıtlarını ver. O DS olmadan doğrulayıcılar zone'u (ve DANE/TLSA
// kayıtlarını) güvensiz sayar — o yüzden ikisi burada birlikte yaşar.
function DNSSECSection({ domainId, readOnly = false }: { domainId: number; domainName: string; readOnly?: boolean }) {
    const { t } = useI18n();
    const [secured, setSecured] = useState(false);
    const [ds, setDs] = useState<string[]>([]);
	const [busy, setBusy] = useState(false);
	const [loaded, setLoaded] = useState(false);
	const [statusError, setStatusError] = useState('');

	useEffect(() => {
		fetch(`/api/v1/domains/${domainId}/dnssec`)
			.then(async (r) => {
				if (!r.ok) throw new Error(apiErrorText(await readApiError(r), t));
				return r.json();
			})
			.then((d) => {
				setSecured(d.secured === true);
				setDs(d.ds || []);
				setStatusError('');
			})
			.catch((e) => setStatusError(e instanceof Error && e.message ? e.message : t('common.error')))
			.finally(() => setLoaded(true));
	}, [domainId, t]);

    const sign = async () => {
        if (readOnly) return;
        setBusy(true);
		try {
			const r = await fetch(`/api/v1/domains/${domainId}/dnssec`, { method: 'POST' });
			if (!r.ok) throw new Error(apiErrorText(await readApiError(r), t));
			const d = await r.json();
            setSecured(d.secured === true);
            setDs(d.ds || []);
            setStatusError('');
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
			{statusError ? (
				<div className="flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/10 p-3 text-sm text-fg">
					<AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
					<span>{statusError}</span>
				</div>
			) : secured ? (
                <>
                    <p className="mb-3 text-sm text-fg-muted">{t('dnssec.dsHint')}</p>
                    {/* Field-by-field: most registrars (Hostinger et al.) ask for
                        Key Tag / Algorithm / Digest Type / Digest separately, not
                        the raw line. Show both — the fields to fill the form, the
                        raw line for registrars that take one string.
                        / Alan-alan: çoğu kayıt operatörü (Hostinger vb.) Key Tag /
                        Algorithm / Digest Type / Digest'i ayrı ayrı ister, ham
                        satırı değil. İkisini de göster. */}
                    <div className="space-y-3">
                        {ds.map((rec) => {
                            const f = parseDS(rec);
                            return (
                                <div key={rec} className="rounded-lg border border-border bg-surface-2/40 p-3">
                                    <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                                        <DSField label={t('dnssec.keyTag')} value={f.keyTag} />
                                        <DSField label={t('dnssec.algorithm')} value={f.algo} note={algoLabel[f.algo]} />
                                        <DSField label={t('dnssec.digestType')} value={f.digestType} note={digestLabel[f.digestType]} />
                                        <DSField label={t('dnssec.digest')} value={f.digest} mono />
                                    </div>
                                    <div className="mt-2 flex items-center gap-2 border-t border-border pt-2">
                                        <span className="shrink-0 text-xs text-fg-subtle">{t('dnssec.rawRecord')}</span>
                                        <code className="min-w-0 flex-1 overflow-x-auto rounded bg-surface-2 px-2 py-1 font-mono text-xs text-fg-muted">
                                            {rec}
                                        </code>
                                        <button
                                            onClick={() => navigator.clipboard.writeText(rec).then(() => showToast('success', t('vpn.copied')))}
                                            title={t('vpn.copy')}
                                            className="shrink-0 rounded-md p-1.5 text-fg-muted hover:bg-surface-2 hover:text-fg"
                                        >
                                            <Copy className="h-4 w-4" />
                                        </button>
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                </>
            ) : (
                <div className="flex flex-wrap items-center justify-between gap-3">
                    <p className="text-sm text-fg-muted">{t('dnssec.offHint')}</p>
                    {!readOnly && (
                        <Button variant="primary" disabled={busy} onClick={sign}>
                            {t('dnssec.sign')}
                        </Button>
                    )}
                </div>
            )}
        </section>
    );
}
