import { useEffect, useState, useCallback } from 'react';
import { Shield, Plus, Trash2, Download, Copy, Power, RefreshCw, Laptop } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { useAuth } from '../auth/AuthContext';
import { PageHeader, Button, EmptyState, StatusDot, inputClass } from './ui';

interface VPNStatus {
    installed: boolean;
    configured: boolean;
    running: boolean;
    server_public_key?: string;
    port?: number;
    endpoint?: string;
    peer_count: number;
}

interface VPNPeer {
    id: number;
    name: string;
    ip: string;
    created_at: string;
    last_handshake: number;
    rx_bytes: number;
    tx_bytes: number;
    subscription?: string;
}

function fmtBytes(n: number): string {
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
    return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

// The built-in VPN. Admin sets the WireGuard server up once (the package
// itself is installed from the Services catalogue); after that, peers are
// issued per device. The client config — private key included — is shown
// exactly once at creation, because the panel never stores it.
// Yerleşik VPN. WireGuard sunucusunu yönetici bir kez kurar (paketin kendisi
// Servisler kataloğundan kurulur); sonrasında cihaz başına peer verilir.
// İstemci config'i — özel anahtar dahil — oluşturmada tam bir kez gösterilir;
// panel onu asla saklamaz.
export function VPNPage() {
    const { t } = useI18n();
    const { role } = useAuth();
    const [status, setStatus] = useState<VPNStatus | null>(null);
    const [peers, setPeers] = useState<VPNPeer[]>([]);
    const [loading, setLoading] = useState(true);
    const [busy, setBusy] = useState(false);
    const [newName, setNewName] = useState('');
    const [issued, setIssued] = useState<{ name: string; config: string } | null>(null);

    const load = useCallback(async () => {
        try {
            const [s, p] = await Promise.all([
                fetch('/api/v1/vpn/status').then((r) => (r.ok ? r.json() : null)),
                fetch('/api/v1/vpn/peers').then((r) => (r.ok ? r.json() : { peers: [] })),
            ]);
            if (s) setStatus(s);
            setPeers(p.peers || []);
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setLoading(false);
        }
    }, [t]);

    useEffect(() => {
        load();
    }, [load]);

    const setup = async () => {
        setBusy(true);
        try {
            const r = await fetch('/api/v1/vpn/setup', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({}),
            });
            const d = await r.json();
            if (!r.ok) throw new Error(d.error);
            showToast('success', t('vpn.setupDone'));
            load();
        } catch (e) {
            showToast('error', e instanceof Error && e.message ? e.message : t('common.error'));
        } finally {
            setBusy(false);
        }
    };

    const addPeer = async () => {
        const name = newName.trim();
        if (!name) return;
        setBusy(true);
        try {
            const r = await fetch('/api/v1/vpn/peers', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name }),
            });
            const d = await r.json();
            if (!r.ok) throw new Error(d.error);
            setIssued({ name, config: d.client_config });
            setNewName('');
            showToast('success', t('vpn.peerAdded'));
            load();
        } catch (e) {
            showToast('error', e instanceof Error && e.message ? e.message : t('common.error'));
        } finally {
            setBusy(false);
        }
    };

    const removePeer = async (peer: VPNPeer) => {
        if (!confirm(t('vpn.deleteConfirm', { name: peer.name }))) return;
        try {
            const r = await fetch(`/api/v1/vpn/peers/${peer.id}`, { method: 'DELETE' });
            if (!r.ok) throw new Error();
            showToast('success', t('vpn.peerRemoved'));
            load();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    const downloadConfig = () => {
        if (!issued) return;
        const blob = new Blob([issued.config], { type: 'text/plain' });
        const a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = `${issued.name.replace(/[^a-zA-Z0-9-]/g, '_')}.conf`;
        a.click();
        URL.revokeObjectURL(a.href);
    };

    const copyConfig = () => {
        if (!issued) return;
        navigator.clipboard.writeText(issued.config).then(
            () => showToast('success', t('vpn.copied')),
            () => showToast('error', t('common.error')),
        );
    };

    // "Connected" means a handshake in the last 3 minutes — WireGuard
    // re-handshakes about every 2 minutes on an active tunnel.
    // "Bağlı", son 3 dakikada el sıkışma demektir — WireGuard etkin tünelde
    // ~2 dakikada bir yeniden el sıkışır.
    const isOnline = (p: VPNPeer) => p.last_handshake > 0 && Date.now() / 1000 - p.last_handshake < 180;

    if (loading) {
        return (
            <div className="flex items-center justify-center py-20">
                <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
            </div>
        );
    }

    return (
        <div className="space-y-6">
            <PageHeader title={t('vpn.title')} subtitle={t('vpn.subtitle')} />

            {/* Server status */}
            <section className="rounded-xl border border-border bg-surface p-5">
                <div className="flex flex-wrap items-center gap-4">
                    <span className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
                        <Shield className="h-5 w-5" />
                    </span>
                    <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2 text-sm font-semibold text-fg">
                            <StatusDot ok={!!status?.running} />
                            {status?.running
                                ? t('vpn.serverRunning', { endpoint: `${status.endpoint}:${status.port}` })
                                : status?.installed
                                    ? t('vpn.serverStopped')
                                    : t('vpn.notInstalled')}
                        </div>
                        <p className="text-xs text-fg-muted">
                            {status?.running ? t('vpn.peerCount', { count: String(status.peer_count) }) : t('vpn.serverHint')}
                        </p>
                    </div>
                    {role === 'admin' && status?.installed && !status.running && (
                        <Button variant="primary" disabled={busy} onClick={setup}>
                            <Power className="mr-1.5 h-4 w-4" />
                            {t('vpn.setup')}
                        </Button>
                    )}
                    <button
                        onClick={load}
                        title={t('files.refresh')}
                        className="rounded-md p-1.5 text-fg-muted hover:bg-surface-2 hover:text-fg"
                    >
                        <RefreshCw className="h-4 w-4" />
                    </button>
                </div>
            </section>

            {/* One-time client config after issuing a peer */}
            {issued && (
                <section className="rounded-xl border border-warning/40 bg-warning/5 p-5">
                    <h3 className="mb-1 text-sm font-semibold text-fg">{t('vpn.configTitle', { name: issued.name })}</h3>
                    <p className="mb-3 text-xs text-warning">{t('vpn.configOnce')}</p>
                    <pre className="mb-3 overflow-x-auto rounded-lg bg-surface-2 p-3 text-xs text-fg">{issued.config}</pre>
                    <div className="flex gap-2">
                        <Button variant="primary" onClick={downloadConfig}>
                            <Download className="mr-1.5 h-4 w-4" />
                            {t('vpn.download')}
                        </Button>
                        <Button variant="secondary" onClick={copyConfig}>
                            <Copy className="mr-1.5 h-4 w-4" />
                            {t('vpn.copy')}
                        </Button>
                        <Button variant="secondary" onClick={() => setIssued(null)}>{t('common.back')}</Button>
                    </div>
                </section>
            )}

            {/* Add peer */}
            {status?.running && (
                <section className="rounded-xl border border-border bg-surface p-5">
                    <h3 className="mb-3 text-sm font-semibold text-fg">{t('vpn.addPeer')}</h3>
                    <div className="flex flex-wrap gap-2">
                        <input
                            value={newName}
                            onChange={(e) => setNewName(e.target.value)}
                            onKeyDown={(e) => e.key === 'Enter' && addPeer()}
                            placeholder={t('vpn.peerNamePlaceholder')}
                            maxLength={60}
                            className={`${inputClass} max-w-xs`}
                        />
                        <Button variant="primary" disabled={busy || !newName.trim()} onClick={addPeer}>
                            <Plus className="mr-1.5 h-4 w-4" />
                            {t('vpn.issue')}
                        </Button>
                    </div>
                </section>
            )}

            {/* Peers */}
            <section>
                <h3 className="mb-3 text-sm font-semibold text-fg">{t('vpn.peers')}</h3>
                {peers.length === 0 ? (
                    <EmptyState icon={Laptop} title={t('vpn.noPeers')} hint={t('vpn.noPeersHint')} />
                ) : (
                    <div className="overflow-x-auto rounded-xl border border-border bg-surface">
                        <table className="w-full text-sm">
                            <thead>
                                <tr className="border-b border-border text-left text-xs text-fg-muted">
                                    <th className="px-4 py-2.5 font-medium">{t('vpn.colName')}</th>
                                    <th className="px-4 py-2.5 font-medium">{t('vpn.colIP')}</th>
                                    <th className="px-4 py-2.5 font-medium">{t('vpn.colStatus')}</th>
                                    <th className="px-4 py-2.5 font-medium">{t('vpn.colTraffic')}</th>
                                    <th className="px-4 py-2.5" />
                                </tr>
                            </thead>
                            <tbody>
                                {peers.map((p) => (
                                    <tr key={p.id} className="border-b border-border last:border-0">
                                        <td className="px-4 py-2.5">
                                            <span className="font-medium text-fg">{p.name}</span>
                                            {p.subscription && (
                                                <span className="ml-2 text-xs text-fg-subtle">{p.subscription}</span>
                                            )}
                                        </td>
                                        <td className="px-4 py-2.5 font-mono text-xs text-fg-muted">{p.ip}</td>
                                        <td className="px-4 py-2.5">
                                            <span className="flex items-center gap-1.5 text-xs">
                                                <StatusDot ok={isOnline(p)} />
                                                {isOnline(p)
                                                    ? t('vpn.online')
                                                    : p.last_handshake > 0
                                                        ? new Date(p.last_handshake * 1000).toLocaleString()
                                                        : t('vpn.neverConnected')}
                                            </span>
                                        </td>
                                        <td className="px-4 py-2.5 text-xs text-fg-muted">
                                            ↓ {fmtBytes(p.rx_bytes)} · ↑ {fmtBytes(p.tx_bytes)}
                                        </td>
                                        <td className="px-4 py-2.5 text-right">
                                            <button
                                                onClick={() => removePeer(p)}
                                                title={t('common.remove')}
                                                className="rounded-md p-1.5 text-fg-muted hover:bg-danger/10 hover:text-danger"
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
    );
}
