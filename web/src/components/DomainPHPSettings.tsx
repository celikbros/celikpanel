import { useState } from 'react';
import { Save } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { Button, Checking, CouldNotCheck, Field, FormActions, FormSection, RemoteGate, ResultUnknown, inputClass } from './ui';
import { useHostingCapabilities } from '../lib/hostingCapabilities';
import { apiErrorText, readApiError } from '../lib/apiError';
import { useRemote } from '../lib/remote';
import { useLostAnswer } from '../lib/lostAnswer';

interface DomainPHPSettingsProps {
    domainId: number;
    domainName: string;
    currentVersion: string;
    onVersionChange: (version: string) => void;
    readOnly?: boolean;
    isAdditionalUser?: boolean;
}

interface PoolConfig {
    pm: string;
    pm_max_children: number;
    pm_start_servers: number;
    pm_min_spare_servers: number;
    pm_max_spare_servers: number;
    user: string;
    group: string;
}

interface PHPSettings {
    php_version: string;
    pool_name: string;
    /** Absent when the server has no pool to show for this domain. */
    pool_config: PoolConfig | null;
    /** The tenant-safe list a team member may pick from. */
    availableVersions: string[];
}

const poolFields: (keyof PoolConfig)[] = ['pm', 'pm_max_children', 'pm_start_servers', 'pm_min_spare_servers', 'pm_max_spare_servers', 'user', 'group'];

const phpVersionPattern = /^[0-9]{1,2}\.[0-9]{1,2}$/;

function parseAvailablePHPVersions(value: unknown): string[] {
    if (!Array.isArray(value)) return [];

    const parsed: string[] = [];
    const seen = new Set<string>();
    for (const item of value) {
        if (typeof item !== 'string' || !phpVersionPattern.test(item)) return [];
        if (seen.has(item)) continue;
        seen.add(item);
        parsed.push(item);
    }
    return parsed;
}

// The PHP settings of one domain, as its own address answers. An answer
// without the version is not the contract; a pool that is present is taken as
// the server wrote it, and no field of it is filled in here.
// Bir alan adının PHP ayarları, kendi adresinin yanıtladığı gibi. Sürümü
// taşımayan yanıt sözleşme değildir; var olan havuz sunucunun yazdığı gibi
// alınır ve hiçbir alanı burada doldurulmaz.
function decodePHPSettings(raw: unknown): PHPSettings {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('shape');
    const body = raw as Record<string, unknown>;
    if (typeof body.php_version !== 'string') throw new Error('field');
    const pool = body.pool_config;
    if (pool !== undefined && pool !== null && (typeof pool !== 'object' || Array.isArray(pool))) throw new Error('pool');
    return {
        php_version: body.php_version,
        pool_name: typeof body.pool_name === 'string' ? body.pool_name : '',
        pool_config: (pool as PoolConfig | null | undefined) ?? null,
        availableVersions: parseAvailablePHPVersions(body.available_versions),
    };
}

export function DomainPHPSettings({
    domainId,
    currentVersion,
    onVersionChange,
    readOnly = false,
    isAdditionalUser = false,
}: DomainPHPSettingsProps) {
    const { t } = useI18n();
    const [saving, setSaving] = useState(false);
    const [savingPool, setSavingPool] = useState(false);
    // The version picked here and not applied yet; null until the person picks.
    // Burada seçilip henüz uygulanmamış sürüm; kişi seçene dek null.
    const [picked, setPicked] = useState<string | null>(null);
    // Pool values that were sent, when their answer was lost and the settings
    // read again do not show them: the form keeps them over that newer answer.
    // Yanıtı yiten ve yeniden okunan ayarlarda görünmeyen havuz değerleri: form
    // onları o yeni yanıtın üzerinde tutar.
    const [poolDraft, setPoolDraft] = useState<{ from: number; value: PoolConfig } | null>(null);
    // Only versions that actually exist on this host — a hard-coded "8.3" on
    // a server without PHP was a settings page for a ghost. The current
    // version stays selectable even if its tree vanished (honest state).
    //
    // An administrator's list is the server's capabilities, read once and
    // shared (lib/hostingCapabilities.ts): while it is being checked, or could
    // not be checked, only the current version is listed and the screen says
    // which of the two it is. A team member's list is the tenant-safe
    // available_versions that arrive with this domain's own settings; the
    // server-wide inventory is never read for them. Those versions exist only
    // for a known answer of this domain's own address, so nothing of another
    // domain's answer is carried over.
    // Yalnız bu makinede gerçekten var olan sürümler — PHP'siz sunucuda sabit
    // "8.3", hayalete ayar sayfasıydı. Mevcut sürüm, ağacı kaybolsa bile
    // seçilebilir kalır (dürüst durum). Yöneticinin listesi sunucunun
    // yetenekleridir; kontrol edilirken ya da edilemediğinde yalnız geçerli
    // sürüm listelenir ve ekran hangisi olduğunu söyler. Ekip üyesinin listesi
    // bu alan adının kendi ayarlarıyla gelen available_versions'tır; yalnız bu
    // alan adının kendi adresinin bilinen yanıtı için vardır.
    const capabilities = useHostingCapabilities({ enabled: !isAdditionalUser });
    const settings = useRemote(`/api/v1/domains/${domainId}/php`, decodePHPSettings);
    const teamVersions = settings.remote.state === 'known' ? settings.remote.value.availableVersions : [];
    const versions = isAdditionalUser
        ? teamVersions
        : capabilities.remote.state === 'known' ? capabilities.remote.value.php_versions : [];
    const answer = useLostAnswer(() => settings.retry());
    // A change is sent only for settings the server sent and is not being
    // asked about again.
    // Değişiklik yalnız sunucunun gönderdiği ve yeniden sorulmayan ayarlar
    // için gönderilir.
    const settled = settings.remote.state === 'known' && !settings.reading && !answer.holding;
    const selectedVersion = picked ?? (settings.remote.state === 'known' ? settings.remote.value.php_version : currentVersion);

    const handleVersionChange = async () => {
        if (readOnly || !settled || selectedVersion === currentVersion) return;
        if (isAdditionalUser && !versions.includes(selectedVersion)) return;
        if (!confirm(t('php.changeConfirm', { from: currentVersion, to: selectedVersion }))) return;
        setSaving(true);
        try {
            const res = await answer.send(`/api/v1/domains/${domainId}/php`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ php_version: selectedVersion }),
            }, {
                // The settings read again name the version the domain runs. If
                // it is the picked one, the pick is done; if not, it stays
                // picked and the notice says it is not shown as saved.
                // Yeniden okunan ayarlar alan adının sürümünü söyler. Seçilen
                // sürümse seçim tamamdır; değilse seçili kalır.
                shows: (read) => (read[0].value as PHPSettings).php_version === selectedVersion,
                made: () => {
                    onVersionChange(selectedVersion);
                    setPicked(null);
                },
            });
            if (!res) return;
            if (!res.ok) {
                showToast('error', apiErrorText(await readApiError(res), t, 'php.changeFailed'));
                setPicked(null);
                return;
            }
            showToast('success', t('php.changed', { version: selectedVersion }));
            answer.settle();
            onVersionChange(selectedVersion);
            setPicked(null);
            await settings.retry();
        } finally {
            setSaving(false);
        }
    };

    const handleSavePool = async (e: React.FormEvent<HTMLFormElement>) => {
        e.preventDefault();
        if (readOnly || !settled) return;
        const fd = new FormData(e.currentTarget);
        const pool: PoolConfig = {
            pm: fd.get('pm') as string,
            pm_max_children: parseInt(fd.get('pm_max_children') as string),
            pm_start_servers: parseInt(fd.get('pm_start_servers') as string),
            pm_min_spare_servers: parseInt(fd.get('pm_min_spare_servers') as string),
            pm_max_spare_servers: parseInt(fd.get('pm_max_spare_servers') as string),
            user: fd.get('user') as string,
            group: fd.get('group') as string,
        };
        setSavingPool(true);
        try {
            const res = await answer.send(`/api/v1/domains/${domainId}/php/pool`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ version: currentVersion, pool_config: pool }),
            }, {
                // The pool read again either holds every value as it was sent,
                // or it does not; then the form keeps what was sent.
                // Yeniden okunan havuz her değeri ya gönderildiği gibi tutar ya
                // da tutmaz; o zaman form gönderileni korur.
                shows: (read) => {
                    const now = (read[0].value as PHPSettings).pool_config;
                    if (!now) return null;
                    return poolFields.every((name) => String(now[name]) === String(pool[name]));
                },
                notMade: (read) => setPoolDraft({ from: read[0].observedAt, value: pool }),
            });
            if (!res) return;
            if (!res.ok) {
                showToast('error', apiErrorText(await readApiError(res), t, 'php.poolFailed'));
                return;
            }
            showToast('success', t('php.poolSaved'));
            answer.settle();
            await settings.retry();
        } finally {
            setSavingPool(false);
        }
    };

    return (
        <RemoteGate
            remote={settings.remote}
            checking={t('php.checking')}
            failed={t('php.unknown')}
            onRetry={() => void settings.retry()}
            busy={settings.reading}
        >
            {(shown) => {
                const keptPool = poolDraft && poolDraft.from === shown.observedAt ? poolDraft.value : null;
                const pc = shown.value.pool_config ? keptPool ?? shown.value.pool_config : null;
                const pending = selectedVersion !== currentVersion;
                const displayedVersions = currentVersion && !versions.includes(currentVersion)
                    ? [currentVersion, ...versions]
                    : versions;
                const canSave = settled && !shown.stale;
                return (
                    <div>
                        <ResultUnknown answer={answer} className="mb-4" />
                        <FormSection title={t('php.version')} description={pending ? t('php.reloadWarning') : undefined}>
                            <div className="flex gap-2">
                                <select
                                    value={selectedVersion}
                                    onChange={(e) => setPicked(e.target.value)}
                                    disabled={readOnly || (isAdditionalUser && versions.length === 0)}
                                    className={inputClass}
                                >
                                    {displayedVersions.map((v) => (
                                        <option key={v} value={v}>
                                            PHP {v}
                                        </option>
                                    ))}
                                </select>
                                <Button
                                    variant="primary"
                                    icon={Save}
                                    onClick={handleVersionChange}
                                    disabled={readOnly || saving || !canSave || !pending || (isAdditionalUser && !versions.includes(selectedVersion))}
                                >
                                    {saving ? t('php.applying') : t('php.apply')}
                                </Button>
                            </div>
                            {/* A line of its own height under the picker, so the form
                                below does not move when the versions arrive.
                                Seçicinin altında kendi yüksekliği olan satır; sürümler
                                gelince alttaki form yerinden oynamaz. */}
                            {!isAdditionalUser && (
                                <div className="min-h-5">
                                    {capabilities.remote.state === 'loading' && <Checking label={t('php.checkingVersions')} />}
                                    {capabilities.remote.state === 'unknown' && (
                                        <CouldNotCheck
                                            text={t('php.versionsUnknown')}
                                            onRetry={() => void capabilities.retry()}
                                            busy={capabilities.reading}
                                        />
                                    )}
                                </div>
                            )}
                        </FormSection>

                        <FormSection title={t('php.pool')} description={`${t('php.poolName')}: ${shown.value.pool_name}`}>
                            {pending ? (
                                <div className="rounded-lg border border-warning-mark/60 bg-warning-mark/20 p-4">
                                    <p className="text-sm font-medium text-fg">{t('php.applyFirst')}</p>
                                    <p className="mt-0.5 text-xs text-fg-muted">{t('php.applyFirstHint')}</p>
                                </div>
                            ) : pc ? (
                                // The fields hold what the server sent, and a
                                // newer answer builds the form again.
                                // Alanlar sunucunun gönderdiğini tutar; daha
                                // yeni yanıt formu yeniden kurar.
                                <form key={`${shown.observedAt}${keptPool ? ':kept' : ''}`} onSubmit={handleSavePool}>
                                    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                                        <Field label={t('php.pmMode')}>
                                            <select name="pm" defaultValue={pc.pm} disabled={readOnly} className={inputClass}>
                                                <option value="dynamic">dynamic</option>
                                                <option value="static">static</option>
                                                <option value="ondemand">ondemand</option>
                                            </select>
                                        </Field>
                                        <Field label={t('php.maxChildren')}>
                                            <input type="number" name="pm_max_children" defaultValue={pc.pm_max_children} readOnly={readOnly} className={inputClass} />
                                        </Field>
                                        <Field label={t('php.startServers')}>
                                            <input type="number" name="pm_start_servers" defaultValue={pc.pm_start_servers} readOnly={readOnly} className={inputClass} />
                                        </Field>
                                        <Field label={t('php.minSpare')}>
                                            <input type="number" name="pm_min_spare_servers" defaultValue={pc.pm_min_spare_servers} readOnly={readOnly} className={inputClass} />
                                        </Field>
                                        <Field label={t('php.maxSpare')}>
                                            <input type="number" name="pm_max_spare_servers" defaultValue={pc.pm_max_spare_servers} readOnly={readOnly} className={inputClass} />
                                        </Field>
                                        <Field label={t('php.user')}>
                                            <input type="text" name="user" defaultValue={pc.user} readOnly={readOnly} className={inputClass} />
                                        </Field>
                                        <Field label={t('php.group')}>
                                            <input type="text" name="group" defaultValue={pc.group} readOnly={readOnly} className={inputClass} />
                                        </Field>
                                    </div>
                                    {!readOnly && (
                                        <FormActions>
                                            <Button type="submit" variant="primary" icon={Save} disabled={!canSave || savingPool}>
                                                {t('php.savePool')}
                                            </Button>
                                        </FormActions>
                                    )}
                                </form>
                            ) : (
                                <p className="text-sm text-fg-muted">{t('php.poolUnavailable')}</p>
                            )}
                        </FormSection>
                    </div>
                );
            }}
        </RemoteGate>
    );
}
