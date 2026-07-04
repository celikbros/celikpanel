import { useState, useEffect } from 'react';
import { Save, Plus, Trash2, Globe } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { FormSection, ToggleRow, FormActions, Button, inputClass } from './ui';

interface DomainGeneralSettingsProps {
    domainId: number;
    domainName: string;
}

interface GeneralSettings {
    domain_id: number;
    domain_name: string;
    document_root: string;
    web_server: string;
    redirect_www: boolean;
    redirect_https: boolean;
    aliases: string[];
}

export function DomainGeneralSettings({ domainId, domainName }: DomainGeneralSettingsProps) {
    const { t } = useI18n();
    const [settings, setSettings] = useState<GeneralSettings | null>(null);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [newAlias, setNewAlias] = useState('');
    const [webServers, setWebServers] = useState<string[]>(['nginx']);

    useEffect(() => {
        loadSettings();
        loadWebServers();
    }, [domainId]);

    const loadWebServers = async () => {
        try {
            const res = await fetch('/api/v1/system/check');
            if (!res.ok) return;
            const data = await res.json();
            const servers: string[] = [];
            if (data.nginx) servers.push('nginx');
            if (data.apache) servers.push('apache');
            if (servers.length) setWebServers(servers);
        } catch {
            /* keep default */
        }
    };

    const loadSettings = async () => {
        setLoading(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/general`);
            if (!res.ok) throw new Error();
            setSettings(await res.json());
        } catch {
            showToast('error', t('general.loadFailed'));
        } finally {
            setLoading(false);
        }
    };

    const handleSave = async (e: React.FormEvent<HTMLFormElement>) => {
        e.preventDefault();
        if (!settings) return;
        const fd = new FormData(e.currentTarget);
        setSaving(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/general`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    document_root: fd.get('document_root') as string,
                    web_server: fd.get('web_server') as string,
                    redirect_www: fd.get('redirect_www') === 'on',
                    redirect_https: fd.get('redirect_https') === 'on',
                }),
            });
            if (!res.ok) throw new Error();
            showToast('success', t('general.saved'));
            loadSettings();
        } catch {
            showToast('error', t('general.saveFailed'));
        } finally {
            setSaving(false);
        }
    };

    const handleAddAlias = async () => {
        if (!newAlias.trim()) return;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/aliases`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ alias: newAlias }),
            });
            if (!res.ok) throw new Error();
            showToast('success', t('general.aliasAdded', { name: newAlias }));
            setNewAlias('');
            loadSettings();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    const handleDeleteAlias = async (alias: string) => {
        if (!confirm(t('general.confirmDeleteAlias', { name: alias }))) return;
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/aliases/${alias}`, { method: 'DELETE' });
            if (!res.ok) throw new Error();
            showToast('success', t('general.aliasDeleted', { name: alias }));
            loadSettings();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    if (loading) {
        return (
            <div className="flex items-center justify-center py-16">
                <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
            </div>
        );
    }
    if (!settings) return <p className="text-danger">{t('general.loadFailed')}</p>;

    return (
        <div>
            <form onSubmit={handleSave}>
                <FormSection title={t('general.docRoot')} description={t('general.docRootHint')}>
                    <input
                        type="text"
                        name="document_root"
                        defaultValue={settings.document_root}
                        placeholder="/var/www/html"
                        className={`${inputClass} font-mono`}
                    />
                </FormSection>

                <FormSection
                    title={t('general.webServer')}
                    description={webServers.length > 1 ? t('general.webServerHintMulti') : t('general.webServerHintSingle')}
                >
                    {webServers.length > 1 ? (
                        <select name="web_server" defaultValue={settings.web_server} className={inputClass}>
                            {webServers.map((s) => (
                                <option key={s} value={s}>
                                    {s.charAt(0).toUpperCase() + s.slice(1)}
                                </option>
                            ))}
                        </select>
                    ) : (
                        <>
                            <p className="text-sm text-fg">{webServers[0].charAt(0).toUpperCase() + webServers[0].slice(1)}</p>
                            <input type="hidden" name="web_server" value={webServers[0]} />
                        </>
                    )}
                </FormSection>

                <FormSection title={t('general.redirects')}>
                    <ToggleRow
                        name="redirect_www"
                        defaultChecked={settings.redirect_www}
                        label={t('general.redirectWww')}
                        hint={`${domainName} → www.${domainName}`}
                    />
                    <ToggleRow
                        name="redirect_https"
                        defaultChecked={settings.redirect_https}
                        label={t('general.forceHttps')}
                        hint={t('general.forceHttpsHint')}
                    />
                </FormSection>

                <FormActions>
                    <Button type="submit" variant="primary" icon={Save} disabled={saving}>
                        {saving ? t('general.saving') : t('general.save')}
                    </Button>
                </FormActions>
            </form>

            <div className="mt-6 border-t border-border pt-5">
                <h3 className="text-sm font-semibold text-fg">{t('general.aliases')}</h3>
                <p className="mt-0.5 text-xs text-fg-muted">{t('general.aliasesHint')}</p>

                <div className="mt-3 flex gap-2">
                    <input
                        type="text"
                        value={newAlias}
                        onChange={(e) => setNewAlias(e.target.value)}
                        onKeyDown={(e) => e.key === 'Enter' && (e.preventDefault(), handleAddAlias())}
                        placeholder={t('general.aliasPlaceholder')}
                        className={inputClass}
                    />
                    <Button variant="primary" icon={Plus} onClick={handleAddAlias}>
                        {t('general.add')}
                    </Button>
                </div>

                {settings.aliases && settings.aliases.length > 0 ? (
                    <div className="mt-3 space-y-2">
                        {settings.aliases.map((alias) => (
                            <div
                                key={alias}
                                className="flex items-center justify-between rounded-lg border border-border bg-surface-2/50 px-3 py-2"
                            >
                                <span className="flex items-center gap-2 font-mono text-sm text-fg">
                                    <Globe className="h-4 w-4 text-fg-subtle" />
                                    {alias}
                                </span>
                                <button
                                    onClick={() => handleDeleteAlias(alias)}
                                    className="rounded-md p-1.5 text-fg-subtle transition-colors hover:bg-surface-3 hover:text-danger"
                                >
                                    <Trash2 className="h-4 w-4" />
                                </button>
                            </div>
                        ))}
                    </div>
                ) : (
                    <p className="mt-3 text-center text-sm text-fg-subtle">{t('general.noAliases')}</p>
                )}
            </div>
        </div>
    );
}
