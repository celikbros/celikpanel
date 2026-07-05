import { FolderTree } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { EmptyState } from './ui';
import { useI18n } from '../i18n';

interface VsftpdManagementProps {
    onBack: () => void;
}

// vsftpd has no in-panel config yet, so the page is the honest shell (real
// status + start/stop) plus a clear "coming soon" note — no invented panels.
//
// vsftpd'nin henüz panel içi yapılandırması yok; bu yüzden sayfa dürüst
// kabuktan (gerçek durum + başlat/durdur) ve net bir "yakında" notundan
// ibarettir — uydurma paneller yok.
export function VsftpdManagement({ onBack }: VsftpdManagementProps) {
    const { t } = useI18n();
    return (
        <ServiceShell serviceId="vsftpd" name="FTP (vsftpd)" icon={FolderTree} onBack={onBack}>
            <EmptyState icon={FolderTree} title={t('vsftpd.configTitle')} hint={t('vsftpd.configSoon')} />
        </ServiceShell>
    );
}
