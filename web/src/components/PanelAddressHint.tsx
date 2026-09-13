import { useEffect, useState } from 'react';
import { useI18n } from '../i18n';

// Only server-provided managed certificate metadata supplies the link. Never
// use query strings or stored browser values as a sign-in destination.
// Giriş bağlantısı sunucunun sertifika bilgisinden gelir; sorgu veya tarayıcıdaki kayıtlı değerler kullanılmaz.
export function PanelAddressHint() {
    const { t } = useI18n();
    const [address, setAddress] = useState('');
    useEffect(() => {
        const controller = new AbortController();
        const timeout = window.setTimeout(() => controller.abort(), 5000);
        void fetch('/api/v1/panel/access-address', { cache: 'no-store', signal: controller.signal })
            .then(async response => {
                if (!response.ok) return;
                const { hostname } = await response.json();
                if (controller.signal.aborted || typeof hostname !== 'string' || hostname.length > 253
                    || !hostname.includes('.') || hostname === window.location.hostname
                    || !hostname.split('.').every(label => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label))) return;
                const target = new URL(window.location.origin);
                target.protocol = 'https:';
                target.hostname = hostname;
                target.pathname = '/';
                setAddress(target.href);
            }).catch(() => {}).finally(() => window.clearTimeout(timeout));
        return () => { controller.abort(); window.clearTimeout(timeout); };
    }, []);
    if (!address) return null;
    return <aside className="mb-5 rounded-lg border border-border bg-surface-2 p-4 text-sm text-fg">
        <p>{t('login.panelAddressHint')}</p>
        <a href={address} className="mt-2 block break-all text-primary underline underline-offset-4">{address}</a>
    </aside>;
}
