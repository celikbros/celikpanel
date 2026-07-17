import { useEffect, useState } from 'react';
import { Globe, X, Lock, Server, FileCode2, Network } from 'lucide-react';
import { showToast } from './Toast';
import { useI18n } from '../i18n';
import { ErrorBanner } from './ui';
import { readApiError, apiErrorText, type ApiError } from '../lib/apiError';

interface AddDomainModalProps {
    onClose: () => void;
    onSuccess: () => void;
}

// What this server can host right now — drives which choices the dialog
// offers. A php site needs a web server + PHP-FPM, a static site needs a web
// server, a DNS-only domain needs nothing. The requirement follows the ROLE
// the domain will play, not a fixed service list.
// Bu sunucunun şu anda neyi barındırabildiği — pencerenin hangi seçenekleri
// sunacağını belirler. php sitesi web sunucusu + PHP-FPM ister, statik site
// web sunucusu ister, yalnız-DNS domain hiçbir şey istemez. Gereksinim,
// domain'in üstleneceği ROLÜ izler; sabit bir servis listesini değil.
interface HostingCapabilities {
    web_server: string;
    php_versions: string[];
    dns_server: string;
    mail_server: boolean;
}

type ProjectType = 'php' | 'static' | 'dnsonly';

const API_BASE = '/api/v1';

export function AddDomainModal({ onClose, onSuccess }: AddDomainModalProps) {
    const { t } = useI18n();
    const [domainName, setDomainName] = useState('');
    const [caps, setCaps] = useState<HostingCapabilities | null>(null);
    const [projectType, setProjectType] = useState<ProjectType>('php');
    const [phpVersion, setPHPVersion] = useState('');
    const [sslEnabled, setSSLEnabled] = useState(false);
    const [loading, setLoading] = useState(false);
    // The full contract object, not just text: a coded refusal may carry an
    // in-panel fix path that ErrorBanner turns into a button.
    // Yalnız metin değil sözleşme nesnesi: kodlu ret, ErrorBanner'ın düğmeye
    // çevirdiği panel-içi çözüm yolu taşıyabilir.
    const [error, setError] = useState<ApiError | null>(null);

    // Load capabilities once, then default to the best type that can actually
    // work here: php if possible, else static, else DNS-only.
    //
    // Defensive against a null list field (php_versions etc): the backend now
    // always sends [], but trusting that from the frontend is how this broke
    // once already — a null[0] access threw inside this .then(), which the
    // trailing .catch() silently turned into "reset caps to null", making
    // every requirement check read as "unknown" and the DNS-only type fail
    // open. `?? []` here means a bad payload degrades to "nothing available"
    // (safe default) instead of a crash that erases the whole capability read.
    //
    // Yetenekleri bir kez yükle, sonra burada gerçekten çalışabilecek en iyi
    // tipe varsayılan yap: mümkünse php, değilse statik, değilse yalnız-DNS.
    //
    // Null bir liste alanına (php_versions vb.) karşı savunmacı: backend artık
    // her zaman [] gönderiyor, ama bunu frontend'den varsaymak bir kez tam
    // buradan bozulmasına yol açtı — bu .then() içinde bir null[0] erişimi
    // fırlattı, ardındaki .catch() bunu sessizce "caps'i null'a sıfırla"ya
    // çevirdi; bu da her gereksinim denetimini "bilinmiyor" yaptı ve
    // yalnız-DNS tipini açık bıraktı (fail open). Buradaki `?? []`, bozuk bir
    // yükün tüm yetenek okumasını silen bir çökme yerine "hiçbir şey uygun
    // değil"e (güvenli varsayılan) düşmesini sağlar.
    useEffect(() => {
        fetch(`${API_BASE}/hosting/capabilities`)
            .then((r) => (r.ok ? r.json() : null))
            .then((raw: HostingCapabilities | null) => {
                if (!raw) return;
                const c: HostingCapabilities = { ...raw, php_versions: raw.php_versions ?? [] };
                setCaps(c);
                setPHPVersion(c.php_versions[0] ?? '');
                if (c.web_server && c.php_versions.length > 0) setProjectType('php');
                else if (c.web_server) setProjectType('static');
                else setProjectType('dnsonly');
            })
            .catch(() => setCaps(null));
    }, []);

    // Product rule (D-009): this server serves its domains' DNS itself — with
    // no DNS server installed, no domain of any type can be added. One clear
    // blocker instead of a confusing "install one OR manage DNS elsewhere".
    // caps === null covers both "still loading" and "fetch failed": either
    // way, nothing is known to be available yet, so nothing is offered.
    // Ürün kuralı (D-009): bu sunucu, domain'lerinin DNS'ini kendisi sunar —
    // DNS sunucusu kurulu değilken hiçbir tipte domain eklenemez. caps ===
    // null hem "hâlâ yükleniyor" hem "çekme başarısız oldu"yu kapsar: her iki
    // durumda da hiçbir şeyin uygun olduğu bilinmiyordur, o yüzden hiçbiri
    // sunulmaz.
    const dnsMissing = !caps || caps.dns_server === '';
    const phpAvailable = !!caps && !dnsMissing && caps.web_server !== '' && caps.php_versions.length > 0;
    const staticAvailable = !!caps && !dnsMissing && caps.web_server !== '';

    const typeOptions: {
        id: ProjectType;
        icon: typeof Globe;
        available: boolean;
        requirement: string | null;
    }[] = [
        { id: 'php', icon: FileCode2, available: phpAvailable, requirement: !caps || dnsMissing ? null : caps.web_server === '' ? t('domains.add.needsWebServer') : caps.php_versions.length === 0 ? t('domains.add.needsPhp') : null },
        { id: 'static', icon: Server, available: staticAvailable, requirement: !caps || dnsMissing || staticAvailable ? null : t('domains.add.needsWebServer') },
        { id: 'dnsonly', icon: Network, available: !dnsMissing, requirement: null },
    ];

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setLoading(true);
        setError(null);

        try {
            const body: Record<string, unknown> = {
                domain: domainName,
                project_type: projectType,
                ssl_type: projectType !== 'dnsonly' && sslEnabled ? 'letsencrypt' : 'none',
            };
            if (projectType === 'php' && phpVersion) body.php_version = phpVersion;

            const res = await fetch(`${API_BASE}/domains/create`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            });

            if (!res.ok) {
                const apiErr = await readApiError(res);
                if (!apiErr.message && !apiErr.code) apiErr.message = t('domains.add.failed');
                setError(apiErr);
                showToast('error', apiErrorText(apiErr, t, 'domains.add.failed'));
                return;
            }

            showToast('success', t('domains.add.created', { name: domainName }));
            onSuccess();
        } catch {
            const apiErr: ApiError = { message: t('domains.add.failed') };
            setError(apiErr);
            showToast('error', apiErr.message);
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
            <div className="bg-surface border border-border rounded-xl p-8 max-w-2xl w-full max-h-[90vh] overflow-y-auto">
                <div className="flex justify-between items-center mb-6">
                    <div className="flex items-center gap-3">
                        <div className="p-2 bg-primary/10 rounded-lg">
                            <Globe className="w-6 h-6 text-primary" />
                        </div>
                        <div>
                            <h2 className="text-xl font-bold text-fg">{t('domains.add.title')}</h2>
                            <p className="text-sm text-fg-subtle">{t('domains.add.subtitle')}</p>
                        </div>
                    </div>
                    <button onClick={onClose} className="p-2 hover:bg-surface-2 rounded-lg transition-colors">
                        <X className="w-5 h-5 text-fg-muted" />
                    </button>
                </div>

                <ErrorBanner error={error} className="mb-6" />

                {dnsMissing && (
                    <div className="mb-6 p-4 bg-warning/10 border border-warning/30 rounded-lg text-sm text-fg">
                        {t('domains.add.needsDns')}
                    </div>
                )}

                <form onSubmit={handleSubmit} className="space-y-4">
                    <div>
                        <label className="block text-sm font-medium text-fg-muted mb-2">
                            {t('domains.add.domainName')}
                        </label>
                        <input
                            type="text"
                            value={domainName}
                            onChange={(e) => setDomainName(e.target.value)}
                            className="w-full bg-surface-2 border border-border rounded-lg px-4 py-3 text-fg focus:outline-none focus:border-primary"
                            placeholder="example.com"
                            required
                        />
                        <p className="text-xs text-fg-subtle mt-1">{t('domains.add.domainHint')}</p>
                    </div>

                    <div>
                        <label className="block text-sm font-medium text-fg-muted mb-2">
                            {t('domains.add.hostingType')}
                        </label>
                        <div className="grid gap-2">
                            {typeOptions.map(({ id, icon: Icon, available, requirement }) => (
                                <label
                                    key={id}
                                    className={`flex items-start gap-3 rounded-lg border p-3 transition-colors ${
                                        projectType === id
                                            ? 'border-primary bg-primary/5'
                                            : 'border-border bg-surface-2/50'
                                    } ${available ? 'cursor-pointer hover:border-primary/50' : 'opacity-60'}`}
                                >
                                    <input
                                        type="radio"
                                        name="projectType"
                                        checked={projectType === id}
                                        disabled={!available}
                                        onChange={() => setProjectType(id)}
                                        className="mt-1"
                                    />
                                    <Icon className="mt-0.5 h-4 w-4 shrink-0 text-fg-subtle" />
                                    <div className="min-w-0">
                                        <div className="text-sm font-medium text-fg">{t(`domains.add.type.${id}`)}</div>
                                        <p className="text-xs text-fg-subtle">{t(`domains.add.type.${id}.desc`)}</p>
                                        {requirement && (
                                            <p className="mt-1 text-xs font-medium text-warning">{requirement}</p>
                                        )}
                                    </div>
                                </label>
                            ))}
                        </div>
                    </div>

                    {projectType === 'php' && caps && caps.php_versions.length > 0 && (
                        <div>
                            <label className="block text-sm font-medium text-fg-muted mb-2">
                                {t('domains.add.phpVersion')}
                            </label>
                            <select
                                value={phpVersion}
                                onChange={(e) => setPHPVersion(e.target.value)}
                                className="w-full bg-surface-2 border border-border rounded-lg px-4 py-3 text-fg focus:outline-none focus:border-primary"
                            >
                                {caps.php_versions.map((v) => (
                                    <option key={v} value={v}>PHP {v}</option>
                                ))}
                            </select>
                        </div>
                    )}

                    {projectType !== 'dnsonly' && (
                        <div className="flex items-start gap-3 p-4 bg-surface-2/50 rounded-lg">
                            <input
                                type="checkbox"
                                id="ssl"
                                checked={sslEnabled}
                                onChange={(e) => setSSLEnabled(e.target.checked)}
                                className="mt-1"
                            />
                            <div className="flex-1">
                                <label htmlFor="ssl" className="flex items-center gap-2 text-sm font-medium text-fg-muted cursor-pointer">
                                    <Lock className="w-4 h-4" />
                                    {t('domains.add.ssl')}
                                </label>
                                <p className="text-xs text-fg-subtle mt-1">{t('domains.add.sslHint')}</p>
                            </div>
                        </div>
                    )}

                    {caps && caps.dns_server !== '' && (
                        <p className="text-xs text-fg-subtle">
                            {t('domains.add.dnsServed', { server: caps.dns_server })}
                        </p>
                    )}

                    <div className="flex gap-3 pt-4">
                        <button
                            type="submit"
                            disabled={loading || dnsMissing}
                            className="flex-1 bg-primary hover:bg-primary-hover disabled:bg-surface-3 text-white px-6 py-3 rounded-lg transition-colors font-medium"
                        >
                            {loading ? t('domains.add.creating') : t('domains.add.create')}
                        </button>
                        <button
                            type="button"
                            onClick={onClose}
                            className="px-6 py-3 bg-surface-2 hover:bg-surface-3 text-fg-muted rounded-lg transition-colors"
                        >
                            {t('common.cancel')}
                        </button>
                    </div>
                </form>
            </div>
        </div>
    );
}
