import { useState, useEffect } from 'react';
import {
    Globe, Plus, Trash2
} from 'lucide-react';
import { showToast } from './Toast';

interface DNSRecord {
    id: number;
    name: string;
    type: string;
    content: string;
    ttl: number;
    prio?: number;
    disabled: boolean;
}

interface DomainDNSManagerProps {
    domainId: number;
    domainName: string;
}

export function DomainDNSManager({ domainId, domainName }: DomainDNSManagerProps) {
    const [records, setRecords] = useState<DNSRecord[]>([]);
    const [loading, setLoading] = useState(true);
    const [zoneExists, setZoneExists] = useState<boolean | null>(null);

    // New Record Form
    const [showAddForm, setShowAddForm] = useState(false);
    const [newType, setNewType] = useState('A');
    const [newName, setNewName] = useState('@');
    const [newContent, setNewContent] = useState('');
    const [newTTL, setNewTTL] = useState(3600);
    const [newPrio, setNewPrio] = useState(0);

    const recordTypes = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV'];

    useEffect(() => {
        checkZone();
    }, [domainId]);

    const checkZone = async () => {
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/zone`);
            if (res.ok) {
                setZoneExists(true);
                loadRecords();
            } else {
                setZoneExists(false);
                setLoading(false);
            }
        } catch (err) {
            setZoneExists(false);
            setLoading(false);
        }
    };

    const loadRecords = async () => {
        setLoading(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/records`);
            if (res.ok) {
                const data = await res.json();
                setRecords(data.records || []);
            }
        } catch (err) {
            showToast('error', 'Failed to load DNS records');
        } finally {
            setLoading(false);
        }
    };

    const createZone = async () => {
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/zone`, { method: 'POST' });
            if (res.ok) {
                showToast('success', 'DNS Zone created');
                setZoneExists(true);
                loadRecords();
            } else {
                showToast('error', 'Failed to create zone');
            }
        } catch (err) {
            showToast('error', 'Failed to create zone');
        }
    };

    const addRecord = async () => {
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/records`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    name: newName,
                    type: newType,
                    content: newContent,
                    ttl: newTTL,
                    prio: newPrio
                })
            });
            if (res.ok) {
                showToast('success', 'Record added');
                setShowAddForm(false);
                setNewContent('');
                loadRecords();
            } else {
                showToast('error', 'Failed to add record');
            }
        } catch (err) {
            showToast('error', 'Failed to add record');
        }
    };

    const deleteRecord = async (id: number) => {
        if (!confirm('Delete this record?')) return;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/dns/records?id=${id}`, {
                method: 'DELETE'
            });
            if (res.ok) {
                showToast('success', 'Record deleted');
                loadRecords();
            }
        } catch (err) {
            showToast('error', 'Failed to delete record');
        }
    };

    if (loading && zoneExists === null) return <div className="text-fg">Checking DNS...</div>;

    if (zoneExists === false) {
        return (
            <div className="flex flex-col items-center justify-center p-10 bg-surface-2/50 rounded-lg border border-border">
                <Globe className="w-12 h-12 text-fg-subtle mb-4" />
                <h3 className="text-lg font-semibold text-fg mb-2">DNS Zone Not Active</h3>
                <p className="text-fg-muted text-center mb-6">
                    PowerDNS zone for <span className="text-fg">{domainName}</span> is not created yet.<br />
                    Enable DNS management to start adding records.
                </p>
                <button
                    onClick={createZone}
                    className="px-4 py-2 bg-primary hover:bg-primary-hover text-white rounded-lg flex items-center gap-2"
                >
                    <Plus className="w-4 h-4" />
                    Enable DNS Zone
                </button>
            </div>
        );
    }

    return (
        <div className="space-y-4">
            <div className="flex justify-between items-center">
                <h3 className="text-lg font-semibold text-fg">DNS Records for {domainName}</h3>
                <button
                    onClick={() => setShowAddForm(true)}
                    className="px-3 py-1.5 bg-primary hover:bg-primary-hover text-white rounded flex items-center gap-2 text-sm"
                >
                    <Plus className="w-4 h-4" />
                    Add Record
                </button>
            </div>

            {showAddForm && (
                <div className="bg-surface-2/50 border border-border rounded-lg p-4 mb-4">
                    <div className="grid grid-cols-12 gap-2 mb-3">
                        <div className="col-span-2">
                            <label className="text-xs text-fg-muted block mb-1">Type</label>
                            <select
                                value={newType}
                                onChange={e => setNewType(e.target.value)}
                                className="w-full bg-surface border border-border-strong rounded px-2 py-1.5 text-fg text-sm"
                            >
                                {recordTypes.map(t => <option key={t} value={t}>{t}</option>)}
                            </select>
                        </div>
                        <div className="col-span-3">
                            <label className="text-xs text-fg-muted block mb-1">Name (@ for root)</label>
                            <input
                                type="text"
                                value={newName}
                                onChange={e => setNewName(e.target.value)}
                                className="w-full bg-surface border border-border-strong rounded px-2 py-1.5 text-fg text-sm"
                                placeholder="@"
                            />
                        </div>
                        <div className="col-span-5">
                            <label className="text-xs text-fg-muted block mb-1">Content (Value)</label>
                            <input
                                type="text"
                                value={newContent}
                                onChange={e => setNewContent(e.target.value)}
                                className="w-full bg-surface border border-border-strong rounded px-2 py-1.5 text-fg text-sm"
                                placeholder="192.168.1.1"
                            />
                        </div>
                        <div className="col-span-2">
                            <label className="text-xs text-fg-muted block mb-1">TTL</label>
                            <input
                                type="number"
                                value={newTTL}
                                onChange={e => setNewTTL(parseInt(e.target.value))}
                                className="w-full bg-surface border border-border-strong rounded px-2 py-1.5 text-fg text-sm"
                            />
                        </div>
                    </div>
                    {(newType === 'MX' || newType === 'SRV') && (
                        <div className="mb-3">
                            <label className="text-xs text-fg-muted block mb-1">Priority</label>
                            <input
                                type="number"
                                value={newPrio}
                                onChange={e => setNewPrio(parseInt(e.target.value))}
                                className="w-20 bg-surface border border-border-strong rounded px-2 py-1.5 text-fg text-sm"
                            />
                        </div>
                    )}
                    <div className="flex justify-end gap-2">
                        <button
                            onClick={() => setShowAddForm(false)}
                            className="px-3 py-1.5 bg-surface-3 hover:bg-surface-3 rounded text-fg text-sm"
                        >
                            Cancel
                        </button>
                        <button
                            onClick={addRecord}
                            className="px-3 py-1.5 bg-success hover:bg-success rounded text-white text-sm"
                        >
                            Save Record
                        </button>
                    </div>
                </div>
            )}

            <div className="bg-surface-2/50 border border-border rounded-lg overflow-hidden">
                <table className="w-full text-left text-sm text-fg-muted">
                    <thead className="bg-surface/50 uppercase text-xs font-semibold">
                        <tr>
                            <th className="px-4 py-3">Typ</th>
                            <th className="px-4 py-3">Name</th>
                            <th className="px-4 py-3">Content</th>
                            <th className="px-4 py-3">TTL</th>
                            <th className="px-4 py-3 text-right">Actions</th>
                        </tr>
                    </thead>
                    <tbody className="divide-y divide-border">
                        {records.map(rec => (
                            <tr key={rec.id} className="hover:bg-surface-3/30">
                                <td className="px-4 py-3">
                                    <span className={`
                                        px-2 py-0.5 rounded text-xs font-bold
                                        ${rec.type === 'A' ? 'bg-primary/20 text-primary' : ''}
                                        ${rec.type === 'MX' ? 'bg-orange-500/20 text-orange-400' : ''}
                                        ${rec.type === 'CNAME' ? 'bg-purple-500/20 text-purple-400' : ''}
                                        ${rec.type === 'TXT' ? 'bg-surface-3/20 text-fg-muted' : ''}
                                    `}>
                                        {rec.type}
                                    </span>
                                </td>
                                <td className="px-4 py-3 text-fg font-medium">{rec.name}</td>
                                <td className="px-4 py-3 break-all">
                                    {rec.prio !== undefined && rec.prio !== 0 ? <span className="text-orange-400 mr-2">[{rec.prio}]</span> : ''}
                                    {rec.content}
                                </td>
                                <td className="px-4 py-3">{rec.ttl}</td>
                                <td className="px-4 py-3 text-right">
                                    <button
                                        onClick={() => deleteRecord(rec.id)}
                                        className="p-1 hover:bg-surface-3 rounded text-fg-muted hover:text-danger"
                                    >
                                        <Trash2 className="w-4 h-4" />
                                    </button>
                                </td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>
        </div>
    );
}
