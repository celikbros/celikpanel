import { useEffect, useState } from 'react';
import { Server, Inbox, ShieldCheck, ShieldAlert, Copy, Check } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { apiErrorText, readApiError } from '../lib/apiError';
import { decodeMailSetup, mailSetupURL, type MailProtocol } from '../lib/mailSetup';
import { LOADING, readRemote, useRemote, type Remote } from '../lib/remote';
import { isStaleWrite, StaleNotice } from './CurrentSettings';
import { Button, Checking, CouldNotCheck, RemoteGate, inputClass } from './ui';

interface Props {
    domainId: number;
    domainName: string;
    readOnly?: boolean;
}

interface CatchAll {
    enabled: boolean;
    destination: string;
    // The catch-all this page read; every change carries it back.
    // Bu sayfanın okuduğu catch-all; her değişiklik onu geri taşır.
    version: string;
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

function decodeCatchAll(raw: unknown): CatchAll {
    const answer = raw as Partial<CatchAll> | null;
    if (!answer || typeof answer.enabled !== 'boolean' || typeof answer.version !== 'string' || !answer.version) {
        throw new Error('catch-all');
    }
    return { enabled: answer.enabled, destination: typeof answer.destination === 'string' ? answer.destination : '', version: answer.version };
}

function decodeRbl(raw: unknown): RblReport {
    const answer = raw as Partial<RblReport> | null;
    if (!answer || typeof answer.ip !== 'string' || !Array.isArray(answer.results)) throw new Error('rbl');
    return answer as RblReport;
}

// The "Settings" tab of the mail manager: how to configure a client, the
// domain catch-all, and an RBL blocklist check for the sending IP. Every
// value here is real — client settings are this server's actual ports, the
// RBL check is a live DNS lookup, the catch-all is persisted and pushed to
// postfix.
//
// Each of the three is read from the server and is drawn as being read, could
// not be read, or known (9 Oct 2026). The catch-all field holds nothing until
// the current address has been read: before, it could be typed into at once,
// and what was typed replaced an address the page had never shown.
//
// Mail yöneticisinin "Ayarlar" sekmesi: bir istemci nasıl ayarlanır, domain
// catch-all'ı ve gönderim IP'si için RBL kara-liste kontrolü. Buradaki her
// değer gerçektir. Üçü de sunucudan okunur ve okunuyor, okunamadı ya da
// biliniyor olarak çizilir. Catch-all alanı, geçerli adres okunana dek hiçbir
// şey tutmaz.
export function MailSettingsPanel({ domainId, domainName, readOnly = false }: Props) {
    const { t } = useI18n();
    const setup = useRemote(mailSetupURL(domainId), decodeMailSetup);
    const [rbl, setRbl] = useState<Remote<RblReport> | null>(null);

    useEffect(() => setRbl(null), [domainId]);

    const runRbl = async () => {
        setRbl(LOADING);
        setRbl(await readRemote(`/api/v1/domains/${domainId}/mail/rbl`, decodeRbl));
    };

    const report = rbl?.state === 'known' ? rbl.value : null;
    const listedCount = report?.results.filter((r) => r.listed).length ?? 0;

    return (
        <div className="space-y-6">
            {/* Client setup / İstemci kurulumu */}
            <section className="rounded-xl border border-border bg-surface p-5">
                <div className="mb-1 flex items-center gap-2">
                    <Server className="h-4 w-4 text-primary" aria-hidden="true" />
                    <h3 className="font-semibold text-fg">{t('mail.setup.title')}</h3>
                </div>
                <p className="mb-4 text-sm text-fg-muted">{t('mail.setup.hint')}</p>
                <div className="min-h-[7.5rem]">
                    <RemoteGate
                        remote={setup.remote}
                        checking={t('mail.setup.checking')}
                        failed={t('mail.setup.unknown')}
                        onRetry={() => void setup.retry()}
                        busy={setup.reading}
                    >
                        {({ value }) => (
                            <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                                <ProtoCard title="IMAP" sub={t('mail.setup.incoming')} proto={value.imap} />
                                <ProtoCard title="POP3" sub={t('mail.setup.incomingAlt')} proto={value.pop3} />
                                <ProtoCard title="SMTP" sub={t('mail.setup.outgoing')} proto={value.smtp} />
                            </div>
                        )}
                    </RemoteGate>
                </div>
                <p className="mt-3 flex flex-wrap items-start gap-1.5 text-xs text-fg-muted">
                    <span className="font-medium text-fg">{t('mail.setup.username')}:</span>
                    {t('mail.setup.usernameHint', { example: `info@${domainName}` })}
                </p>
            </section>

            <CatchAllSection domainId={domainId} domainName={domainName} readOnly={readOnly} />

            {/* RBL check / RBL kontrolü */}
            <section className="rounded-xl border border-border bg-surface p-5">
                <div className="mb-1 flex items-center gap-2">
                    <ShieldCheck className="h-4 w-4 text-primary" aria-hidden="true" />
                    <h3 className="font-semibold text-fg">{t('mail.rbl.title')}</h3>
                </div>
                <p className="mb-4 text-sm text-fg-muted">{t('mail.rbl.hint')}</p>
                <Button variant="secondary" onClick={() => void runRbl()} loading={rbl?.state === 'loading'}>
                    {rbl?.state === 'loading' ? t('mail.rbl.checking') : t('mail.rbl.check')}
                </Button>

                {/* A check that did not complete says nothing about the
                    address: neither "clean" nor "listed" is drawn for it.
                    Tamamlanmayan kontrol adres hakkında bir şey söylemez. */}
                {rbl?.state === 'unknown' && (
                    <CouldNotCheck className="mt-4" text={t('mail.rbl.unknown')} onRetry={() => void runRbl()} />
                )}

                {report && (
                    <div className="mt-4">
                        <div
                            className={`mb-3 flex items-center gap-2 rounded-lg border p-3 text-sm ${
                                listedCount === 0
                                    ? 'border-success/30 bg-success/10 text-success'
                                    : 'border-danger/30 bg-danger/10 text-danger'
                            }`}
                        >
                            {listedCount === 0 ? <ShieldCheck className="h-4 w-4" aria-hidden="true" /> : <ShieldAlert className="h-4 w-4" aria-hidden="true" />}
                            <span>
                                {listedCount === 0
                                    ? t('mail.rbl.clean', { ip: report.ip })
                                    : t('mail.rbl.listed', { ip: report.ip, n: listedCount })}
                            </span>
                        </div>
                        <ul className="divide-y divide-border rounded-lg border border-border">
                            {report.results.map((r) => (
                                <li key={r.zone} className="flex items-center justify-between gap-3 px-3 py-2 text-sm">
                                    <span className="min-w-0 break-all font-mono text-xs text-fg-muted">{r.zone}</span>
                                    {r.listed ? (
                                        <span className="inline-flex shrink-0 items-center gap-1.5 text-danger">
                                            <ShieldAlert className="h-3.5 w-3.5" aria-hidden="true" />
                                            {t('mail.rbl.onList')}
                                        </span>
                                    ) : (
                                        <span className="inline-flex shrink-0 items-center gap-1.5 text-success">
                                            <Check className="h-3.5 w-3.5" aria-hidden="true" />
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

// The domain's catch-all address. The address on the server is read first and
// shown; only then can it be changed, and every change carries the version of
// what was read, so it cannot replace an address this page did not show.
// Domain'in catch-all adresi. Önce sunucudaki adres okunur ve gösterilir;
// ancak ondan sonra değiştirilebilir.
function CatchAllSection({ domainId, domainName, readOnly }: { domainId: number; domainName: string; readOnly: boolean }) {
    const { t } = useI18n();
    const url = `/api/v1/domains/${domainId}/mail/catch-all`;
    const { remote, reading, retry } = useRemote(url, decodeCatchAll);
    const current = remote.state === 'known' ? remote.value : null;
    const [draft, setDraft] = useState('');
    const [busy, setBusy] = useState(false);
    const [stale, setStale] = useState(false);
    const [refused, setRefused] = useState('');

    // The field follows what the server holds each time that is read.
    // Alan, her okunduğunda sunucunun tuttuğunu izler.
    useEffect(() => {
        setDraft(current?.destination ?? '');
        setStale(false);
        setRefused('');
    }, [current?.version]);

    const change = async (method: 'PUT' | 'DELETE') => {
        if (readOnly || !current) return;
        setBusy(true);
        setRefused('');
        try {
            const res = method === 'PUT'
                ? await fetch(url, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ destination: draft.trim(), version: current.version }),
                })
                : await fetch(`${url}?version=${encodeURIComponent(current.version)}`, { method: 'DELETE' });
            if (!res.ok) {
                // A stale change keeps what was typed and says how to go on;
                // nothing was written.
                // Eskimiş değişiklik, yazılanı tutar; hiçbir şey yazılmadı.
                const error = await readApiError(res);
                if (isStaleWrite(error)) setStale(true);
                else setRefused(res.status === 400 ? t('mail.catchAllInvalid') : apiErrorText(error, t, 'mail.catchAll.notSaved'));
                return;
            }
            showToast('success', t(method === 'PUT' ? 'mail.catchAllSaved' : 'mail.catchAllRemoved'));
            await retry();
        } catch {
            setRefused(t('mail.catchAll.notSaved'));
        } finally {
            setBusy(false);
        }
    };

    const locked = readOnly || !current || busy || stale;
    const typed = draft.trim();
    return (
        <section className="rounded-xl border border-border bg-surface p-5" aria-busy={remote.state === 'loading' || busy}>
            <div className="mb-1 flex items-center gap-2">
                <Inbox className="h-4 w-4 text-primary" aria-hidden="true" />
                <h3 className="font-semibold text-fg">{t('mail.catchAll.title')}</h3>
            </div>
            <p className="mb-4 text-sm text-fg-muted">{t('mail.catchAll.hint', { domain: domainName })}</p>
            {remote.state === 'unknown' ? (
                <CouldNotCheck text={t('mail.catchAll.unknown')} onRetry={() => void retry()} busy={reading} />
            ) : (
                <>
                    {stale && (
                        <StaleNotice textKey="mail.catchAll.stale" actionKey="mail.catchAll.reload" onReload={() => void retry().then(() => setStale(false))} busy={reading} />
                    )}
                    {/* What is set now, before anything can be changed. While it
                        is being read this line is the reading line, in the same
                        place and at the same height.
                        Bir şey değiştirilmeden önce şimdi neyin ayarlı olduğu. */}
                    <div className="mb-3 flex min-h-[1.5rem] items-center">
                        {current ? (
                            <p className="break-words text-sm text-fg">
                                {current.enabled ? t('mail.catchAll.active', { destination: current.destination }) : t('mail.catchAll.none')}
                            </p>
                        ) : (
                            <Checking label={t('mail.catchAll.checking')} />
                        )}
                    </div>
                    <div className="flex flex-wrap items-end gap-3">
                        <label className="min-w-0 flex-1 basis-64">
                            <span className="mb-1 block text-xs text-fg-muted">{t('mail.catchAll.destination')}</span>
                            <input
                                type="email"
                                value={draft}
                                onChange={(e) => { setDraft(e.target.value); setRefused(''); }}
                                placeholder={current ? `inbox@${domainName}` : undefined}
                                className={`${inputClass} disabled:bg-surface-2 disabled:text-fg-muted`}
                                disabled={locked}
                                aria-invalid={refused ? true : undefined}
                                aria-describedby={refused ? 'catch-all-refused' : undefined}
                                autoComplete="off"
                            />
                        </label>
                        {!readOnly && (
                            <Button
                                variant="primary"
                                onClick={() => void change('PUT')}
                                loading={busy}
                                disabled={locked || !typed || (!!current && current.enabled && typed === current.destination)}
                            >
                                {current?.enabled ? t('mail.catchAll.update') : t('mail.catchAll.enable')}
                            </Button>
                        )}
                        {!readOnly && (!current || current.enabled) && (
                            <Button variant="secondary" onClick={() => void change('DELETE')} disabled={locked || !current?.enabled}>
                                {t('mail.catchAll.disable')}
                            </Button>
                        )}
                    </div>
                    {refused && (
                        <p id="catch-all-refused" role="alert" className="mt-2 max-w-[75ch] break-words text-xs leading-relaxed text-danger">{refused}</p>
                    )}
                </>
            )}
        </section>
    );
}

function ProtoCard({ title, sub, proto }: { title: string; sub: string; proto: MailProtocol }) {
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
                    <div className="text-xs text-fg-muted">{sub}</div>
                </div>
                <button onClick={copy} title={t('common.copy')} aria-label={t('common.copy')} className="rounded-md p-1.5 text-fg-muted hover:bg-surface-2 hover:text-fg">
                    {copied ? <Check className="h-3.5 w-3.5 text-success" aria-hidden="true" /> : <Copy className="h-3.5 w-3.5" aria-hidden="true" />}
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
            <dt className="text-fg-muted">{label}</dt>
            <dd className={`truncate text-fg ${mono ? 'font-mono' : ''}`}>{value}</dd>
        </div>
    );
}
