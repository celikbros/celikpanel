import { useState, useEffect } from 'react';
import { Plus, Trash2, Globe } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { FormSection, Button, inputClass } from './ui';
import { apiErrorText, readApiError } from '../lib/apiError';

interface DomainGeneralSettingsProps {
    domainId: number;
}

interface GeneralSettings {
    domain_id: number;
    domain_name: string;
    document_root: string;
    web_server: string;
    aliases: string[];
}

export function DomainGeneralSettings({ domainId }: DomainGeneralSettingsProps) {
    const { t } = useI18n();
    const [settings, setSettings] = useState<GeneralSettings | null>(null);
    const [loading, setLoading] = useState(true);
    const [newAlias, setNewAlias] = useState('');

    useEffect(() => {
        loadSettings();
    }, [domainId]);

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

    const handleAddAlias = async () => {
        const alias = newAlias.trim();
        if (!alias) return;
        try {
            const request = (confirmCertificateReissue: boolean) =>
                fetch(`/api/v1/domains/${domainId}/aliases`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        alias,
                        confirm_certificate_reissue: confirmCertificateReissue,
                    }),
                });
            let res = await request(false);
            if (!res.ok) {
                let apiError = await readApiError(res);
                if (apiError.code === 'ALIAS_CERTIFICATE_REISSUE_REQUIRED') {
                    if (!confirm(t('general.aliasReissueAddConfirm', { name: alias }))) return;
                    res = await request(true);
                    if (!res.ok) apiError = await readApiError(res);
                }
                if (!res.ok) {
                    showToast('error', apiErrorText(apiError, t, 'common.error'));
                    if (apiError.code === 'ALIAS_CERTIFICATE_ACTIVATION_PENDING') {
                        await loadSettings();
                    }
                    return;
                }
            }
            showToast('success', t('general.aliasAdded', { name: alias }));
            setNewAlias('');
            await loadSettings();
        } catch {
            showToast('error', t('common.error'));
        }
    };

    const handleDeleteAlias = async (alias: string) => {
        if (!confirm(t('general.confirmDeleteAlias', { name: alias }))) return;
        try {
            const endpoint = `/api/v1/domains/${domainId}/aliases/${encodeURIComponent(alias)}`;
            let res = await fetch(endpoint, { method: 'DELETE' });
            if (!res.ok) {
                let apiError = await readApiError(res);
                if (apiError.code === 'ALIAS_CERTIFICATE_REISSUE_REQUIRED') {
                    if (!confirm(t('general.aliasReissueDeleteConfirm', { name: alias }))) return;
                    res = await fetch(`${endpoint}?confirm_certificate_reissue=true`, { method: 'DELETE' });
                    if (!res.ok) apiError = await readApiError(res);
                }
                if (!res.ok) {
                    showToast('error', apiErrorText(apiError, t));
                    if (apiError.code === 'ALIAS_CERTIFICATE_ACTIVATION_PENDING') {
                        await loadSettings();
                    }
                    return;
                }
            }
            showToast('success', t('general.aliasDeleted', { name: alias }));
            await loadSettings();
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
            <FormSection title={t('general.docRoot')} description={t('general.docRootHint')}>
                <p className="break-all rounded-lg border border-border bg-surface-2 px-3 py-2 font-mono text-sm text-fg">
                    {settings.document_root}
                </p>
            </FormSection>

            <FormSection title={t('general.webServer')} description={t('general.webServerHintSingle')}>
                <p className="text-sm text-fg">
                    {settings.web_server.charAt(0).toUpperCase() + settings.web_server.slice(1)}
                </p>
            </FormSection>

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
