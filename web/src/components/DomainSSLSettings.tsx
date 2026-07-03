import { useState, useEffect } from 'react';
import { Shield, CheckCircle, AlertTriangle, XCircle, Settings as SettingsIcon, Lock, Upload } from 'lucide-react';
import { showToast } from './Toast';

interface DomainSSLSettingsProps {
    domainId: number;
    domainName: string;
}

interface SSLCertificate {
    id: number;
    type: string;
    issuer: string;
    subject: string;
    issued_at: string;
    expires_at: string;
    days_until_expiry: number;
    auto_renew: boolean;
    renewal_status: string;
    status: string;
}

interface SSLSettings {
    force_https: boolean;
    hsts_enabled: boolean;
    hsts_max_age: number;
}

interface SSLData {
    domain_id: number;
    domain_name: string;
    has_certificate: boolean;
    certificate?: SSLCertificate;
    settings: SSLSettings;
}

export function DomainSSLSettings({ domainId, domainName }: DomainSSLSettingsProps) {
    const [data, setData] = useState<SSLData | null>(null);
    const [loading, setLoading] = useState(true);
    const [issuing, setIssuing] = useState(false);
    const [email, setEmail] = useState('');
    const [autoRenew, setAutoRenew] = useState(true);

    // Custom certificate states
    const [certSource, setCertSource] = useState<'letsencrypt' | 'custom'>('letsencrypt');
    const [certFile, setCertFile] = useState<File | null>(null);
    const [keyFile, setKeyFile] = useState<File | null>(null);
    const [chainFile, setChainFile] = useState<File | null>(null);
    const [uploading, setUploading] = useState(false);

    useEffect(() => {
        loadSSLData();
    }, [domainId]);

    const loadSSLData = async () => {
        setLoading(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/ssl`);
            if (res.ok) {
                const sslData = await res.json();
                setData(sslData);
            } else {
                showToast('error', 'Failed to load SSL data');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to load SSL data');
        } finally {
            setLoading(false);
        }
    };

    const handleIssueLetsEncrypt = async () => {
        if (!email) {
            showToast('error', 'Please enter an email address');
            return;
        }

        if (!confirm(`Issue Let's Encrypt certificate for ${domainName}?\n\nThis will:\n- Validate domain ownership\n- Issue a free SSL certificate\n- Configure HTTPS\n\nContinue?`)) {
            return;
        }

        setIssuing(true);
        try {
            const res = await fetch(`/api/v1/domains/${domainId}/ssl/letsencrypt`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ email, auto_renew: autoRenew })
            });

            if (res.ok) {
                showToast('success', 'SSL certificate issued successfully!');
                loadSSLData();
            } else {
                const error = await res.text();
                showToast('error', `Failed to issue certificate: ${error}`);
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to issue certificate');
        } finally {
            setIssuing(false);
        }
    };

    const handleUploadCertificate = async (e: React.FormEvent) => {
        e.preventDefault();

        if (!certFile || !keyFile) {
            showToast('error', 'Certificate and private key are required');
            return;
        }

        if (!confirm(`Upload custom SSL certificate for ${domainName}?`)) {
            return;
        }

        setUploading(true);
        try {
            const formData = new FormData();
            formData.append('certificate', certFile);
            formData.append('private_key', keyFile);
            if (chainFile) {
                formData.append('chain', chainFile);
            }

            const res = await fetch(`/api/v1/domains/${domainId}/ssl/upload`, {
                method: 'POST',
                body: formData
            });

            if (res.ok) {
                showToast('success', 'SSL certificate installed successfully!');
                setCertFile(null);
                setKeyFile(null);
                setChainFile(null);
                loadSSLData();
            } else {
                const error = await res.text();
                showToast('error', `Failed to upload certificate: ${error}`);
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to upload certificate');
        } finally {
            setUploading(false);
        }
    };

    const handleUpdateSettings = async (updates: Partial<SSLSettings>) => {
        if (!data) return;

        const newSettings = { ...data.settings, ...updates };

        try {
            const res = await fetch(`/api/v1/domains/${domainId}/ssl/settings`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(newSettings)
            });

            if (res.ok) {
                showToast('success', 'SSL settings updated');
                loadSSLData();
            } else {
                showToast('error', 'Failed to update settings');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to update settings');
        }
    };

    const handleDeleteCertificate = async () => {
        if (!confirm(`Remove SSL certificate from ${domainName}?\n\nThis will disable HTTPS for this domain.`)) {
            return;
        }

        try {
            const res = await fetch(`/api/v1/domains/${domainId}/ssl`, {
                method: 'DELETE'
            });

            if (res.ok) {
                showToast('success', 'SSL certificate removed');
                loadSSLData();
            } else {
                showToast('error', 'Failed to remove certificate');
            }
        } catch (err) {
            console.error(err);
            showToast('error', 'Failed to remove certificate');
        }
    };

    const getCertificateStatusIcon = (cert?: SSLCertificate) => {
        if (!cert) return <XCircle className="w-6 h-6 text-fg-subtle" />;
        if (cert.days_until_expiry < 0) return <XCircle className="w-6 h-6 text-danger" />;
        if (cert.days_until_expiry < 30) return <AlertTriangle className="w-6 h-6 text-warning" />;
        return <CheckCircle className="w-6 h-6 text-success" />;
    };

    const getCertificateStatusText = (cert?: SSLCertificate) => {
        if (!cert) return 'No Certificate';
        if (cert.days_until_expiry < 0) return 'Expired';
        if (cert.days_until_expiry < 30) return 'Expiring Soon';
        return 'Valid';
    };

    if (loading) {
        return (
            <div className="flex items-center justify-center h-64">
                <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
            </div>
        );
    }

    if (!data) {
        return <div className="text-danger">Failed to load SSL data</div>;
    }

    return (
        <div className="space-y-6">
            <div>
                <h3 className="text-lg font-bold text-fg mb-2">SSL/TLS Certificate</h3>
                <p className="text-sm text-fg-muted">
                    Manage SSL certificates and HTTPS settings for {domainName}
                </p>
            </div>

            {/* Certificate Status */}
            <div className="bg-surface-2/50 rounded-lg p-6 border border-border">
                <div className="flex items-start gap-4">
                    <div className="mt-1">
                        {getCertificateStatusIcon(data.certificate)}
                    </div>
                    <div className="flex-1">
                        <h4 className="text-md font-semibold text-fg mb-2">
                            Certificate Status: {getCertificateStatusText(data.certificate)}
                        </h4>

                        {data.has_certificate && data.certificate ? (
                            <div className="space-y-2">
                                <div className="grid grid-cols-2 gap-4 text-sm">
                                    <div>
                                        <span className="text-fg-muted">Type:</span>
                                        <span className="ml-2 text-fg">{data.certificate.type === 'letsencrypt' ? "Let's Encrypt" : 'Custom'}</span>
                                    </div>
                                    <div>
                                        <span className="text-fg-muted">Issuer:</span>
                                        <span className="ml-2 text-fg">{data.certificate.issuer}</span>
                                    </div>
                                    <div>
                                        <span className="text-fg-muted">Expires:</span>
                                        <span className="ml-2 text-fg">
                                            {new Date(data.certificate.expires_at).toLocaleDateString()}
                                            <span className={`ml-2 ${data.certificate.days_until_expiry < 30 ? 'text-warning' : 'text-success'}`}>
                                                ({data.certificate.days_until_expiry} days)
                                            </span>
                                        </span>
                                    </div>
                                    {data.certificate.type === 'letsencrypt' && (
                                        <div>
                                            <span className="text-fg-muted">Auto-renew:</span>
                                            <span className="ml-2 text-fg">
                                                {data.certificate.auto_renew ? '✅ Enabled' : '❌ Disabled'}
                                            </span>
                                        </div>
                                    )}
                                </div>
                                <button
                                    onClick={handleDeleteCertificate}
                                    className="mt-4 px-4 py-2 bg-danger text-white rounded hover:bg-danger text-sm"
                                >
                                    Remove Certificate
                                </button>
                            </div>
                        ) : (
                            <p className="text-sm text-fg-muted">
                                No SSL certificate installed. Issue a free Let's Encrypt certificate or upload your own.
                            </p>
                        )}
                    </div>
                </div>
            </div>

            {/* Let's Encrypt Section */}
            {!data.has_certificate && (
                <div className="bg-surface-2/50 rounded-lg p-6 border border-border">
                    {/* Tab Selection */}
                    <div className="flex border-b border-border mb-6">
                        <button
                            onClick={() => setCertSource('letsencrypt')}
                            className={`px-4 py-2 text-sm font-medium transition-colors ${certSource === 'letsencrypt'
                                ? 'text-primary border-b-2 border-primary'
                                : 'text-fg-muted hover:text-fg-muted'
                                }`}
                        >
                            <div className="flex items-center gap-2">
                                <Shield className="w-4 h-4" />
                                Let's Encrypt (Ücretsiz)
                            </div>
                        </button>
                        <button
                            onClick={() => setCertSource('custom')}
                            className={`px-4 py-2 text-sm font-medium transition-colors ${certSource === 'custom'
                                ? 'text-primary border-b-2 border-primary'
                                : 'text-fg-muted hover:text-fg-muted'
                                }`}
                        >
                            <div className="flex items-center gap-2">
                                <Upload className="w-4 h-4" />
                                Custom Certificate
                            </div>
                        </button>
                    </div>

                    {/* Let's Encrypt Form */}
                    {certSource === 'letsencrypt' && (
                        <>
                            <div className="flex items-center gap-2 mb-4">
                                <Shield className="w-5 h-5 text-success" />
                                <h4 className="text-md font-semibold text-fg">Let's Encrypt Certificate</h4>
                            </div>
                            <p className="text-sm text-fg-muted mb-4">
                                Ücretsiz SSL sertifikası alın. Otomatik yenileme ile 90 günlük sertifika.
                            </p>
                            <div className="space-y-4">
                                <div>
                                    <label className="block text-sm text-fg-muted mb-2">Email Address</label>
                                    <input
                                        type="email"
                                        value={email}
                                        onChange={(e) => setEmail(e.target.value)}
                                        placeholder="admin@example.com"
                                        className="w-full bg-surface border border-border rounded px-4 py-2 text-fg focus:border-primary focus:outline-none"
                                    />
                                    <p className="text-xs text-fg-subtle mt-1">
                                        Yenileme bildirimleri ve hesap kurtarma için kullanılır
                                    </p>
                                </div>
                                <label className="flex items-center gap-2 cursor-pointer">
                                    <input
                                        type="checkbox"
                                        checked={autoRenew}
                                        onChange={(e) => setAutoRenew(e.target.checked)}
                                        className="w-4 h-4 bg-surface border-border rounded focus:ring-primary"
                                    />
                                    <span className="text-sm text-fg">Otomatik yenileme aktif</span>
                                </label>
                                <button
                                    onClick={handleIssueLetsEncrypt}
                                    disabled={issuing || !email}
                                    className="px-6 py-2 bg-success text-white rounded hover:bg-success disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2"
                                >
                                    <Lock className="w-4 h-4" />
                                    {issuing ? 'Sertifika alınıyor...' : 'Sertifika Al'}
                                </button>
                            </div>
                        </>
                    )}

                    {/* Custom Certificate Form */}
                    {certSource === 'custom' && (
                        <>
                            <div className="flex items-center gap-2 mb-4">
                                <Upload className="w-5 h-5 text-primary" />
                                <h4 className="text-md font-semibold text-fg">Custom Certificate Upload</h4>
                            </div>
                            <p className="text-sm text-fg-muted mb-4">
                                Başka bir sağlayıcıdan (Comodo, DigiCert, vb.) aldığınız sertifikayı yükleyin.
                            </p>
                            <form onSubmit={handleUploadCertificate} className="space-y-4">
                                <div>
                                    <label className="block text-sm text-fg-muted mb-2">
                                        Certificate (PEM format) <span className="text-danger">*</span>
                                    </label>
                                    <input
                                        type="file"
                                        accept=".pem,.crt,.cer"
                                        onChange={(e) => setCertFile(e.target.files?.[0] || null)}
                                        className="w-full bg-surface border border-border rounded px-4 py-2 text-white file:mr-4 file:py-1 file:px-4 file:rounded file:border-0 file:bg-primary file:text-white file:cursor-pointer"
                                    />
                                </div>
                                <div>
                                    <label className="block text-sm text-fg-muted mb-2">
                                        Private Key (PEM format) <span className="text-danger">*</span>
                                    </label>
                                    <input
                                        type="file"
                                        accept=".pem,.key"
                                        onChange={(e) => setKeyFile(e.target.files?.[0] || null)}
                                        className="w-full bg-surface border border-border rounded px-4 py-2 text-white file:mr-4 file:py-1 file:px-4 file:rounded file:border-0 file:bg-primary file:text-white file:cursor-pointer"
                                    />
                                </div>
                                <div>
                                    <label className="block text-sm text-fg-muted mb-2">
                                        CA Bundle / Chain (opsiyonel)
                                    </label>
                                    <input
                                        type="file"
                                        accept=".pem,.crt,.cer"
                                        onChange={(e) => setChainFile(e.target.files?.[0] || null)}
                                        className="w-full bg-surface border border-border rounded px-4 py-2 text-fg file:mr-4 file:py-1 file:px-4 file:rounded file:border-0 file:bg-surface-3 file:text-fg file:cursor-pointer"
                                    />
                                    <p className="text-xs text-fg-subtle mt-1">
                                        Intermediate sertifikaları içerir
                                    </p>
                                </div>
                                <button
                                    type="submit"
                                    disabled={uploading || !certFile || !keyFile}
                                    className="px-6 py-2 bg-primary text-white rounded hover:bg-primary-hover disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2"
                                >
                                    <Upload className="w-4 h-4" />
                                    {uploading ? 'Yükleniyor...' : 'Sertifika Yükle'}
                                </button>
                            </form>
                        </>
                    )}
                </div>
            )}

            {/* SSL Settings */}
            <div className="bg-surface-2/50 rounded-lg p-6 border border-border">
                <div className="flex items-center gap-2 mb-4">
                    <SettingsIcon className="w-5 h-5 text-primary" />
                    <h4 className="text-md font-semibold text-fg">HTTPS Settings</h4>
                </div>
                <div className="space-y-3">
                    <label className="flex items-center gap-3 cursor-pointer">
                        <input
                            type="checkbox"
                            checked={data.settings.force_https}
                            onChange={(e) => handleUpdateSettings({ force_https: e.target.checked })}
                            className="w-4 h-4 bg-surface border-border rounded focus:ring-primary"
                        />
                        <div>
                            <div className="text-fg text-sm">Force HTTPS</div>
                            <div className="text-xs text-fg-muted">
                                Redirect all HTTP requests to HTTPS
                            </div>
                        </div>
                    </label>
                    <label className="flex items-center gap-3 cursor-pointer">
                        <input
                            type="checkbox"
                            checked={data.settings.hsts_enabled}
                            onChange={(e) => handleUpdateSettings({ hsts_enabled: e.target.checked })}
                            className="w-4 h-4 bg-surface border-border rounded focus:ring-primary"
                        />
                        <div>
                            <div className="text-fg text-sm">Enable HSTS</div>
                            <div className="text-xs text-fg-muted">
                                HTTP Strict Transport Security (recommended)
                            </div>
                        </div>
                    </label>
                </div>
            </div>
        </div>
    );
}
