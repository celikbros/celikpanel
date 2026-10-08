import { useState } from 'react';
import { Database, Plus, Trash2, RefreshCw, ExternalLink } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { readApiError } from '../lib/apiError';
import { Button, Checking, CouldNotCheck, RemoteGate } from './ui';
import { decodeList, lastKnown, mapRemote, useRemote, type Remote } from '../lib/remote';
import { useHostingCapabilities, type CapabilitiesRemote } from '../lib/hostingCapabilities';

interface DomainDatabaseManagerProps {
    domainId: number;
    domainName: string;
    readOnly?: boolean;
    isAdditionalUser?: boolean;
}

type DatabaseType = 'mysql' | 'postgresql';
type DatabaseEngine = { value: DatabaseType; label: string };

function parseAvailableDatabaseTypes(value: unknown): DatabaseEngine[] {
    if (!Array.isArray(value)) return [];

    const parsed: DatabaseEngine[] = [];
    const seen = new Set<DatabaseType>();
    for (const item of value) {
        if (item !== 'mysql' && item !== 'postgresql') return [];
        if (seen.has(item)) continue;
        seen.add(item);
        parsed.push({
            value: item,
            label: item === 'mysql' ? 'MySQL / MariaDB' : 'PostgreSQL',
        });
    }
    return parsed;
}

// The database web tools (phpMyAdmin / phpPgAdmin). Installed → a launch
// button opening the panel-proxied tool. Parent engine present but the tool
// not → a hint pointing to Services. Neither → nothing (the parent-engine
// requirement means this whole page would be hidden anyway). The card is
// drawn only for a known answer; while the capabilities are being read, or
// could not be read, the manager above says so once, for both of them.
// Veritabanı web araçları (phpMyAdmin / phpPgAdmin). Kurulu → panel-vekilli
// aracı açan bir düğme. Üst motor var ama araç yok → Servisler'e yönlendiren
// bir ipucu. Hiçbiri → hiçbir şey. Kart yalnız bilinen yanıt için çizilir.
function DBToolsCard({ capabilities }: { capabilities: CapabilitiesRemote }) {
    const { t } = useI18n();
    if (capabilities.state !== 'known') return null;
    const caps = capabilities.value;

    const tools = [
        { id: 'phpmyadmin', label: 'phpMyAdmin', engine: 'mariadb' },
        { id: 'phppgadmin', label: 'phpPgAdmin', engine: 'postgresql' },
    ];
    const installed = new Set(caps.db_tools);
    const engines = new Set(caps.database_servers);
    // Only tools whose parent engine is installed are relevant here.
    // Yalnız üst motoru kurulu olan araçlar burada anlamlıdır.
    const relevant = tools.filter((tl) => engines.has(tl.engine));
    if (relevant.length === 0) return null;

    return (
        <div className="rounded-lg border border-border bg-surface-2/50 p-4">
            <h4 className="mb-1 text-sm font-semibold text-fg">{t('dbtools.title')}</h4>
            <p className="mb-3 text-xs text-fg-subtle">{t('dbtools.hint')}</p>
            <div className="flex flex-wrap gap-2">
                {relevant.map((tl) =>
                    installed.has(tl.id) ? (
                        <a
                            key={tl.id}
                            href={`/dbtool/${tl.id}/`}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-3 py-1.5 text-xs font-semibold text-primary-fg hover:bg-primary/90"
                        >
                            <ExternalLink className="h-3.5 w-3.5" />
                            {t('dbtools.open', { name: tl.label })}
                        </a>
                    ) : (
                        <span
                            key={tl.id}
                            className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-surface px-3 py-1.5 text-xs font-medium text-fg-subtle"
                        >
                            {t('dbtools.install', { name: tl.label })}
                        </span>
                    ),
                )}
            </div>
        </div>
    );
}

interface DatabaseInfo {
    id: number;
    name: string;
    type: string;
    user: string;
    created_at: string;
}

// One answer carries this domain's databases and, for a team member, the
// engines that member may create on. A body that is not this shape is unknown.
// Tek yanıt bu alan adının veritabanlarını ve ekip üyesi için oluşturabileceği
// motorları taşır. Bu biçimde olmayan gövde bilinmeyendir.
interface DomainDatabases {
    databases: DatabaseInfo[];
    availableTypes: DatabaseEngine[];
}

function decodeDomainDatabases(raw: unknown): DomainDatabases {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('shape');
    const payload = raw as Record<string, unknown>;
    return {
        databases: decodeList<DatabaseInfo>(payload.databases),
        availableTypes: parseAvailableDatabaseTypes(payload.available_types),
    };
}

const engineLabels: Record<string, DatabaseEngine> = {
    mariadb: { value: 'mysql', label: 'MySQL / MariaDB' },
    postgresql: { value: 'postgresql', label: 'PostgreSQL' },
};

export function DomainDatabaseManager({
    domainId,
    domainName,
    readOnly = false,
    isAdditionalUser = false,
}: DomainDatabaseManagerProps) {
    const { t } = useI18n();
    const [creating, setCreating] = useState(false);
    const [showCreateForm, setShowCreateForm] = useState(false);

    // The list is read through lib/remote.ts: being read, read, or not
    // readable. "No databases yet" is drawn only for an answer the server gave.
    // A new domain is a new address, so nothing of the previous domain's
    // answer is ever shown for this one.
    // Liste lib/remote.ts üzerinden okunur: okunuyor, okundu ya da okunamadı.
    // "Henüz veritabanı yok" yalnız sunucunun verdiği yanıt için çizilir. Yeni
    // alan adı yeni adrestir; önceki alan adının yanıtı bunun için gösterilmez.
    const list = useRemote(`/api/v1/domains/${domainId}/databases`, decodeDomainDatabases);
    const listed = lastKnown(list.remote);

    // Only engines that are actually installed may be offered — a dropdown
    // with MySQL and PostgreSQL on a server that runs neither is a settings
    // page for ghosts. The engine ids map to the panel's db types.
    //
    // Server-wide capability inventory is admin-only. A team member is given
    // only the tenant-safe available_types returned with this domain's
    // databases, so their engines come from the list's own answer and no
    // server-wide read is made for them.
    //
    // Either way the engines are known, being checked, or could not be
    // checked. A database is created only on an engine the server named: there
    // is no default engine.
    // Yalnız gerçekten kurulu motorlar sunulabilir. Sunucu geneli envanter
    // yalnız yöneticiye aittir; ekip üyesine yalnız bu alan adının
    // veritabanlarıyla dönen available_types verilir. Her iki durumda motorlar
    // ya bilinir, ya kontrol ediliyordur, ya da kontrol edilememiştir;
    // varsayılan motor yoktur.
    const capabilities = useHostingCapabilities({ enabled: !isAdditionalUser });
    const engineSource: Remote<DatabaseEngine[]> = isAdditionalUser
        ? mapRemote(list.remote, (value) => value.availableTypes)
        : mapRemote(capabilities.remote, (value) => value.database_servers.flatMap((id) => engineLabels[id] ?? []));
    const engines = engineSource.state === 'known' ? engineSource.value : [];
    const canCreate = !readOnly && engineSource.state === 'known' && engines.length > 0;

    // Form state
    const [dbName, setDbName] = useState('');
    const [chosenType, setDbType] = useState<DatabaseType | null>(null);
    const dbType = engines.some((engine) => engine.value === chosenType) ? chosenType : engines[0]?.value ?? null;
    const [dbPassword, setDbPassword] = useState('');

    const handleCreateDatabase = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!canCreate || dbType === null) return;

        if (!dbName || !dbPassword) {
            showToast('error', 'Name and password are required');
            return;
        }

        setCreating(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/databases`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    name: dbName,
                    type: dbType,
                    password: dbPassword
                })
            });

            if (res.ok) {
                const data = await res.json();
                showToast('success', `Database "${data.name}" created successfully`);
                setShowCreateForm(false);
                setDbName('');
                setDbPassword('');
                void list.retry();
            } else {
                showToast('error', (await readApiError(res)).message || 'Failed to create database');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to create database');
        } finally {
            setCreating(false);
        }
    };

    const handleDeleteDatabase = async (db: DatabaseInfo) => {
        if (readOnly || list.remote.state !== 'known') return;
        if (!confirm(`Delete database "${db.name}"?\n\nThis action cannot be undone. All data will be lost.`)) {
            return;
        }

        try {
            const res = await fetch(`/api/v1/domains/${domainId}/databases/${db.id}`, {
                method: 'DELETE'
            });

            if (res.ok) {
                showToast('success', `Database "${db.name}" deleted`);
                void list.retry();
            } else {
                showToast('error', 'Failed to delete database');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to delete database');
        }
    };

    return (
        <div className="space-y-6">
            <div>
                <h3 className="text-lg font-bold text-fg mb-2">Database Management</h3>
                <p className="text-sm text-fg-muted">
                    Manage databases for {domainName}
                </p>
            </div>

            {/* Create Database Button. It is in its place from the start and
                becomes usable when the engines are known; the line beside it
                says why it is not usable yet.
                Veritabanı Oluştur düğmesi. Baştan yerindedir ve motorlar
                bilindiğinde kullanılabilir olur; yanındaki satır henüz neden
                kullanılamadığını söyler. */}
            {!readOnly && !showCreateForm && !(engineSource.state === 'known' && engines.length === 0) && (
                <div className="flex flex-wrap items-center gap-3">
                    {/* The shared primary button: its face is readable in both
                        themes, enabled and disabled. The hand-written one drew
                        white on the dark theme's light primary.
                        Ortak birincil düğme: yüzü iki temada da, etkin ve
                        kapalıyken okunur. Elle yazılanı, koyu temanın açık
                        birincil rengi üstüne beyaz çiziyordu. */}
                    <Button variant="primary" icon={Plus} disabled={!canCreate} onClick={() => setShowCreateForm(true)}>
                        Create Database
                    </Button>
                    {engineSource.state === 'loading' && <Checking label={t('db.checkingEngines')} />}
                </div>
            )}

            {/* The engines could not be checked. For a team member that is the
                list's own read, and the list says so below.
                Motorlar kontrol edilemedi. Ekip üyesinde bu listenin kendi
                okumasıdır ve liste bunu aşağıda söyler. */}
            {!readOnly && !isAdditionalUser && engineSource.state === 'unknown' && (
                <CouldNotCheck
                    text={t('db.enginesUnknown')}
                    onRetry={() => void capabilities.retry()}
                    busy={capabilities.reading}
                />
            )}

            {!readOnly && engineSource.state === 'known' && engines.length === 0 && (
                <div className="rounded-lg border border-info/30 bg-info/10 px-4 py-3 text-sm text-fg">
                    {isAdditionalUser ? t('db.teamEngineUnavailable') : t('databases.noServersHint')}
                </div>
            )}

            {/* Create Database Form */}
            {showCreateForm && canCreate && (
                <div className="bg-surface-2/50 rounded-lg p-6 border border-border">
                    <h4 className="text-md font-semibold text-fg mb-4">Create New Database</h4>
                    <form onSubmit={handleCreateDatabase} className="space-y-4">
                        <div>
                            <label className="block text-sm text-fg-muted mb-2">Database Name</label>
                            <input
                                type="text"
                                value={dbName}
                                onChange={(e) => setDbName(e.target.value)}
                                placeholder="myapp"
                                className="w-full bg-surface border border-border rounded px-4 py-2 text-fg focus:border-primary"
                                required
                            />
                            <p className="text-xs text-fg-subtle mt-1">
                                Will be prefixed with domain name: {domainName.replace(/\./g, '_')}_{dbName}
                            </p>
                        </div>

                        <div>
                            <label className="block text-sm text-fg-muted mb-2">Database Type</label>
                            <select
                                value={dbType ?? ''}
                                onChange={(e) => setDbType(e.target.value as DatabaseType)}
                                className="w-full bg-surface border border-border rounded px-4 py-2 text-fg focus:border-primary"
                            >
                                {engines.map((eng) => (
                                    <option key={eng.value} value={eng.value}>{eng.label}</option>
                                ))}
                            </select>
                        </div>

                        <div>
                            <label className="block text-sm text-fg-muted mb-2">Password</label>
                            <input
                                type="password"
                                value={dbPassword}
                                onChange={(e) => setDbPassword(e.target.value)}
                                placeholder="Enter a strong password"
                                className="w-full bg-surface border border-border rounded px-4 py-2 text-fg focus:border-primary"
                                required
                            />
                        </div>

                        <div className="flex gap-2">
                            <button
                                type="submit"
                                disabled={creating || dbType === null}
                                className="px-6 py-2 bg-success text-white rounded hover:bg-success disabled:opacity-50 flex items-center gap-2"
                            >
                                <Database className="w-4 h-4" />
                                {creating ? 'Creating...' : 'Create Database'}
                            </button>
                            <button
                                type="button"
                                onClick={() => {
                                    setShowCreateForm(false);
                                    setDbName('');
                                    setDbPassword('');
                                }}
                                className="px-6 py-2 bg-surface-3 text-fg rounded hover:bg-surface-3"
                            >
                                Cancel
                            </button>
                        </div>
                    </form>
                </div>
            )}

            {/* Database List */}
            <div className="bg-surface-2/50 rounded-lg border border-border">
                <div className="flex items-center justify-between p-4 border-b border-border">
                    <div className="flex items-center gap-2">
                        <Database className="w-5 h-5 text-primary" />
                        <h4 className="text-md font-semibold text-fg">Databases</h4>
                        {listed && (
                            <span className="text-sm text-fg-muted">
                                ({listed.value.databases.length})
                            </span>
                        )}
                    </div>
                    <button
                        onClick={() => void list.retry()}
                        disabled={list.reading}
                        className="p-2 text-fg-muted hover:text-fg transition-colors"
                        title="Refresh"
                        aria-label="Refresh"
                    >
                        <RefreshCw className={`w-4 h-4 ${list.reading ? 'animate-spin' : ''}`} />
                    </button>
                </div>

                <div className="p-4">
                    <RemoteGate
                        remote={list.remote}
                        checking={t('db.checking')}
                        failed={t('db.unknown')}
                        onRetry={() => void list.retry()}
                        busy={list.reading}
                    >
                        {(shown) => (shown.value.databases.length === 0 ? (
                        <div className="text-center text-fg-subtle py-12">
                            <Database className="w-12 h-12 mx-auto mb-2 opacity-50" />
                            <p>{t('databases.empty.databases')}</p>
                            {canCreate && (
                                <p className="text-sm mt-1">{t('databases.empty.databasesHint')}</p>
                            )}
                        </div>
                    ) : (
                        <div className="space-y-3">
                            {shown.value.databases.map((db) => (
                                <div
                                    key={db.id}
                                    className="bg-surface border border-border rounded p-4 hover:border-border-strong transition-colors"
                                >
                                    <div className="flex items-start justify-between">
                                        <div className="flex-1">
                                            <div className="flex items-center gap-2 mb-2">
                                                <Database className="w-4 h-4 text-primary" />
                                                <h5 className="font-mono text-fg font-semibold">{db.name}</h5>
                                                <span className={`text-xs px-2 py-0.5 rounded ${db.type === 'mysql'
                                                        ? 'bg-primary/50 text-primary'
                                                        : 'bg-surface-2 text-fg-muted'
                                                    }`}>
                                                    {db.type.toUpperCase()}
                                                </span>
                                            </div>
                                            <div className="grid grid-cols-2 gap-2 text-sm">
                                                <div>
                                                    <span className="text-fg-muted">User:</span>
                                                    <span className="ml-2 text-fg font-mono">{db.user}</span>
                                                </div>
                                                <div>
                                                    <span className="text-fg-muted">Created:</span>
                                                    <span className="ml-2 text-fg">
                                                        {new Date(db.created_at).toLocaleDateString()}
                                                    </span>
                                                </div>
                                            </div>
                                        </div>
                                        {!readOnly && (
                                            <div className="flex gap-2">
                                                <button
                                                    onClick={() => handleDeleteDatabase(db)}
                                                    disabled={shown.stale}
                                                    className="p-2 text-danger hover:bg-danger/30 rounded transition-colors disabled:pointer-events-none disabled:opacity-40"
                                                    title="Delete database"
                                                    aria-label="Delete database"
                                                >
                                                    <Trash2 className="w-4 h-4" />
                                                </button>
                                            </div>
                                        )}
                                    </div>
                                </div>
                            ))}
                        </div>
                        ))}
                    </RemoteGate>
                </div>
            </div>

            {!isAdditionalUser && <DBToolsCard capabilities={capabilities.remote} />}
        </div>
    );
}
