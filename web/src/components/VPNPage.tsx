import { useCallback, useEffect, useMemo, useState } from 'react';
import QRCode from 'qrcode';
import {
    AlertTriangle,
    CheckCircle2,
    Copy,
    Download,
    KeyRound,
    Laptop,
    Network,
    Plus,
    Power,
    Radio,
    RefreshCw,
    Server,
    Shield,
    Trash2,
} from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { useAuth } from '../auth/AuthContext';
import { apiErrorText, readApiError, type ApiError } from '../lib/apiError';
import { Button, EmptyState, ErrorBanner, PageHeader, StatusDot, inputClass } from './ui';
import { HelpButton } from './HelpDrawer';

interface VPNSyncSummary {
    in_sync: boolean;
    pending: number;
    errors: number;
}

interface VPNPolicy {
    interface: string;
    network: string;
    server_address: string;
    listen_protocol: string;
    listen_port: number;
    client_dns: string;
    allowed_ips: string;
    full_tunnel: boolean;
    nat_required: boolean;
    forward_required: boolean;
    firewall_required: boolean;
}

interface VPNStatus {
    installed: boolean;
    configured: boolean;
    running: boolean;
    server_public_key?: string;
    port?: number;
    endpoint?: string;
    peer_count: number;
    sync?: VPNSyncSummary;
    policy?: VPNPolicy;
}

interface VPNPeer {
    id: number;
    subscription_id?: number;
    subscription?: string;
    name: string;
    ip: string;
    created_at: string;
    last_handshake: number;
    rx_bytes: number;
    tx_bytes: number;
    desired_state: 'active' | 'revoked';
    sync_state: 'pending' | 'applied' | 'error';
    sync_error?: string;
}

interface Subscription {
    id: number;
    name: string;
    owner?: string;
}

type VPNView = 'overview' | 'devices' | 'policy';

interface Entitlement {
    product_id: string;
    status: string;
    expires_at?: string;
}

interface IssuedConfig {
    id: number;
    name: string;
    config: string;
    deliveryToken: string;
    acknowledged: boolean;
    ackError?: ApiError;
}

function asApiError(error: unknown): ApiError {
    return error && typeof error === 'object' && 'message' in error
        ? error as ApiError
        : { message: '' };
}

function fmtBytes(value: number): string {
    if (value < 1024) return `${value} B`;
    if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
    if (value < 1024 * 1024 * 1024) return `${(value / (1024 * 1024)).toFixed(1)} MB`;
    return `${(value / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

async function apiJSON<T>(input: RequestInfo | URL, init?: RequestInit): Promise<T> {
    const response = await fetch(input, { cache: 'no-store', ...init });
    if (!response.ok) {
        throw await readApiError(response);
    }
    return await response.json() as T;
}

function StateBadge({
    tone,
    children,
}: {
    tone: 'success' | 'warning' | 'danger' | 'neutral';
    children: React.ReactNode;
}) {
    const classes = {
        success: 'bg-success/10 text-success',
        warning: 'bg-warning/10 text-warning',
        danger: 'bg-danger/10 text-danger',
        neutral: 'bg-surface-2 text-fg-muted',
    }[tone];
    return (
        <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${classes}`}>
            {children}
        </span>
    );
}

function ReadOnlyValue({
    label,
    value,
    mono,
}: {
    label: string;
    value: string;
    mono?: boolean;
}) {
    return (
        <div className="rounded-lg border border-border bg-surface-2/40 px-4 py-3">
            <div className="text-xs font-medium text-fg-subtle">{label}</div>
            <div className={`mt-1 break-all text-sm font-medium text-fg ${mono ? 'font-mono' : ''}`}>
                {value}
            </div>
        </div>
    );
}

// VPNPage exposes only product-safe actions. Network paths, ports, commands and
// raw WireGuard text remain release-managed; the UI displays their effective
// state and lets an administrator request a full desired-state reconciliation.
// VPNPage yalnız ürün için güvenli işlemleri açar. Ağ yolları, portlar, komutlar
// ve ham WireGuard metni sürüm tarafından yönetilir; arayüz etkin durumu gösterir
// ve yöneticinin istenen durumun tamamını güvenle yeniden eşitlemesini sağlar.
export function VPNPage() {
    const { t } = useI18n();
    const { role } = useAuth();
    const [view, setView] = useState<VPNView>('overview');
    const [status, setStatus] = useState<VPNStatus | null>(null);
    const [peers, setPeers] = useState<VPNPeer[]>([]);
    const [subscriptions, setSubscriptions] = useState<Subscription[]>([]);
    const [selectedSubscriptionID, setSelectedSubscriptionID] = useState<number | null>(null);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState<ApiError | null>(null);
    const [busyAction, setBusyAction] = useState<string | null>(null);
    const [newName, setNewName] = useState('');
    const [issued, setIssued] = useState<IssuedConfig | null>(null);
    const [qrURL, setQRURL] = useState('');

    const load = useCallback(async () => {
        setLoadError(null);
        try {
            const [server, peerPayload, subscriptionPayload] = await Promise.all([
                apiJSON<VPNStatus>('/api/v1/vpn/status'),
                apiJSON<{ peers?: VPNPeer[] }>('/api/v1/vpn/peers'),
                apiJSON<{ subscriptions?: Array<{ id?: number; name?: string; owner?: string }> }>(
                    '/api/v1/subscriptions',
                ),
            ]);
            const accessibleSubscriptions = (subscriptionPayload.subscriptions ?? [])
                .filter((item): item is Subscription => (
                    Number.isInteger(item.id) && (item.id ?? 0) > 0 && !!item.name?.trim()
                ))
                .map((item) => ({ id: item.id, name: item.name.trim(), owner: item.owner }));
            const entitlementPayloads = await Promise.all(accessibleSubscriptions.map(
                (subscription) => apiJSON<{ entitlements?: Entitlement[] }>(
                    `/api/v1/subscriptions/${subscription.id}/entitlements`,
                ),
            ));
            const now = Date.now();
            const availableSubscriptions = accessibleSubscriptions.filter((_, index) => (
                (entitlementPayloads[index]?.entitlements ?? []).some((entitlement) => (
                    entitlement.product_id === 'vpn' &&
                    entitlement.status === 'active' &&
                    (!entitlement.expires_at || Date.parse(entitlement.expires_at) > now)
                ))
            ));
            setStatus(server);
            setPeers(peerPayload.peers ?? []);
            setSubscriptions(availableSubscriptions);
            setSelectedSubscriptionID((current) => (
                availableSubscriptions.some((subscription) => subscription.id === current)
                    ? current
                    : availableSubscriptions[0]?.id ?? null
            ));
        } catch (error) {
            const apiError = asApiError(error);
            setLoadError(apiError);
            showToast('error', apiErrorText(apiError, t));
        } finally {
            setLoading(false);
        }
    }, [t]);

    useEffect(() => {
        void load();
    }, [load]);

    useEffect(() => {
        let cancelled = false;
        setQRURL('');
        if (!issued) return () => { cancelled = true; };
        void QRCode.toDataURL(issued.config, {
            width: 256,
            margin: 1,
            errorCorrectionLevel: 'M',
        }).then((url) => {
            if (!cancelled) setQRURL(url);
        }).catch(() => {
            if (!cancelled) setQRURL('');
        });
        return () => {
            cancelled = true;
        };
    }, [issued]);

    const policy = status?.policy;
    const effectivePort = status?.port ?? policy?.listen_port;
    const endpointHost = status?.endpoint;
    const endpoint = endpointHost && effectivePort
        ? `${endpointHost.includes(':') && !endpointHost.startsWith('[') ? `[${endpointHost}]` : endpointHost}:${effectivePort}`
        : t('vpn.valueUnavailable');
    const protocolPort = policy && effectivePort
        ? `${policy.listen_protocol}/${effectivePort}`
        : t('vpn.valueUnavailable');
    const policyMatches = !!status?.configured && !!policy &&
        effectivePort === policy.listen_port;
    const syncReady = !!status?.running && !!status?.sync?.in_sync && policyMatches;
    const activePeers = useMemo(
        () => peers.filter((peer) => peer.desired_state === 'active').length,
        [peers],
    );

    const setup = async () => {
        setBusyAction('setup');
        try {
            await apiJSON('/api/v1/vpn/setup', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: '{}',
            });
            showToast('success', t('vpn.setupDone'));
            await load();
        } catch (error) {
            showToast('error', apiErrorText(asApiError(error), t));
        } finally {
            setBusyAction(null);
        }
    };

    const resync = async () => {
        setBusyAction('sync');
        try {
            await apiJSON('/api/v1/vpn/sync', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: '{}',
            });
            showToast('success', t('vpn.resyncDone'));
            await load();
        } catch (error) {
            showToast('error', apiErrorText(asApiError(error), t));
        } finally {
            setBusyAction(null);
        }
    };

    const acknowledgeDelivery = async (receipt: IssuedConfig, manageBusy = true): Promise<boolean> => {
        if (manageBusy) setBusyAction('ack');
        try {
            await apiJSON(`/api/v1/vpn/peers/${receipt.id}/ack`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ delivery_token: receipt.deliveryToken }),
            });
            setIssued((current) => current?.id === receipt.id
                ? { ...current, acknowledged: true, ackError: undefined }
                : current);
            return true;
        } catch (error) {
            const apiError = asApiError(error);
            setIssued((current) => current?.id === receipt.id
                ? { ...current, acknowledged: false, ackError: apiError }
                : current);
            showToast('error', apiErrorText(apiError, t));
            return false;
        } finally {
            if (manageBusy) setBusyAction(null);
        }
    };

    const addPeer = async () => {
        const name = newName.trim();
        if (!name || !selectedSubscriptionID) return;
        setBusyAction('create');
        try {
            const created = await apiJSON<{
                id: number;
                client_config: string;
                delivery_token: string;
            }>('/api/v1/vpn/peers', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    name,
                    subscription_id: selectedSubscriptionID,
                }),
            });
            const receipt: IssuedConfig = {
                id: created.id,
                name,
                config: created.client_config,
                deliveryToken: created.delivery_token,
                acknowledged: false,
            };
            setIssued(receipt);
            setNewName('');
            if (await acknowledgeDelivery(receipt, false)) {
                showToast('success', t('vpn.peerAdded'));
                await load();
            }
        } catch (error) {
            showToast('error', apiErrorText(asApiError(error), t));
        } finally {
            setBusyAction(null);
        }
    };

    const removePeer = async (peer: VPNPeer) => {
        if (!confirm(t('vpn.deleteConfirm', { name: peer.name }))) return;
        setBusyAction(`delete-${peer.id}`);
        try {
            await apiJSON(`/api/v1/vpn/peers/${peer.id}`, { method: 'DELETE' });
            showToast('success', t('vpn.peerRemoved'));
            await load();
        } catch (error) {
            showToast('error', apiErrorText(asApiError(error), t));
        } finally {
            setBusyAction(null);
        }
    };

    const downloadConfig = () => {
        if (!issued) return;
        const url = URL.createObjectURL(new Blob([issued.config], { type: 'text/plain' }));
        const anchor = document.createElement('a');
        anchor.href = url;
        anchor.download = `${issued.name.replace(/[^a-zA-Z0-9-]/g, '_')}.conf`;
        anchor.click();
        window.setTimeout(() => URL.revokeObjectURL(url), 0);
    };

    const copyConfig = () => {
        if (!issued) return;
        void navigator.clipboard.writeText(issued.config).then(
            () => showToast('success', t('vpn.copied')),
            () => showToast('error', t('common.error')),
        );
    };

    const isOnline = (peer: VPNPeer) => (
        peer.last_handshake > 0 &&
        Date.now() / 1000 - peer.last_handshake < 180
    );

    if (loading) {
        return (
            <div className="flex items-center justify-center p-4 py-20 sm:p-6 md:p-8">
                <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
            </div>
        );
    }

    if (!status) {
        return (
            <div className="p-4 sm:p-6 md:p-8">
                <PageHeader
                    title={t('vpn.title')}
                    subtitle={t('vpn.subtitle')}
                    actions={<HelpButton serviceId="wireguard" name="WireGuard VPN" />}
                />
                <ErrorBanner error={loadError} className="mb-4" />
                <Button
                    icon={RefreshCw}
                    disabled={busyAction !== null}
                    onClick={() => {
                        setLoading(true);
                        void load();
                    }}
                >
                    {t('common.retry')}
                </Button>
            </div>
        );
    }

    return (
        <div className="p-4 sm:p-6 md:p-8">
            <PageHeader
                title={t('vpn.title')}
                subtitle={t('vpn.subtitle')}
                actions={(
                    <>
                        <HelpButton serviceId="wireguard" name="WireGuard VPN" />
                        <Button icon={RefreshCw} disabled={busyAction !== null} onClick={() => void load()}>
                            {t('files.refresh')}
                        </Button>
                    </>
                )}
            />

            {loadError && (
                <div className="mb-5 space-y-3">
                    <ErrorBanner error={loadError} />
                    <Button icon={RefreshCw} onClick={() => void load()}>{t('common.retry')}</Button>
                </div>
            )}

            <div className="mb-5 flex flex-wrap gap-1 border-b border-border">
                {([
                    ['overview', t('vpn.tabOverview')],
                    ['devices', t('vpn.tabDevices')],
                    ['policy', t('vpn.tabPolicy')],
                ] as Array<[VPNView, string]>).map(([id, label]) => (
                    <button
                        key={id}
                        type="button"
                        onClick={() => setView(id)}
                        className={`border-b-2 px-4 py-2.5 text-sm font-medium transition-colors ${
                            view === id
                                ? 'border-primary text-primary'
                                : 'border-transparent text-fg-muted hover:text-fg'
                        }`}
                    >
                        {label}
                    </button>
                ))}
            </div>

            {view === 'overview' && (
                <div className="space-y-5">
                    <section className="rounded-xl border border-border bg-surface p-5 shadow-card">
                        <div className="flex flex-wrap items-start gap-4">
                            <span className="flex h-11 w-11 items-center justify-center rounded-xl bg-primary/10 text-primary">
                                <Shield className="h-6 w-6" />
                            </span>
                            <div className="min-w-0 flex-1">
                                <div className="flex flex-wrap items-center gap-2">
                                    <h2 className="text-lg font-semibold text-fg">{t('vpn.serverTitle')}</h2>
                                    <StateBadge tone={status?.running ? 'success' : 'warning'}>
                                        {status?.running ? t('vpn.running') : t('vpn.notRunning')}
                                    </StateBadge>
                                    <StateBadge tone="neutral">{t('vpn.releaseManaged')}</StateBadge>
                                </div>
                                <p className="mt-1 text-sm text-fg-muted">
                                    {status?.running
                                        ? t('vpn.serverRunning', { endpoint })
                                        : status?.installed
                                            ? t('vpn.serverStopped')
                                            : t('vpn.notInstalled')}
                                </p>
                                {!status.installed && role !== 'admin' && (
                                    <p className="mt-2 text-xs text-warning">{t('vpn.adminInstallRequired')}</p>
                                )}
                            </div>
                            {role === 'admin' && !status.installed && (
                                <Button
                                    variant="primary"
                                    icon={Server}
                                    disabled={busyAction !== null}
                                    onClick={() => window.location.assign('/services')}
                                >
                                    {t('vpn.installWireGuard')}
                                </Button>
                            )}
                            {role === 'admin' && status?.installed && !status.running && (
                                <Button
                                    variant="primary"
                                    icon={Power}
                                    disabled={busyAction !== null}
                                    onClick={() => void setup()}
                                >
                                    {t('vpn.setup')}
                                </Button>
                            )}
                        </div>
                        <div className="mt-5 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                            <ReadOnlyValue label={t('vpn.endpoint')} value={endpoint} mono />
                            <ReadOnlyValue label={t('vpn.protocolPort')} value={protocolPort} />
                            <ReadOnlyValue label={t('vpn.activeDevices')} value={String(activePeers)} />
                            <ReadOnlyValue
                                label={t('vpn.desiredLedger')}
                                value={syncReady ? t('vpn.syncReady') : t('vpn.syncAttention')}
                            />
                        </div>
                    </section>

                    <section className={`rounded-xl border p-5 ${
                        syncReady
                            ? 'border-success/30 bg-success/5'
                            : 'border-warning/40 bg-warning/5'
                    }`}>
                        <div className="flex flex-wrap items-start gap-3">
                            {syncReady
                                ? <CheckCircle2 className="mt-0.5 h-5 w-5 text-success" />
                                : <AlertTriangle className="mt-0.5 h-5 w-5 text-warning" />}
                            <div className="min-w-0 flex-1">
                                <h3 className="font-semibold text-fg">
                                    {syncReady ? t('vpn.syncReady') : t('vpn.syncAttention')}
                                </h3>
                                <p className="mt-1 text-sm text-fg-muted">
                                    {t('vpn.syncDescription', {
                                        pending: String(status?.sync?.pending ?? 0),
                                        errors: String(status?.sync?.errors ?? 0),
                                    })}
                                </p>
                                <p className="mt-2 text-xs text-fg-subtle">{t('vpn.sqliteDesiredState')}</p>
                            </div>
                            {role === 'admin' && (
                                <Button
                                    icon={RefreshCw}
                                    disabled={busyAction !== null || !status?.configured}
                                    onClick={() => void resync()}
                                >
                                    {t('vpn.resync')}
                                </Button>
                            )}
                        </div>
                    </section>

                    <div className="grid gap-4 lg:grid-cols-3">
                        <section className="rounded-xl border border-border bg-surface p-5 shadow-card">
                            <Server className="h-5 w-5 text-primary" />
                            <h3 className="mt-3 font-semibold text-fg">{t('vpn.serverPolicyCard')}</h3>
                            <p className="mt-1 text-sm text-fg-muted">{t('vpn.serverPolicyHint')}</p>
                            <button
                                type="button"
                                onClick={() => setView('policy')}
                                className="mt-4 text-sm font-medium text-primary hover:underline"
                            >
                                {t('vpn.viewPolicy')}
                            </button>
                        </section>
                        <section className="rounded-xl border border-border bg-surface p-5 shadow-card">
                            <Laptop className="h-5 w-5 text-primary" />
                            <h3 className="mt-3 font-semibold text-fg">{t('vpn.deviceCard')}</h3>
                            <p className="mt-1 text-sm text-fg-muted">
                                {t('vpn.peerCount', { count: String(activePeers) })}
                            </p>
                            <button
                                type="button"
                                onClick={() => setView('devices')}
                                className="mt-4 text-sm font-medium text-primary hover:underline"
                            >
                                {t('vpn.manageDevices')}
                            </button>
                        </section>
                        <section className="rounded-xl border border-border bg-surface p-5 shadow-card">
                            <KeyRound className="h-5 w-5 text-primary" />
                            <h3 className="mt-3 font-semibold text-fg">{t('vpn.keyCard')}</h3>
                            <p className="mt-1 break-all font-mono text-xs text-fg-muted">
                                {status?.server_public_key || t('vpn.valueUnavailable')}
                            </p>
                        </section>
                    </div>
                </div>
            )}

            {view === 'devices' && (
                <div className="space-y-5">
                    {issued && (
                        <section className="rounded-xl border border-warning/40 bg-warning/5 p-5">
                            <div className="flex flex-wrap gap-5">
                                <div className="min-w-0 flex-1">
                                    <h3 className="text-base font-semibold text-fg">
                                        {t('vpn.configTitle', { name: issued.name })}
                                    </h3>
                                    {issued.ackError && (
                                        <div className="mt-3">
                                            <ErrorBanner error={issued.ackError} />
                                            <p className="mt-2 text-xs text-danger">{t('vpn.deliveryAckFailed')}</p>
                                        </div>
                                    )}
                                    <p className="mt-1 text-sm text-warning">{t('vpn.configOnce')}</p>
                                    <pre className="mt-4 max-h-72 overflow-auto rounded-lg bg-surface-2 p-3 text-xs text-fg">
                                        {issued.config}
                                    </pre>
                                </div>
                                <div className="w-full max-w-64 shrink-0">
                                    <div className="rounded-xl border border-border bg-white p-2">
                                        {qrURL
                                            ? <img src={qrURL} alt={t('vpn.qrAlt', { name: issued.name })} className="w-full" />
                                            : <div className="aspect-square animate-pulse rounded-lg bg-surface-2" />}
                                    </div>
                                    <p className="mt-2 text-center text-xs text-fg-muted">{t('vpn.scanQR')}</p>
                                </div>
                            </div>
                            <div className="mt-4 flex flex-wrap gap-2">
                                <Button variant="primary" icon={Download} onClick={downloadConfig}>
                                    {t('vpn.download')}
                                </Button>
                                <Button icon={Copy} onClick={copyConfig}>{t('vpn.copy')}</Button>
                                {!issued.acknowledged && (
                                    <Button
                                        icon={RefreshCw}
                                        disabled={busyAction !== null}
                                        onClick={() => void acknowledgeDelivery(issued).then((ok) => {
                                            if (ok) {
                                                showToast('success', t('vpn.deliveryConfirmed'));
                                                void load();
                                            }
                                        })}
                                    >
                                        {t('vpn.retryDeliveryAck')}
                                    </Button>
                                )}
                                <Button disabled={!issued.acknowledged} onClick={() => {
                                    setIssued(null);
                                    setQRURL('');
                                }}>{t('vpn.savedClose')}</Button>
                            </div>
                        </section>
                    )}

                    <section className="rounded-xl border border-border bg-surface p-5 shadow-card">
                        <div className="mb-4">
                            <h3 className="font-semibold text-fg">{t('vpn.addPeer')}</h3>
                            <p className="mt-1 text-sm text-fg-muted">{t('vpn.subscriptionRequired')}</p>
                        </div>
                        <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] lg:items-end">
                            <label className="block">
                                <span className="mb-1 block text-sm font-medium text-fg-muted">
                                    {t('vpn.subscription')}
                                </span>
                                <select
                                    value={selectedSubscriptionID ?? ''}
                                    onChange={(event) => setSelectedSubscriptionID(
                                        event.target.value ? Number(event.target.value) : null,
                                    )}
                                    className={inputClass}
                                >
                                    <option value="">{t('vpn.selectSubscription')}</option>
                                    {subscriptions.map((subscription) => (
                                        <option key={subscription.id} value={subscription.id}>
                                            {subscription.name}
                                            {subscription.owner ? ` — ${subscription.owner}` : ''}
                                        </option>
                                    ))}
                                </select>
                            </label>
                            <label className="block">
                                <span className="mb-1 block text-sm font-medium text-fg-muted">
                                    {t('vpn.deviceName')}
                                </span>
                                <input
                                    value={newName}
                                    onChange={(event) => setNewName(event.target.value)}
                                    onKeyDown={(event) => {
                                        if (event.key === 'Enter') void addPeer();
                                    }}
                                    placeholder={t('vpn.peerNamePlaceholder')}
                                    maxLength={60}
                                    className={inputClass}
                                />
                            </label>
                            <Button
                                variant="primary"
                                icon={Plus}
                                disabled={
                                    busyAction !== null ||
                                    !status?.running ||
                                    !newName.trim() ||
                                    !selectedSubscriptionID
                                }
                                onClick={() => void addPeer()}
                            >
                                {t('vpn.issue')}
                            </Button>
                        </div>
                        {subscriptions.length === 0 && (
                            <p className="mt-3 text-sm text-warning">{t('vpn.noSubscriptions')}</p>
                        )}
                    </section>

                    <section>
                        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                            <div>
                                <h3 className="font-semibold text-fg">{t('vpn.peers')}</h3>
                                <p className="text-sm text-fg-muted">{t('vpn.deviceStateHint')}</p>
                            </div>
                            {role === 'admin' && (
                                <Button
                                    icon={RefreshCw}
                                    disabled={busyAction !== null || !status?.configured}
                                    onClick={() => void resync()}
                                >
                                    {t('vpn.resync')}
                                </Button>
                            )}
                        </div>
                        {peers.length === 0 ? (
                            <EmptyState icon={Laptop} title={t('vpn.noPeers')} hint={t('vpn.noPeersHint')} />
                        ) : (
                            <div className="overflow-x-auto rounded-xl border border-border bg-surface shadow-card">
                                <table className="w-full text-sm">
                                    <thead>
                                        <tr className="border-b border-border text-left text-xs text-fg-muted">
                                            <th scope="col" className="px-4 py-3 font-medium">{t('vpn.colName')}</th>
                                            <th scope="col" className="px-4 py-3 font-medium">{t('vpn.subscription')}</th>
                                            <th scope="col" className="px-4 py-3 font-medium">{t('vpn.colIP')}</th>
                                            <th scope="col" className="px-4 py-3 font-medium">{t('vpn.desiredState')}</th>
                                            <th scope="col" className="px-4 py-3 font-medium">{t('vpn.syncState')}</th>
                                            <th scope="col" className="px-4 py-3 font-medium">{t('vpn.colStatus')}</th>
                                            <th scope="col" className="px-4 py-3 font-medium">{t('vpn.colTraffic')}</th>
                                            <th scope="col" className="px-4 py-3" />
                                        </tr>
                                    </thead>
                                    <tbody>
                                        {peers.map((peer) => (
                                            <tr key={peer.id} className="border-b border-border last:border-0">
                                                <td className="px-4 py-3 font-medium text-fg">{peer.name}</td>
                                                <td className="px-4 py-3 text-xs text-fg-muted">
                                                    {peer.subscription ?? '—'}
                                                </td>
                                                <td className="px-4 py-3 font-mono text-xs text-fg-muted">{peer.ip}</td>
                                                <td className="px-4 py-3">
                                                    <StateBadge tone={peer.desired_state === 'active' ? 'success' : 'warning'}>
                                                        {peer.desired_state === 'active'
                                                            ? t('vpn.desiredActive')
                                                            : t('vpn.desiredRevoked')}
                                                    </StateBadge>
                                                </td>
                                                <td className="px-4 py-3">
                                                    <div className="space-y-1">
                                                        <StateBadge
                                                            tone={
                                                                peer.sync_state === 'applied'
                                                                    ? 'success'
                                                                    : peer.sync_state === 'error'
                                                                        ? 'danger'
                                                                        : 'warning'
                                                            }
                                                        >
                                                            {t(`vpn.sync.${peer.sync_state}`)}
                                                        </StateBadge>
                                                        {peer.sync_error && role === 'admin' && (
                                                            <p className="max-w-52 text-xs text-danger">{peer.sync_error}</p>
                                                        )}
                                                    </div>
                                                </td>
                                                <td className="px-4 py-3">
                                                    <span className="flex items-center gap-1.5 text-xs">
                                                        <StatusDot ok={isOnline(peer)} />
                                                        {isOnline(peer)
                                                            ? t('vpn.online')
                                                            : peer.last_handshake > 0
                                                                ? new Date(peer.last_handshake * 1000).toLocaleString()
                                                                : t('vpn.neverConnected')}
                                                    </span>
                                                </td>
                                                <td className="px-4 py-3 whitespace-nowrap text-xs text-fg-muted">
                                                    ↓ {fmtBytes(peer.rx_bytes)} · ↑ {fmtBytes(peer.tx_bytes)}
                                                </td>
                                                <td className="px-4 py-3 text-right">
                                                    <button
                                                        type="button"
                                                        onClick={() => void removePeer(peer)}
                                                        disabled={busyAction !== null}
                                                        title={t('common.remove')}
                                                        aria-label={t('vpn.removeDevice', { name: peer.name })}
                                                        className="rounded-md p-1.5 text-fg-muted hover:bg-danger/10 hover:text-danger disabled:opacity-50"
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
                    </section>
                </div>
            )}

            {view === 'policy' && (
                <div className="space-y-5">
                    <section className="rounded-xl border border-border bg-surface p-5 shadow-card">
                        <div className="flex flex-wrap items-start gap-3">
                            <Network className="mt-0.5 h-5 w-5 text-primary" />
                            <div className="min-w-0 flex-1">
                                <div className="flex flex-wrap items-center gap-2">
                                    <h3 className="font-semibold text-fg">{t('vpn.policyTitle')}</h3>
                                    <StateBadge tone="neutral">{t('vpn.readOnly')}</StateBadge>
                                </div>
                                <p className="mt-1 text-sm text-fg-muted">{t('vpn.policyDescription')}</p>
                            </div>
                                <p className="mt-2 text-xs text-fg-subtle">{t('vpn.noEditableProfile')}</p>
                        </div>
                        <div className="mt-5 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                            <ReadOnlyValue label={t('vpn.interface')} value={policy?.interface ?? t('vpn.valueUnavailable')} mono />
                            <ReadOnlyValue label={t('vpn.network')} value={policy?.network ?? t('vpn.valueUnavailable')} mono />
                            <ReadOnlyValue label={t('vpn.serverAddress')} value={policy?.server_address ?? t('vpn.valueUnavailable')} mono />
                            <ReadOnlyValue label={t('vpn.protocolPort')} value={protocolPort} />
                            <ReadOnlyValue label={t('vpn.clientDNS')} value={policy?.client_dns ?? t('vpn.valueUnavailable')} mono />
                            <ReadOnlyValue label={t('vpn.allowedIPs')} value={policy?.allowed_ips ?? t('vpn.valueUnavailable')} mono />
                            <ReadOnlyValue label={t('vpn.tunnelMode')} value={policy ? t('vpn.fullTunnel') : t('vpn.valueUnavailable')} />
                            <ReadOnlyValue label={t('vpn.endpoint')} value={endpoint} mono />
                            <ReadOnlyValue
                                label={t('vpn.publicKey')}
                                value={status?.server_public_key ?? t('vpn.valueUnavailable')}
                                mono
                            />
                        </div>
                    </section>

                    <section className="rounded-xl border border-border bg-surface p-5 shadow-card">
                        <h3 className="font-semibold text-fg">{t('vpn.hostRequirements')}</h3>
                        <p className="mt-1 text-sm text-fg-muted">{t('vpn.hostRequirementsHint')}</p>
                        <div className="mt-4 grid gap-3 lg:grid-cols-3">
                            {[
                                [Radio, t('vpn.reqFirewall'), t('vpn.reqFirewallHint'), policy?.firewall_required],
                                [Network, t('vpn.reqForwarding'), t('vpn.reqForwardingHint'), policy?.forward_required],
                                [Server, t('vpn.reqNAT'), t('vpn.reqNATHint'), policy?.nat_required],
                            ].map(([Icon, title, hint, required]) => {
                                const RequirementIcon = Icon as typeof Radio;
                                return (
                                    <div key={String(title)} className="rounded-lg border border-border bg-surface-2/40 p-4">
                                        <RequirementIcon className="h-5 w-5 text-primary" />
                                        <div className="mt-2 flex items-center gap-2">
                                            <h4 className="font-medium text-fg">{String(title)}</h4>
                                            {typeof required === 'boolean'
                                                ? (
                                                    <StateBadge tone={required ? 'success' : 'neutral'}>
                                                        {required ? t('vpn.required') : t('vpn.notRequired')}
                                                    </StateBadge>
                                                )
                                                : <StateBadge tone="neutral">{t('vpn.valueUnavailable')}</StateBadge>}
                                        </div>
                                        <p className="mt-1 text-xs text-fg-muted">{String(hint)}</p>
                                    </div>
                                );
                            })}
                        </div>
                    </section>

                    <section className="rounded-xl border border-primary/20 bg-primary/5 p-5">
                        <div className="flex gap-3">
                            <Shield className="mt-0.5 h-5 w-5 text-primary" />
                            <div>
                                <h3 className="font-semibold text-fg">{t('vpn.managedSafetyTitle')}</h3>
                                <p className="mt-1 text-sm text-fg-muted">{t('vpn.managedSafetyHint')}</p>
                            </div>
                        </div>
                    </section>
                </div>
            )}
        </div>
    );
}
