import { useState, useEffect } from 'react';
import { Save, RefreshCw } from 'lucide-react';

interface ExtendedPHPConfig {
    // Performance & Security
    memory_limit: string;
    max_execution_time: string;
    max_input_time: string;
    post_max_size: string;
    upload_max_filesize: string;
    opcache_enable: string;
    disable_functions: string;

    // Common Settings
    include_path: string;
    session_save_path: string;
    realpath_cache_size: string;
    open_basedir: string;
    error_reporting: string;
    display_errors: string;
    log_errors: string;
    allow_url_fopen: string;
    file_uploads: string;
    short_open_tag: string;

    // Additional Directives
    additional_directives: string;
}

interface PHPExtendedConfigProps {
    version: string;
}

export function PHPExtendedConfig({ version }: PHPExtendedConfigProps) {
    const [config, setConfig] = useState<ExtendedPHPConfig | null>(null);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [activeSection, setActiveSection] = useState<'performance' | 'common' | 'advanced'>('performance');

    const fetchConfig = async () => {
        setLoading(true);
        try {
            const res = await fetch(`/api/v1/php/extended-config?version=${version}`);
            if (res.ok) {
                setConfig(await res.json());
            } else {
                alert('Failed to load configuration');
            }
        } catch (err) {
            console.error(err);
            alert('Failed to load configuration');
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        fetchConfig();
    }, [version]);

    const handleSave = async () => {
        if (!config) return;
        if (!confirm(`Apply changes to php.ini for PHP ${version}?`)) return;

        setSaving(true);
        try {
            const res = await fetch('/api/v1/php/extended-config', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    version,
                    config
                })
            });

            if (res.ok) {
                alert('Configuration saved successfully');
            } else {
                alert('Failed to save configuration');
            }
        } catch (err) {
            console.error(err);
            alert('Failed to save configuration');
        } finally {
            setSaving(false);
        }
    };

    if (loading) {
        return (
            <div className="flex items-center justify-center h-64">
                <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-500"></div>
            </div>
        );
    }

    if (!config) return null;

    const renderInput = (label: string, key: keyof ExtendedPHPConfig, placeholder?: string) => (
        <div>
            <label className="block text-xs text-gray-400 mb-1 font-mono">{label}</label>
            <input
                type="text"
                value={config[key]}
                onChange={e => setConfig({ ...config, [key]: e.target.value })}
                placeholder={placeholder}
                className="w-full bg-gray-900 border border-gray-700 rounded px-3 py-2 text-white text-sm font-mono focus:border-blue-500 focus:outline-none"
            />
        </div>
    );

    const renderSelect = (label: string, key: keyof ExtendedPHPConfig, options: string[]) => (
        <div>
            <label className="block text-xs text-gray-400 mb-1 font-mono">{label}</label>
            <select
                value={config[key]}
                onChange={e => setConfig({ ...config, [key]: e.target.value })}
                className="w-full bg-gray-900 border border-gray-700 rounded px-3 py-2 text-white text-sm font-mono focus:border-blue-500 focus:outline-none"
            >
                {options.map(opt => (
                    <option key={opt} value={opt}>{opt}</option>
                ))}
            </select>
        </div>
    );

    return (
        <div className="space-y-6">
            {/* Section Tabs */}
            <div className="flex border-b border-gray-700">
                <button
                    onClick={() => setActiveSection('performance')}
                    className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${activeSection === 'performance'
                        ? 'border-blue-500 text-blue-400'
                        : 'border-transparent text-gray-400 hover:text-white'
                        }`}
                >
                    Performance & Security
                </button>
                <button
                    onClick={() => setActiveSection('common')}
                    className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${activeSection === 'common'
                        ? 'border-blue-500 text-blue-400'
                        : 'border-transparent text-gray-400 hover:text-white'
                        }`}
                >
                    Common Settings
                </button>
                <button
                    onClick={() => setActiveSection('advanced')}
                    className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${activeSection === 'advanced'
                        ? 'border-blue-500 text-blue-400'
                        : 'border-transparent text-gray-400 hover:text-white'
                        }`}
                >
                    Additional Directives
                </button>
            </div>

            <div className="bg-gray-800/50 rounded-lg p-6 border border-gray-700">
                {activeSection === 'performance' && (
                    <div className="grid grid-cols-2 gap-6">
                        {renderSelect('memory_limit', 'memory_limit', ['64M', '128M', '256M', '512M', '1G', '2G'])}
                        {renderSelect('max_execution_time', 'max_execution_time', ['30', '60', '120', '300', '600'])}
                        {renderSelect('max_input_time', 'max_input_time', ['30', '60', '120', '300'])}
                        {renderSelect('post_max_size', 'post_max_size', ['8M', '16M', '32M', '64M', '128M'])}
                        {renderSelect('upload_max_filesize', 'upload_max_filesize', ['2M', '8M', '16M', '32M', '64M', '128M'])}
                        {renderSelect('opcache.enable', 'opcache_enable', ['0', '1'])}
                        <div className="col-span-2">
                            {renderInput('disable_functions', 'disable_functions')}
                        </div>
                    </div>
                )}

                {activeSection === 'common' && (
                    <div className="grid grid-cols-2 gap-6">
                        {renderInput('include_path', 'include_path')}
                        {renderInput('session.save_path', 'session_save_path')}
                        {renderInput('realpath_cache_size', 'realpath_cache_size')}
                        {renderInput('open_basedir', 'open_basedir')}
                        {renderInput('error_reporting', 'error_reporting')}
                        {renderSelect('display_errors', 'display_errors', ['Off', 'On'])}
                        {renderSelect('log_errors', 'log_errors', ['Off', 'On'])}
                        {renderSelect('allow_url_fopen', 'allow_url_fopen', ['Off', 'On'])}
                        {renderSelect('file_uploads', 'file_uploads', ['Off', 'On'])}
                        {renderSelect('short_open_tag', 'short_open_tag', ['Off', 'On'])}
                    </div>
                )}

                {activeSection === 'advanced' && (
                    <div className="space-y-4">
                        <p className="text-sm text-gray-400">
                            Add custom php.ini directives here. One directive per line.
                            Example: <code className="bg-gray-900 px-1 rounded">date.timezone = UTC</code>
                        </p>
                        <textarea
                            value={config.additional_directives}
                            onChange={e => setConfig({ ...config, additional_directives: e.target.value })}
                            className="w-full h-64 bg-gray-900 border border-gray-700 rounded p-4 text-white font-mono text-sm focus:border-blue-500 focus:outline-none"
                            placeholder="; Custom directives"
                        />
                    </div>
                )}

                <div className="mt-8 pt-6 border-t border-gray-700 flex justify-end gap-3">
                    <button
                        onClick={fetchConfig}
                        className="px-4 py-2 text-gray-400 hover:text-white text-sm font-medium flex items-center gap-2"
                    >
                        <RefreshCw className="w-4 h-4" /> Reset
                    </button>
                    <button
                        onClick={handleSave}
                        disabled={saving}
                        className="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 text-sm font-medium flex items-center gap-2 disabled:opacity-50"
                    >
                        <Save className="w-4 h-4" />
                        {saving ? 'Saving...' : 'Save Configuration'}
                    </button>
                </div>
            </div>
        </div>
    );
}
