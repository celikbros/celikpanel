import { BrowserRouter, Routes, Route, Navigate, useNavigate, useParams, useLocation } from 'react-router-dom';
import { useState, useEffect } from 'react';
import { Login } from './components/Login';
import { api, type CurrentUser } from './lib/api';
import { Layout } from './components/Layout';
import { Dashboard } from './components/Dashboard';
import { Domains } from './components/Domains';
import { DomainDetail } from './components/DomainDetail';
import { DatabaseManagementV2 } from './components/DatabaseManagementV2';
import { ServiceList } from './components/ServiceList';
import { ConfigEditor } from './components/ConfigEditor';
import { Settings } from './components/Settings';
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
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-500"></div>
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
  const onBack = () => navigate('/services');

  switch (serviceId) {
    case 'php-fpm':
      return <PHPManagement initialVersion={versions[0]} availableVersions={versions} onBack={onBack} />;
    case 'nginx':
      return <NginxManagement initialVersion={versions[0]} onBack={onBack} />;
    case 'fail2ban':
      return <Fail2banManagement initialVersion={versions[0]} onBack={onBack} />;
    case 'postfix':
      return <PostfixManagement initialVersion={versions[0]} onBack={onBack} />;
    case 'dovecot':
      return <DovecotManagement initialVersion={versions[0]} onBack={onBack} />;
    case 'pdns':
      return <PowerDNSManagement initialVersion={versions[0]} onBack={onBack} />;
    case 'vsftpd':
      return <VsftpdManagement initialVersion={versions[0]} onBack={onBack} />;
    case 'postgresql':
      return <PostgreSQLManagement initialVersion={versions[0]} onBack={onBack} />;
    case 'mariadb':
      return <MariaDBManagement initialVersion={versions[0]} onBack={onBack} />;
    default:
      return (
        <div className="p-8 text-center">
          <h2 className="text-xl font-bold mb-4">Management UI for {serviceId}</h2>
          <p className="text-gray-400 mb-4">Coming soon...</p>
          <button onClick={onBack} className="px-4 py-2 bg-gray-800 rounded hover:bg-gray-700">
            Go Back
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
        .then((services: any[]) => {
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
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-500"></div>
      </div>
    );
  }

  if (!versions.length) return null;

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

// Main Layout with navigation
function MainLayout({ children, currentPath }: { children: React.ReactNode; currentPath: string }) {
  const navigate = useNavigate();

  const getPageFromPath = (path: string) => {
    if (path === '/') return 'dashboard';
    if (path.startsWith('/domains')) return 'domains';
    if (path.startsWith('/databases')) return 'databases';
    if (path.startsWith('/services')) return 'services';
    if (path.startsWith('/settings')) return 'settings';
    return 'dashboard';
  };

  const handlePageChange = (page: string) => {
    switch (page) {
      case 'dashboard':
        navigate('/');
        break;
      case 'domains':
        navigate('/domains');
        break;
      case 'databases':
        navigate('/databases');
        break;
      case 'services':
        navigate('/services');
        break;
      case 'settings':
        navigate('/settings');
        break;
    }
  };

  return (
    <Layout currentPage={getPageFromPath(currentPath)} onPageChange={handlePageChange}>
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
      <div className="min-h-screen flex items-center justify-center bg-gray-950">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-500"></div>
      </div>
    );
  }

  if (!user) {
    return <Login onSuccess={setUser} />;
  }

  return <AppRoutes />;
}

function App() {
  return <AuthGate />;
}

export default App;
