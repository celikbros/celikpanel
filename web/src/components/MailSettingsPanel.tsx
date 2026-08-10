import { useEffect, useState } from 'react';
import { Server, Inbox, ShieldCheck, ShieldAlert, Loader2, Copy, Check } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, inputClass } from './ui';

interface Props {
    domainId: number;
    domainName: string;
    readOnly?: boolean;
}

interface SetupProtocol {
    host: string;
    port: number;
    security: string;
    auth_required?: boolean;
}
interface Setup {
    mail_host: string;
    imap: SetupProtocol;
    pop3: SetupProtocol;
    smtp: SetupProtocol;
    username_is_full_email: boolean;
}
interface RblResult {
    zone: string;
    listed: boolean;
    detail?: string;
}
interface RblReport {
    ip: string;
    results: RblResult[];
}

// The "Settings" tab of the mail manager: how to configure a client, the
// domain catch-all, and an RBL blocklist check for the sending IP. Every
// value here is real — client settings are this server's actual ports, the
// RBL check is a live DNS lookup, the catch-all is persisted and pushed to
// postfix.
// Mail yöneticisinin "Ayarlar" sekmesi: bir istemci nasıl ayarlanır, domain
// catch-all'ı ve gönderim IP'si için RBL kara-liste kontrolü. Buradaki her
// değer gerçektir.
export function MailSettingsPanel({ domainId, domainName, readOnly = false }: Props) {
    const { t } = useI18n();
    const [setup, setSetup] = useState<Setup | null>(null);
    const [catchAll, setCatchAll] = useState('');
    const [catchAllEnabled, setCatchAllEnabled] = useState(false);
    const [savingCatchAll, setSavingCatchAll] = useState(false);
    const [rbl, setRbl] = useState<RblReport | null>(null);
    const [rblLoading, setRblLoading] = useState(false);

    useEffect(() => {
        fetch(`/api/v1/domains/${domainId}/mail/setup`)
            .then((r) => (r.ok ? r.json() : null))
            .then(setSetup)
            .catch(() => {});
        fetch(`/api/v1/domains/${domainId}/mail/catch-all`)
            .then((r) => (r.ok ? r.json() : null))
            .then((d) => {
                if (d) {
                    setCatchAllEnabled(!!d.enabled);
                    setCatchAll(d.destination || '');
                }
            })
            .catch(() => {});
    }, [domainId]);

    const saveCatchAll = async () => {
        if (readOnly) return;
        setSavingCatchAll(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/mail/catch-all`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ destination: catchAll.trim() }),
            });
            if (!res.ok) throw new Error();
            setCatchAllEnabled(true);
            showToast('success', t('mail.catchAllSaved'));
        } catch {
            showToast('error', t('mail.catchAllInvalid'));
        } finally {
            setSavingCatchAll(false);
        }
    };

    const removeCatchAll = async () => {
        if (readOnly) return;
        setSavingCatchAll(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/mail/catch-all`, { method: 'DELETE' });
            if (!res.ok) throw new Error();
            setCatchAllEnabled(false);
            setCatchAll('');
            showToast('success', t('mail.catchAllRemoved'));
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setSavingCatchAll(false);
        }
    };

    const runRbl = async () => {
        setRblLoading(true);
        setRbl(null);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/mail/rbl`);
            if (!res.ok) throw new Error();
            setRbl(await res.json());
        } catch {
            showToast('error', t('mail.rblFailed'));
        } finally {
            setRblLoading(false);
        }
    };

    const listedCount = rbl?.results.filter((r) => r.listed).length ?? 0;

    return (
        <div className="space-y-6">
            {/* Client setup / İstemci kurulumu */}
            <section className="rounded-xl border border-border bg-surface p-5">
                <div className="mb-1 flex items-center gap-2">
                    <Server className="h-4 w-4 text-primary" />
                    <h3 className="font-semibold text-fg">{t('mail.setup.title')}</h3>
                </div>
                <p className="mb-4 text-sm text-fg-muted">{t('mail.setup.hint')}</p>
                {setup ? (
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                        <ProtoCard title="IMAP" sub={t('mail.setup.incoming')} proto={setup.imap} />
                        <ProtoCard title="POP3" sub={t('mail.setup.incomingAlt')} proto={setup.pop3} />
                        <ProtoCard title="SMTP" sub={t('mail.setup.outgoing')} proto={setup.smtp} />
                    </div>
                ) : (
                    <div className="py-4 text-sm text-fg-subtle">{t('common.loading')}</div>
                )}
                <p className="mt-3 flex items-start gap-1.5 text-xs text-fg-subtle">
                    <span className="font-medium text-fg-muted">{t('mail.setup.username')}:</span>
                    {t('mail.setup.usernameHint', { example: `info@${domainName}` })}
                </p>
            </section>

            {/* Catch-all */}
            <section className="rounded-xl border border-border bg-surface p-5">
                <div className="mb-1 flex items-center gap-2">
                    <Inbox className="h-4 w-4 text-primary" />
                    <h3 className="font-semibold text-fg">{t('mail.catchAll.title')}</h3>
                </div>
                <p className="mb-4 text-sm text-fg-muted">{t('mail.catchAll.hint', { domain: domainName })}</p>
                <div className="flex flex-wrap items-end gap-3">
                    <label className="min-w-0 flex-1">
                        <span className="mb-1 block text-xs text-fg-muted">{t('mail.catchAll.destination')}</span>
                        <input
                            value={catchAll}
                            onChange={(e) => setCatchAll(e.target.value)}
                            placeholder={`inbox@${domainName}`}
                            className={inputClass}
                            disabled={readOnly}
                        />
                    </label>
                    {!readOnly && (
                        <Button variant="primary" onClick={saveCatchAll} disabled={savingCatchAll || !catchAll.trim()}>
                            {catchAllEnabled ? t('mail.catchAll.update') : t('mail.catchAll.enable')}
                        </Button>
                    )}
                    {!readOnly && catchAllEnabled && (
                        <Button variant="secondary" onClick={removeCatchAll} disabled={savingCatchAll}>
                            {t('mail.catchAll.disable')}
                        </Button>
                    )}
                </div>
                {catchAllEnabled && (
                    <p className="mt-2 text-xs text-success">{t('mail.catchAll.active', { destination: catchAll })}</p>
                )}
            </section>

            {/* RBL check / RBL kontrolü */}
            <section className="rounded-xl border border-border bg-surface p-5">
                <div className="mb-1 flex items-center gap-2">
                    <ShieldCheck className="h-4 w-4 text-primary" />
                    <h3 className="font-semibold text-fg">{t('mail.rbl.title')}</h3>
                </div>
                <p className="mb-4 text-sm text-fg-muted">{t('mail.rbl.hint')}</p>
                <Button variant="secondary" onClick={runRbl} disabled={rblLoading}>
                    {rblLoading ? (
                        <>
                            <Loader2 className="h-4 w-4 animate-spin" />
                            {t('mail.rbl.checking')}
                        </>
                    ) : (
                        t('mail.rbl.check')
                    )}
                </Button>

                {rbl && (
                    <div className="mt-4">
                        <div
                            className={`mb-3 flex items-center gap-2 rounded-lg border p-3 text-sm ${
                                listedCount === 0
                                    ? 'border-success/30 bg-success/10 text-success'
                                    : 'border-danger/30 bg-danger/10 text-danger'
                            }`}
                        >
                            {listedCount === 0 ? <ShieldCheck className="h-4 w-4" /> : <ShieldAlert className="h-4 w-4" />}
                            <span>
                                {listedCount === 0
                                    ? t('mail.rbl.clean', { ip: rbl.ip })
                                    : t('mail.rbl.listed', { ip: rbl.ip, n: listedCount })}
                            </span>
                        </div>
                        <ul className="divide-y divide-border rounded-lg border border-border">
                            {rbl.results.map((r) => (
                                <li key={r.zone} className="flex items-center justify-between px-3 py-2 text-sm">
                                    <span className="font-mono text-xs text-fg-muted">{r.zone}</span>
                                    {r.listed ? (
                                        <span className="inline-flex items-center gap-1.5 text-danger">
                                            <ShieldAlert className="h-3.5 w-3.5" />
                                            {t('mail.rbl.onList')}
                                        </span>
                                    ) : (
                                        <span className="inline-flex items-center gap-1.5 text-success">
                                            <Check className="h-3.5 w-3.5" />
                                            {t('mail.rbl.notListed')}
                                        </span>
                                    )}
                                </li>
                            ))}
                        </ul>
                    </div>
                )}
            </section>
        </div>
    );
}

function ProtoCard({ title, sub, proto }: { title: string; sub: string; proto: SetupProtocol }) {
    const { t } = useI18n();
    const [copied, setCopied] = useState(false);
    const copy = () => {
        navigator.clipboard?.writeText(`${proto.host}:${proto.port}`).then(() => {
            setCopied(true);
            setTimeout(() => setCopied(false), 1200);
        });
    };
    return (
        <div className="rounded-lg border border-border bg-surface-2/40 p-3">
            <div className="mb-2 flex items-center justify-between">
                <div>
                    <div className="text-sm font-semibold text-fg">{title}</div>
                    <div className="text-xs text-fg-subtle">{sub}</div>
                </div>
                <button onClick={copy} title={t('common.copy')} className="rounded-md p-1 text-fg-subtle hover:bg-surface-2 hover:text-fg">
                    {copied ? <Check className="h-3.5 w-3.5 text-success" /> : <Copy className="h-3.5 w-3.5" />}
                </button>
            </div>
            <dl className="space-y-1 text-xs">
                <Row label={t('mail.setup.server')} value={proto.host} mono />
                <Row label={t('mail.setup.port')} value={String(proto.port)} mono />
                <Row label={t('mail.setup.security')} value={proto.security} />
            </dl>
        </div>
    );
}

function Row({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
    return (
        <div className="flex items-center justify-between gap-2">
            <dt className="text-fg-subtle">{label}</dt>
            <dd className={`truncate text-fg ${mono ? 'font-mono' : ''}`}>{value}</dd>
        </div>
    );
}
