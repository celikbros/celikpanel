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
        <div className="min-h-screen bg-gray-950 text-gray-100 font-sans">
            {/* Header */}
            <div className="bg-gray-900 border-b border-gray-800 p-4">
                <div className="max-w-7xl mx-auto flex items-center justify-between">
                    <div className="flex items-center gap-4">
                        <button
                            onClick={onBack}
                            className="p-2 hover:bg-gray-800 rounded-lg transition-colors text-gray-400 hover:text-white"
                        >
                            <ArrowLeft className="w-5 h-5" />
                        </button>
                        <div className="flex items-center gap-3">
                            <span className="text-2xl">{serviceIcon}</span>
                            <div>
                                <h1 className="text-xl font-bold">{serviceName} Management</h1>
                                <p className="text-xs text-gray-500">Core Service</p>
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
                        <div className="bg-gray-900 rounded-xl border border-gray-800 overflow-hidden">
                            <div className="p-4 border-b border-gray-800 bg-gray-800/30">
                                <p className="text-xs font-bold text-gray-500 uppercase tracking-wider">
                                    {serviceName} {activeVersion}
                                </p>
                            </div>
                            <nav className="p-2 space-y-1">
                                {tabs?.map(tab => (
                                    <button
                                        key={tab.id}
                                        onClick={() => onTabChange?.(tab.id)}
                                        className={`w-full flex items-center gap-3 px-4 py-3 text-sm font-medium rounded-lg transition-colors ${activeTab === tab.id
                                            ? 'bg-blue-600 text-white'
                                            : 'text-gray-400 hover:bg-gray-800 hover:text-white'
                                            }`}
                                    >
                                        {tab.icon || <Activity className="w-4 h-4" />}
                                        {tab.label}
                                    </button>
                                ))}
                            </nav>
                        </div>

                        {/* Context Info Box */}
                        <div className="mt-6 bg-blue-900/10 border border-blue-900/30 rounded-xl p-4">
                            <div className="flex items-start gap-3">
                                <div className="mt-1">
                                    <Activity className="w-4 h-4 text-blue-400" />
                                </div>
                                <div>
                                    <h4 className="text-sm font-bold text-blue-400">Context Aware</h4>
                                    <p className="text-xs text-blue-300/70 mt-1">
                                        All actions performed here will only affect
                                        <span className="font-bold text-white mx-1">{serviceName} {activeVersion}</span>.
                                        Other versions are safe.
                                    </p>
                                </div>
                            </div>
                        </div>
                    </div>
                )}

                {/* Main Content Area */}
                <div className="flex-1">
                    <div className={hideSidebar ? "" : "bg-gray-900 rounded-xl border border-gray-800 min-h-[600px] p-6"}>
                        {/* Breadcrumb / Header for Content - Optional if sidebar hidden but good for context */}
                        {!hideSidebar && (
                            <div className="mb-6 pb-4 border-b border-gray-800 flex justify-between items-center">
                                <div>
                                    <h2 className="text-xl font-bold text-white">
                                        {tabs?.find(t => t.id === activeTab)?.label}
                                    </h2>
                                    <p className="text-sm text-gray-500 mt-1">
                                        Configuring {serviceName} <span className="text-blue-400 font-mono font-bold">{activeVersion}</span>
                                    </p>
                                </div>

                                {/* VERSION SELECTOR */}
                                {versions.length > 1 && (
                                    <div className="flex items-center gap-2 bg-gray-800 px-3 py-2 rounded-lg border border-gray-700">
                                        <span className="text-xs text-gray-400 font-medium">Version:</span>
                                        <select
                                            value={activeVersion}
                                            onChange={(e) => onVersionChange(e.target.value)}
                                            className="bg-gray-900 text-white border border-blue-500/50 rounded px-2 py-1 text-sm font-bold focus:outline-none focus:ring-2 focus:ring-blue-500"
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
