import type { ReactNode } from 'react';
import { Boxes } from 'lucide-react';
import { useI18n } from '../i18n';
import { lastKnown } from '../lib/remote';
import { useManagedServices } from '../lib/managedServices';
import { Button, Checking, CouldNotCheck, KnownEmpty } from './ui';

// The management page of one component, addressed by its id. The versions the
// page needs come with the navigation from the Components page; after a reload
// they are read from the stored records. The person stays on this address
// whatever that read says: a read that failed is said so with Retry, and an id
// the catalogue does not have is said so with the way back. Both were a silent
// return to the list before 9 Oct 2026.
//
// Bir bileşenin, kimliğiyle adreslenen yönetim sayfası. Sayfanın gereksindiği
// sürümler Bileşenler sayfasından gelen gezinmeyle taşınır; yeniden yüklemede
// kayıtlı kayıtlardan okunur. Okuma ne derse desin kişi bu adreste kalır:
// başarısız okuma Tekrar dene ile, katalogda olmayan kimlik geri dönüş yoluyla
// söylenir. İkisi de 9 Eki 2026'dan önce listeye sessiz bir dönüştü.
export function ServiceRecordLookup({
    serviceId,
    carriedVersions,
    onBack,
    children,
}: {
    serviceId: string;
    /** The versions the Components page handed over with the navigation. */
    carriedVersions?: string[];
    onBack: () => void;
    children: (versions: string[]) => ReactNode;
}) {
    const { t } = useI18n();
    const catalogue = useManagedServices();
    const known = lastKnown(catalogue.remote);
    const record = known?.value.services.find((service) => service.id === serviceId);
    const versions = carriedVersions ?? (Array.isArray(record?.versions) ? (record.versions as string[]) : null);

    if (versions !== null) return <>{children(versions)}</>;

    const back = <Button type="button" onClick={onBack}>{t('nav.services')}</Button>;
    return (
        <div className="p-6 md:p-8">
            {catalogue.remote.state === 'loading' ? (
                <Checking label={t('component.checking')} />
            ) : known ? (
                <KnownEmpty
                    of={known}
                    icon={Boxes}
                    title={t('component.absent', { id: serviceId })}
                    hint={t('component.absentHint')}
                    action={back}
                />
            ) : (
                <>
                    <CouldNotCheck text={t('component.unknown')} onRetry={() => void catalogue.retry()} busy={catalogue.reading} />
                    <div className="mt-3">{back}</div>
                </>
            )}
        </div>
    );
}
