import { useState } from 'react';
import { FileCode, Files, Hexagon, ArrowLeftRight, ExternalLink, Play, Square, RotateCw, type LucideIcon } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { Button, Checking, CouldNotCheck, RemoteGate, ResultUnknown, StatusDot, inputClass } from './ui';
import { useHostingCapabilities } from '../lib/hostingCapabilities';
import { readApiError, apiErrorText } from '../lib/apiError';
import { decodeListIn, lastKnown, useRefreshEvery, useRemote } from '../lib/remote';
import { useLostAnswer, type LostAnswerHandle } from '../lib/lostAnswer';

// Hosting type for a domain (roadmap 3A): pick what the site IS, fill the
// type-specific fields, apply. For node projects the live application panel
// (systemd state, PID/memory, logs, start/stop) appears below — all real
// data from the agent.
//
// Bir domain'in barındırma tipi (yol haritası 3A): sitenin NE olduğunu seç,
// tipe özgü alanları doldur, uygula. Node projelerinde canlı uygulama paneli
// (systemd durumu, PID/bellek, günlükler, başlat/durdur) altta görünür —
// hepsi agent'tan gerçek veri.

interface HostingState {
    project_type: string;
    app_port?: number;
    start_command?: string;
    runtime_version?: string;
    forward_to?: string;
    forward_code?: number;
    php_version?: string;
    document_root?: string;
}

const typeDefs: { id: string; icon: LucideIcon; labelKey: TranslationKey; descKey: TranslationKey }[] = [
    { id: 'php', icon: FileCode, labelKey: 'hosting.type.php', descKey: 'hosting.desc.php' },
    { id: 'static', icon: Files, labelKey: 'hosting.type.static', descKey: 'hosting.desc.static' },
    { id: 'node', icon: Hexagon, labelKey: 'hosting.type.node', descKey: 'hosting.desc.node' },
    { id: 'proxy', icon: ArrowLeftRight, labelKey: 'hosting.type.proxy', descKey: 'hosting.desc.proxy' },
    { id: 'forwarding', icon: ExternalLink, labelKey: 'hosting.type.forwarding', descKey: 'hosting.desc.forwarding' },
];

// The saved hosting settings of a domain: an answer without a project type is
// not the contract. Nothing is filled in.
// Bir alan adının kayıtlı barındırma ayarları: proje tipi taşımayan yanıt
// sözleşme değildir. Hiçbir şey doldurulmaz.
function decodeHosting(raw: unknown): HostingState {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('shape');
    if (typeof (raw as { project_type?: unknown }).project_type !== 'string') throw new Error('field');
    return raw as HostingState;
}

// The settings Apply sends, by the type they belong to. A value the server may
// rewrite is compared as text without surrounding space.
// Uygula'nın gönderdiği ayarlar, ait oldukları tipe göre.
const sentFields: Record<string, (keyof HostingState)[]> = {
    node: ['start_command', 'runtime_version'],
    proxy: ['forward_to'],
    forwarding: ['forward_to', 'forward_code'],
};
const sameField = (a: HostingState, b: HostingState, name: keyof HostingState) => String(a[name] ?? '').trim() === String(b[name] ?? '').trim();

// Whether the settings that were read again show what Apply sent, judged only
// from the answers (10 Oct 2026): the type and its own fields as sent, yes; the
// same settings as before the change, no; anything else is not decided here.
// Yeniden okunan ayarların Uygula'nın gönderdiğini gösterip göstermediği:
// tip ve alanları gönderildiği gibiyse evet; değişiklikten öncekiyle aynıysa
// hayır; başka her durumda burada karar verilmez.
function showsHosting(before: HostingState, now: HostingState, sent: HostingState): boolean | null {
    const names = sentFields[sent.project_type] ?? [];
    if (now.project_type === sent.project_type && names.every((name) => sameField(now, sent, name))) return true;
    const all: (keyof HostingState)[] = ['project_type', 'start_command', 'runtime_version', 'forward_to', 'forward_code'];
    return all.every((name) => sameField(now, before, name)) ? false : null;
}

const NODE_RUNTIMES_URL = '/api/v1/runtimes/node';
const decodeNodeRuntimes = (raw: unknown) => decodeListIn<string>(raw, 'installed');

export function HostingTypePanel({ domainId }: { domainId: number; domainName: string }) {
    const { t } = useI18n();
    const [saving, setSaving] = useState(false);

    // A type whose requirement is missing on this server must say so instead
    // of failing at Apply: switching to PHP needs PHP-FPM installed. PHP is
    // marked unavailable only when the server is KNOWN to have none. While
    // that is being checked nothing is marked; when it could not be checked
    // the panel says so below the picker and offers the read again.
    // Gereksinimi bu sunucuda eksik olan bir tip, Uygula'da patlamak yerine
    // bunu söylemeli: PHP'ye geçmek kurulu PHP-FPM ister. PHP, ancak sunucuda
    // hiç olmadığı BİLİNİYORSA kullanılamaz gösterilir. Kontrol edilirken
    // hiçbir şey işaretlenmez; kontrol edilemediğinde bölüm bunu seçicinin
    // altında söyler ve okumayı yeniden sunar.
    const capabilities = useHostingCapabilities();
    const phpKnownMissing = capabilities.remote.state === 'known' && capabilities.remote.value.php_versions.length === 0;

    // The form exists only for settings the server sent. What the person
    // changes is kept beside the answer it was changed from; a newer answer
    // (after Apply, or after a change whose answer was lost) is what the form
    // shows next, so the screen never goes on showing a draft as if it were
    // saved.
    // Form yalnız sunucunun gönderdiği ayarlar için vardır. Kişinin değiştirdiği,
    // değiştirildiği yanıtın yanında tutulur; daha yeni bir yanıt (Uygula'dan
    // ya da yanıtı yiten değişiklikten sonra) formun gösterdiği olur.
    const hosting = useRemote(`/api/v1/domains/${domainId}/hosting`, decodeHosting);
    const [edit, setEdit] = useState<{ from: number; value: HostingState } | null>(null);
    const answer = useLostAnswer(() => hosting.retry());

    const apply = async (form: HostingState) => {
        if (hosting.remote.state !== 'known' || answer.holding) return;
        const before = hosting.remote.value;
        setSaving(true);
        try {
            const res = await answer.send(`/api/v1/domains/${domainId}/hosting`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(form),
            }, {
                // If the answer is lost and the settings read again are still
                // the earlier ones, what was typed is put back over that newer
                // answer instead of being replaced by it unseen.
                // Yanıt yiter ve yeniden okunan ayarlar hâlâ öncekilerse, yazılan
                // o yeni yanıtın üzerine geri konur.
                shows: (read) => showsHosting(before, read[0].value as HostingState, form),
                notMade: (read) => setEdit({ from: read[0].observedAt, value: form }),
            });
            if (!res) return;
            if (!res.ok) {
                showToast('error', apiErrorText(await readApiError(res), t));
                return;
            }
            showToast('success', t('hosting.saved'));
            answer.settle();
            await hosting.retry();
        } finally {
            setSaving(false);
        }
    };

    return (
        <RemoteGate
            remote={hosting.remote}
            checking={t('hosting.checking')}
            failed={t('hosting.unknown')}
            onRetry={() => void hosting.retry()}
            busy={hosting.reading}
        >
            {(shown) => (
                <HostingForm
                    domainId={domainId}
                    saved={shown.value}
                    state={edit && edit.from === shown.observedAt ? edit.value : shown.value}
                    setState={(value) => setEdit({ from: shown.observedAt, value })}
                    phpKnownMissing={phpKnownMissing}
                    capabilities={capabilities}
                    answer={answer}
                    canApply={!shown.stale && !hosting.reading && !saving && !answer.holding}
                    onApply={apply}
                />
            )}
        </RemoteGate>
    );
}

function HostingForm({
    domainId,
    saved,
    state,
    setState,
    phpKnownMissing,
    capabilities,
    answer,
    canApply,
    onApply,
}: {
    domainId: number;
    /** What the server has saved; the live application panel follows this, not the draft. */
    saved: HostingState;
    state: HostingState;
    setState: (next: HostingState) => void;
    phpKnownMissing: boolean;
    capabilities: ReturnType<typeof useHostingCapabilities>;
    answer: LostAnswerHandle;
    canApply: boolean;
    onApply: (form: HostingState) => void;
}) {
    const { t } = useI18n();
    // Which Node.js versions exist is asked only for a Node.js project. Until
    // it is known the list holds the saved version and nothing invented.
    // Hangi Node.js sürümlerinin kurulu olduğu yalnız Node.js projesi için
    // sorulur. Bilinene dek listede kayıtlı sürüm vardır, uydurma hiçbir şey yok.
    const runtimes = useRemote(state.project_type === 'node' ? NODE_RUNTIMES_URL : null, decodeNodeRuntimes);
    const installed = runtimes.remote.state === 'known' ? runtimes.remote.value : [];
    const versions = state.runtime_version && !installed.includes(state.runtime_version)
        ? [state.runtime_version, ...installed]
        : installed;

    return (
        <div className="space-y-5">
            {/* Type picker */}
            <div>
                <span className="mb-2 block text-sm font-medium text-fg-muted">{t('hosting.typeLabel')}</span>
                <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-5">
                    {typeDefs.map(({ id, icon: Icon, labelKey, descKey }) => {
                        const active = state.project_type === id;
                        const unavailable = id === 'php' && phpKnownMissing;
                        return (
                            <button
                                key={id}
                                onClick={() => setState({ ...state, project_type: id })}
                                disabled={unavailable}
                                title={unavailable ? t('hosting.phpMissing') : t(descKey)}
                                className={`flex flex-col items-start gap-1.5 rounded-xl border p-3 text-left transition-colors ${
                                    active
                                        ? 'border-primary bg-primary/10'
                                        : 'border-border bg-surface hover:border-primary/40 hover:bg-surface-2'
                                } ${unavailable ? 'cursor-not-allowed opacity-50' : ''}`}
                            >
                                <Icon className={`h-5 w-5 ${active ? 'text-primary' : 'text-fg-muted'}`} />
                                <span className={`text-sm font-semibold ${active ? 'text-primary' : 'text-fg'}`}>{t(labelKey)}</span>
                                {unavailable && (
                                    <span className="text-xs font-medium text-warning">{t('hosting.phpMissing')}</span>
                                )}
                            </button>
                        );
                    })}
                </div>
                <p className="mt-2 text-xs text-fg-subtle">
                    {t(typeDefs.find((d) => d.id === state.project_type)?.descKey ?? 'hosting.desc.php')}
                </p>
                {capabilities.remote.state === 'unknown' && (
                    <CouldNotCheck
                        className="mt-3"
                        text={t('hosting.phpUnknown')}
                        onRetry={() => void capabilities.retry()}
                        busy={capabilities.reading}
                    />
                )}
            </div>

            {/* Type-specific fields */}
            {state.project_type === 'node' && (
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <label>
                        <span className="mb-1 block text-xs text-fg-muted">{t('hosting.startCommand')}</span>
                        <input
                            value={state.start_command || ''}
                            onChange={(e) => setState({ ...state, start_command: e.target.value })}
                            placeholder="node server.js"
                            className={`${inputClass} font-mono`}
                        />
                    </label>
                    <label>
                        <span className="mb-1 block text-xs text-fg-muted">{t('hosting.nodeVersion')}</span>
                        {/* The "system default" option is gone (B3d): the panel
                            only operates what it installed — an unnamed
                            interpreter can't be listed, sized or protected
                            from deletion. A legacy save with '' still renders
                            (disabled placeholder) so the select never lies
                            about the stored value; the server refuses new
                            saves without a version (RUNTIME_VERSION_REQUIRED).
                            "Sistem varsayılanı" seçeneği gitti (B3d): panel
                            yalnız kendi kurduğunu işletir — adsız yorumlayıcı
                            listelenemez, ölçülemez, silinmekten korunamaz.
                            '' ile kayıtlı eski site yine çizilir (pasif yer
                            tutucu); select saklanan değer hakkında yalan
                            söylemez, sunucu sürümsüz yeni kaydı reddeder. */}
                        <select
                            value={state.runtime_version || ''}
                            onChange={(e) => setState({ ...state, runtime_version: e.target.value })}
                            className={inputClass}
                        >
                            {!state.runtime_version && (
                                <option value="" disabled>
                                    {t('hosting.pickVersion')}
                                </option>
                            )}
                            {versions.map((v) => (
                                <option key={v} value={v}>
                                    {v}
                                </option>
                            ))}
                        </select>
                    </label>
                    {/* One line while the versions are read, in a place that
                        is there in every state; the notice when they could
                        not be.
                        Sürümler okunurken her durumda yerinde duran tek satır;
                        okunamadığında bildirim. */}
                    <div className="min-h-[1.25rem] sm:col-span-2">
                        {runtimes.remote.state === 'loading' && <Checking label={t('hosting.nodeChecking')} />}
                        {runtimes.remote.state === 'unknown' && (
                            <CouldNotCheck
                                text={t('hosting.nodeUnknown')}
                                onRetry={() => void runtimes.retry()}
                                busy={runtimes.reading}
                            />
                        )}
                    </div>
                    <p className="text-xs text-fg-subtle sm:col-span-2">
                        {t('hosting.portNote')}
                        {state.app_port ? ` · ${t('hosting.assignedPort')}: ${state.app_port}` : ''}
                    </p>
                </div>
            )}

            {(state.project_type === 'forwarding' || state.project_type === 'proxy') && (
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <label>
                        <span className="mb-1 block text-xs text-fg-muted">
                            {state.project_type === 'proxy' ? t('hosting.upstream') : t('hosting.forwardTo')}
                        </span>
                        <input
                            value={state.forward_to || ''}
                            onChange={(e) => setState({ ...state, forward_to: e.target.value })}
                            placeholder="https://example.com"
                            className={`${inputClass} font-mono`}
                        />
                    </label>
                    {state.project_type === 'forwarding' && (
                        <label>
                            <span className="mb-1 block text-xs text-fg-muted">{t('hosting.forwardCode')}</span>
                            <select
                                value={state.forward_code || 301}
                                onChange={(e) => setState({ ...state, forward_code: Number(e.target.value) })}
                                className={inputClass}
                            >
                                <option value={301}>{t('hosting.code301')}</option>
                                <option value={302}>{t('hosting.code302')}</option>
                            </select>
                        </label>
                    )}
                </div>
            )}

            <ResultUnknown answer={answer} />

            <div className="flex justify-end">
                <Button variant="primary" onClick={() => onApply(state)} disabled={!canApply}>
                    {t('hosting.save')}
                </Button>
            </div>

            {/* The live panel belongs to the application the server runs, so
                it follows the saved type: a Node.js type that is only picked
                here has no application to ask about yet.
                Canlı panel, sunucunun çalıştırdığı uygulamaya aittir; bu
                yüzden kayıtlı tipi izler: yalnız burada seçilmiş bir Node.js
                tipinin henüz sorulacak uygulaması yoktur. */}
            {saved.project_type === 'node' && <AppPanel domainId={domainId} />}
            {/* Installing a Node VERSION happens in one place: the Services
                page's version drawer (B3b) — a runtime install is a server
                decision, not a per-domain setting. This page only SELECTS
                among what is already installed.
                Node SÜRÜMÜ kurmak tek yerde olur: Servisler sayfasındaki
                sürüm çekmecesi (B3b) — runtime kurulumu sunucu kararıdır,
                domain başına ayar değil. Bu sayfa yalnız kurulu olanlar
                arasından SEÇER. */}
        </div>
    );
}

interface AppStatus {
    exists: boolean;
    active: string;
    pid: number;
    memory_mb: number;
}

// AppPanel: live systemd state + controls + journald logs for the domain's app.
// AppPanel: domain uygulaması için canlı systemd durumu + kontroller + journald günlükleri.
function decodeAppStatus(raw: unknown): AppStatus {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('shape');
    if (typeof (raw as { active?: unknown }).active !== 'string') throw new Error('field');
    return raw as AppStatus;
}

const decodeAppLogs = (raw: unknown) => decodeListIn<string>(raw, 'lines');

// The state and the logs are read again every five seconds. A read that fails
// raises nothing: the earlier state stays, marked once as the earlier state,
// and the next tick asks again. "Stopped" and "No log lines yet" are said only
// for an answer; start, stop and restart act only on a state the server sent
// and that is not being questioned.
// Durum ve günlükler beş saniyede bir yeniden okunur. Başarısız okuma hiçbir
// şey yükseltmez: önceki durum, önceki durum diye bir kez işaretlenerek kalır.
// "Durdu" ve "Henüz günlük satırı yok" yalnız bir yanıt için söylenir;
// başlat, durdur ve yeniden başlat yalnız sunucunun gönderdiği durumda çalışır.
function AppPanel({ domainId }: { domainId: number }) {
    const { t, locale } = useI18n();
    const status = useRemote(`/api/v1/domains/${domainId}/app/status`, decodeAppStatus);
    const logs = useRemote(`/api/v1/domains/${domainId}/app/logs?lines=50`, decodeAppLogs);
    useRefreshEvery(status, 5000);
    useRefreshEvery(logs, 5000);
    const answer = useLostAnswer(() => status.retry());
    const [busy, setBusy] = useState(false);

    const refresh = () => {
        void status.retry();
        void logs.retry();
    };

    const act = async (action: 'start' | 'stop' | 'restart') => {
        if (status.remote.state !== 'known' || answer.holding) return;
        setBusy(true);
        try {
            const res = await answer.send(`/api/v1/domains/${domainId}/app/${action}`, { method: 'POST' });
            if (!res) return;
            if (!res.ok) showToast('error', apiErrorText(await readApiError(res), t));
            else answer.settle();
            await new Promise((r) => setTimeout(r, 800));
            refresh();
        } finally {
            setBusy(false);
        }
    };

    const shownStatus = lastKnown(status.remote);
    const shownLogs = lastKnown(logs.remote);
    const failed = status.remote.state === 'unknown' || logs.remote.state === 'unknown';
    const earlier = status.remote.state === 'unknown' ? status.remote.previous : logs.remote.state === 'unknown' ? logs.remote.previous : undefined;
    const active = shownStatus?.value.active;
    const running = active === 'active';
    const stateKey: TranslationKey =
        active === 'active'
            ? 'hosting.app.state.active'
            : active === 'failed'
              ? 'hosting.app.state.failed'
              : 'hosting.app.state.inactive';
    // Start, stop and restart need the state as the server has it now.
    // Başlat, durdur ve yeniden başlat, sunucudaki güncel durumu ister.
    const canAct = status.remote.state === 'known' && !busy && !answer.holding;

    return (
        <div className="rounded-xl border border-border bg-surface-2/40 p-4">
            <div className="mb-3 flex min-h-[2.25rem] flex-wrap items-center gap-3">
                <h4 className="text-sm font-semibold text-fg">{t('hosting.app.title')}</h4>
                {shownStatus ? (
                    <span className={`inline-flex items-center gap-1.5 text-sm ${active === 'failed' ? 'text-danger' : 'text-fg-muted'}`}>
                        <StatusDot ok={running} />
                        {t(stateKey)}
                        {running && (
                            <span className="text-xs text-fg-subtle">
                                · {t('hosting.app.pid')} {shownStatus.value.pid} · {t('hosting.app.memory')} {shownStatus.value.memory_mb} MB
                            </span>
                        )}
                    </span>
                ) : (
                    status.remote.state === 'loading' && <Checking label={t('hosting.app.checking')} />
                )}
                <div className="ml-auto flex flex-wrap items-center gap-1">
                    <Button icon={Play} onClick={() => act('start')} disabled={!canAct || running}>
                        {t('services.start')}
                    </Button>
                    <Button icon={Square} onClick={() => act('stop')} disabled={!canAct || !running}>
                        {t('services.stop')}
                    </Button>
                    <Button icon={RotateCw} onClick={() => act('restart')} disabled={!canAct}>
                        {t('services.restart')}
                    </Button>
                </div>
            </div>

            <ResultUnknown answer={answer} className="mb-3" />
            {/* One notice for the panel, whichever of its two reads failed.
                Hangisi başarısız olursa olsun, panel için tek bildirim. */}
            {failed && (
                <CouldNotCheck
                    className="mb-3"
                    text={earlier
                        ? t('hosting.app.stale', { time: new Date(earlier.observedAt).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' }) })
                        : t('hosting.app.unknown')}
                    onRetry={refresh}
                    busy={status.reading || logs.reading}
                />
            )}

            {shownLogs ? (
                <>
                    <p className="mb-1 text-xs font-medium text-fg-subtle">{t('hosting.app.logs')}</p>
                    <div className="max-h-56 overflow-auto rounded-lg bg-bg p-2 font-mono text-xs text-fg-muted">
                        {shownLogs.value.length === 0 ? (
                            <span className="font-sans text-fg-subtle">{t('hosting.app.empty')}</span>
                        ) : (
                            shownLogs.value.map((line, i) => <div key={i}>{line}</div>)
                        )}
                    </div>
                </>
            ) : (
                logs.remote.state === 'loading' && <Checking label={t('hosting.app.logsChecking')} />
            )}
        </div>
    );
}

// AdminNodeInstall lived here until B3b. It was the second address for the
// same decision — a hosting-settings tab quietly installing server-wide
// runtimes — and it rendered on every tab regardless of project type. The
// single address is now the Services page's version drawer.
// AdminNodeInstall B3b'ye kadar buradaydı. Aynı kararın ikinci adresiydi —
// bir barındırma-ayarları sekmesi sessizce sunucu-geneli runtime kuruyordu —
// ve proje tipinden bağımsız her sekmede çiziliyordu. Tek adres artık
// Servisler sayfasındaki sürüm çekmecesidir.
