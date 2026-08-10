import { useEffect, useState } from 'react';
import { useI18n } from '../i18n';

export default function WebmailAccess({ domainId }: { domainId: number }) {
    const { t } = useI18n();
    const [path, setPath] = useState<'/webmail/' | null>();

    useEffect(() => {
        let current = true;
        setPath(undefined);
        fetch(`/api/v1/domains/${domainId}/mail/setup`)
            .then((response) => (response.ok ? response.json() : null))
            .then((setup: Record<string, unknown> | null) => {
                // Only the panel's fixed public proxy may become a link. Any
                // unavailable, malformed or external value fails closed.
                if (current) setPath(setup?.webmail_available === true && setup.webmail_url === '/webmail/' ? setup.webmail_url : null);
            }, () => {
                if (current) setPath(null);
            });
        return () => { current = false; };
    }, [domainId]);

    return (
        <div className='mb-4 flex items-center justify-between gap-3 rounded-lg border border-border p-3'>
            <div>
                <div className='text-sm font-medium'>{t('mail.webmail.title')}</div>
                <p className='text-xs text-fg-muted'>
                    {path === undefined ? t('mail.webmail.checking') : path ? t('mail.webmail.available') : t('mail.webmail.unavailable')}
                </p>
            </div>
            {path && (
                <a href={path} target='_blank' rel='noopener noreferrer' className='rounded-lg border border-border px-3 py-1.5 text-sm font-medium hover:bg-surface-2'>
                    {t('mail.webmail.open')}
                </a>
            )}
        </div>
    );
}
