import { ReactNode } from 'react';
import { ArrowLeft, Activity } from 'lucide-react';

interface ServiceManagementLayoutProps {
    serviceName: string;
    serviceIcon: string;
    versions: string[];
    activeVersion: string;
    onVersionChange: (version: string) => void;
    onBack: () => void;
    children: ReactNode;
    activeTab?: string;
    onTabChange?: (tab: string) => void;
    tabs?: { id: string; label: string; icon?: ReactNode }[];
}

export function ServiceManagementLayout({
    serviceName,
    serviceIcon,
    versions,
    activeVersion,
    onVersionChange,
    onBack,
    children,
    activeTab,
    onTabChange,
    tabs,
    hideSidebar = false
}: ServiceManagementLayoutProps & { hideSidebar?: boolean }) {
    return (
        <div className="min-h-screen bg-bg text-fg font-sans">
            {/* Header */}
            <div className="bg-surface border-b border-border p-4">
                <div className="max-w-7xl mx-auto flex items-center justify-between">
                    <div className="flex items-center gap-4">
                        <button
                            onClick={onBack}
                            className="p-2 hover:bg-surface-2 rounded-lg transition-colors text-fg-muted hover:text-fg"
                        >
                            <ArrowLeft className="w-5 h-5" />
                        </button>
                        <div className="flex items-center gap-3">
                            <span className="text-2xl">{serviceIcon}</span>
                            <div>
                                <h1 className="text-xl font-bold">{serviceName} Management</h1>
                                <p className="text-xs text-fg-subtle">Core Service</p>
                            </div>
                        </div>
                    </div>
                </div>
            </div>

            {/* Main Container - Full width if no sidebar, otherwise max-w-7xl */}
            <div className={`${hideSidebar ? 'w-full px-8' : 'max-w-7xl mx-auto p-6'} flex gap-8 transition-all duration-300`}>
                {/* Sidebar Navigation - Conditionally Rendered */}
                {!hideSidebar && (
                    <div className="w-64 flex-shrink-0">
                        <div className="bg-surface rounded-xl border border-border overflow-hidden">
                            <div className="p-4 border-b border-border bg-surface-2/30">
                                <p className="text-xs font-bold text-fg-subtle uppercase tracking-wider">
                                    {serviceName} {activeVersion}
                                </p>
                            </div>
                            <nav className="p-2 space-y-1">
                                {tabs?.map(tab => (
                                    <button
                                        key={tab.id}
                                        onClick={() => onTabChange?.(tab.id)}
                                        className={`w-full flex items-center gap-3 px-4 py-3 text-sm font-medium rounded-lg transition-colors ${activeTab === tab.id
                                            ? 'bg-primary text-white'
                                            : 'text-fg-muted hover:bg-surface-2 hover:text-fg'
                                            }`}
                                    >
                                        {tab.icon || <Activity className="w-4 h-4" />}
                                        {tab.label}
                                    </button>
                                ))}
                            </nav>
                        </div>

                        {/* Context Info Box */}
                        <div className="mt-6 bg-primary/15/10 border border-primary/30/30 rounded-xl p-4">
                            <div className="flex items-start gap-3">
                                <div className="mt-1">
                                    <Activity className="w-4 h-4 text-primary" />
                                </div>
                                <div>
                                    <h4 className="text-sm font-bold text-primary">Context Aware</h4>
                                    <p className="text-xs text-primary/70 mt-1">
                                        All actions performed here will only affect
                                        <span className="font-bold text-fg mx-1">{serviceName} {activeVersion}</span>.
                                        Other versions are safe.
                                    </p>
                                </div>
                            </div>
                        </div>
                    </div>
                )}

                {/* Main Content Area */}
                <div className="flex-1">
                    <div className={hideSidebar ? "" : "bg-surface rounded-xl border border-border min-h-[600px] p-6"}>
                        {/* Breadcrumb / Header for Content - Optional if sidebar hidden but good for context */}
                        {!hideSidebar && (
                            <div className="mb-6 pb-4 border-b border-border flex justify-between items-center">
                                <div>
                                    <h2 className="text-xl font-bold text-fg">
                                        {tabs?.find(t => t.id === activeTab)?.label}
                                    </h2>
                                    <p className="text-sm text-fg-subtle mt-1">
                                        Configuring {serviceName} <span className="text-primary font-mono font-bold">{activeVersion}</span>
                                    </p>
                                </div>

                                {/* VERSION SELECTOR */}
                                {versions.length > 1 && (
                                    <div className="flex items-center gap-2 bg-surface-2 px-3 py-2 rounded-lg border border-border">
                                        <span className="text-xs text-fg-muted font-medium">Version:</span>
                                        <select
                                            value={activeVersion}
                                            onChange={(e) => onVersionChange(e.target.value)}
                                            className="bg-surface text-fg border border-primary/50 rounded px-2 py-1 text-sm font-bold focus:outline-none focus:ring-2 focus:ring-primary"
                                        >
                                            {versions.map(v => (
                                                <option key={v} value={v}>{v}</option>
                                            ))}
                                        </select>
                                    </div>
                                )}
                            </div>
                        )}

                        {children}
                    </div>
                </div>
            </div>
        </div>
    );
}
