import { useState } from 'react';
import { useNavigate } from '../router';
import { Database, Plus, Trash2, Users, Server } from 'lucide-react';
import { showToast } from './Toast';
import { AddDatabaseModalV2 } from './AddDatabaseModalV2';
import { AddUserModalV2 } from './AddUserModalV2';
import { useI18n } from '../i18n';
import { useAuth } from '../auth/AuthContext';
import { Button, KnownEmpty, RemoteGate, ResultUnknown, StatusDot } from './ui';
import { PageHeader } from './PageHeader';
import { DatabaseAccountStrip } from './DatabaseAccountStrip';
import { decodeList, lastKnown, useRemote, type Remote } from '../lib/remote';
import { useLostAnswer } from '../lib/lostAnswer';
import { OnceOnlyNotice } from './OnceOnlyNotice';

// One API surface (B1, Jul 18): the former /api/v2 lives under /api/v1 now.
// Tek API yüzeyi (B1, 18 Tem): eski /api/v2 artık /api/v1 altında.
const API_BASE = '/api/v1';

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
    // R-065. Only an administrator is told these, so only an administrator
    // sees the account strip. Both are absent for anybody else.
    // R-065. Bunlar yalnizca yoneticiye soylenir.
    admin_username?: string;
    is_local?: boolean;
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
// Every list on this page is read through lib/remote.ts and is in one of three
// states. "No database engine installed" and "No databases yet" are claims
// about the server: they are drawn only for an answer the server gave. Until
// then the page says what it is reading; after a failed read it says that the
// read failed and offers it again, and nothing that creates, removes or
// re-keys anything is enabled.
//
// Veritabanları sayfası. Sunucular otomatik keşfedilir ("sunucu ekle"
// sürtünmesi yok), seçici olarak gösterilir; her sunucunun yoğun, modern
// dilimizde Veritabanları ve Kullanıcılar tabloları vardır.
//
// Bu sayfadaki her liste lib/remote.ts üzerinden okunur ve üç durumdan
// birindedir. "Kurulu veritabanı motoru yok" ve "Henüz veritabanı yok" sunucu
// hakkında iddialardır; yalnız sunucunun verdiği yanıt için çizilir. O zamana
// dek sayfa ne okuduğunu söyler; başarısız okumadan sonra okumanın başarısız
// olduğunu söyler ve yeniden sunar; oluşturan, kaldıran ya da parola yenileyen
// hiçbir denetim etkin olmaz.
export function DatabaseManagementV2() {
    const { t } = useI18n();
    const { role } = useAuth();
    const navigate = useNavigate();
    const isAdmin = role === 'admin';
    const servers = useRemote(`${API_BASE}/database-servers`, decodeList<DatabaseServer>);
    const [selectedId, setSelectedId] = useState<number | null>(null);
    const [activeTab, setActiveTab] = useState<'databases' | 'users'>('databases');
    const [showAddDatabase, setShowAddDatabase] = useState(false);
    const [showAddUser, setShowAddUser] = useState(false);

    // The selected server is always the row of the LATEST answer with the
    // chosen id, never a copy kept from an earlier one. A kept copy went stale
    // after every change to the panel's account: the strip went on showing an
    // account that had just been removed, and its "New password" button then
    // created that account again.
    // Seçili sunucu her zaman SON yanıttaki o kimlikli satırdır; önceki bir
    // yanıttan saklanan kopya değil. Saklanan kopya, panel hesabındaki her
    // değişiklikten sonra bayatlıyordu: şerit az önce kaldırılan hesabı
    // göstermeyi sürdürüyor, "Yeni parola" düğmesi de o hesabı yeniden
    // oluşturuyordu.
    const listedServers = lastKnown(servers.remote)?.value ?? [];
    const selectedServer = listedServers.find((s) => s.id === selectedId) ?? listedServers[0] ?? null;
    const serversCurrent = servers.remote.state === 'known';

    const serverURL = selectedServer ? `${API_BASE}/database-servers/${selectedServer.id}` : null;
    const databases = useRemote(serverURL && `${serverURL}/databases`, decodeList<DatabaseItem>);
    const users = useRemote(serverURL && `${serverURL}/users`, decodeList<DatabaseUser>);
    const active = activeTab === 'databases' ? databases : users;
    // Creating a database on a server carries an identity the server keeps
    // (D-029): a lost answer has been asked for once more before the dialog
    // hears of it, and at most one database was made. When there is still no
    // result both lists are read again; the dialog asks them whether they name
    // the database, and nothing is created or deleted until they answer.
    // Sunucuda veritabanı oluşturma, sunucunun sakladığı bir kimlik taşır
    // (D-029). Sonuç yine yoksa iki liste de yeniden okunur; yanıtlanana dek
    // hiçbir şey oluşturulmaz ya da silinmez.
    const createAnswer = useLostAnswer(() => Promise.all([databases.retry(), users.retry()]));
    // The database that was made while the answer carrying its new user's
    // password, shown only once, did not reach this page.
    // Yapılan, ama yeni kullanıcısının yalnızca bir kez gösterilen parolasını
    // taşıyan yanıtı bu sayfaya ulaşmayan veritabanı.
    const [passwordNotShown, setPasswordNotShown] = useState<{ name: string; user: string } | null>(null);
    const knownUsers = users.remote.state === 'known' ? users.remote.value : null;
    // A count is a claim too: it is a number once the list is known, "…" while
    // it is being read and "–" when it could not be read.
    // Sayı da bir iddiadır: liste bilinince sayı, okunurken "…", okunamayınca "–".
    const countOf = (remote: Remote<unknown[]>) =>
        remote.state === 'known' ? String(remote.value.length) : remote.state === 'loading' ? '…' : '–';

    const handleDeleteDatabase = async (id: number, name: string) => {
        if (!confirm(t('databases.confirmDeleteDb', { name }))) return;
        try {
            const res = await fetch(`${API_BASE}/databases/${id}`, { method: 'DELETE' });
            if (!res.ok) throw new Error();
            showToast('success', t('databases.dbDeleted'));
            void databases.retry();
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
            void users.retry();
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

            {/* R-069. This page is the customers' databases, and only those.
                The panel's own SQLite files moved to Settings, where the rest
                of the server's own machinery lives - the product separates
                HOSTING from SERVER in its navigation and this page is a
                hosting page.
                R-069. Bu sayfa musterilerin veritabanlaridir, yalnizca onlar. */}
            <RemoteGate
                remote={servers.remote}
                checking={t('databases.checkingServers')}
                failed={t('databases.serversUnknown')}
                onRetry={() => void servers.retry()}
                busy={servers.reading}
                className="py-1"
            >
                {(shown) => (shown.value.length === 0 ? (
                    /* No engine installed → the honest guidance, not a blank
                       page. Databases are served by MariaDB/PostgreSQL; with
                       neither installed there is nothing to manage yet.
                       / Motor yoksa boş sayfa değil dürüst yönlendirme. */
                    <KnownEmpty
                        of={shown}
                        icon={Database}
                        title={t('databases.noServers')}
                        hint={t('databases.noServersHint')}
                        action={
                            <Button variant="primary" icon={Server} onClick={() => navigate('/services')}>
                                {t('domains.goServices')}
                            </Button>
                        }
                    />
                ) : (
                    <>
            {/* Server selector — auto-discovered engines */}
            <div className="mb-4 flex flex-wrap gap-2">
                {shown.value.map((s) => {
                    const selected = selectedServer?.id === s.id;
                    return (
                        <button
                            key={s.id}
                            onClick={() => setSelectedId(s.id)}
                            className={`flex items-center gap-2.5 rounded-xl border px-4 py-2.5 text-left transition-colors ${
                                selected
                                    ? 'border-primary bg-primary/5'
                                    : 'border-border bg-surface hover:bg-surface-2'
                            }`}
                        >
                            <span className="text-2xl leading-none">{s.type_icon}</span>
                            <span>
                                <span className="flex items-center gap-2 text-base font-semibold text-fg">
                                    {s.name}
                                    {s.is_default && (
                                        <span className="rounded bg-primary/10 px-1.5 py-0.5 text-xs font-semibold uppercase text-primary">
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

            {/* The account strip acts on what it shows, so it is drawn only for
                the current answer: after a failed refresh the earlier row may
                describe an account that no longer exists.
                Hesap şeridi gösterdiği şey üzerinde işlem yapar; bu yüzden
                yalnız güncel yanıt için çizilir. */}
            {selectedServer && isAdmin && (
                <DatabaseAccountStrip
                    key={selectedServer.id}
                    server={selectedServer}
                    onChanged={() => servers.retry()}
                    current={serversCurrent && !servers.reading}
                />
            )}

            {/* While the dialog is open the notice stands in it, beside what
                was typed; once it is closed, here.
                İletişim kutusu açıkken bildirim onun içinde, yazılanın yanında
                durur; kapandığında burada. */}
            {!showAddDatabase && <ResultUnknown answer={createAnswer} className="mb-4" />}
            <OnceOnlyNotice
                className="mb-4"
                text={passwordNotShown === null ? null : t('databases.passwordNotShown', passwordNotShown)}
                onClose={() => setPasswordNotShown(null)}
            />

            {selectedServer && (
                <div className="rounded-xl border border-border-strong bg-surface">
                    {/* Tabs */}
                    <div className="flex items-center gap-1 border-b border-border px-3 pt-2">
                        <TabButton
                            active={activeTab === 'databases'}
                            onClick={() => setActiveTab('databases')}
                            icon={Database}
                            label={t('databases.tab.databases')}
                            count={countOf(databases.remote)}
                        />
                        <TabButton
                            active={activeTab === 'users'}
                            onClick={() => setActiveTab('users')}
                            icon={Users}
                            label={t('databases.tab.users')}
                            count={countOf(users.remote)}
                        />
                    </div>

                    {/* Creating needs the current lists: the dialog offers the
                        users that exist, and a list that could not be read is
                        not offered as "no users".
                        Oluşturma güncel listeleri ister: pencere var olan
                        kullanıcıları sunar; okunamayan liste "kullanıcı yok"
                        diye sunulmaz. */}
                    <div className="flex items-center justify-between p-3">
                        {activeTab === 'databases' ? (
                            <Button
                                variant="primary"
                                icon={Plus}
                                disabled={!serversCurrent || databases.remote.state !== 'known' || knownUsers === null || createAnswer.holding}
                                onClick={() => setShowAddDatabase(true)}
                            >
                                {t('databases.addDatabase')}
                            </Button>
                        ) : (
                            <Button
                                variant="primary"
                                icon={Plus}
                                disabled={!serversCurrent || knownUsers === null}
                                onClick={() => setShowAddUser(true)}
                            >
                                {t('databases.addUser')}
                            </Button>
                        )}
                        {active.remote.state === 'known' && (
                            <span className="text-xs text-fg-subtle">
                                {t('common.itemsTotal', { n: active.remote.value.length })}
                            </span>
                        )}
                    </div>

                    {activeTab === 'databases' ? (
                            <RemoteGate
                                remote={databases.remote}
                                checking={t('databases.checkingDatabases')}
                                failed={t('databases.databasesUnknown')}
                                onRetry={() => void databases.retry()}
                                busy={databases.reading}
                                className="mx-4 mb-4"
                            >
                                {(list) => (list.value.length === 0 ? (
                                    <div className="px-4 pb-4">
                                        <KnownEmpty
                                            of={list}
                                            icon={Database}
                                            title={t('databases.empty.databases')}
                                            hint={t('databases.empty.databasesHint')}
                                        />
                                    </div>
                                ) : (
                                    <Table
                                        columns={[t('databases.col.name'), t('databases.col.users'), '']}
                                        rows={list.value.map((d) => (
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
                                                <td className="row-actions px-4 py-3 text-right">
                                                    <DeleteBtn
                                                        label={t('databases.deleteDatabase', { name: d.name })}
                                                        disabled={list.stale || createAnswer.holding}
                                                        onClick={() => handleDeleteDatabase(d.id, d.name)}
                                                    />
                                                </td>
                                            </tr>
                                        ))}
                                    />
                                ))}
                            </RemoteGate>
                    ) : (
                            <RemoteGate
                                remote={users.remote}
                                checking={t('databases.checkingUsers')}
                                failed={t('databases.usersUnknown')}
                                onRetry={() => void users.retry()}
                                busy={users.reading}
                                className="mx-4 mb-4"
                            >
                                {(list) => (list.value.length === 0 ? (
                                    <div className="px-4 pb-4">
                                        <KnownEmpty
                                            of={list}
                                            icon={Users}
                                            title={t('databases.empty.users')}
                                            hint={t('databases.empty.usersHint')}
                                        />
                                    </div>
                                ) : (
                                    <Table
                                        columns={[t('databases.col.username'), t('databases.col.databases'), '']}
                                        rows={list.value.map((u) => (
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
                                                <td className="row-actions px-4 py-3 text-right">
                                                    <DeleteBtn
                                                        label={t('databases.deleteUser', { name: u.username })}
                                                        disabled={list.stale || createAnswer.holding}
                                                        onClick={() => handleDeleteUser(u.id, u.username)}
                                                    />
                                                </td>
                                            </tr>
                                        ))}
                                    />
                                ))}
                            </RemoteGate>
                    )}
                </div>
            )}

            {showAddDatabase && selectedServer && knownUsers !== null && (
                <AddDatabaseModalV2
                    answer={createAnswer}
                    onPasswordNotShown={(name, user) => {
                        setPasswordNotShown({ name, user });
                        setShowAddDatabase(false);
                        void databases.retry();
                        void users.retry();
                    }}
                    serverId={selectedServer.id}
                    serverName={selectedServer.name}
                    existingUsers={knownUsers.map((u) => ({ id: u.id, username: u.username }))}
                    onClose={() => setShowAddDatabase(false)}
                    onSuccess={() => {
                        setShowAddDatabase(false);
                        void databases.retry();
                        void users.retry();
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
                        void users.retry();
                    }}
                />
            )}
                    </>
                ))}
            </RemoteGate>
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
    count: string;
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
            <span className="min-w-[1.5rem] rounded-full bg-surface-2 px-1.5 py-0.5 text-center text-xs text-fg-muted">{count}</span>
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
                            <th key={i} className={`px-4 py-2.5 ${i === columns.length - 1 ? 'text-right' : ''} ${c === '' ? 'row-actions' : ''}`}>
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

function DeleteBtn({ onClick, label, disabled }: { onClick: () => void; label: string; disabled?: boolean }) {
    return (
        <button
            onClick={onClick}
            disabled={disabled}
            title={label}
            aria-label={label}
            className="rounded-md p-1.5 text-fg-subtle transition-colors hover:bg-surface-2 hover:text-danger disabled:pointer-events-none disabled:opacity-40"
        >
            <Trash2 className="h-4 w-4" />
        </button>
    );
}
