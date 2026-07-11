import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { Database, Plus, Trash2, Users, Server } from 'lucide-react';
import { showToast } from './Toast';
import { AddDatabaseModalV2 } from './AddDatabaseModalV2';
import { AddUserModalV2 } from './AddUserModalV2';
import { useI18n } from '../i18n';
import { PageHeader, Button, EmptyState, StatusDot } from './ui';

const API_BASE = '/api/v2';

interface DatabaseServer {
    id: number;
    type_id: number;
    type_name: string;
    type_icon: string;
    name: string;
    version: string;
    host: string;
    port: number;
    is_default: boolean;
    status: string;
    created_at: string;
}

interface DatabaseItem {
    id: number;
    name: string;
    users: string[];
    created_at: string;
}

interface DatabaseUser {
    id: number;
    username: string;
    databases: string[];
    created_at: string;
}

// Databases page. Servers are auto-discovered (no "add server" friction),
// shown as a selector; each server has Databases and Users tables in our
// dense, modern language.
//
// Veritabanları sayfası. Sunucular otomatik keşfedilir ("sunucu ekle"
// sürtünmesi yok), seçici olarak gösterilir; her sunucunun yoğun, modern
// dilimizde Veritabanları ve Kullanıcılar tabloları vardır.
export function DatabaseManagementV2() {
    const { t } = useI18n();
    const navigate = useNavigate();
    const [servers, setServers] = useState<DatabaseServer[]>([]);
    const [selectedServer, setSelectedServer] = useState<DatabaseServer | null>(null);
    const [activeTab, setActiveTab] = useState<'databases' | 'users'>('databases');
    const [databases, setDatabases] = useState<DatabaseItem[]>([]);
    const [users, setUsers] = useState<DatabaseUser[]>([]);
    const [loading, setLoading] = useState(false);
    const [serversLoaded, setServersLoaded] = useState(false);
    const [showAddDatabase, setShowAddDatabase] = useState(false);
    const [showAddUser, setShowAddUser] = useState(false);

    useEffect(() => {
        loadServers();
    }, []);

    useEffect(() => {
        if (selectedServer) {
            loadDatabases(selectedServer.id);
            loadUsers(selectedServer.id);
        }
    }, [selectedServer]);

    const loadServers = async () => {
        try {
            const res = await fetch(`${API_BASE}/database-servers`);
            if (!res.ok) throw new Error();
            const data: DatabaseServer[] = await res.json();
            setServers(data || []);
            setSelectedServer((cur) => cur ?? (data && data.length > 0 ? data[0] : null));
        } catch {
            showToast('error', t('common.error'));
        } finally {
            setServersLoaded(true);
        }
    };

    const loadDatabases = async (serverId: number) => {
        setLoading(true);
        try {
            const res = await fetch(`${API_BASE}/database-servers/${serverId}/databases`);
            setDatabases(res.ok ? (await res.json()) || [] : []);
        } finally {
            setLoading(false);
        }
    };

    const loadUsers = async (serverId: number) => {
        try {
            const res = await fetch(`${API_BASE}/database-servers/${serverId}/users`);
            setUsers(res.ok ? (await res.json()) || [] : []);
        } catch {
            /* silent */
        }
    };

    const handleDeleteDatabase = async (id: number, name: string) => {
        if (!confirm(t('databases.confirmDeleteDb', { name }))) return;
        try {
            const res = await fetch(`${API_BASE}/databases/${id}`, { method: 'DELETE' });
            if (!res.ok) throw new Error();
            showToast('success', t('databases.dbDeleted'));
            if (selectedServer) loadDatabases(selectedServer.id);
        } catch {
            showToast('error', t('common.error'));
        }
    };

    const handleDeleteUser = async (id: number, name: string) => {
        if (!confirm(t('databases.confirmDeleteUser', { name }))) return;
        try {
            const res = await fetch(`${API_BASE}/database-users/${id}`, { method: 'DELETE' });
            if (!res.ok) throw new Error();
            showToast('success', t('databases.userDeleted'));
            if (selectedServer) loadUsers(selectedServer.id);
        } catch {
            showToast('error', t('common.error'));
        }
    };

    return (
        <div className="p-6 md:p-8">
            <PageHeader
                title={t('nav.databases')}
                subtitle={t('databases.subtitle')}
                breadcrumb={[t('common.home'), t('nav.databases')]}
            />

            {/* No engine installed → the honest guidance, not a blank page.
                Databases are served by MariaDB/PostgreSQL; with neither
                installed there is nothing to manage yet.
                / Motor yoksa boş sayfa değil dürüst yönlendirme. Veritabanları
                MariaDB/PostgreSQL tarafından sunulur; ikisi de kurulu değilken
                henüz yönetilecek bir şey yoktur. */}
            {serversLoaded && servers.length === 0 && (
                <EmptyState
                    icon={Database}
                    title={t('databases.noServers')}
                    hint={t('databases.noServersHint')}
                    action={
                        <Button variant="primary" icon={Server} onClick={() => navigate('/services')}>
                            {t('domains.goServices')}
                        </Button>
                    }
                />
            )}

            {/* Server selector — auto-discovered engines */}
            <div className="mb-4 flex flex-wrap gap-2">
                {servers.map((s) => {
                    const active = selectedServer?.id === s.id;
                    return (
                        <button
                            key={s.id}
                            onClick={() => setSelectedServer(s)}
                            className={`flex items-center gap-2.5 rounded-xl border px-4 py-2.5 text-left transition-colors ${
                                active
                                    ? 'border-primary bg-primary/5'
                                    : 'border-border bg-surface hover:bg-surface-2'
                            }`}
                        >
                            <span className="text-2xl leading-none">{s.type_icon}</span>
                            <span>
                                <span className="flex items-center gap-2 text-base font-semibold text-fg">
                                    {s.name}
                                    {s.is_default && (
                                        <span className="rounded bg-primary/10 px-1.5 py-0.5 text-[10px] font-semibold uppercase text-primary">
                                            {t('databases.default')}
                                        </span>
                                    )}
                                </span>
                                <span className="flex items-center gap-1.5 text-xs text-fg-subtle">
                                    <StatusDot ok={s.status === 'active'} />
                                    {s.host}:{s.port} · {s.version}
                                </span>
                            </span>
                        </button>
                    );
                })}
            </div>

            {selectedServer && (
                <div className="rounded-xl border border-border bg-surface shadow-card">
                    {/* Tabs */}
                    <div className="flex items-center gap-1 border-b border-border px-3 pt-2">
                        <TabButton
                            active={activeTab === 'databases'}
                            onClick={() => setActiveTab('databases')}
                            icon={Database}
                            label={t('databases.tab.databases')}
                            count={databases.length}
                        />
                        <TabButton
                            active={activeTab === 'users'}
                            onClick={() => setActiveTab('users')}
                            icon={Users}
                            label={t('databases.tab.users')}
                            count={users.length}
                        />
                    </div>

                    <div className="flex items-center justify-between p-3">
                        {activeTab === 'databases' ? (
                            <Button variant="primary" icon={Plus} onClick={() => setShowAddDatabase(true)}>
                                {t('databases.addDatabase')}
                            </Button>
                        ) : (
                            <Button variant="primary" icon={Plus} onClick={() => setShowAddUser(true)}>
                                {t('databases.addUser')}
                            </Button>
                        )}
                        <span className="text-xs text-fg-subtle">
                            {t('common.itemsTotal', { n: activeTab === 'databases' ? databases.length : users.length })}
                        </span>
                    </div>

                    {loading ? (
                        <div className="flex items-center justify-center py-12">
                            <div className="h-7 w-7 animate-spin rounded-full border-b-2 border-primary" />
                        </div>
                    ) : activeTab === 'databases' ? (
                        databases.length === 0 ? (
                            <EmptyStateInline
                                icon={Database}
                                title={t('databases.empty.databases')}
                                hint={t('databases.empty.databasesHint')}
                            />
                        ) : (
                            <Table
                                columns={[t('databases.col.name'), t('databases.col.users'), '']}
                                rows={databases.map((d) => (
                                    <tr key={d.id} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                        <td className="px-4 py-3">
                                            <span className="flex items-center gap-2 text-base font-medium text-fg">
                                                <Database className="h-4 w-4 text-fg-subtle" />
                                                {d.name}
                                            </span>
                                        </td>
                                        <td className="px-4 py-3">
                                            <Chips items={d.users} />
                                        </td>
                                        <td className="px-4 py-3 text-right">
                                            <DeleteBtn onClick={() => handleDeleteDatabase(d.id, d.name)} />
                                        </td>
                                    </tr>
                                ))}
                            />
                        )
                    ) : users.length === 0 ? (
                        <EmptyStateInline
                            icon={Users}
                            title={t('databases.empty.users')}
                            hint={t('databases.empty.usersHint')}
                        />
                    ) : (
                        <Table
                            columns={[t('databases.col.username'), t('databases.col.databases'), '']}
                            rows={users.map((u) => (
                                <tr key={u.id} className="border-b border-border last:border-0 hover:bg-surface-2/60">
                                    <td className="px-4 py-3">
                                        <span className="flex items-center gap-2 text-base font-medium text-fg">
                                            <Users className="h-4 w-4 text-fg-subtle" />
                                            {u.username}
                                        </span>
                                    </td>
                                    <td className="px-4 py-3">
                                        <Chips items={u.databases} />
                                    </td>
                                    <td className="px-4 py-3 text-right">
                                        <DeleteBtn onClick={() => handleDeleteUser(u.id, u.username)} />
                                    </td>
                                </tr>
                            ))}
                        />
                    )}
                </div>
            )}

            {showAddDatabase && selectedServer && (
                <AddDatabaseModalV2
                    serverId={selectedServer.id}
                    serverName={selectedServer.name}
                    existingUsers={users.map((u) => ({ id: u.id, username: u.username }))}
                    onClose={() => setShowAddDatabase(false)}
                    onSuccess={() => {
                        setShowAddDatabase(false);
                        loadDatabases(selectedServer.id);
                        loadUsers(selectedServer.id);
                    }}
                />
            )}
            {showAddUser && selectedServer && (
                <AddUserModalV2
                    serverId={selectedServer.id}
                    serverName={selectedServer.name}
                    onClose={() => setShowAddUser(false)}
                    onSuccess={() => {
                        setShowAddUser(false);
                        loadUsers(selectedServer.id);
                    }}
                />
            )}
        </div>
    );
}

function TabButton({
    active,
    onClick,
    icon: Icon,
    label,
    count,
}: {
    active: boolean;
    onClick: () => void;
    icon: typeof Database;
    label: string;
    count: number;
}) {
    return (
        <button
            onClick={onClick}
            className={`flex items-center gap-2 border-b-2 px-3 pb-2.5 pt-1.5 text-sm font-medium transition-colors ${
                active ? 'border-primary text-primary' : 'border-transparent text-fg-muted hover:text-fg'
            }`}
        >
            <Icon className="h-4 w-4" />
            {label}
            <span className="rounded-full bg-surface-2 px-1.5 py-0.5 text-[11px] text-fg-muted">{count}</span>
        </button>
    );
}

function Table({ columns, rows }: { columns: string[]; rows: React.ReactNode }) {
    return (
        <div className="overflow-x-auto">
            <table className="w-full text-sm">
                <thead>
                    <tr className="border-b border-border text-left text-xs font-semibold text-fg-muted">
                        {columns.map((c, i) => (
                            <th key={i} className={`px-4 py-2.5 ${i === columns.length - 1 ? 'text-right' : ''}`}>
                                {c}
                            </th>
                        ))}
                    </tr>
                </thead>
                <tbody>{rows}</tbody>
            </table>
        </div>
    );
}

function Chips({ items }: { items: string[] }) {
    if (!items || items.length === 0) return <span className="text-fg-subtle">—</span>;
    return (
        <div className="flex flex-wrap gap-1">
            {items.map((i) => (
                <span key={i} className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-xs text-fg-muted">
                    {i}
                </span>
            ))}
        </div>
    );
}

function DeleteBtn({ onClick }: { onClick: () => void }) {
    return (
        <button
            onClick={onClick}
            className="rounded-md p-1.5 text-fg-subtle transition-colors hover:bg-surface-2 hover:text-danger"
        >
            <Trash2 className="h-4 w-4" />
        </button>
    );
}

function EmptyStateInline({ icon, title, hint }: { icon: typeof Database; title: string; hint: string }) {
    return (
        <div className="px-4 pb-4">
            <EmptyState icon={icon} title={title} hint={hint} />
        </div>
    );
}
