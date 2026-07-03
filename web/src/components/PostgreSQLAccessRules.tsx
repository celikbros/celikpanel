import { useState, useEffect } from 'react';
import { api } from '../lib/api';
import { Save, Shield, Plus, Trash2, AlertCircle } from 'lucide-react';

interface PostgreSQLAccessRulesProps {
    configPath: string;
}

interface AccessRule {
    id: number;
    type: string;
    database: string;
    user: string;
    address: string;
    method: string;
    originalLine?: string; // To track if it's an edit of an existing line
    isNew?: boolean;
}

export function PostgreSQLAccessRules({ configPath }: PostgreSQLAccessRulesProps) {
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [rules, setRules] = useState<AccessRule[]>([]);

    useEffect(() => {
        loadConfig();
    }, [configPath]);

    const loadConfig = async () => {
        setLoading(true);
        try {
            const res = await api.getConfig(configPath);
            parseConfig(res.Content);
        } catch (err) {
            console.error(err);
        } finally {
            setLoading(false);
        }
    };

    const parseConfig = (content: string) => {
        const lines = content.split('\n');
        const parsedRules: AccessRule[] = [];

        lines.forEach((line, index) => {
            const trimmed = line.trim();
            if (!trimmed || trimmed.startsWith('#')) return;

            // Split by whitespace
            const parts = trimmed.split(/\s+/);

            // Basic validation
            // local: type, db, user, method (4 parts)
            // host: type, db, user, address, method (5 parts)

            if (parts.length >= 4) {
                const type = parts[0];
                let rule: AccessRule = {
                    id: index, // Use line index as temporary ID
                    type: type,
                    database: parts[1],
                    user: parts[2],
                    address: '',
                    method: '',
                    originalLine: line
                };

                if (type === 'local') {
                    rule.method = parts[3];
                    rule.address = '-'; // Not applicable
                } else {
                    // host, hostssl, etc.
                    if (parts.length >= 5) {
                        rule.address = parts[3];
                        rule.method = parts[4];
                    } else {
                        // Invalid host line? Skip or mark partial
                        return;
                    }
                }
                parsedRules.push(rule);
            }
        });
        setRules(parsedRules);
    };

    const handleAddRule = () => {
        const newRule: AccessRule = {
            id: Date.now(),
            type: 'host',
            database: 'all',
            user: 'all',
            address: '0.0.0.0/0',
            method: 'scram-sha-256',
            isNew: true
        };
        setRules([...rules, newRule]);
    };

    const handleDeleteRule = (id: number) => {
        setRules(rules.filter(r => r.id !== id));
    };

    const handleUpdateRule = (id: number, field: keyof AccessRule, value: string) => {
        setRules(rules.map(r => r.id === id ? { ...r, [field]: value } : r));
    };

    const handleSave = async () => {
        setSaving(true);
        try {
            // Strategy: Reconstruct the file.
            // We want to keep comments?
            // "rawContent" contains everything.
            // But if we deleted a rule, we must remove it from rawContent.
            // This is complex because we didn't track line numbers perfectly against edits.

            // Safer Strategy:
            // 1. Keep all Comment lines from original file.
            // 2. Append our Rules at the end? NO, order matters.

            // Hybrid Strategy:
            // Use specific marker? OR just Rewrite the file cleanly with standard header.
            // pg_hba.conf order matters significantly.

            // Let's Rewrite the file but try to preserve header comments if they exist at top.
            // Or just generate a clean file. Users who use Visual Editor expect clean output often.

            let output = `# PostgreSQL Client Authentication Configuration File
# Managed by CelikPanel
#
# TYPE  DATABASE        USER            ADDRESS                 METHOD
`;

            rules.forEach(rule => {
                let line = '';
                if (rule.type === 'local') {
                    // Align columns
                    line = `${rule.type.padEnd(7)} ${rule.database.padEnd(15)} ${rule.user.padEnd(15)} ${''.padEnd(23)} ${rule.method}`;
                } else {
                    line = `${rule.type.padEnd(7)} ${rule.database.padEnd(15)} ${rule.user.padEnd(15)} ${rule.address.padEnd(23)} ${rule.method}`;
                }
                output += line + '\n';
            });

            await api.saveConfig(configPath, output);
            alert('Access rules saved! Reload PostgreSQL to apply.');
            // Re-parse to get fresh IDs
            parseConfig(output);
        } catch (err: any) {
            alert('Error saving rules: ' + err.message);
        } finally {
            setSaving(false);
        }
    };

    if (loading) return <div className="p-8 text-center text-fg-subtle">Loading access rules...</div>;

    return (
        <div className="bg-surface/50 border border-border rounded-xl p-6">
            <div className="flex items-center justify-between mb-6">
                <h3 className="text-lg font-bold text-fg flex items-center gap-2">
                    <Shield className="w-5 h-5 text-success" />
                    Access Rules (pg_hba.conf)
                </h3>
                <button
                    onClick={handleAddRule}
                    className="flex items-center gap-2 px-4 py-2 bg-surface-2 hover:bg-surface-3 text-primary rounded-lg text-sm font-bold transition-colors"
                >
                    <Plus size={16} /> Add Rule
                </button>
            </div>

            <div className="overflow-x-auto">
                <table className="w-full text-left border-collapse">
                    <thead>
                        <tr className="text-xs font-bold text-fg-subtle uppercase tracking-wider border-b border-border">
                            <th className="pb-3 pl-2">Type</th>
                            <th className="pb-3">Database</th>
                            <th className="pb-3">User</th>
                            <th className="pb-3">Address</th>
                            <th className="pb-3">Method</th>
                            <th className="pb-3 text-right pr-2">Actions</th>
                        </tr>
                    </thead>
                    <tbody className="text-sm">
                        {rules.map((rule) => (
                            <tr key={rule.id} className="border-b border-border/50 hover:bg-surface-2/30 transition-colors group">
                                <td className="py-2 pl-2">
                                    <select
                                        value={rule.type}
                                        onChange={(e) => handleUpdateRule(rule.id, 'type', e.target.value)}
                                        className="bg-transparent text-fg focus:outline-none focus:text-primary font-mono w-20 cursor-pointer"
                                    >
                                        <option value="local" className="bg-surface">local</option>
                                        <option value="host" className="bg-surface">host</option>
                                        <option value="hostssl" className="bg-surface">hostssl</option>
                                    </select>
                                </td>
                                <td className="py-2">
                                    <input
                                        type="text"
                                        value={rule.database}
                                        onChange={(e) => handleUpdateRule(rule.id, 'database', e.target.value)}
                                        className="bg-transparent text-fg-muted focus:outline-none focus:text-primary font-mono w-full"
                                    />
                                </td>
                                <td className="py-2">
                                    <input
                                        type="text"
                                        value={rule.user}
                                        onChange={(e) => handleUpdateRule(rule.id, 'user', e.target.value)}
                                        className="bg-transparent text-fg-muted focus:outline-none focus:text-primary font-mono w-full"
                                    />
                                </td>
                                <td className="py-2">
                                    {rule.type !== 'local' ? (
                                        <input
                                            type="text"
                                            value={rule.address}
                                            onChange={(e) => handleUpdateRule(rule.id, 'address', e.target.value)}
                                            className="bg-transparent text-fg-muted focus:outline-none focus:text-primary font-mono w-32"
                                        />
                                    ) : <span className="text-fg-subtle">-</span>}
                                </td>
                                <td className="py-2">
                                    <select
                                        value={rule.method}
                                        onChange={(e) => handleUpdateRule(rule.id, 'method', e.target.value)}
                                        className="bg-transparent text-warning focus:outline-none focus:text-warning font-mono w-24 cursor-pointer"
                                    >
                                        <option value="md5" className="bg-surface">md5</option>
                                        <option value="scram-sha-256" className="bg-surface">scram-sha-256</option>
                                        <option value="peer" className="bg-surface">peer</option>
                                        <option value="ident" className="bg-surface">ident</option>
                                        <option value="trust" className="bg-surface text-danger">trust</option>
                                        <option value="reject" className="bg-surface text-danger">reject</option>
                                    </select>
                                </td>
                                <td className="py-2 text-right pr-2">
                                    <button
                                        onClick={() => handleDeleteRule(rule.id)}
                                        className="p-1.5 text-fg-subtle hover:text-danger hover:bg-danger/10 rounded-lg transition-colors"
                                    >
                                        <Trash2 size={16} />
                                    </button>
                                </td>
                            </tr>
                        ))}
                        {rules.length === 0 && (
                            <tr>
                                <td colSpan={6} className="py-8 text-center text-fg-subtle italic">
                                    No access rules found. Add one to allow connections.
                                </td>
                            </tr>
                        )}
                    </tbody>
                </table>
            </div>

            <div className="mt-4 flex items-center justify-between">
                <div className="text-xs text-fg-subtle flex items-center gap-1">
                    <AlertCircle size={14} />
                    <span>Order matters! First matching rule is used.</span>
                </div>
                <button
                    onClick={handleSave}
                    disabled={saving}
                    className="flex items-center gap-2 px-6 py-2 bg-primary hover:bg-primary text-white rounded-lg font-bold transition-all disabled:opacity-50"
                >
                    <Save size={18} />
                    {saving ? 'Saving...' : 'Save Rules'}
                </button>
            </div>
        </div>
    );
}
