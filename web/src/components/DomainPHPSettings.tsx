import { useState, useEffect } from 'react';
import { Save } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { FormSection, Field, FormActions, Button, inputClass } from './ui';

interface DomainPHPSettingsProps {
    domainId: number;
    domainName: string;
    currentVersion: string;
    onVersionChange: (version: string) => void;
}

interface PHPSettings {
    domain_id: number;
    domain_name: string;
    php_version: string;
    pool_name: string;
    pool_config?: any;
}

export function DomainPHPSettings({ domainId, currentVersion, onVersionChange }: DomainPHPSettingsProps) {
    const { t } = useI18n();
    const [settings, setSettings] = useState<PHPSettings | null>(null);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [selectedVersion, setSelectedVersion] = useState(currentVersion);
    const [versions, setVersions] = useState<string[]>(['8.3']);

    useEffect(() => {
        loadSettings();
        loadVersions();
    }, [domainId]);

    const loadVersions = async () => {
        try {
            const res = await fetch('/api/v1/managed-services');
            if (!res.ok) return;
            const services = await res.json();
            const php = services.find((s: any) => s.id === 'php-fpm');
            if (php?.versions?.length) setVersions(php.versions);
        } catch {
            /* keep default */
        }
    };

    const loadSettings = async () => {
        setLoading(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/php`);
            if (!res.ok) throw new Error();
            const data = await res.json();
            setSettings(data);
            setSelectedVersion(data.php_version);
        } catch {
            showToast('error', t('php.loadFailed'));
        } finally {
            setLoading(false);
        }
    };

    const handleVersionChange = async () => {
        if (selectedVersion === currentVersion) return;
        if (!confirm(t('php.changeConfirm', { from: currentVersion, to: selectedVersion }))) return;
        setSaving(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/php`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ php_version: selectedVersion }),
            });
            if (!res.ok) throw new Error();
            showToast('success', t('php.changed', { version: selectedVersion }));
            onVersionChange(selectedVersion);
            await loadSettings();
        } catch {
            showToast('error', t('php.changeFailed'));
            setSelectedVersion(currentVersion);
        } finally {
            setSaving(false);
        }
    };

    const handleSavePool = async (e: React.FormEvent<HTMLFormElement>) => {
        e.preventDefault();
        const fd = new FormData(e.currentTarget);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/php/pool`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    version: currentVersion,
                    pool_config: {
                        pm: fd.get('pm') as string,
                        pm_max_children: parseInt(fd.get('pm_max_children') as string),
                        pm_start_servers: parseInt(fd.get('pm_start_servers') as string),
                        pm_min_spare_servers: parseInt(fd.get('pm_min_spare_servers') as string),
                        pm_max_spare_servers: parseInt(fd.get('pm_max_spare_servers') as string),
                        user: fd.get('user') as string,
                        group: fd.get('group') as string,
                    },
                }),
            });
            if (!res.ok) throw new Error();
            showToast('success', t('php.poolSaved'));
            loadSettings();
        } catch {
            showToast('error', t('php.poolFailed'));
        }
    };

    if (loading) {
        return (
            <div className="flex items-center justify-center py-16">
                <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
            </div>
        );
    }
    if (!settings) return <p className="text-danger">{t('php.loadFailed')}</p>;

    const pc = settings.pool_config;
    const pending = selectedVersion !== currentVersion;

    return (
        <div>
            <FormSection title={t('php.version')} description={pending ? t('php.reloadWarning') : undefined}>
                <div className="flex gap-2">
                    <select
                        value={selectedVersion}
                        onChange={(e) => setSelectedVersion(e.target.value)}
                        className={inputClass}
                    >
                        {versions.map((v) => (
                            <option key={v} value={v}>
                                PHP {v}
                            </option>
                        ))}
                    </select>
                    <Button
                        variant="primary"
                        icon={Save}
                        onClick={handleVersionChange}
                        disabled={saving || !pending}
                    >
                        {saving ? t('php.applying') : t('php.apply')}
                    </Button>
                </div>
            </FormSection>

            <FormSection title={t('php.pool')} description={`${t('php.poolName')}: ${settings.pool_name}`}>
                {pending ? (
                    <div className="rounded-lg border border-warning/40 bg-warning/10 p-4">
                        <p className="text-sm font-medium text-fg">{t('php.applyFirst')}</p>
                        <p className="mt-0.5 text-xs text-fg-muted">{t('php.applyFirstHint')}</p>
                    </div>
                ) : pc ? (
                    <form onSubmit={handleSavePool}>
                        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                            <Field label={t('php.pmMode')}>
                                <select name="pm" defaultValue={pc.pm || 'dynamic'} className={inputClass}>
                                    <option value="dynamic">dynamic</option>
                                    <option value="static">static</option>
                                    <option value="ondemand">ondemand</option>
                                </select>
                            </Field>
                            <Field label={t('php.maxChildren')}>
                                <input type="number" name="pm_max_children" defaultValue={pc.pm_max_children || 5} className={inputClass} />
                            </Field>
                            <Field label={t('php.startServers')}>
                                <input type="number" name="pm_start_servers" defaultValue={pc.pm_start_servers || 2} className={inputClass} />
                            </Field>
                            <Field label={t('php.minSpare')}>
                                <input type="number" name="pm_min_spare_servers" defaultValue={pc.pm_min_spare_servers || 1} className={inputClass} />
                            </Field>
                            <Field label={t('php.maxSpare')}>
                                <input type="number" name="pm_max_spare_servers" defaultValue={pc.pm_max_spare_servers || 3} className={inputClass} />
                            </Field>
                            <Field label={t('php.user')}>
                                <input type="text" name="user" defaultValue={pc.user || 'www-data'} className={inputClass} />
                            </Field>
                            <Field label={t('php.group')}>
                                <input type="text" name="group" defaultValue={pc.group || 'www-data'} className={inputClass} />
                            </Field>
                        </div>
                        <FormActions>
                            <Button type="submit" variant="primary" icon={Save}>
                                {t('php.savePool')}
                            </Button>
                        </FormActions>
                    </form>
                ) : (
                    <p className="text-sm text-fg-muted">{t('php.poolUnavailable')}</p>
                )}
            </FormSection>
        </div>
    );
}
