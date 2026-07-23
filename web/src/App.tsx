import { BrowserRouter, Routes, Route, Navigate, useNavigate, useParams, useLocation } from 'react-router-dom';
import { useState, useEffect } from 'react';
import { useI18n } from './i18n';
import { Login } from './components/Login';
import { api, type CurrentUser } from './lib/api';
import { AuthProvider, useAuth } from './auth/AuthContext';
import { navItems, navItemsForRole, canAccessPath } from './nav';
import { Layout } from './components/Layout';
import { Dashboard } from './components/Dashboard';
import { Domains } from './components/Domains';
import { DomainDetail } from './components/DomainDetail';
import { DatabaseManagementV2 } from './components/DatabaseManagementV2';
import { ServiceList } from './components/ServiceList';
import { MonitoringPage } from './components/MonitoringPage';
import { ConfigEditor } from './components/ConfigEditor';
import { Settings } from './components/Settings';
import { UsersPage } from './components/UsersPage';
import { ImportPage } from './components/ImportPage';
import { AuditLogPage } from './components/AuditLogPage';
import { AddonsPage } from './components/AddonsPage';
import { VPNPage } from './components/VPNPage';
import { PHPManagement } from './components/PHPManagement';
import { NginxManagement } from './components/NginxManagement';
import { Fail2banManagement } from './components/Fail2banManagement';
import { PostfixManagement } from './components/PostfixManagement';
import { DovecotManagement } from './components/DovecotManagement';
import { PowerDNSManagement } from './components/PowerDNSManagement';
import { VsftpdManagement } from './components/VsftpdManagement';
import { PostgreSQLManagement } from './components/PostgreSQLManagement';
import { MariaDBManagement } from './components/MariaDBManagement';

// Domain Detail Wrapper - fetches domain ID from domain name
function DomainDetailPage() {
  const { domainName } = useParams();
  const navigate = useNavigate();
  const [domainId, setDomainId] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchDomain = async () => {
      try {
        const res = await fetch('/api/v1/domains');
        if (res.ok) {
          const domains = await res.json();
          const domain = domains.find((d: any) => d.domain_name === domainName);
          if (domain) {
            setDomainId(domain.id);
          } else {
            navigate('/domains');
          }
        }
      } catch (err) {
        console.error('Failed to fetch domain:', err);
        navigate('/domains');
      } finally {
        setLoading(false);
      }
    };
    fetchDomain();
  }, [domainName, navigate]);

  if (loading) {
    return (
      <PageWithLayout>
        <div className="flex items-center justify-center h-full">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
        </div>
      </PageWithLayout>
    );
  }

  if (!domainId) return null;

  return (
    <PageWithLayout>
      <DomainDetail
        domainId={domainId}
        onBack={() => navigate('/domains')}
      />
    </PageWithLayout>
  );
}

// Service Management Wrapper
interface ServiceManagementProps {
  serviceId: string;
  versions: string[];
}

function ServiceManagement({ serviceId, versions }: ServiceManagementProps) {
  const navigate = useNavigate();
  const { t } = useI18n();
  const onBack = () => navigate('/services');

  switch (serviceId) {
    case 'php-fpm':
      return <PHPManagement versions={versions} onBack={onBack} />;
    case 'nginx':
      return <NginxManagement onBack={onBack} />;
    case 'fail2ban':
      return <Fail2banManagement onBack={onBack} />;
    case 'postfix':
      return <PostfixManagement onBack={onBack} />;
    case 'dovecot':
      return <DovecotManagement onBack={onBack} />;
    case 'pdns':
      return <PowerDNSManagement onBack={onBack} />;
    case 'vsftpd':
      return <VsftpdManagement onBack={onBack} />;
    case 'postgresql':
      return <PostgreSQLManagement onBack={onBack} />;
    case 'mariadb':
      return <MariaDBManagement onBack={onBack} />;
    default:
      // Honest and localized: some services (node — its versions live in the
      // Services page drawer) have no dedicated page, and saying so beats an
      // English-only "Coming soon" that promises nothing in particular.
      // Dürüst ve yerelleştirilmiş: bazı servislerin (node — sürümleri
      // Servisler sayfasındaki çekmecede) özel sayfası yok; bunu söylemek,
      // belirsiz bir şey vadeden İngilizce "Coming soon"dan iyidir.
      return (
        <div className="p-8 text-center">
          <p className="mb-4 text-fg-muted">{t('services.noManagePage')}</p>
          <button onClick={onBack} className="px-4 py-2 bg-surface-2 rounded-lg hover:bg-surface-3">
            {t('common.back')}
          </button>
        </div>
      );
  }
}



function ServiceManagementPage() {
  const { serviceId } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const [versions, setVersions] = useState<string[]>(location.state?.versions || []);
  const [loading, setLoading] = useState(!location.state?.versions);

  useEffect(() => {
    if (!serviceId) return;

    // If versions are not in state (e.g. refresh), fetch them
    if (versions.length === 0) {
      setLoading(true);
      fetch('/api/v1/managed-services')
        .then(res => res.json())
        .then((data: any) => {
          const services: any[] = data?.services || [];
          const service = services.find(s => s.id === serviceId);
          if (service) {
            setVersions(service.versions);
          } else {
            // Service not found
            navigate('/services');
          }
        })
        .catch(() => navigate('/services'))
        .finally(() => setLoading(false));
    }
  }, [serviceId, versions.length, navigate]);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
      </div>
    );
  }

  // No empty-versions bailout: with the "default" sentinel dead (B3b), an
  // installed nginx/postfix legitimately has versions: [] — bailing out here
  // rendered a BLANK page behind every such Manage click.
  // Boş-sürüm kaçışı yok: "default" sentinel'i öldüğünden (B3b) kurulu bir
  // nginx/postfix meşru olarak versions: [] taşır — burada kaçmak, böyle her
  // Yönet tıklamasının arkasında BOŞ sayfa çiziyordu.
  return <ServiceManagement serviceId={serviceId!} versions={versions} />;
}


// Services Page with config editor
function ServicesPage() {
  const [selectedConfigPath, setSelectedConfigPath] = useState<string | null>(null);
  const navigate = useNavigate();

  if (selectedConfigPath) {
    return <ConfigEditor path={selectedConfigPath} onBack={() => setSelectedConfigPath(null)} />;
  }

  return (
    <ServiceList
      onSelectConfig={setSelectedConfigPath}
      onManageService={(serviceId: string, versions: string[]) => {
        // Navigate with state to avoid refetch if possible
        navigate(`/services/${serviceId}`, { state: { versions } });
      }}
    />
  );
}

// Main Layout with navigation. The active nav id and navigation targets
// both come from the shared nav registry, and access is guarded by role.
// Aktif nav kimliği ve navigasyon hedefleri paylaşılan nav kaydından gelir
// ve erişim role göre korunur.
function MainLayout({ children, currentPath }: { children: React.ReactNode; currentPath: string }) {
  const navigate = useNavigate();
  const { role } = useAuth();

  // Longest matching path wins so "/domains/x" resolves to the domains item.
  // En uzun eşleşen yol kazanır; böylece "/domains/x" domains öğesine çözülür.
  const activeId =
    [...navItems]
      .sort((a, b) => b.path.length - a.path.length)
      .find((item) => (item.path === '/' ? currentPath === '/' : currentPath.startsWith(item.path)))?.id ?? 'dashboard';

  const handlePageChange = (id: string) => {
    const target = navItems.find((item) => item.id === id);
    if (target) navigate(target.path);
  };

  // A role that cannot see this section is bounced home. The API would
  // reject the calls anyway; this keeps the UI honest.
  // Bu bölümü göremeyen bir rol eve geri gönderilir. API zaten çağrıları
  // reddederdi; bu, arayüzü dürüst tutar.
  if (!canAccessPath(role, currentPath) && !navItemsForRole(role).some((i) => currentPath.startsWith(i.path) && i.path !== '/')) {
    return <Navigate to="/" replace />;
  }

  return (
    <Layout currentPage={activeId} onPageChange={handlePageChange}>
      {children}
    </Layout>
  );
}

// Page wrapper component that provides layout
function PageWithLayout({ children }: { children: React.ReactNode }) {
  const currentPath = window.location.pathname;
  return <MainLayout currentPath={currentPath}>{children}</MainLayout>;
}

function AppRoutes() {
  return (
    <BrowserRouter>
      <Routes>
        {/* Dashboard */}
        <Route path="/" element={<PageWithLayout><Dashboard /></PageWithLayout>} />

        {/* Domains */}
        <Route path="/domains" element={<PageWithLayout><Domains /></PageWithLayout>} />
        <Route path="/domains/:domainName" element={<DomainDetailPage />} />  {/* Layout handled inside DomainDetailPage */}

        {/* Databases */}
        <Route path="/databases" element={<PageWithLayout><DatabaseManagementV2 /></PageWithLayout>} />

        {/* Services */}
        <Route path="/services" element={<PageWithLayout><ServicesPage /></PageWithLayout>} />
        <Route path="/services/:serviceId" element={<PageWithLayout><ServiceManagementPage /></PageWithLayout>} />
        <Route path="/monitoring" element={<PageWithLayout><MonitoringPage /></PageWithLayout>} />

        {/* Users (admin + reseller) */}
        <Route path="/users" element={<PageWithLayout><UsersPage /></PageWithLayout>} />

        {/* Import (admin) */}
        <Route path="/import" element={<PageWithLayout><ImportPage /></PageWithLayout>} />
        <Route path="/audit" element={<PageWithLayout><AuditLogPage /></PageWithLayout>} />
        <Route path="/addons" element={<PageWithLayout><AddonsPage /></PageWithLayout>} />
        <Route path="/vpn" element={<PageWithLayout><VPNPage /></PageWithLayout>} />

        {/* Settings */}
        <Route path="/settings" element={<PageWithLayout><Settings /></PageWithLayout>} />

        {/* Fallback */}
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  );
}

// AuthGate is the front door: it resolves the current session before any
// page renders, shows the login screen when there is none, and drops back
// to login if a session expires mid-use (any API 401).
//
// AuthGate ön kapıdır: herhangi bir sayfa render edilmeden önce mevcut
// oturumu çözer, oturum yoksa giriş ekranını gösterir ve kullanım
// sırasında oturum düşerse (herhangi bir API 401) girişe geri döner.
function AuthGate() {
  const [user, setUser] = useState<CurrentUser | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api.me().then(setUser).catch(() => setUser(null)).finally(() => setLoading(false));
  }, []);

  // Watch every API response; a 401 means the session is gone, so return
  // to the login screen instead of showing broken pages.
  // Her API yanıtını izle; bir 401 oturumun gittiği anlamına gelir, bozuk
  // sayfalar göstermek yerine giriş ekranına dön.
  useEffect(() => {
    const originalFetch = window.fetch;
    window.fetch = async (...args) => {
      const res = await originalFetch(...args);
      const url = typeof args[0] === 'string' ? args[0] : (args[0] as Request).url;
      if (res.status === 401 && url.includes('/api/') && !url.includes('/auth/login')) {
        setUser(null);
      }
      return res;
    };
    return () => { window.fetch = originalFetch; };
  }, []);

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-bg">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
      </div>
    );
  }

  if (!user) {
    return <Login onSuccess={setUser} />;
  }

  return (
    <AuthProvider user={user} onLogout={() => setUser(null)}>
      <AppRoutes />
    </AuthProvider>
  );
}

function App() {
  return <AuthGate />;
}

export default App;
