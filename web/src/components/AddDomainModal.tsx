import { useState } from 'react';
import { Globe, X, Lock } from 'lucide-react';
import { showToast } from './Toast';

interface AddDomainModalProps {
    onClose: () => void;
    onSuccess: () => void;
}

const API_BASE = '/api/v1';

export function AddDomainModal({ onClose, onSuccess }: AddDomainModalProps) {
    const [domainName, setDomainName] = useState('');
    const [phpVersion, setPHPVersion] = useState('8.3');
    const [sslEnabled, setSSLEnabled] = useState(false);
    const [tempDomain, setTempDomain] = useState(false);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setLoading(true);
        setError(null);

        try {
            const res = await fetch(`${API_BASE}/domains/create`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    domain: domainName,
                    php_version: phpVersion,
                    ssl_enabled: sslEnabled,
                    temp_domain: tempDomain,
                }),
            });

            if (!res.ok) {
                const errorData = await res.json().catch(() => ({}));
                throw new Error(errorData.error || 'Failed to create domain');
            }

            showToast('success', `Domain ${domainName} created successfully!`);
            onSuccess();
        } catch (err: any) {
            setError(err.message);
            showToast('error', err.message);
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
            <div className="bg-gray-900 border border-gray-800 rounded-xl p-8 max-w-2xl w-full mx-4">
                <div className="flex justify-between items-center mb-6">
                    <div className="flex items-center gap-3">
                        <div className="p-2 bg-blue-500/10 rounded-lg">
                            <Globe className="w-6 h-6 text-blue-400" />
                        </div>
                        <div>
                            <h2 className="text-xl font-bold text-gray-100">Add Domain</h2>
                            <p className="text-sm text-gray-500">Create a new hosted domain</p>
                        </div>
                    </div>
                    <button
                        onClick={onClose}
                        className="p-2 hover:bg-gray-800 rounded-lg transition-colors"
                    >
                        <X className="w-5 h-5 text-gray-400" />
                    </button>
                </div>

                {error && (
                    <div className="mb-6 p-4 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm">
                        {error}
                    </div>
                )}

                <form onSubmit={handleSubmit} className="space-y-4">
                    <div>
                        <label className="block text-sm font-medium text-gray-300 mb-2">
                            Domain Name
                        </label>
                        <input
                            type="text"
                            value={domainName}
                            onChange={(e) => setDomainName(e.target.value)}
                            className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-3 text-gray-100 focus:outline-none focus:border-blue-500"
                            placeholder="example.com"
                            required
                        />
                        <p className="text-xs text-gray-500 mt-1">Enter the domain name without www</p>
                    </div>

                    <div>
                        <label className="block text-sm font-medium text-gray-300 mb-2">
                            PHP Version
                        </label>
                        <select
                            value={phpVersion}
                            onChange={(e) => setPHPVersion(e.target.value)}
                            className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-3 text-gray-100 focus:outline-none focus:border-blue-500"
                        >
                            <option value="8.3">PHP 8.3</option>
                            <option value="8.4">PHP 8.4</option>
                        </select>
                    </div>

                    <div className="flex items-start gap-3 p-4 bg-gray-800/50 rounded-lg">
                        <input
                            type="checkbox"
                            id="ssl"
                            checked={sslEnabled}
                            onChange={(e) => setSSLEnabled(e.target.checked)}
                            className="mt-1"
                        />
                        <div className="flex-1">
                            <label htmlFor="ssl" className="flex items-center gap-2 text-sm font-medium text-gray-300 cursor-pointer">
                                <Lock className="w-4 h-4" />
                                Enable SSL (Let's Encrypt)
                            </label>
                            <p className="text-xs text-gray-500 mt-1">
                                Automatically provision and renew SSL certificate
                            </p>
                        </div>
                    </div>

                    <div className="flex items-start gap-3 p-4 bg-gray-800/50 rounded-lg">
                        <input
                            type="checkbox"
                            id="temp"
                            checked={tempDomain}
                            onChange={(e) => setTempDomain(e.target.checked)}
                            className="mt-1"
                        />
                        <div className="flex-1">
                            <label htmlFor="temp" className="text-sm font-medium text-gray-300 cursor-pointer">
                                Temporary Domain
                            </label>
                            <p className="text-xs text-gray-500 mt-1">
                                Add a temporary subdomain for testing before DNS propagation
                            </p>
                        </div>
                    </div>

                    <div className="flex gap-3 pt-4">
                        <button
                            type="submit"
                            disabled={loading}
                            className="flex-1 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-700 text-white px-6 py-3 rounded-lg transition-colors font-medium"
                        >
                            {loading ? 'Creating...' : 'Create Domain'}
                        </button>
                        <button
                            type="button"
                            onClick={onClose}
                            className="px-6 py-3 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-lg transition-colors"
                        >
                            Cancel
                        </button>
                    </div>
                </form>
            </div>
        </div>
    );
}
