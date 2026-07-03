import { useState, useEffect } from 'react';
import {
    ArrowLeft, Globe, Settings, Lock, Database,
    Activity, HardDrive, ExternalLink, RefreshCw,
    Shield, Code2, Folder, Mail, Terminal
} from 'lucide-react';
import { DomainPHPSettings } from './DomainPHPSettings';
import { DomainGeneralSettings } from './DomainGeneralSettings';
import { DomainSSLSettings } from './DomainSSLSettings';
import { DomainLogsViewer } from './DomainLogsViewer';
import { DomainDatabaseManager } from './DomainDatabaseManager';
import { DomainFileManager } from './DomainFileManager';
import { DomainBackupManager } from './DomainBackupManager';
import { DomainCronManager } from './DomainCronManager';
import { DomainMailManager } from './DomainMailManager';
import { DomainDNSManager } from './DomainDNSManager';

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

interface DomainDetailProps {
    domainId: number;
    onBack: () => void;
}

// Quick Action Button Component
function QuickAction({
    icon: Icon,
    label,
    description,
    color,
    onClick
}: {
    icon: any;
    label: string;
    description: string;
    color: string;
    onClick?: () => void;
}) {
    const colorClasses: { [key: string]: string } = {
        blue: 'from-primary/20 to-primary/10 border-primary/30 hover:border-primary/50 text-primary',
        green: 'from-success/20 to-success/10 border-success/30 hover:border-success/50 text-success',
        purple: 'from-purple-500/20 to-purple-600/10 border-purple-500/30 hover:border-purple-400/50 text-purple-400',
        orange: 'from-orange-500/20 to-orange-600/10 border-orange-500/30 hover:border-orange-400/50 text-orange-400',
        cyan: 'from-cyan-500/20 to-cyan-600/10 border-cyan-500/30 hover:border-cyan-400/50 text-cyan-400',
        pink: 'from-pink-500/20 to-pink-600/10 border-pink-500/30 hover:border-pink-400/50 text-pink-400',
    };

    return (
        <button
            onClick={onClick}
            className={`group relative p-4 rounded-xl border bg-gradient-to-br ${colorClasses[color]} transition-all duration-300 hover:scale-[1.02] hover:shadow-lg text-left`}
        >
            <div className="flex items-start gap-3">
                <div className={`p-2.5 rounded-lg bg-gradient-to-br ${colorClasses[color].split(' ')[0]} ${colorClasses[color].split(' ')[1]}`}>
                    <Icon className="w-5 h-5" />
                </div>
                <div className="flex-1 min-w-0">
                    <h4 className="font-semibold text-fg text-sm">{label}</h4>
                    <p className="text-xs text-fg-muted mt-0.5 line-clamp-1">{description}</p>
                </div>
            </div>
        </button>
    );
}

// Status Card Component
function StatusCard({
    icon: Icon,
    label,
    value,
    status,
    subtext
}: {
    icon: any;
    label: string;
    value: string;
    status: 'success' | 'warning' | 'error' | 'info';
    subtext?: string;
}) {
    const statusColors = {
        success: 'text-success bg-success/10 border-success/30',
        warning: 'text-warning bg-warning/10 border-warning/30',
        error: 'text-danger bg-danger/10 border-danger/30',
        info: 'text-primary bg-primary/10 border-primary/30',
    };

    return (
        <div className={`p-4 rounded-xl border ${statusColors[status]} backdrop-blur-sm`}>
            <div className="flex items-center gap-3">
                <Icon className="w-5 h-5" />
                <div className="flex-1">
                    <p className="text-xs text-fg-muted">{label}</p>
                    <p className="font-semibold text-fg">{value}</p>
                    {subtext && <p className="text-xs text-fg-subtle">{subtext}</p>}
                </div>
            </div>
        </div>
    );
}

export function DomainDetail({ domainId, onBack }: DomainDetailProps) {
    const [domain, setDomain] = useState<Domain | null>(null);
    const [loading, setLoading] = useState(true);
    const [activeSection, setActiveSection] = useState<string | null>(null);

    useEffect(() => {
        loadDomain();
    }, [domainId]);

    const loadDomain = async () => {
        try {
            const res = await fetch('/api/v1/domains');
            if (!res.ok) throw new Error('Failed to load domains');
            const domains = await res.json();
            const found = domains.find((d: Domain) => d.id === domainId);
            if (found) {
                setDomain(found);
            } else {
                onBack();
            }
        } catch (err) {
            console.error(err);
            onBack();
        } finally {
            setLoading(false);
        }
    };

    const formatBytes = (bytes: number = 0) => {
        if (bytes === 0) return '0 MB';
        const mb = bytes / (1024 * 1024);
        if (mb < 1024) return `${mb.toFixed(1)} MB`;
        return `${(mb / 1024).toFixed(2)} GB`;
    };

    if (loading) {
        return (
            <div className="h-full flex items-center justify-center">
                <div className="flex flex-col items-center gap-3">
                    <div className="relative">
                        <div className="w-12 h-12 border-4 border-primary/30 rounded-full"></div>
                        <div className="w-12 h-12 border-4 border-primary border-t-transparent rounded-full animate-spin absolute top-0"></div>
                    </div>
                    <p className="text-fg-muted text-sm">Loading domain...</p>
                </div>
            </div>
        );
    }

    if (!domain) {
        return (
            <div className="h-full flex items-center justify-center">
                <div className="text-center">
                    <Globe className="w-16 h-16 text-fg-subtle mx-auto mb-4" />
                    <p className="text-fg-muted">Domain not found</p>
                    <button onClick={onBack} className="mt-4 px-4 py-2 bg-primary rounded-lg text-white">
                        Go Back
                    </button>
                </div>
            </div>
        );
    }

    // If a section is active, show its detailed view
    if (activeSection) {
        return (
            <div className="h-full overflow-auto p-6">
                {/* Section Header */}
                <div className="flex items-center gap-4 mb-6">
                    <button
                        onClick={() => setActiveSection(null)}
                        className="p-2 hover:bg-surface-2 rounded-lg transition-colors"
                    >
                        <ArrowLeft className="w-5 h-5 text-fg-muted" />
                    </button>
                    <div>
                        <h2 className="text-xl font-bold text-fg">{domain.domain_name}</h2>
                        <p className="text-sm text-fg-muted capitalize">{activeSection.replace('-', ' ')}</p>
                    </div>
                </div>

                {/* Section Content */}
                <div className="bg-surface/50 backdrop-blur-sm border border-border rounded-2xl p-6">
                    {activeSection === 'general' && (
                        <DomainGeneralSettings domainId={domain.id} domainName={domain.domain_name} />
                    )}
                    {activeSection === 'php' && (
                        <DomainPHPSettings
                            domainId={domain.id}
                            domainName={domain.domain_name}
                            currentVersion={domain.php_version}
                            onVersionChange={(v) => setDomain({ ...domain, php_version: v })}
                        />
                    )}
                    {activeSection === 'ssl' && (
                        <DomainSSLSettings domainId={domain.id} domainName={domain.domain_name} />
                    )}
                    {activeSection === 'database' && (
                        <DomainDatabaseManager domainId={domain.id} domainName={domain.domain_name} />
                    )}
                    {activeSection === 'logs' && (
                        <DomainLogsViewer domainId={domain.id} domainName={domain.domain_name} />
                    )}
                    {activeSection === 'files' && (
                        <DomainFileManager domainId={domain.id} domainName={domain.domain_name} />
                    )}
                    {activeSection === 'backup' && (
                        <DomainBackupManager domainId={domain.id} domainName={domain.domain_name} />
                    )}
                    {activeSection === 'cron' && (
                        <DomainCronManager domainId={domain.id} domainName={domain.domain_name} />
                    )}
                    {activeSection === 'mail' && (
                        <DomainMailManager domainId={domain.id} domainName={domain.domain_name} />
                    )}
                    {activeSection === 'dns' && (
                        <DomainDNSManager domainId={domain.id} domainName={domain.domain_name} />
                    )}
                </div>
            </div>
        );
    }

    // Main Dashboard View
    return (
        <div className="h-full overflow-auto relative bg-bg">
            {/* Background Pattern - relative to content area only */}
            <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_top,_var(--tw-gradient-stops))] from-primary/20 via-surface to-surface pointer-events-none"></div>

            <div className="relative p-6 space-y-6">
                {/* Header */}
                <div className="flex items-center justify-between">
                    <div className="flex items-center gap-4">
                        <button
                            onClick={onBack}
                            className="p-2.5 hover:bg-surface-2/80 rounded-xl transition-all duration-200 border border-transparent hover:border-border"
                        >
                            <ArrowLeft className="w-5 h-5 text-fg-muted" />
                        </button>
                        <div>
                            <div className="flex items-center gap-3">
                                <h1 className="text-2xl font-bold text-fg">{domain.domain_name}</h1>
                                <span className={`px-2.5 py-0.5 rounded-full text-xs font-medium flex items-center gap-1.5 ${domain.status === 'active'
                                    ? 'bg-success/20 text-success border border-success/30'
                                    : 'bg-surface-2 text-fg-muted'
                                    }`}>
                                    <span className={`w-1.5 h-1.5 rounded-full ${domain.status === 'active' ? 'bg-success' : 'bg-surface-3'}`}></span>
                                    {domain.status}
                                </span>
                            </div>
                            <p className="text-sm text-fg-muted mt-1">Domain Dashboard</p>
                        </div>
                    </div>
                    <div className="flex items-center gap-2">
                        <a
                            href={`https://${domain.domain_name}`}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="flex items-center gap-2 px-4 py-2 bg-surface-2/80 hover:bg-surface-3 rounded-xl transition-colors border border-border text-sm text-fg-muted"
                        >
                            <ExternalLink className="w-4 h-4" />
                            Visit Site
                        </a>
                        <button
                            onClick={loadDomain}
                            className="p-2.5 hover:bg-surface-2/80 rounded-xl transition-colors border border-border"
                            title="Refresh"
                        >
                            <RefreshCw className="w-4 h-4 text-fg-muted" />
                        </button>
                    </div>
                </div>

                {/* Status Cards */}
                <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                    <StatusCard
                        icon={Shield}
                        label="SSL Certificate"
                        value={domain.ssl_enabled ? 'Active' : 'Not Installed'}
                        status={domain.ssl_enabled ? 'success' : 'warning'}
                    />
                    <StatusCard
                        icon={Code2}
                        label="PHP Version"
                        value={`PHP ${domain.php_version}`}
                        status="info"
                    />
                    <StatusCard
                        icon={HardDrive}
                        label="Disk Usage"
                        value={formatBytes(domain.disk_usage)}
                        status="info"
                    />
                    <StatusCard
                        icon={Activity}
                        label="Monthly Traffic"
                        value={formatBytes(domain.bandwidth)}
                        status="info"
                    />
                </div>

                {/* Quick Actions Grid */}
                <div>
                    <h3 className="text-sm font-semibold text-fg-muted uppercase tracking-wider mb-4">Quick Actions</h3>
                    <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-3">
                        <QuickAction
                            icon={Settings}
                            label="General Settings"
                            description="Web server & domain config"
                            color="blue"
                            onClick={() => setActiveSection('general')}
                        />
                        <QuickAction
                            icon={Code2}
                            label="PHP Configuration"
                            description="Version & FPM pool settings"
                            color="purple"
                            onClick={() => setActiveSection('php')}
                        />
                        <QuickAction
                            icon={Lock}
                            label="SSL/TLS Certificates"
                            description="HTTPS & security settings"
                            color="green"
                            onClick={() => setActiveSection('ssl')}
                        />
                        <QuickAction
                            icon={Database}
                            label="Databases"
                            description="MySQL & PostgreSQL"
                            color="orange"
                            onClick={() => setActiveSection('database')}
                        />
                        <QuickAction
                            icon={Folder}
                            label="File Manager"
                            description="Browse & edit files"
                            color="cyan"
                            onClick={() => setActiveSection('files')}
                        />
                        <QuickAction
                            icon={Terminal}
                            label="Access Logs"
                            description="View error & access logs"
                            color="pink"
                            onClick={() => setActiveSection('logs')}
                        />
                        <QuickAction
                            icon={RefreshCw}
                            label="Backup & Restore"
                            description="Create & restore backups"
                            color="orange"
                            onClick={() => setActiveSection('backup')}
                        />
                        <QuickAction
                            icon={Activity}
                            label="Cron Jobs"
                            description="Scheduled tasks"
                            color="green"
                            onClick={() => setActiveSection('cron')}
                        />
                        <QuickAction
                            icon={Mail}
                            label="Email Settings"
                            description="Mail accounts & forwarding"
                            color="blue"
                            onClick={() => setActiveSection('mail')}
                        />
                        <QuickAction
                            icon={Globe}
                            label="DNS Manager"
                            description="Records & Zones"
                            color="cyan"
                            onClick={() => setActiveSection('dns')}
                        />
                    </div>
                </div>

                {/* Site Preview & Info */}
                <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
                    {/* Site Preview */}
                    <div className="lg:col-span-2 bg-surface/50 backdrop-blur-sm border border-border rounded-2xl p-6">
                        <h3 className="text-sm font-semibold text-fg-muted uppercase tracking-wider mb-4">Site Preview</h3>
                        <div className="relative aspect-video bg-surface-2 rounded-xl overflow-hidden border border-border">
                            <iframe
                                src={`https://${domain.domain_name}`}
                                className="w-full h-full"
                                style={{ transform: 'scale(0.5)', transformOrigin: 'top left', width: '200%', height: '200%' }}
                                sandbox="allow-same-origin"
                            />
                            <div className="absolute inset-0 bg-gradient-to-t from-surface/80 to-transparent pointer-events-none"></div>
                            <a
                                href={`https://${domain.domain_name}`}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="absolute bottom-4 left-4 flex items-center gap-2 px-3 py-1.5 bg-primary hover:bg-primary-hover rounded-lg text-sm text-white transition-colors"
                            >
                                <ExternalLink className="w-3.5 h-3.5" />
                                Open in New Tab
                            </a>
                        </div>
                    </div>

                    {/* Domain Info */}
                    <div className="bg-surface/50 backdrop-blur-sm border border-border rounded-2xl p-6">
                        <h3 className="text-sm font-semibold text-fg-muted uppercase tracking-wider mb-4">Domain Info</h3>
                        <div className="space-y-4">
                            <div>
                                <p className="text-xs text-fg-subtle mb-1">Domain Name</p>
                                <p className="text-fg font-mono text-sm">{domain.domain_name}</p>
                            </div>
                            <div>
                                <p className="text-xs text-fg-subtle mb-1">Created</p>
                                <p className="text-fg text-sm">
                                    {new Date(domain.created_at).toLocaleDateString('en-US', {
                                        year: 'numeric',
                                        month: 'long',
                                        day: 'numeric'
                                    })}
                                </p>
                            </div>
                            <div>
                                <p className="text-xs text-fg-subtle mb-1">PHP Version</p>
                                <p className="text-fg text-sm">PHP {domain.php_version}</p>
                            </div>
                            <div>
                                <p className="text-xs text-fg-subtle mb-1">SSL Status</p>
                                <div className="flex items-center gap-2">
                                    {domain.ssl_enabled ? (
                                        <>
                                            <span className="w-2 h-2 rounded-full bg-success"></span>
                                            <span className="text-success text-sm">Secure (HTTPS)</span>
                                        </>
                                    ) : (
                                        <>
                                            <span className="w-2 h-2 rounded-full bg-warning"></span>
                                            <span className="text-warning text-sm">Not Installed</span>
                                        </>
                                    )}
                                </div>
                            </div>
                            <div className="pt-4 border-t border-border">
                                <button
                                    onClick={() => setActiveSection('general')}
                                    className="w-full px-4 py-2.5 bg-primary hover:bg-primary-hover rounded-xl text-white text-sm font-medium transition-colors flex items-center justify-center gap-2"
                                >
                                    <Settings className="w-4 h-4" />
                                    Manage Domain
                                </button>
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    );
}
