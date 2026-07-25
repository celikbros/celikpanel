import { FolderTree } from 'lucide-react';
import { ServiceShell } from './ServiceShell';
import { ComponentPanels } from './ComponentDetail';

interface VsftpdManagementProps {
    onBack: () => void;
    onSelectConfig?: (path: string) => void;
}

// vsftpd has no bespoke visual config yet — but "no bespoke page" no longer
// means "empty page": the derived panels show its real state, its vsftpd.conf
// (opens in the editor), its ports and its journal. The old EmptyState said
// "coming soon" while the panel already knew all of that (operator, 25 Jul:
// "birçok servisin manage sayfaları berbat... veya yok").
//
// vsftpd'nin henüz kendine özgü görsel yapılandırması yok — ama "özel sayfa
// yok" artık "boş sayfa" demek değil: türetilmiş paneller gerçek durumunu,
// vsftpd.conf'unu (editörde açılır), portlarını ve günlüğünü gösterir. Eski
// EmptyState "yakında" derken panel bunların hepsini zaten biliyordu
// (operatör, 25 Tem: "birçok servisin manage sayfaları berbat... veya yok").
export function VsftpdManagement({ onBack, onSelectConfig }: VsftpdManagementProps) {
    return (
        <ServiceShell serviceId="vsftpd" name="FTP (vsftpd)" icon={FolderTree} onBack={onBack}>
            <ComponentPanels serviceId="vsftpd" onSelectConfig={onSelectConfig} />
        </ServiceShell>
    );
}
