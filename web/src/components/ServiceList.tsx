import React, { useEffect, useState } from 'react';
import { Settings } from 'lucide-react';

interface ManagedService {
    id: string;
    name: string;
    description: string;
    icon: string;
    category: string;
    versions: string[];
    status: string;
    is_installed: boolean;
}

interface ServiceListProps {
    onSelectConfig: (path: string) => void;
    onManageService?: (serviceId: string, versions: string[]) => void;
}

export function ServiceList({ onManageService }: ServiceListProps) {
    const [services, setServices] = useState<ManagedService[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        loadServices();
    }, []);

    const loadServices = () => {
        setLoading(true);
        fetch('/api/v1/managed-services')
            .then(res => res.json())
            .then(setServices)
            .catch(err => setError(err.message))
            .finally(() => setLoading(false));
    };

    const handleAction = async (serviceId: string, version: string, action: 'start' | 'stop' | 'restart') => {
        try {
            let serviceName: string;
            if (serviceId === 'php-fpm') {
                serviceName = version !== 'default' ? `php${version}-fpm` : 'php-fpm';
            } else if (serviceId === 'postgresql' || serviceId === 'mariadb') {
                serviceName = serviceId === 'mariadb' ? 'mariadb' : 'postgresql';
            } else {
                serviceName = serviceId;
            }

            const response = await fetch('/api/v1/service/action', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: serviceName, action }),
            });

            if (!response.ok) {
                throw new Error('Service action failed');
            }

            await new Promise(resolve => setTimeout(resolve, 1000));

            window.dispatchEvent(new CustomEvent('service-status-changed', {
                detail: { serviceName, serviceId, version }
            }));
        } catch (err: any) {
            alert(`Error: ${err.message}`);
        }
    };

    if (loading) return (
        <div className="flex flex-col items-center justify-center p-12 text-fg-muted">
            <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary mb-4"></div>
            <p>Loading services...</p>
        </div>
    );
    if (error) return <div className="text-danger">Error: {error}</div>;

    // Unified Service Card Component
    const ServiceCard = ({ service }: { service: ManagedService }) => {
        const categoryColors: Record<string, string> = {
            web: 'bg-primary/40 text-primary border-primary/50',
            database: 'bg-purple-900/40 text-purple-400 border-purple-900/50',
            email: 'bg-orange-900/40 text-orange-400 border-orange-900/50',
            security: 'bg-danger/40 text-danger border-danger/50',
            dns: 'bg-cyan-900/40 text-cyan-400 border-cyan-900/50',
            ftp: 'bg-primary/40 text-primary border-primary/50',
            cache: 'bg-warning/40 text-warning border-warning/50',
        };
        const badgeClass = categoryColors[service.category] || 'bg-surface-2 text-fg-muted';

        return (
            <div className="bg-surface border border-border rounded-xl p-6 hover:border-border transition-colors group flex flex-col h-full">
                <div className="flex justify-between items-start mb-4">
                    <div className="flex items-center gap-3">
                        <div className="text-3xl">{service.icon}</div>
                        <div>
                            <h3 className="text-lg font-bold text-fg">{service.name}</h3>
                            <p className="text-sm text-fg-subtle line-clamp-2">{service.description}</p>
                        </div>
                    </div>
                    <span className={`text-[10px] uppercase font-bold px-2 py-0.5 rounded border ${badgeClass}`}>
                        {service.category}
                    </span>
                </div>

                <div className="flex-1">
                    {service.versions.length > 1 && (
                        <div className="mb-3 p-3 bg-surface-2/50 rounded-lg border border-border">
                            <p className="text-xs text-fg-muted mb-2 font-semibold">Installed Versions:</p>
                            <div className="space-y-2">
                                {service.versions.map(version => (
                                    <ServiceVersionRow key={version} service={service} version={version} handleAction={handleAction} />
                                ))}
                            </div>
                        </div>
                    )}

                    {service.versions.length === 1 && (
                        <div className="mb-4">
                            <SingleVersionStatus serviceId={service.id} version={service.versions[0]} />
                        </div>
                    )}
                </div>

                <div className="flex gap-2 items-center mt-auto pt-4 border-t border-border">
                    {service.versions.length === 1 && (
                        <div className="flex gap-1">
                            <button
                                onClick={() => handleAction(service.id, service.versions[0], 'start')}
                                className="px-3 py-1.5 bg-success/30 text-success rounded hover:bg-success/50 transition-colors text-xs"
                            >
                                Start
                            </button>
                            <button
                                onClick={() => handleAction(service.id, service.versions[0], 'stop')}
                                className="px-3 py-1.5 bg-danger/30 text-danger rounded hover:bg-danger/50 transition-colors text-xs"
                            >
                                Stop
                            </button>
                            <button
                                onClick={() => handleAction(service.id, service.versions[0], 'restart')}
                                className="px-3 py-1.5 bg-warning/30 text-warning rounded hover:bg-warning/50 transition-colors text-xs"
                            >
                                Restart
                            </button>
                        </div>
                    )}
                    <button
                        onClick={() => onManageService?.(service.id, service.versions)}
                        className="ml-auto px-4 py-1.5 bg-primary text-white rounded hover:bg-primary-hover transition-colors flex items-center gap-2 text-sm"
                    >
                        <Settings className="w-4 h-4" />
                        Manage
                    </button>
                </div>
            </div>
        );
    };

    const ServiceVersionRow = ({ service, version, handleAction }: any) => {
        const [versionStatus, setVersionStatus] = React.useState<{ active: boolean } | null>(null);

        React.useEffect(() => {
            let serviceName: string;
            if (service.id === 'php-fpm') {
                serviceName = version !== 'default' ? `php${version}-fpm` : 'php-fpm';
            } else if (service.id === 'postgresql' || service.id === 'mariadb') {
                serviceName = service.id === 'mariadb' ? 'mariadb' : 'postgresql';
            } else {
                serviceName = service.id;
            }

            fetch(`/api/v1/service/status?name=${serviceName}`)
                .then(r => r.ok ? r.json() : null)
                .then(data => setVersionStatus(data))
                .catch(() => setVersionStatus(null));

            const handleStatusChange = (event: Event) => {
                const customEvent = event as CustomEvent;
                const { serviceId: changedServiceId, version: changedVersion } = customEvent.detail;
                if (changedServiceId === service.id && changedVersion === version) {
                    fetch(`/api/v1/service/status?name=${serviceName}`)
                        .then(r => r.ok ? r.json() : null)
                        .then(data => setVersionStatus(data));
                }
            };
            window.addEventListener('service-status-changed', handleStatusChange);
            return () => window.removeEventListener('service-status-changed', handleStatusChange);

        }, [service.id, version]);


        return (
            <div className="flex items-center justify-between p-2 bg-surface/50 rounded">
                <div className="flex items-center gap-2">
                    <span className="text-sm text-primary font-mono font-semibold">{version}</span>
                    {versionStatus && (
                        <span className={`text-xs px-2 py-0.5 rounded ${versionStatus.active ? 'bg-success/30 text-success' : 'bg-surface-3 text-fg-muted'}`}>
                            {versionStatus.active ? 'Running' : 'Stopped'}
                        </span>
                    )}
                </div>
                <div className="flex gap-1">
                    <button onClick={() => handleAction(service.id, version, 'start')} className="px-2 py-1 bg-success/30 text-success rounded hover:bg-success/50 transition-colors text-xs">Start</button>
                    <button onClick={() => handleAction(service.id, version, 'stop')} className="px-2 py-1 bg-danger/30 text-danger rounded hover:bg-danger/50 transition-colors text-xs">Stop</button>
                    <button onClick={() => handleAction(service.id, version, 'restart')} className="px-2 py-1 bg-warning/30 text-warning rounded hover:bg-warning/50 transition-colors text-xs">Restart</button>
                </div>
            </div>
        )
    }

    const SingleVersionStatus = ({ serviceId, version }: { serviceId: string; version: string }) => {
        const [versionStatus, setVersionStatus] = React.useState<{ active: boolean } | null>(null);
        const [refreshTrigger, setRefreshTrigger] = React.useState(0);

        React.useEffect(() => {
            let serviceName: string;
            if (serviceId === 'php-fpm') {
                serviceName = version !== 'default' ? `php${version}-fpm` : 'php-fpm';
            } else if (serviceId === 'postgresql' || serviceId === 'mariadb') {
                serviceName = serviceId === 'mariadb' ? 'mariadb' : 'postgresql';
            } else {
                serviceName = serviceId;
            }

            fetch(`/api/v1/service/status?name=${serviceName}`)
                .then(r => r.ok ? r.json() : null)
                .then(data => setVersionStatus(data))
                .catch(() => setVersionStatus(null));
        }, [serviceId, version, refreshTrigger]);

        React.useEffect(() => {
            const handleStatusChange = (event: Event) => {
                const customEvent = event as CustomEvent;
                const { serviceId: changedServiceId, version: changedVersion } = customEvent.detail;
                if (changedServiceId === serviceId && changedVersion === version) {
                    setRefreshTrigger(prev => prev + 1);
                }
            };
            window.addEventListener('service-status-changed', handleStatusChange);
            return () => window.removeEventListener('service-status-changed', handleStatusChange);
        }, [serviceId, version]);

        if (!versionStatus) return null;

        return (
            <div className="flex items-center gap-2 p-2 bg-surface-2/30 rounded">
                <span className="text-xs text-fg-muted">Status:</span>
                <span className={`text-xs px-2 py-0.5 rounded ${versionStatus.active ? 'bg-success/30 text-success' : 'bg-surface-3 text-fg-muted'}`}>
                    {versionStatus.active ? 'Running' : 'Stopped'}
                </span>
            </div>
        );
    };

    return (
        <div className="p-6 space-y-6">
            <div>
                <h1 className="text-2xl font-bold text-fg">Services</h1>
                <p className="text-sm text-fg-muted mt-1">
                    Manage core system services
                </p>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-6 auto-rows-fr">
                {services.map(service => (
                    <ServiceCard key={service.id} service={service} />
                ))}
            </div>
        </div>
    );
}
