import { Globe, Plus, Trash2, Search, Shield, ExternalLink, Settings, HardDrive, Activity } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { AddDomainModal } from './AddDomainModal';
import { showToast } from './Toast';

interface Domain {
    id: number;
    domain_name: string;
    php_version: string;
    ssl_enabled: boolean;
    status: string;
    created_at: string;
    disk_usage?: number;
    bandwidth?: number;
}

const API_BASE = '/api/v1';

export function Domains() {
    const navigate = useNavigate();
    const [domains, setDomains] = useState<Domain[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [showAddModal, setShowAddModal] = useState(false);
    const [searchQuery, setSearchQuery] = useState('');
    const [selectedDomains, setSelectedDomains] = useState<number[]>([]);

    useEffect(() => {
        loadDomains();
    }, []);

    const loadDomains = async () => {
        setLoading(true);
        setError(null);
        try {
            const res = await fetch(`${API_BASE}/domains`);
            if (!res.ok) throw new Error('Failed to load domains');
            const data = await res.json();
            setDomains(data || []);
        } catch (err: any) {
            setError(err.message);
            showToast('error', 'Failed to load domains');
        } finally {
            setLoading(false);
        }
    };

    const handleDelete = async (id: number, domain: string) => {
        if (!confirm(`Are you sure you want to delete ${domain}?`)) return;

        try {
            const res = await fetch(`${API_BASE}/domains/${id}`, { method: 'DELETE' });
            if (!res.ok) throw new Error('Failed to delete domain');
            showToast('success', `Domain ${domain} deleted successfully`);
            loadDomains();
        } catch (err: any) {
            showToast('error', err.message);
        }
    };

    const toggleSelectAll = () => {
        if (selectedDomains.length === filteredDomains.length) {
            setSelectedDomains([]);
        } else {
            setSelectedDomains(filteredDomains.map(d => d.id));
        }
    };

    const toggleSelect = (id: number) => {
        if (selectedDomains.includes(id)) {
            setSelectedDomains(selectedDomains.filter(d => d !== id));
        } else {
            setSelectedDomains([...selectedDomains, id]);
        }
    };

    const filteredDomains = domains.filter(d =>
        d.domain_name.toLowerCase().includes(searchQuery.toLowerCase())
    );

    const formatBytes = (bytes: number = 0) => {
        if (bytes === 0) return '0 MB';
        const mb = bytes / (1024 * 1024);
        if (mb < 1024) return `${mb.toFixed(1)} MB`;
        return `${(mb / 1024).toFixed(2)} GB`;
    };

    if (loading) {
        return (
            <div className="flex flex-col items-center justify-center p-12 text-gray-400">
                <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-500 mb-4"></div>
                <p>Loading domains...</p>
            </div>
        );
    }

    if (error) {
        return <div className="text-red-400">Error: {error}</div>;
    }

    return (
        <>
            <div className="space-y-4">
                {/* Header */}
                <div className="flex justify-between items-center">
                    <div>
                        <h2 className="text-2xl font-bold text-gray-100">Domains</h2>
                        <p className="text-sm text-gray-400 mt-1">
                            {domains.length} domain{domains.length !== 1 ? 's' : ''} registered
                        </p>
                    </div>
                    <button
                        onClick={() => setShowAddModal(true)}
                        className="flex items-center gap-2 bg-blue-600 hover:bg-blue-700 text-white px-5 py-2.5 rounded-lg transition-colors font-medium"
                    >
                        <Plus className="w-4 h-4" />
                        Add Domain
                    </button>
                </div>

                {domains.length === 0 ? (
                    <div className="bg-gray-900 border border-gray-800 rounded-xl p-12 text-center">
                        <Globe className="w-16 h-16 text-gray-600 mx-auto mb-4" />
                        <h3 className="text-xl font-bold text-gray-300 mb-2">No domains yet</h3>
                        <p className="text-gray-500 mb-6">Get started by adding your first domain</p>
                        <button
                            onClick={() => setShowAddModal(true)}
                            className="inline-flex items-center gap-2 bg-blue-600 hover:bg-blue-700 text-white px-6 py-3 rounded-lg transition-colors font-medium"
                        >
                            <Plus className="w-5 h-5" />
                            Add Domain
                        </button>
                    </div>
                ) : (
                    <div className="bg-gray-900 border border-gray-800 rounded-xl overflow-hidden">
                        {/* Toolbar */}
                        <div className="p-4 border-b border-gray-800 flex items-center justify-between gap-4">
                            <div className="flex items-center gap-2">
                                <button
                                    onClick={() => setShowAddModal(true)}
                                    className="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 text-white rounded text-sm font-medium flex items-center gap-1"
                                >
                                    <Plus className="w-3.5 h-3.5" />
                                    Add Domain
                                </button>
                                {selectedDomains.length > 0 && (
                                    <button
                                        onClick={() => {
                                            if (confirm(`Delete ${selectedDomains.length} selected domains?`)) {
                                                // Delete selected domains
                                            }
                                        }}
                                        className="px-3 py-1.5 bg-red-900/50 hover:bg-red-900 text-red-400 rounded text-sm font-medium flex items-center gap-1"
                                    >
                                        <Trash2 className="w-3.5 h-3.5" />
                                        Remove ({selectedDomains.length})
                                    </button>
                                )}
                            </div>
                            <div className="relative">
                                <Search className="w-4 h-4 text-gray-500 absolute left-3 top-1/2 -translate-y-1/2" />
                                <input
                                    type="text"
                                    placeholder="Search domains..."
                                    value={searchQuery}
                                    onChange={(e) => setSearchQuery(e.target.value)}
                                    className="pl-9 pr-4 py-1.5 bg-gray-800 border border-gray-700 rounded text-sm text-white placeholder-gray-500 focus:border-blue-500 focus:outline-none w-64"
                                />
                            </div>
                        </div>

                        {/* Table */}
                        <div className="overflow-x-auto">
                            <table className="w-full">
                                <thead className="bg-gray-800/50">
                                    <tr>
                                        <th className="w-10 px-4 py-3">
                                            <input
                                                type="checkbox"
                                                checked={selectedDomains.length === filteredDomains.length && filteredDomains.length > 0}
                                                onChange={toggleSelectAll}
                                                className="w-4 h-4 bg-gray-700 border-gray-600 rounded"
                                            />
                                        </th>
                                        <th className="text-left px-4 py-3 text-xs font-semibold text-gray-400 uppercase tracking-wider">
                                            Domain Name
                                        </th>
                                        <th className="text-left px-4 py-3 text-xs font-semibold text-gray-400 uppercase tracking-wider">
                                            PHP
                                        </th>
                                        <th className="text-left px-4 py-3 text-xs font-semibold text-gray-400 uppercase tracking-wider">
                                            <div className="flex items-center gap-1">
                                                <HardDrive className="w-3.5 h-3.5" />
                                                Disk
                                            </div>
                                        </th>
                                        <th className="text-left px-4 py-3 text-xs font-semibold text-gray-400 uppercase tracking-wider">
                                            <div className="flex items-center gap-1">
                                                <Activity className="w-3.5 h-3.5" />
                                                Traffic
                                            </div>
                                        </th>
                                        <th className="text-left px-4 py-3 text-xs font-semibold text-gray-400 uppercase tracking-wider">
                                            Status
                                        </th>
                                        <th className="text-right px-4 py-3 text-xs font-semibold text-gray-400 uppercase tracking-wider">
                                            Actions
                                        </th>
                                    </tr>
                                </thead>
                                <tbody className="divide-y divide-gray-800">
                                    {filteredDomains.map(domain => (
                                        <tr
                                            key={domain.id}
                                            className="hover:bg-gray-800/30 transition-colors group"
                                        >
                                            <td className="px-4 py-3">
                                                <input
                                                    type="checkbox"
                                                    checked={selectedDomains.includes(domain.id)}
                                                    onChange={() => toggleSelect(domain.id)}
                                                    className="w-4 h-4 bg-gray-700 border-gray-600 rounded"
                                                />
                                            </td>
                                            <td className="px-4 py-3">
                                                <div className="flex items-center gap-3">
                                                    <div className="flex items-center gap-2">
                                                        {domain.ssl_enabled ? (
                                                            <Shield className="w-4 h-4 text-green-400" />
                                                        ) : (
                                                            <Globe className="w-4 h-4 text-gray-500" />
                                                        )}
                                                        <button
                                                            onClick={() => navigate(`/domains/${encodeURIComponent(domain.domain_name)}`)}
                                                            className="text-blue-400 hover:text-blue-300 font-medium text-left"
                                                        >
                                                            {domain.domain_name}
                                                        </button>
                                                    </div>
                                                </div>
                                            </td>
                                            <td className="px-4 py-3">
                                                <span className="text-gray-300 text-sm">{domain.php_version}</span>
                                            </td>
                                            <td className="px-4 py-3">
                                                <span className="text-gray-400 text-sm">{formatBytes(domain.disk_usage)}</span>
                                            </td>
                                            <td className="px-4 py-3">
                                                <span className="text-gray-400 text-sm">{formatBytes(domain.bandwidth)}/mo</span>
                                            </td>
                                            <td className="px-4 py-3">
                                                <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${domain.status === 'active'
                                                    ? 'bg-green-900/30 text-green-400'
                                                    : 'bg-gray-800 text-gray-400'
                                                    }`}>
                                                    <span className={`w-1.5 h-1.5 rounded-full mr-1.5 ${domain.status === 'active' ? 'bg-green-400' : 'bg-gray-500'
                                                        }`}></span>
                                                    {domain.status}
                                                </span>
                                            </td>
                                            <td className="px-4 py-3">
                                                <div className="flex items-center justify-end gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                                                    <a
                                                        href={`https://${domain.domain_name}`}
                                                        target="_blank"
                                                        rel="noopener noreferrer"
                                                        className="p-1.5 text-gray-400 hover:text-white hover:bg-gray-700 rounded"
                                                        title="Visit Site"
                                                    >
                                                        <ExternalLink className="w-4 h-4" />
                                                    </a>
                                                    <button
                                                        onClick={() => navigate(`/domains/${encodeURIComponent(domain.domain_name)}`)}
                                                        className="p-1.5 text-gray-400 hover:text-blue-400 hover:bg-gray-700 rounded"
                                                        title="Settings"
                                                    >
                                                        <Settings className="w-4 h-4" />
                                                    </button>
                                                    <button
                                                        onClick={() => handleDelete(domain.id, domain.domain_name)}
                                                        className="p-1.5 text-gray-400 hover:text-red-400 hover:bg-gray-700 rounded"
                                                        title="Delete"
                                                    >
                                                        <Trash2 className="w-4 h-4" />
                                                    </button>
                                                </div>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>

                        {/* Footer */}
                        <div className="px-4 py-3 border-t border-gray-800 flex items-center justify-between text-sm text-gray-400">
                            <span>{filteredDomains.length} of {domains.length} domains</span>
                            {searchQuery && (
                                <button
                                    onClick={() => setSearchQuery('')}
                                    className="text-blue-400 hover:text-blue-300"
                                >
                                    Clear search
                                </button>
                            )}
                        </div>
                    </div>
                )}
            </div>

            {showAddModal && (
                <AddDomainModal
                    onClose={() => setShowAddModal(false)}
                    onSuccess={() => {
                        setShowAddModal(false);
                        loadDomains();
                    }}
                />
            )}
        </>
    );
}
