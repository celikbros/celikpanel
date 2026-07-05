import { useState, useEffect, useCallback } from 'react';
import { FileCode, Files, Hexagon, ArrowLeftRight, ExternalLink, Play, Square, RotateCw, Download, type LucideIcon } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import type { TranslationKey } from '../i18n/en';
import { useAuth } from '../auth/AuthContext';
import { Button, StatusDot, inputClass } from './ui';

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

export function HostingTypePanel({ domainId }: { domainId: number; domainName: string }) {
    const { t } = useI18n();
    const [state, setState] = useState<HostingState | null>(null);
    const [saving, setSaving] = useState(false);
    const [versions, setVersions] = useState<string[]>([]);
    const [systemVersion, setSystemVersion] = useState('');

    const load = useCallback(async () => {
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/hosting`);
            if (res.ok) setState(await res.json());
            const vr = await fetch('/api/v1/runtimes/node');
            if (vr.ok) {
                const d = await vr.json();
                setVersions(d.installed || []);
                setSystemVersion(d.system_version || '');
            }
        } catch {
            showToast('error', t('common.error'));
        }
    }, [domainId]);

    useEffect(() => {
        load();
    }, [load]);

    const apply = async () => {
        if (!state) return;
        setSaving(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/hosting`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(state),
            });
            if (!res.ok) {
                showToast('error', (await res.text()).trim() || t('common.error'));
                return;
            }
            showToast('success', t('hosting.saved'));
            load();
        } finally {
            setSaving(false);
        }
    };

    if (!state) {
        return (
            <div className="flex items-center justify-center py-12">
                <div className="h-7 w-7 animate-spin rounded-full border-b-2 border-primary" />
            </div>
        );
    }

    return (
        <div className="space-y-5">
            {/* Type picker */}
            <div>
                <span className="mb-2 block text-sm font-medium text-fg-muted">{t('hosting.typeLabel')}</span>
                <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-5">
                    {typeDefs.map(({ id, icon: Icon, labelKey, descKey }) => {
                        const active = state.project_type === id;
                        return (
                            <button
                                key={id}
                                onClick={() => setState({ ...state, project_type: id })}
                                title={t(descKey)}
                                className={`flex flex-col items-start gap-1.5 rounded-xl border p-3 text-left transition-colors ${
                                    active
                                        ? 'border-primary bg-primary/10'
                                        : 'border-border bg-surface hover:border-primary/40 hover:bg-surface-2'
                                }`}
                            >
                                <Icon className={`h-5 w-5 ${active ? 'text-primary' : 'text-fg-muted'}`} />
                                <span className={`text-sm font-semibold ${active ? 'text-primary' : 'text-fg'}`}>{t(labelKey)}</span>
                            </button>
                        );
                    })}
                </div>
                <p className="mt-2 text-xs text-fg-subtle">
                    {t(typeDefs.find((d) => d.id === state.project_type)?.descKey ?? 'hosting.desc.php')}
                </p>
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
                        <select
                            value={state.runtime_version || ''}
                            onChange={(e) => setState({ ...state, runtime_version: e.target.value })}
                            className={inputClass}
                        >
                            <option value="">
                                {t('hosting.systemDefault')}
                                {systemVersion ? ` (${systemVersion})` : ''}
                            </option>
                            {versions.map((v) => (
                                <option key={v} value={v}>
                                    {v}
                                </option>
                            ))}
                        </select>
                    </label>
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

            <div className="flex justify-end">
                <Button variant="primary" onClick={apply} disabled={saving}>
                    {t('hosting.save')}
                </Button>
            </div>

            {state.project_type === 'node' && <AppPanel domainId={domainId} />}
            <AdminNodeInstall onInstalled={load} />
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
function AppPanel({ domainId }: { domainId: number }) {
    const { t } = useI18n();
    const [status, setStatus] = useState<AppStatus | null>(null);
    const [logs, setLogs] = useState<string[]>([]);
    const [busy, setBusy] = useState(false);

    const refresh = useCallback(async () => {
        try {
            const [sr, lr] = await Promise.all([
                fetch(`/api/v1/domains/${domainId}/app/status`),
                fetch(`/api/v1/domains/${domainId}/app/logs?lines=50`),
            ]);
            if (sr.ok) setStatus(await sr.json());
            if (lr.ok) setLogs((await lr.json()).lines || []);
        } catch {
            /* silent — panel refreshes on next action */
        }
    }, [domainId]);

    useEffect(() => {
        refresh();
        const timer = setInterval(refresh, 5000);
        return () => clearInterval(timer);
    }, [refresh]);

    const act = async (action: 'start' | 'stop' | 'restart') => {
        setBusy(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/app/${action}`, { method: 'POST' });
            if (!res.ok) showToast('error', (await res.text()).trim() || t('common.error'));
            await new Promise((r) => setTimeout(r, 800));
            refresh();
        } finally {
            setBusy(false);
        }
    };

    const running = status?.active === 'active';
    const stateKey: TranslationKey =
        status?.active === 'active'
            ? 'hosting.app.state.active'
            : status?.active === 'failed'
              ? 'hosting.app.state.failed'
              : 'hosting.app.state.inactive';

    return (
        <div className="rounded-xl border border-border bg-surface-2/40 p-4">
            <div className="mb-3 flex flex-wrap items-center gap-3">
                <h4 className="text-sm font-semibold text-fg">{t('hosting.app.title')}</h4>
                {status && (
                    <span className={`inline-flex items-center gap-1.5 text-sm ${status.active === 'failed' ? 'text-danger' : 'text-fg-muted'}`}>
                        <StatusDot ok={running} />
                        {t(stateKey)}
                        {running && (
                            <span className="text-xs text-fg-subtle">
                                · {t('hosting.app.pid')} {status.pid} · {t('hosting.app.memory')} {status.memory_mb} MB
                            </span>
                        )}
                    </span>
                )}
                <div className="ml-auto flex items-center gap-1">
                    <Button icon={Play} onClick={() => act('start')} disabled={busy || running}>
                        {t('services.start')}
                    </Button>
                    <Button icon={Square} onClick={() => act('stop')} disabled={busy || !running}>
                        {t('services.stop')}
                    </Button>
                    <Button icon={RotateCw} onClick={() => act('restart')} disabled={busy}>
                        {t('services.restart')}
                    </Button>
                </div>
            </div>

            <p className="mb-1 text-xs font-medium text-fg-subtle">{t('hosting.app.logs')}</p>
            <div className="max-h-56 overflow-auto rounded-lg bg-bg p-2 font-mono text-xs text-fg-muted">
                {logs.length === 0 ? (
                    <span className="text-fg-subtle">{t('hosting.app.empty')}</span>
                ) : (
                    logs.map((line, i) => <div key={i}>{line}</div>)
                )}
            </div>
        </div>
    );
}

// AdminNodeInstall: install a new runtime version, admins only.
// AdminNodeInstall: yeni bir runtime sürümü kur; yalnızca yöneticiler.
function AdminNodeInstall({ onInstalled }: { onInstalled: () => void }) {
    const { t } = useI18n();
    const { role } = useAuth();
    const [version, setVersion] = useState('');
    const [installing, setInstalling] = useState(false);

    if (role !== 'admin') return null;

    const install = async () => {
        setInstalling(true);
        try {
            const res = await fetch('/api/v1/runtimes/node', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ version }),
            });
            if (!res.ok) {
                showToast('error', (await res.text()).trim() || t('common.error'));
                return;
            }
            showToast('success', t('hosting.installed'));
            setVersion('');
            onInstalled();
        } finally {
            setInstalling(false);
        }
    };

    return (
        <div className="rounded-xl border border-dashed border-border p-4">
            <h4 className="mb-1 text-sm font-semibold text-fg">{t('hosting.installTitle')}</h4>
            <p className="mb-3 text-xs text-fg-subtle">{t('hosting.installHint')}</p>
            <div className="flex items-center gap-2">
                <input
                    value={version}
                    onChange={(e) => setVersion(e.target.value)}
                    placeholder="24.18.0"
                    className={`${inputClass} max-w-[10rem] font-mono`}
                />
                <Button variant="primary" icon={Download} onClick={install} disabled={installing || !/^\d+\.\d+\.\d+$/.test(version)}>
                    {installing ? t('hosting.installing') : t('hosting.install')}
                </Button>
            </div>
        </div>
    );
}
