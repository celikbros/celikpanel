import { useState, useEffect } from 'react';
import { Mail, Activity, Trash2, RefreshCw, RotateCw } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, ErrorBanner, RemoteGate, StatusDot, inputClass } from './ui';
import type { TranslationKey } from '../i18n/en';
import { apiErrorText, readApiError, type ApiError } from '../lib/apiError';
import { decodeList, useRemote } from '../lib/remote';
import { CurrentGate, StaleNotice, isStaleWrite, readCurrent, type Current } from './CurrentSettings';

interface PostfixManagementProps {
    onBack: () => void;
}

interface PostfixQueueItem {
    id: string;
    size: string;
    sender: string;
    arrival: string;
    status: string;
}

const decodeQueue = (raw: unknown) => decodeList<PostfixQueueItem>(raw);

// The causes of an unreadable queue the server verifies itself. Any other
// answer gets the sentence that names no cause.
// Sunucunun kendisinin doğruladığı okunamayan-kuyruk nedenleri.
const queueCauses: Record<string, TranslationKey> = {
    postfix_config: 'postfix.queue.unreadable.postfix_config',
};

// The policy a failed save carries beside its error, or undefined. A body that
// is not JSON carries none; the caller then reads the policy again.
// Başarısız bir kaydın hatasının yanında taşıdığı politika; yoksa undefined.
function writtenPolicy(raw: string): MailPolicy | undefined {
    try {
        const body: unknown = JSON.parse(raw);
        const policy = body && typeof body === 'object' ? (body as { policy?: MailPolicy }).policy : undefined;
        return policy && typeof policy.version === 'string' && policy.version ? policy : undefined;
    } catch {
        return undefined;
    }
}

// The mail queue of this server.
//
// The queue is read from Postfix and is one of three things (9 Oct 2026): being
// read, could not be read, or known. "The mail queue is empty" and the counts
// are said only for a queue the server answered; a read that failed used to
// say "empty" too. Flush and delete act on the queue that is shown, so they
// are off until it is known, and their answer is read instead of assumed.
//
// Bu sunucunun posta kuyruğu. Kuyruk Postfix'ten okunur ve üç şeyden biridir:
// okunuyor, okunamadı ya da biliniyor. "Posta kuyruğu boş" ve sayılar yalnız
// sunucunun yanıtladığı bir kuyruk için söylenir.
export function PostfixManagement({ onBack }: PostfixManagementProps) {
    const { t } = useI18n();
    const [activeTab, setActiveTab] = useState<'queue' | 'logs'>('queue');
    const queue = useRemote('/api/v1/postfix/queue', decodeQueue);
    const [acting, setActing] = useState(false);
    const known = queue.remote.state === 'known' ? queue.remote.value : null;

    const queueAction = async (action: string, id?: string) => {
        if (!known) return;
        const msg =
            action === 'flush' ? t('postfix.confirmFlush') : action === 'delete_all' ? t('postfix.confirmDeleteAll') : t('postfix.confirmDelete', { id: id ?? '' });
        if (!confirm(msg)) return;
        setActing(true);
        try {
            const res = await fetch('/api/v1/postfix/queue', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ action, id }),
            });
            // The action is done only when the server says so.
            // Eylem, ancak sunucu söylediğinde yapılmıştır.
            if (!res.ok) showToast('error', apiErrorText(await readApiError(res), t, 'postfix.actionFailed'));
            else showToast('success', t('postfix.done'));
        } catch {
            showToast('error', t('postfix.actionFailed'));
        } finally {
            setActing(false);
            void queue.retry();
        }
    };

    const count = (status: string) => (known ?? []).filter((item) => item.status === status).length;

    // Why the queue could not be read (10 Oct 2026): the sentence for a cause
    // the server verified, else one that names no cause, and the line Postfix
    // itself printed. It used to say "check that Postfix is running", which a
    // stopped Postfix does not cause and was not the cause when measured.
    // Kuyruğun neden okunamadığı: sunucunun doğruladığı nedenin cümlesi, yoksa
    // neden adlandırmayan cümle ve Postfix'in kendi yazdığı satır.
    const queueUnreadable = () => {
        const error = queue.remote.state === 'unknown' ? queue.remote.reason : null;
        if (error?.code !== 'MAIL_QUEUE_UNREADABLE') return t('postfix.queue.unreadable');
        const sentence = t(queueCauses[error.reason ?? ''] ?? 'postfix.queue.unreadable');
        const detail = error.vars?.detail;
        return detail ? `${sentence} ${t('postfix.queue.said', { detail })}` : sentence;
    };

    return (
        <ServiceShell serviceId="postfix" name="Postfix" icon={Mail} onBack={onBack}>
            <div className="mb-4 flex items-center gap-1 border-b border-border">
                <Tab active={activeTab === 'queue'} onClick={() => setActiveTab('queue')} icon={Mail} label={t('postfix.tab.queue')} />
                <Tab active={activeTab === 'logs'} onClick={() => setActiveTab('logs')} icon={Activity} label={t('postfix.tab.logs')} />
            </div>

            {activeTab === 'logs' ? (
                <div className="rounded-xl border border-border bg-surface p-10 text-center text-fg-muted">
                    <Activity className="mx-auto mb-3 h-10 w-10 opacity-40" aria-hidden="true" />
                    <p>{t('postfix.logsSoon')}</p>
                </div>
            ) : (
                <div aria-busy={queue.reading || acting}>
                    {/* The counts are the queue's own: they exist once it has
                        been read, in the place they keep.
                        Sayılar kuyruğun kendisinindir; okunduğunda vardır. */}
                    {known && (
                        <div className="mb-4 grid grid-cols-2 gap-3 lg:grid-cols-4">
                            <Stat labelKey="postfix.active" value={count('active')} />
                            <Stat labelKey="postfix.deferred" value={count('deferred')} accent={count('deferred') > 0} />
                            <Stat labelKey="postfix.hold" value={count('hold')} />
                            <Stat labelKey="postfix.corrupt" value={count('corrupt')} danger={count('corrupt') > 0} />
                        </div>
                    )}

                    <div className="mb-3 flex flex-wrap items-center justify-end gap-2">
                        <Button variant="secondary" icon={RefreshCw} loading={queue.reading && !!known} onClick={() => void queue.retry()}>
                            {t('postfix.refresh')}
                        </Button>
                        <Button variant="secondary" icon={RotateCw} onClick={() => queueAction('flush')} disabled={!known || acting}>
                            {t('postfix.flush')}
                        </Button>
                        <Button variant="danger" icon={Trash2} onClick={() => queueAction('delete_all')} disabled={!known || known.length === 0 || acting}>
                            {t('postfix.deleteAll')}
                        </Button>
                    </div>

                    <RemoteGate
                        remote={queue.remote}
                        checking={t('postfix.queue.checking')}
                        failed={queueUnreadable()}
                        onRetry={() => void queue.retry()}
                        busy={queue.reading}
                        className="min-h-[2.75rem]"
                    >
                        {({ value: items, stale }) => (items.length === 0 ? (
                            <div className="rounded-xl border border-border bg-surface p-12 text-center">
                                <Mail className="mx-auto mb-3 h-10 w-10 text-fg-subtle" aria-hidden="true" />
                                <p className="text-fg-muted">{t('postfix.empty')}</p>
                            </div>
                        ) : (
                            <div className="overflow-x-auto rounded-xl border border-border-strong bg-surface">
                                <table className="w-full text-sm">
                                    <thead>
                                        <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                                            <th className="px-4 py-2.5">ID</th>
                                            <th className="px-4 py-2.5">{t('postfix.col.sender')}</th>
                                            <th className="px-4 py-2.5">{t('postfix.col.size')}</th>
                                            <th className="px-4 py-2.5">{t('postfix.col.arrival')}</th>
                                            <th className="px-4 py-2.5">{t('domains.col.status')}</th>
                                            <th className="px-4 py-2.5" />
                                        </tr>
                                    </thead>
                                    <tbody>
                                        {items.map((item) => (
                                            <tr key={item.id} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                                <td className="px-4 py-2.5 font-mono text-fg">{item.id}</td>
                                                <td className="break-all px-4 py-2.5 text-fg-muted">{item.sender}</td>
                                                <td className="whitespace-nowrap px-4 py-2.5 text-fg-muted">{item.size}</td>
                                                <td className="whitespace-nowrap px-4 py-2.5 text-fg-muted">{item.arrival}</td>
                                                <td className="px-4 py-2.5">
                                                    <span className="inline-flex items-center gap-1.5 text-fg-muted">
                                                        <StatusDot ok={item.status === 'active'} />
                                                        {item.status}
                                                    </span>
                                                </td>
                                                <td className="px-4 py-2.5 text-right">
                                                    <button
                                                        onClick={() => queueAction('delete_id', item.id)}
                                                        disabled={stale || acting}
                                                        aria-label={t('postfix.deleteMessage', { id: item.id })}
                                                        title={t('postfix.deleteMessage', { id: item.id })}
                                                        className="rounded-md p-1.5 text-fg-subtle transition-colors hover:bg-surface-2 hover:text-danger disabled:pointer-events-none disabled:opacity-50"
                                                    >
                                                        <Trash2 className="h-4 w-4" aria-hidden="true" />
                                                    </button>
                                                </td>
                                            </tr>
                                        ))}
                                    </tbody>
                                </table>
                            </div>
                        ))}
                    </RemoteGate>
                </div>
            )}
            <MailPolicySection />
        </ServiceShell>
    );
}

function Tab({ active, onClick, icon: Icon, label }: { active: boolean; onClick: () => void; icon: typeof Mail; label: string }) {
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

function Stat({ labelKey, value, accent, danger }: { labelKey: TranslationKey; value: number; accent?: boolean; danger?: boolean }) {
    const { t } = useI18n();
    const color = danger ? 'text-danger' : accent ? 'text-warning' : 'text-fg';
    return (
        <div className="rounded-xl border border-border bg-surface p-4">
            <p className="text-xs font-medium uppercase tracking-wide text-fg-subtle">{t(labelKey)}</p>
            <p className={`mt-1 text-2xl font-bold ${color}`}>{value}</p>
        </div>
    );
}


// Server-wide mail policy (Plesk "server-wide mail settings" core): the
// message size limit and DNSBL protection for incoming mail. Admin-only.
// Sunucu geneli posta politikası: mesaj boyutu sınırı ve gelen posta için
// DNSBL koruması. Yalnız yönetici.
interface MailPolicy {
    message_size_mb: number;
    dnsbl_zones: string[] | null;
    outbound_rate_limit: number;
    // The Postfix values this policy was read from; every save carries it back.
    // Bu politikanın okunduğu Postfix değerleri; her kayıt onu geri taşır.
    version: string;
    // Set when CelikPanel will not rewrite the recipient restrictions it found.
    // CelikPanel bulduğu alıcı kısıtlarını yeniden yazmayacaksa dolar.
    dnsbl_locked?: string;
}

function MailPolicySection() {
    const { t } = useI18n();
    // The form holds nothing until the server's policy has been read: a form
    // filled with defaults was one Save away from replacing the real one.
    // Sunucunun politikası okunana kadar form hiçbir şey tutmaz.
    const [current, setCurrent] = useState<Current<MailPolicy>>({ state: 'loading' });
    const [sizeMB, setSizeMB] = useState(0);
    const [dnsblOn, setDnsblOn] = useState(false);
    const [zones, setZones] = useState('');
    // Outbound rate limit: messages per minute per sending client. 0 = off.
    // Guards against becoming a spam source (a compromised customer account).
    // Giden hız sınırı: gönderen istemci başına dakikadaki mesaj. 0 = kapalı.
    // Spam kaynağı olmaya (ele geçirilmiş müşteri hesabı) karşı koruma.
    const [rateOn, setRateOn] = useState(false);
    const [rate, setRate] = useState(30);
    const [busy, setBusy] = useState(false);
    const [stale, setStale] = useState(false);
    // The policy was written to main.cf and Postfix was not seen to take it: a
    // verified failure after a change (9 Oct 2026), or, when the server could
    // not establish what Postfix did, an unknown outcome (10 Oct 2026). It
    // stays on screen above the saved values until a save succeeds or the
    // policy is read again.
    // Politika main.cf'e yazıldı ve Postfix'in onu aldığı görülmedi: bir
    // değişiklik sonrası doğrulanmış hata ya da bilinmeyen sonuç. Ekranda,
    // kaydedilen değerlerin üstünde kalır.
    const [notReloaded, setNotReloaded] = useState<ApiError | null>(null);

    const show = (policy: MailPolicy) => {
        const z = policy.dnsbl_zones || [];
        setSizeMB(policy.message_size_mb);
        setDnsblOn(z.length > 0);
        setZones(z.length > 0 ? z.join(', ') : 'zen.spamhaus.org');
        setRateOn(policy.outbound_rate_limit > 0);
        setRate(policy.outbound_rate_limit > 0 ? policy.outbound_rate_limit : 30);
        setStale(false);
        setCurrent({ state: 'known', value: policy });
    };

    const load = async () => {
        setNotReloaded(null);
        setCurrent({ state: 'loading' });
        const next = await readCurrent<MailPolicy>('/api/v1/mail/policy');
        if (next.state === 'known') show(next.value);
        else setCurrent(next);
    };

    useEffect(() => {
        void load();
    }, []);

    const policy = current.state === 'known' ? current.value : null;
    const dnsblLocked = policy?.dnsbl_locked || '';

    const save = async () => {
        if (!policy) return;
        setBusy(true);
        try {
            const r = await fetch('/api/v1/mail/policy', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    message_size_mb: sizeMB,
                    dnsbl_zones: dnsblLocked
                        ? policy.dnsbl_zones || []
                        : dnsblOn ? zones.split(',').map((z) => z.trim()).filter(Boolean) : [],
                    outbound_rate_limit: rateOn ? rate : 0,
                    version: policy.version,
                }),
            });
            if (!r.ok) {
                // A stale save keeps what was typed on screen and says how to
                // go on; nothing was written.
                // Eskimiş kayıt, yazılanı ekranda tutar ve nasıl sürüleceğini söyler.
                // The body is read once more beside the error: after a write
                // that Postfix did not take, it also carries the policy that
                // is in main.cf now.
                // Gövde hatanın yanında bir kez daha okunur: Postfix'in
                // almadığı bir yazıdan sonra main.cf'teki politikayı da taşır.
                const raw = await r.text();
                const written = writtenPolicy(raw);
                const error = await readApiError(new Response(raw, { status: r.status }));
                if (isStaleWrite(error)) setStale(true);
                else if (error.code === 'MAIL_POLICY_NOT_RELOADED' || error.code === 'MAIL_POLICY_RELOAD_UNKNOWN') {
                    // main.cf holds the new values: show them, with the version
                    // the next save carries, and say what is known about
                    // Postfix. Only an answer without them is read again.
                    // main.cf yeni değerleri tutuyor: göster ve Postfix
                    // hakkında bilineni söyle.
                    if (written) show(written);
                    else await load();
                    setNotReloaded(error);
                } else showToast('error', apiErrorText(error, t));
                return;
            }
            setNotReloaded(null);
            const answer = (await r.json()) as { policy?: MailPolicy; applied?: string };
            const saved = answer.policy;
            // "Applied" is said only when the server verified the reload. A
            // stopped Postfix was left stopped, and an unchanged policy wrote
            // nothing: each says so.
            // "Uygulandı", yalnız sunucu yeniden yüklemeyi doğruladığında
            // söylenir.
            showToast('success', t(
                answer.applied === 'not_running' ? 'mailpolicy.saved.notRunning'
                    : answer.applied === 'unchanged' ? 'mailpolicy.saved.unchanged'
                        : 'mailpolicy.saved',
            ));
            if (saved?.version) show(saved);
            else void load();
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setBusy(false);
        }
    };

    const lockedCause = () => {
        const key = `mailpolicy.dnsblLocked.${dnsblLocked}` as TranslationKey;
        const text = t(key);
        return text === key ? t('mailpolicy.dnsblLocked.other') : text;
    };

    return (
        <section className="mt-5 rounded-xl border border-border bg-surface p-5" aria-busy={current.state === 'loading' || busy}>
            <h3 className="mb-1 text-sm font-semibold text-fg">{t('mailpolicy.title')}</h3>
            <p className="mb-4 text-sm text-fg-muted">{t('mailpolicy.desc')}</p>
            <CurrentGate state={current.state} unknownKey="mailpolicy.unreadable" onRetry={load} />
            {policy && (
                <>
                    {notReloaded && (
                        <div role="alert" className="mb-4">
                            <ErrorBanner error={notReloaded} />
                            {notReloaded.vars?.detail && (
                                <p className="mt-1.5 max-w-[75ch] break-words text-xs text-fg-muted">
                                    {t(notReloaded.reason === 'check' || notReloaded.reason === 'reload' ? 'mailpolicy.postfixSaid' : 'mailpolicy.observed')}{' '}
                                    <span className="font-mono text-fg">{notReloaded.vars.detail}</span>
                                </p>
                            )}
                        </div>
                    )}
                    {stale && <StaleNotice textKey="mailpolicy.stale" onReload={load} />}
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                        <label className="text-sm">
                            <span className="mb-1 block text-xs text-fg-muted">{t('mailpolicy.maxSize')}</span>
                            <input type="number" min={1} max={200} value={sizeMB}
                                onChange={(e) => setSizeMB(Math.max(1, Math.min(200, parseInt(e.target.value) || 25)))}
                                className={inputClass} />
                        </label>
                        {!dnsblLocked && (
                            <label className="text-sm">
                                <span className="mb-1 block text-xs text-fg-muted">{t('mailpolicy.zones')}</span>
                                <input value={zones} onChange={(e) => setZones(e.target.value)}
                                    disabled={!dnsblOn} className={inputClass} placeholder="zen.spamhaus.org" />
                            </label>
                        )}
                    </div>
                    {dnsblLocked ? (
                        // The DNSBL entries on this server are the owner's own
                        // arrangement: say why they are not offered and what
                        // changes them, instead of showing them as "off".
                        // Bu sunucudaki DNSBL girdileri sahibin kendi düzenidir.
                        <div role="note" className="mt-3 max-w-[75ch] space-y-1 break-words rounded-lg border border-border bg-surface-2/50 p-3 text-sm leading-relaxed text-fg-muted">
                            <p className="text-fg">{lockedCause()}</p>
                            <p>{t('mailpolicy.dnsblLockedAction')}</p>
                        </div>
                    ) : (
                        <>
                            <label className="mt-3 flex min-h-[1.75rem] items-center gap-2 text-sm text-fg">
                                <input type="checkbox" checked={dnsblOn} onChange={(e) => setDnsblOn(e.target.checked)} className="h-4 w-4" />
                                {t('mailpolicy.dnsbl')}
                            </label>
                            <p className="mt-1 text-xs text-fg-subtle">{t('mailpolicy.dnsblHint')}</p>
                        </>
                    )}

                    <label className="mt-4 flex min-h-[1.75rem] items-center gap-2 text-sm text-fg">
                        <input type="checkbox" checked={rateOn} onChange={(e) => setRateOn(e.target.checked)} className="h-4 w-4" />
                        {t('mailpolicy.rate')}
                    </label>
                    <p className="mt-1 text-xs text-fg-subtle">{t('mailpolicy.rateHint')}</p>
                    {rateOn && (
                        <label className="mt-2 block max-w-xs text-sm">
                            <span className="mb-1 block text-xs text-fg-muted">{t('mailpolicy.rateLabel')}</span>
                            <input type="number" min={1} max={10000} value={rate}
                                onChange={(e) => setRate(Math.max(1, Math.min(10000, parseInt(e.target.value) || 30)))}
                                className={inputClass} />
                        </label>
                    )}

                    <div className="mt-4">
                        <Button variant="primary" disabled={busy || stale} onClick={save}>{t('mailpolicy.save')}</Button>
                    </div>
                </>
            )}
        </section>
    );
}
