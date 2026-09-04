import { useEffect, useState } from 'react';
import { useNavigate } from '../router';
import { Globe, X, Lock, Server, Network } from 'lucide-react';
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
    dns_identity_ready: boolean;
    mail_server: boolean;
}

// What the domain is FOR — the only question this screen asks (D-013).
// Runtime (PHP on/off, version, Node…) is a SITE SETTING chosen afterwards on
// the site's own page, not a creation-time decision: a person adding a domain
// knows their purpose, not their interpreter. "website" creates a site the
// panel serves; "dnsonly" creates just the zone. `static` and `php` are the
// same thing with the PHP switch off and on, so they stopped being separate
// choices here.
// Domain NE İÇİN — bu ekranın sorduğu tek soru (D-013). Runtime (PHP açık/kapalı,
// sürüm, Node…) sonradan sitenin kendi sayfasında seçilen bir SİTE AYARIdır,
// oluşturma anı kararı değil: domain ekleyen kişi amacını bilir, yorumlayıcısını
// değil. "website" panelin sunduğu bir site oluşturur; "dnsonly" yalnız zone.
// `static` ile `php` aynı şeyin PHP anahtarı kapalı/açık hâlidir; bu yüzden
// burada ayrı seçenek olmaktan çıktılar.
type Purpose = 'website' | 'dnsonly';

const API_BASE = '/api/v1';

interface DomainCreatePartialSuccess {
    error: string;
    code: 'DNS_PUBLICATION_PENDING';
    partial_success: true;
    domain_id: number;
    domain: string;
    zone_exists: boolean;
    action?: string;
}

// A failed HTTP status does not always mean Create did nothing. When the site
// already exists but DNS publication failed, preserve the structured success
// identity before the generic API-error reader intentionally narrows the body.
async function readDomainCreatePartialSuccess(res: Response): Promise<DomainCreatePartialSuccess | null> {
    try {
        const value: unknown = await res.clone().json();
        if (!value || typeof value !== 'object') return null;
        const data = value as Record<string, unknown>;
        if (
            data.code !== 'DNS_PUBLICATION_PENDING' ||
            data.partial_success !== true ||
            typeof data.domain_id !== 'number' ||
            data.domain_id <= 0 ||
            typeof data.domain !== 'string' ||
            data.domain === ''
        ) {
            return null;
        }
        return {
            error: typeof data.error === 'string' ? data.error : '',
            code: 'DNS_PUBLICATION_PENDING',
            partial_success: true,
            domain_id: data.domain_id,
            domain: data.domain,
            zone_exists: data.zone_exists === true,
            action: typeof data.action === 'string' ? data.action : undefined,
        };
    } catch {
        return null;
    }
}

export function AddDomainModal({ onClose, onSuccess }: AddDomainModalProps) {
    const { t } = useI18n();
    const navigate = useNavigate();
    const [domainName, setDomainName] = useState('');
    const [caps, setCaps] = useState<HostingCapabilities | null>(null);
    const [purpose, setPurpose] = useState<Purpose>('website');
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
                // Default to what can actually work here: a website needs a web
                // server; without one only the DNS zone is possible.
                // Burada gerçekten çalışabilecek olana varsayılan yap: web sitesi
                // web sunucusu ister; o yoksa yalnız DNS zone'u mümkündür.
                setPurpose(c.web_server ? 'website' : 'dnsonly');
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
    const dnsMissing = !caps || caps.dns_server === '' || caps.dns_identity_ready !== true;
    // A website needs a web server — and ONLY a web server. PHP is no longer a
    // precondition here (D-013): a site is created first, its PHP switch is a
    // setting afterwards, so a server without PHP can still host websites.
    // Web sitesi bir web sunucusu ister — ve YALNIZ onu. PHP artık burada ön
    // koşul değildir (D-013): önce site oluşturulur, PHP anahtarı sonradan bir
    // ayardır; yani PHP'siz bir sunucu da web sitesi barındırabilir.
    const websiteAvailable = !!caps && !dnsMissing && caps.web_server !== '';

    const purposeOptions: {
        id: Purpose;
        icon: typeof Globe;
        available: boolean;
        requirement: string | null;
    }[] = [
        { id: 'website', icon: Server, available: websiteAvailable, requirement: !caps || dnsMissing || websiteAvailable ? null : t('domains.add.needsWebServer') },
        { id: 'dnsonly', icon: Network, available: !dnsMissing, requirement: null },
    ];

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        if (dnsMissing) return;
        setLoading(true);
        setError(null);

        try {
            // A website is created as a plain site; PHP is off until the
            // operator turns it on in the site's settings (D-013). "static" is
            // that off state — the wire value stays the same, only the question
            // asked on screen changed.
            // Web sitesi düz bir site olarak oluşturulur; PHP, operatör sitenin
            // ayarlarından açana dek kapalıdır (D-013). "static" o kapalı
            // hâldir — kablo üzerindeki değer aynı kaldı, yalnız ekranda
            // sorulan soru değişti.
            const body: Record<string, unknown> = {
                domain: domainName,
                project_type: purpose === 'website' ? 'static' : 'dnsonly',
                ssl_type: purpose !== 'dnsonly' && sslEnabled ? 'letsencrypt' : 'none',
            };

            const res = await fetch(`${API_BASE}/domains/create`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            });

            if (!res.ok) {
                const partial = await readDomainCreatePartialSuccess(res);
                if (partial) {
                    // Creation is finished; never leave the form enabled for a
                    // duplicate retry. The parent closes this modal and refreshes
                    // the list, then we continue at the newly created domain.
                    const action = partial.action?.startsWith('/domains/')
                        ? partial.action
                        : `/domains/${encodeURIComponent(partial.domain)}`;
                    showToast('warning', t('domains.add.createdDnsPending', { name: partial.domain }));
                    onSuccess();
                    navigate(action);
                    return;
                }
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
                    <div className="mb-6 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-warning/30 bg-warning/10 p-4 text-sm text-fg">
                        {/*
                          Say which half is missing. An engine that is active but
                          has no identity must not be told to "activate BIND or
                          PowerDNS"; the button and the sentence name one fix.
                          Hangi yarının eksik olduğunu söyle. Etkin ama kimliksiz
                          bir motora "BIND ya da PowerDNS'i etkinleştir" denmemeli;
                          düğme ile cümle aynı tek düzeltmeyi adlandırır.
                        */}
                        <span>{caps?.dns_server ? t('err.DNS_SETTINGS_REQUIRED') : t('domains.add.needsDns')}</span>
                        {/*
                          Both halves of "DNS is missing" are fixed in the same
                          place: the DNS infrastructure section installs and
                          activates an engine, and stages the identity. The
                          Services page cannot install a DNS engine any more
                          (DNS_ENGINE_WORKFLOW_REQUIRED), so sending a fresh
                          host there was a dead end (R-029, screen side).
                          "DNS eksik"in iki yarısı da aynı yerde düzelir: DNS
                          altyapısı bölümü motoru kurup etkinleştirir ve kimliği
                          hazırlar. Servisler sayfası artık DNS motoru kuramaz
                          (DNS_ENGINE_WORKFLOW_REQUIRED); taze sunucuyu oraya
                          göndermek çıkmaz sokaktı (R-029, ekran tarafı).
                        */}
                        <button
                            type='button'
                            onClick={() => navigate('/settings?section=dns')}
                            className='rounded-lg bg-primary px-3 py-1.5 text-xs font-semibold text-primary-fg'
                        >
                            {caps?.dns_server
                                ? t('err.DNS_SETTINGS_REQUIRED.action')
                                : t('err.DNS_SERVER_REQUIRED.action')}
                        </button>
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
                            {t('domains.add.purpose')}
                        </label>
                        <div className="grid gap-2">
                            {purposeOptions.map(({ id, icon: Icon, available, requirement }) => (
                                <label
                                    key={id}
                                    className={`flex items-start gap-3 rounded-lg border p-3 transition-colors ${
                                        purpose === id
                                            ? 'border-primary bg-primary/5'
                                            : 'border-border bg-surface-2/50'
                                    } ${available ? 'cursor-pointer hover:border-primary/50' : 'opacity-60'}`}
                                >
                                    <input
                                        type="radio"
                                        name="purpose"
                                        checked={purpose === id}
                                        disabled={!available}
                                        onChange={() => setPurpose(id)}
                                        className="mt-1"
                                    />
                                    <Icon className="mt-0.5 h-4 w-4 shrink-0 text-fg-subtle" />
                                    <div className="min-w-0">
                                        <div className="text-sm font-medium text-fg">{t(`domains.add.purpose.${id}` as Parameters<typeof t>[0])}</div>
                                        <p className="text-xs text-fg-subtle">{t(`domains.add.purpose.${id}.desc` as Parameters<typeof t>[0])}</p>
                                        {requirement && (
                                            <p className="mt-1 text-xs font-medium text-warning">{requirement}</p>
                                        )}
                                    </div>
                                </label>
                            ))}
                        </div>
                    </div>

                    {/* The PHP version picker used to live here. It moved to the
                        site's own settings (D-013): choosing an interpreter is
                        not part of adding a domain, and advertising PHP on a
                        server that has none contradicted "what isn't installed
                        is invisible".
                        PHP sürüm seçici burada dururdu. Sitenin kendi ayarlarına
                        taşındı (D-013): yorumlayıcı seçmek domain eklemenin
                        parçası değildir ve PHP'si olmayan bir sunucuda PHP'yi
                        reklam etmek "kurulu olmayan görünmez" ilkesine aykırıydı. */}

                    {purpose !== 'dnsonly' && (
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
