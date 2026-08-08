import { BrowserRouter, Routes, Route, Navigate, useNavigate, useParams, useLocation } from './router';
import { Component, lazy, Suspense, useState, useEffect, useRef, type ErrorInfo, type ReactNode } from 'react';
import { Login } from './components/Login';
import { api, type CurrentUser } from './lib/api';
import { AuthProvider, useAuth } from './auth/AuthContext';
import { navItems, navItemsForRole, canAccessPath } from './nav';
import { Layout } from './components/Layout';
import { ComponentOperationProvider } from './components/ComponentOperation';
import { useI18n } from './i18n';

const Dashboard = lazy(() => import('./components/Dashboard').then((module) => ({ default: module.Dashboard })));
const Domains = lazy(() => import('./components/Domains').then((module) => ({ default: module.Domains })));
const DomainDetail = lazy(() => import('./components/DomainDetail').then((module) => ({ default: module.DomainDetail })));
const DatabaseManagementV2 = lazy(() => import('./components/DatabaseManagementV2').then((module) => ({ default: module.DatabaseManagementV2 })));
const ServiceList = lazy(() => import('./components/ServiceList').then((module) => ({ default: module.ServiceList })));
const MonitoringPage = lazy(() => import('./components/MonitoringPage').then((module) => ({ default: module.MonitoringPage })));
const ConfigEditor = lazy(() => import('./components/ConfigEditor').then((module) => ({ default: module.ConfigEditor })));
const Settings = lazy(() => import('./components/Settings').then((module) => ({ default: module.Settings })));
const UsersPage = lazy(() => import('./components/UsersPage').then((module) => ({ default: module.UsersPage })));
const ImportPage = lazy(() => import('./components/ImportPage').then((module) => ({ default: module.ImportPage })));
const AuditLogPage = lazy(() => import('./components/AuditLogPage').then((module) => ({ default: module.AuditLogPage })));
const AddonsPage = lazy(() => import('./components/AddonsPage').then((module) => ({ default: module.AddonsPage })));
const VPNPage = lazy(() => import('./components/VPNPage').then((module) => ({ default: module.VPNPage })));
const PHPManagement = lazy(() => import('./components/PHPManagement').then((module) => ({ default: module.PHPManagement })));
const NginxManagement = lazy(() => import('./components/NginxManagement').then((module) => ({ default: module.NginxManagement })));
const Fail2banManagement = lazy(() => import('./components/Fail2banManagement').then((module) => ({ default: module.Fail2banManagement })));
const PostfixManagement = lazy(() => import('./components/PostfixManagement').then((module) => ({ default: module.PostfixManagement })));
const DovecotManagement = lazy(() => import('./components/DovecotManagement').then((module) => ({ default: module.DovecotManagement })));
const PowerDNSManagement = lazy(() => import('./components/PowerDNSManagement').then((module) => ({ default: module.PowerDNSManagement })));
const VsftpdManagement = lazy(() => import('./components/VsftpdManagement').then((module) => ({ default: module.VsftpdManagement })));
const PostgreSQLManagement = lazy(() => import('./components/PostgreSQLManagement').then((module) => ({ default: module.PostgreSQLManagement })));
const MariaDBManagement = lazy(() => import('./components/MariaDBManagement').then((module) => ({ default: module.MariaDBManagement })));
const ComponentDetail = lazy(() => import('./components/ComponentDetail').then((module) => ({ default: module.ComponentDetail })));

// Domain Detail Wrapper - fetches domain ID from domain name
function DomainDetailPage() {
  const { domainName } = useParams();
  const navigate = useNavigate();
  const [domainId, setDomainId] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);
  const requestIdRef = useRef(0);

  useEffect(() => {
    const controller = new AbortController();
    const requestId = ++requestIdRef.current;
    setDomainId(null);
    setLoading(true);

    const fetchDomain = async () => {
      try {
        if (!domainName) {
          navigate('/domains');
          return;
        }

        const res = await fetch('/api/v1/domains', {
          signal: controller.signal,
          cache: 'no-store',
        });
        if (!res.ok) {
          throw new Error(`Failed to fetch domains: ${res.status}`);
        }

        const domains: Array<{ id: number; domain_name: string }> = await res.json();
        if (controller.signal.aborted || requestIdRef.current !== requestId) {
          return;
        }

        const domain = domains.find((item) => item.domain_name === domainName);
        if (domain) {
          setDomainId(domain.id);
        } else {
          navigate('/domains');
        }
      } catch (err) {
        if (controller.signal.aborted || requestIdRef.current !== requestId) {
          return;
        }
        console.error('Failed to fetch domain:', err);
        navigate('/domains');
      } finally {
        if (!controller.signal.aborted && requestIdRef.current === requestId) {
          setLoading(false);
        }
      }
    };
    fetchDomain();

    return () => {
      controller.abort();
    };
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
        key={domainId}
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
  onSelectConfig?: (path: string) => void;
}

function ServiceManagement({ serviceId, versions, onSelectConfig }: ServiceManagementProps) {
  const navigate = useNavigate();
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
      return <DovecotManagement onBack={onBack} onSelectConfig={onSelectConfig} />;
    case 'pdns':
      return <PowerDNSManagement onBack={onBack} />;
    case 'vsftpd':
      return <VsftpdManagement onBack={onBack} onSelectConfig={onSelectConfig} />;
    case 'postgresql':
      return <PostgreSQLManagement onBack={onBack} />;
    case 'mariadb':
      return <MariaDBManagement onBack={onBack} />;
    default:
      // Every other component gets the DERIVED page — status, actions, unit,
      // versions, packages, ports, config files and its own journal — instead
      // of the dead end that used to sit here (operator, 25 Jul: "birçok
      // servisin manage'i doğru düzgün çalışmıyor"). The specialised pages
      // above stay because they do more than describe; this one needs no entry
      // in any list, so a component added tomorrow is manageable at once.
      // Geri kalan her bileşen, burada eskiden duran çıkmaz sokak yerine
      // TÜRETİLMİŞ sayfayı alır: durum, eylemler, unit, sürümler, paketler,
      // portlar, ayar dosyaları ve kendi günlüğü (operatör, 25 Tem: "birçok
      // servisin manage'i doğru düzgün çalışmıyor"). Yukarıdaki özel sayfalar
      // betimlemekten fazlasını yaptıkları için kalır; bunun hiçbir listeye
      // girmesi gerekmez, yani yarın eklenen bileşen anında yönetilebilir.
      return <ComponentDetail serviceId={serviceId} onBack={onBack} onSelectConfig={onSelectConfig} />;
  }
}



function ServiceManagementPage() {
  const { serviceId } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const navigationState = location.state as { versions?: string[] } | null;
  const [versions, setVersions] = useState<string[]>(navigationState?.versions || []);
  const [loading, setLoading] = useState(!navigationState?.versions);
  // Config files listed on the generic page open in the same editor the
  // Components page uses — one editor, not a second copy.
  // Genel sayfada listelenen ayar dosyaları, Bileşenler sayfasının kullandığı
  // editörde açılır — tek editör, ikinci bir kopya değil.
  const [configPath, setConfigPath] = useState<string | null>(null);

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
  if (configPath) {
    return <ConfigEditor path={configPath} onBack={() => setConfigPath(null)} />;
  }

  return <ServiceManagement serviceId={serviceId!} versions={versions} onSelectConfig={setConfigPath} />;
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
        // WireGuard's real management — peers, client configs, QR codes —
        // lives on the VPN page; a generic detail page next to it would be a
        // second, poorer door to the same room (operator, 25 Jul).
        // WireGuard'ın gerçek yönetimi — istemciler, yapılandırmalar, QR —
        // VPN sayfasındadır; yanına genel bir detay sayfası koymak aynı odaya
        // ikinci ve daha kötü bir kapı olurdu (operatör, 25 Tem).
        if (serviceId === 'wireguard') {
          navigate('/vpn');
          return;
        }
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
function PageWithLayout({ children }: { children: ReactNode }) {
  const location = useLocation();
  const currentPath = location.pathname;
  return (
    <MainLayout currentPath={currentPath}>
      <RouteLoadBoundary key={currentPath}>
        <Suspense fallback={<PageLoading />}>{children}</Suspense>
      </RouteLoadBoundary>
    </MainLayout>
  );
}

function PageLoading() {
  const { t } = useI18n();
  return (
    <div className="flex min-h-64 items-center justify-center" role="status" aria-label={t('common.loading')}>
      <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary" />
    </div>
  );
}

interface RouteLoadErrorBoundaryProps {
  children: ReactNode;
  message: string;
  reloadLabel: string;
}

interface RouteLoadErrorBoundaryState {
  failed: boolean;
}

class RouteLoadErrorBoundary extends Component<RouteLoadErrorBoundaryProps, RouteLoadErrorBoundaryState> {
  state: RouteLoadErrorBoundaryState = { failed: false };

  static getDerivedStateFromError(): RouteLoadErrorBoundaryState {
    return { failed: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('Page bundle failed to load', error, info);
  }

  render() {
    if (!this.state.failed) return this.props.children;

    return (
      <div className="mx-auto flex min-h-64 max-w-lg flex-col items-center justify-center gap-4 rounded-xl border border-border bg-surface p-8 text-center">
        <p className="text-sm text-fg-muted">{this.props.message}</p>
        <button
          type="button"
          className="rounded-lg bg-primary px-4 py-2 text-sm font-medium text-white hover:bg-primary-hover"
          onClick={() => window.location.reload()}
        >
          {this.props.reloadLabel}
        </button>
      </div>
    );
  }
}

function RouteLoadBoundary({ children }: { children: ReactNode }) {
  const { t } = useI18n();
  return (
    <RouteLoadErrorBoundary
      message={t('app.pageLoadFailed')}
      reloadLabel={t('app.reload')}
    >
      {children}
    </RouteLoadErrorBoundary>
  );
}

function AppRoutes() {
  return (
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
      <BrowserRouter>
        <ComponentOperationProvider>
          <AppRoutes />
        </ComponentOperationProvider>
      </BrowserRouter>
    </AuthProvider>
  );
}

function App() {
  return <AuthGate />;
}

export default App;
