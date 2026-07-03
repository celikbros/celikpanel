import { LayoutDashboard, Globe, Server, Settings, LogOut } from 'lucide-react';
import { api } from '../lib/api';

export type Page = 'dashboard' | 'domains' | 'databases' | 'databases-v2' | 'services' | 'settings';

interface LayoutProps {
    children: React.ReactNode;
    currentPage: Page;
    onPageChange: (page: Page) => void;
}

export function Layout({ children, currentPage, onPageChange }: LayoutProps) {
    const menuItems = [
        { id: 'dashboard', label: 'Dashboard', icon: LayoutDashboard },
        { id: 'domains', label: 'Domains', icon: Globe },
        { id: 'databases-v2', label: 'Databases', icon: Server },
        { id: 'services', label: 'Services', icon: Server },
        { id: 'settings', label: 'Settings', icon: Settings },
    ];

    return (
        <div className="flex h-screen bg-gray-950 text-gray-100 font-sans">
            {/* Sidebar */}
            <div className="w-64 bg-gray-900 border-r border-gray-800 flex flex-col">
                <div className="p-6 flex items-center gap-3 border-b border-gray-800">
                    <Server className="w-8 h-8 text-blue-500" />
                    <div>
                        <h1 className="text-xl font-bold">CelikPanel</h1>
                        <p className="text-xs text-gray-500">Server Management</p>
                    </div>
                </div>

                <nav className="flex-1 p-4 space-y-1">
                    {menuItems.map((item) => {
                        const Icon = item.icon;
                        const active = currentPage === item.id;
                        const onClick = () => onPageChange(item.id as Page);

                        return (
                            <button
                                key={item.id}
                                onClick={onClick}
                                className={`w-full flex items-center gap-3 px-4 py-3 rounded-lg transition-colors ${active
                                    ? 'bg-blue-600 text-white'
                                    : 'text-gray-400 hover:bg-gray-800 hover:text-gray-200'
                                    }`}
                            >
                                <Icon className="w-5 h-5" />
                                <span className="font-medium">{item.label}</span>
                            </button>
                        );
                    })}
                </nav>

                <div className="p-4 border-t border-gray-800">
                    <button
                        onClick={async () => { await api.logout(); window.location.reload(); }}
                        className="w-full flex items-center gap-3 px-4 py-2 rounded-lg text-gray-400 hover:bg-gray-800 hover:text-gray-200 transition-colors mb-3"
                    >
                        <LogOut className="w-5 h-5" />
                        <span className="font-medium">Çıkış / Logout</span>
                    </button>
                    <div className="text-xs text-gray-500">
                        <p>Server: localhost</p>
                        <p className="mt-1">Version: 1.0.0</p>
                    </div>
                </div>
            </div>

            {/* Main Content */}
            <div className="flex-1 overflow-auto">
                {children}
            </div>
        </div>
    );
}
