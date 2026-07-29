import { useCallback, useEffect, useState } from 'react';
import { AlertTriangle, CheckCircle2, Database, Download, Gauge, HardDrive, LockKeyhole, RefreshCw } from 'lucide-react';
import { useI18n } from '../i18n';
import { apiErrorText, readApiError, type ApiError } from '../lib/apiError';
import { showToast } from './Toast';
import { Button, Card, EmptyState, ErrorBanner } from './ui';

interface SystemDatabase {
    id: string;
    name: string;
    purpose: string;
    kind: string;
    mutable: boolean;
    available: boolean;
    path_hint: string;
    size_bytes: number;
    modified_at: string;
    journal_mode: string;
    user_version: number;
    status: string;
    status_message: string;
    actions: string[];
}

type SystemDatabaseAction = 'check' | 'snapshot' | 'optimize';
interface BusyAction { databaseID: string; action: SystemDatabaseAction }

interface IntegrityCheck {
    integrity_ok: boolean;
    foreign_keys_ok: boolean;
    foreign_key_violations: number;
}

interface PreparedSnapshot {
    download_token: string;
    size_bytes: number;
}

const API_BASE = '/api/v1/system-databases';
const MAX_SNAPSHOT_SIZE = 2 * 1024 * 1024 * 1024;
const SNAPSHOT_TOKEN_PATTERN = /^[0-9a-f]{64}$/;

export function SystemSQLiteManager() {
    const { t, locale } = useI18n();
    const [databases, setDatabases] = useState<SystemDatabase[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<ApiError | null>(null);
    const [busy, setBusy] = useState<BusyAction | null>(null);

    const loadDatabases = useCallback(async (showLoading = true) => {
        if (showLoading) setLoading(true);
        setError(null);
        try {
            const response = await fetch(API_BASE);
            if (!response.ok) throw await readApiError(response);
            const payload = (await response.json()) as { databases?: SystemDatabase[] };
            setDatabases(Array.isArray(payload.databases) ? payload.databases : []);
        } catch (cause) {
            const apiError = normalizeError(cause, t('common.error'));
            setError(apiError);
            showToast('error', apiErrorText(apiError, t));
        } finally {
            if (showLoading) setLoading(false);
        }
    }, [t]);

    useEffect(() => { void loadDatabases(); }, [loadDatabases]);

    const runJSONAction = async (database: SystemDatabase, action: 'check' | 'optimize') => {
        setBusy({ databaseID: database.id, action });
        setError(null);
        try {
            const response = await fetch(`${API_BASE}/${encodeURIComponent(database.id)}/${action}`, { method: 'POST' });
            if (!response.ok) throw await readApiError(response);
            const payload = (await response.json().catch(() => null)) as { check?: IntegrityCheck } | null;
            if (action === 'check' && (
                payload?.check?.integrity_ok !== true ||
                payload.check.foreign_keys_ok !== true
            )) {
                throw {
                    message: t('systemDb.checkFailed', {
                        name: database.name,
                        count: String(payload?.check?.foreign_key_violations ?? 0),
                    }),
                } satisfies ApiError;
            }
            showToast('success', action === 'check'
                ? t('systemDb.checked', { name: database.name })
                : t('systemDb.optimized', { name: database.name }));
            await loadDatabases(false);
        } catch (cause) {
            const apiError = normalizeError(cause, t('common.error'));
            setError(apiError);
            showToast('error', apiErrorText(apiError, t));
        } finally {
            setBusy(null);
        }
    };
    const downloadSnapshot = async (database: SystemDatabase) => {
        setBusy({ databaseID: database.id, action: 'snapshot' });
        setError(null);
        try {
            const response = await fetch(`${API_BASE}/${encodeURIComponent(database.id)}/snapshot`, { method: 'POST' });
            if (!response.ok) throw await readApiError(response);
            const prepared = (await response.json().catch(() => null)) as PreparedSnapshot | null;
            if (
                !prepared ||
                !SNAPSHOT_TOKEN_PATTERN.test(prepared.download_token) ||
                !Number.isSafeInteger(prepared.size_bytes) ||
                prepared.size_bytes <= 0 ||
                prepared.size_bytes > MAX_SNAPSHOT_SIZE
            ) {
                throw { message: t('systemDb.snapshotInvalidResponse') } satisfies ApiError;
            }

            const form = document.createElement('form');
            form.method = 'POST';
            form.action = `${API_BASE}/${encodeURIComponent(database.id)}/snapshot-download`;
            form.target = '_self';
            form.hidden = true;
            const token = document.createElement('input');
            token.type = 'hidden';
            token.name = 'download_token';
            token.value = prepared.download_token;
            form.appendChild(token);
            document.body.appendChild(form);
            try {
                // The native form streams the verified snapshot without buffering a multi-gigabyte Blob.
                // Yerel form, doğrulanmış anlık görüntüyü çok gigabaytlık bir Blob belleğe almadan akıtır.
                form.submit();
                showToast('success', t('systemDb.snapshotReady', { name: database.name }));
            } finally {
                form.remove();
            }
        } catch (cause) {
            const apiError = normalizeError(cause, t('common.error'));
            setError(apiError);
            showToast('error', apiErrorText(apiError, t));
        } finally {
            setBusy(null);
        }
    };

    if (loading) return <div className={'rounded-xl border border-border bg-surface py-16 text-center text-sm text-fg-muted shadow-card'}>{t('systemDb.loading')}</div>;

    return (
        <div className={'space-y-4'}>
            <Card>
                <div className={'flex flex-wrap items-start justify-between gap-4 p-5'}>
                    <div className={'max-w-3xl'}>
                        <div className={'flex items-center gap-2'}>
                            <Database className={'h-5 w-5 text-primary'} />
                            <h2 className={'text-lg font-semibold text-fg'}>{t('systemDb.title')}</h2>
                        </div>
                        <p className={'mt-1 text-sm text-fg-muted'}>{t('systemDb.subtitle')}</p>
                        <div className={'mt-3 flex items-start gap-2 rounded-lg border border-border bg-surface-2 px-3 py-2 text-xs text-fg-muted'}>
                            <LockKeyhole className={'mt-0.5 h-4 w-4 shrink-0 text-success'} />
                            <span>{t('systemDb.safetyHint')}</span>
                        </div>
                    </div>
                    <Button icon={RefreshCw} disabled={busy !== null} onClick={() => void loadDatabases()}>
                        {t('systemDb.refresh')}
                    </Button>
                </div>
            </Card>

            <ErrorBanner error={error} />

            {databases.length === 0 ? (
                <EmptyState icon={HardDrive} title={t('systemDb.empty')} hint={t('systemDb.emptyHint')} />
            ) : (
                <div className={'grid gap-4 xl:grid-cols-2'}>
                    {databases.map((database) => {
                        const actions = Array.isArray(database.actions) ? database.actions : [];
                        const databaseBusy = busy?.databaseID === database.id;
                        const canCheck = actions.includes('check');
                        const canSnapshot = actions.includes('snapshot');
                        const canOptimize = database.mutable && actions.includes('optimize');
                        const availableActions = [
                            canCheck ? t('systemDb.action.check') : null,
                            canSnapshot ? t('systemDb.action.snapshot') : null,
                            canOptimize ? t('systemDb.action.optimize') : null,
                        ].filter((action): action is string => action !== null).join(', ') || t('systemDb.action.none');
                        const localizedName = {
                            panel: t('systemDb.database.panel.name'),
                            powerdns: t('systemDb.database.powerdns.name'),
                            roundcube: t('systemDb.database.roundcube.name'),
                            'component-catalog': t('systemDb.database.componentCatalog.name'),
                        }[database.id] || database.name;
                        const localizedPurpose = {
                            panel: t('systemDb.database.panel.purpose'),
                            powerdns: t('systemDb.database.powerdns.purpose'),
                            roundcube: t('systemDb.database.roundcube.purpose'),
                            'component-catalog': t('systemDb.database.componentCatalog.purpose'),
                        }[database.id] || database.purpose || t('systemDb.noPurpose');
                        const localizedKind = {
                            'control-plane': t('systemDb.kind.controlPlane'),
                            dns: t('systemDb.kind.dns'),
                            webmail: t('systemDb.kind.webmail'),
                            catalog: t('systemDb.kind.catalog'),
                        }[database.kind] || database.kind || 'SQLite';
                        const localizedStatusMessage = {
                            ready: t('systemDb.statusMessage.ready'),
                            missing: t('systemDb.statusMessage.missing'),
                            unsafe: t('systemDb.statusMessage.unsafe'),
                            error: t('systemDb.statusMessage.error'),
                            corrupt: t('systemDb.statusMessage.error'),
                        }[database.status.toLowerCase()] || database.status_message;
                        return (
                            <Card key={database.id} className={!database.available ? 'opacity-80' : ''}>
                                <div className={'p-5'}>
                                    <div className={'flex items-start justify-between gap-3'}>
                                        <div className={'min-w-0'}>
                                            <div className={'flex flex-wrap items-center gap-2'}>
                                                <h3 className={'truncate text-base font-semibold text-fg'}>{localizedName}</h3>
                                                <StatusBadge status={database.status} available={database.available} />
                                            </div>
                                            <p className={'mt-1 text-sm text-fg-muted'}>
                                                {localizedPurpose}
                                            </p>
                                        </div>
                                        <span className={'rounded-md bg-surface-2 px-2 py-1 font-mono text-xs text-fg-muted'}>
                                            {localizedKind}
                                        </span>
                                    </div>

                                    {!database.available && (
                                        <div className={'mt-4 flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-sm text-warning'}>
                                            <AlertTriangle className={'mt-0.5 h-4 w-4 shrink-0'} />
                                            <span>{localizedStatusMessage || t('systemDb.unavailable')}</span>
                                        </div>
                                    )}

                                    {database.available && localizedStatusMessage && (
                                        <p className={'mt-3 text-xs text-fg-muted'}>{localizedStatusMessage}</p>
                                    )}

                                    <dl className={'mt-4 grid grid-cols-2 gap-x-4 gap-y-3 text-sm'}>
                                        <Metadata label={t('systemDb.availableActions')} value={availableActions} />
                                        <Metadata label={t('systemDb.size')} value={formatBytes(database.size_bytes, t('systemDb.unknown'))} />
                                        <Metadata label={t('systemDb.modified')} value={formatDate(database.modified_at, locale, t('systemDb.unknown'))} />
                                        <Metadata label={t('systemDb.journalMode')} value={database.journal_mode || t('systemDb.unknown')} mono />
                                        <Metadata label={t('systemDb.userVersion')} value={String(database.user_version ?? 0)} mono />
                                        <Metadata label={t('systemDb.kind')} value={localizedKind} />
                                    </dl>

                                    {database.path_hint && (
                                        <div className={'mt-4 rounded-lg bg-surface-2 px-3 py-2'}>
                                            <div className={'text-[11px] font-semibold uppercase tracking-wide text-fg-subtle'}>
                                                {t('systemDb.pathHint')}
                                            </div>
                                            <div className={'mt-1 break-all font-mono text-xs text-fg-muted'}>{database.path_hint}</div>
                                        </div>
                                    )}
                                    <div className={'mt-5 flex flex-wrap gap-2 border-t border-border pt-4'}>
                                        {canCheck && (
                                            <Button
                                                icon={CheckCircle2}
                                                disabled={!database.available || busy !== null}
                                                onClick={() => void runJSONAction({ ...database, name: localizedName }, 'check')}
                                            >
                                                {databaseBusy && busy.action === 'check' ? t('systemDb.checking') : t('systemDb.check')}
                                            </Button>
                                        )}
                                        {canSnapshot && (
                                            <Button
                                                icon={Download}
                                                disabled={!database.available || busy !== null}
                                                onClick={() => void downloadSnapshot({ ...database, name: localizedName })}
                                            >
                                                {databaseBusy && busy.action === 'snapshot' ? t('systemDb.snapshotting') : t('systemDb.snapshot')}
                                            </Button>
                                        )}
                                        {canOptimize && (
                                            <Button
                                                icon={Gauge}
                                                disabled={!database.available || busy !== null}
                                                onClick={() => {
                                                    if (window.confirm(t('systemDb.optimizeConfirm', { name: localizedName }))) {
                                                        void runJSONAction({ ...database, name: localizedName }, 'optimize');
                                                    }
                                                }}
                                            >
                                                {databaseBusy && busy.action === 'optimize' ? t('systemDb.optimizing') : t('systemDb.optimize')}
                                            </Button>
                                        )}
                                        {!canCheck && !canSnapshot && !canOptimize && (
                                            <span className={'text-xs text-fg-subtle'}>{t('systemDb.noActions')}</span>
                                        )}
                                    </div>
                                </div>
                            </Card>
                        );
                    })}
                </div>
            )}
        </div>
    );
}

function Metadata({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
    return (
        <div className={'min-w-0'}>
            <dt className={'text-xs text-fg-subtle'}>{label}</dt>
            <dd className={`mt-0.5 truncate text-fg ${mono ? 'font-mono text-xs' : ''}`} title={value}>
                {value}
            </dd>
        </div>
    );
}

function StatusBadge({ status, available }: { status: string; available: boolean }) {
    const { t } = useI18n();
    const normalized = status.toLowerCase();
    const variant = !available
        ? 'border-fg-subtle/30 bg-surface-2 text-fg-muted'
        : normalized === 'ready' || normalized === 'healthy' || normalized === 'ok'
          ? 'border-success/30 bg-success/10 text-success'
          : normalized === 'warning' || normalized === 'degraded'
            ? 'border-warning/30 bg-warning/10 text-warning'
            : normalized === 'error' || normalized === 'corrupt'
              ? 'border-danger/30 bg-danger/10 text-danger'
              : 'border-border bg-surface-2 text-fg-muted';
    const label = !available
        ? t('systemDb.status.unavailable')
        : normalized === 'ready'
          ? t('systemDb.status.ready')
          : normalized === 'healthy' || normalized === 'ok'
          ? t('systemDb.status.healthy')
          : normalized === 'warning' || normalized === 'degraded'
            ? t('systemDb.status.warning')
            : normalized === 'error' || normalized === 'corrupt'
              ? t('systemDb.status.error')
              : t('systemDb.status.unknown');
    return <span className={`rounded-full border px-2 py-0.5 text-[11px] font-medium ${variant}`}>{label}</span>;
}

function normalizeError(cause: unknown, fallback: string): ApiError {
    if (cause && typeof cause === 'object' && 'message' in cause) return cause as ApiError;
    return { message: typeof cause === 'string' ? cause : fallback };
}

function formatBytes(value: number, unknown: string): string {
    if (!Number.isFinite(value) || value < 0) return unknown;
    if (value < 1024) return `${value} B`;
    const units = ['KB', 'MB', 'GB', 'TB'];
    let amount = value / 1024;
    let unit = 0;
    while (amount >= 1024 && unit < units.length - 1) {
        amount /= 1024;
        unit += 1;
    }
    return `${amount >= 10 ? amount.toFixed(0) : amount.toFixed(1)} ${units[unit]}`;
}

function formatDate(value: string, locale: string, unknown: string): string {
    if (!value) return unknown;
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return unknown;
    return new Intl.DateTimeFormat(locale === 'tr' ? 'tr-TR' : 'en-US', {
        dateStyle: 'medium',
        timeStyle: 'short',
    }).format(date);
}
