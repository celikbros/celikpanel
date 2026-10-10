import { BrowserRouter, Routes, Route, Navigate, useNavigate, useParams, useLocation } from './router';
import {
  Component,
  lazy,
  Suspense,
  useCallback,
  useState,
  useEffect,
  useLayoutEffect,
  useRef,
  type ComponentType,
  type ErrorInfo,
  type LazyExoticComponent,
  type ReactNode,
} from 'react';
import { Login } from './components/Login';
import { LicenseOnboarding } from './components/LicenseOnboarding';
import { usePanelSession } from './auth/usePanelSession';
import { RecoveryAccess } from './components/RecoveryAccess';
import { AccessHold } from './components/AccessHold';
import { AuthProvider, useAuth } from './auth/AuthContext';
import type { CurrentUser } from './lib/api';
import { navItems, canAccessPath, type NavAccessContext } from './nav';
import { Layout } from './components/Layout';
import { ComponentOperationProvider } from './components/ComponentOperation';
import { useI18n } from './i18n';
import { Spinner } from './components/ui';
import { sendIdentified } from './lib/requestIdentity';
import {
  publishSystemUpdateAuthentication,
  shouldApplyUnauthorizedResponse,
} from './lib/systemUpdateAuthSignal';

function lazyNamed<TModule, TKey extends keyof TModule>(
  loader: () => Promise<TModule>,
  name: TKey,
): LazyExoticComponent<Extract<TModule[TKey], ComponentType<any>>> {
  return lazy(async () => ({
    default: (await loader())[name] as Extract<TModule[TKey], ComponentType<any>>,
  }));
}

const SystemUpdateOperationProvider = lazyNamed(() => import('./components/SystemUpdateOperation'), 'SystemUpdateOperationProvider');
const ServerSetupGate = lazyNamed(() => import('./components/ServerSetupGate'), 'ServerSetupGate');
const ServerSetup = lazyNamed(() => import('./components/ServerSetup'), 'ServerSetup');
const Dashboard = lazyNamed(() => import('./components/Dashboard'), 'Dashboard');
const Domains = lazyNamed(() => import('./components/Domains'), 'Domains');
const DomainDetailByName = lazyNamed(() => import('./components/DomainDetail'), 'DomainDetailByName');
const DatabaseManagementV2 = lazyNamed(() => import('./components/DatabaseManagementV2'), 'DatabaseManagementV2');
const ServiceList = lazyNamed(() => import('./components/ServiceList'), 'ServiceList');
const MonitoringPage = lazyNamed(() => import('./components/MonitoringPage'), 'MonitoringPage');
const ConfigEditor = lazyNamed(() => import('./components/ConfigEditor'), 'ConfigEditor');
const Settings = lazyNamed(() => import('./components/Settings'), 'Settings');
const UsersPage = lazyNamed(() => import('./components/UsersPage'), 'UsersPage');
const ImportPage = lazyNamed(() => import('./components/ImportPage'), 'ImportPage');
const AuditLogPage = lazyNamed(() => import('./components/AuditLogPage'), 'AuditLogPage');
const AddonsPage = lazyNamed(() => import('./components/AddonsPage'), 'AddonsPage');
const VPNPage = lazyNamed(() => import('./components/VPNPage'), 'VPNPage');
const PHPManagement = lazyNamed(() => import('./components/PHPManagement'), 'PHPManagement');
const NginxManagement = lazyNamed(() => import('./components/NginxManagement'), 'NginxManagement');
const Fail2banManagement = lazyNamed(() => import('./components/Fail2banManagement'), 'Fail2banManagement');
const PostfixManagement = lazyNamed(() => import('./components/PostfixManagement'), 'PostfixManagement');
const DovecotManagement = lazyNamed(() => import('./components/DovecotManagement'), 'DovecotManagement');
const PowerDNSManagement = lazyNamed(() => import('./components/PowerDNSManagement'), 'PowerDNSManagement');
const VsftpdManagement = lazyNamed(() => import('./components/VsftpdManagement'), 'VsftpdManagement');
const PostgreSQLManagement = lazyNamed(() => import('./components/PostgreSQLManagement'), 'PostgreSQLManagement');
const MariaDBManagement = lazyNamed(() => import('./components/MariaDBManagement'), 'MariaDBManagement');
const ComponentDetail = lazyNamed(() => import('./components/ComponentDetail'), 'ComponentDetail');
const ServiceRecordLookup = lazyNamed(() => import('./components/ServiceRecordLookup'), 'ServiceRecordLookup');

// The page of one domain, addressed by its name. Looking the name up, and
// saying what came of it, belongs to the page's own bundle (DomainDetailByName):
// the person stays on this address whether the list could not be read or has
// no such name. Before 9 Oct 2026 both were a silent return to the list.
//
// Bir alan adının, adıyla adreslenen sayfası. Adın aranması ve sonucunun
// söylenmesi sayfanın kendi paketine aittir: liste okunamasa da, öyle bir ad
// olmasa da kişi bu adreste kalır. 9 Eki 2026'dan önce ikisi de listeye sessiz
// bir dönüştü.
function DomainDetailPage() {
  const { domainName } = useParams();
  const navigate = useNavigate();
  return (
    <PageWithLayout>
      <DomainDetailByName domainName={domainName ?? ''} onBack={() => navigate('/domains')} />
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
  // Config files listed on the generic page open in the same editor the
  // Components page uses — one editor, not a second copy.
  // Genel sayfada listelenen ayar dosyaları, Bileşenler sayfasının kullandığı
  // editörde açılır — tek editör, ikinci bir kopya değil.
  const [configPath, setConfigPath] = useState<string | null>(null);

  // No empty-versions bailout: with the "default" sentinel dead (B3b), an
  // installed nginx/postfix legitimately has versions: [] — bailing out here
  // rendered a BLANK page behind every such Manage click.
  // Boş-sürüm kaçışı yok: "default" sentinel'i öldüğünden (B3b) kurulu bir
  // nginx/postfix meşru olarak versions: [] taşır — burada kaçmak, böyle her
  // Yönet tıklamasının arkasında BOŞ sayfa çiziyordu.
  //
  // After a reload the versions are not in the navigation any more; the lookup
  // reads them and stays on this address whatever that read says.
  // Yeniden yüklemeden sonra sürümler gezinmede yoktur; arama onları okur ve
  // okuma ne derse desin bu adreste kalır.
  return (
    <ServiceRecordLookup
      serviceId={serviceId ?? ''}
      carriedVersions={navigationState?.versions}
      onBack={() => navigate('/services')}
    >
      {(versions) => (configPath
        ? <ConfigEditor path={configPath} onBack={() => setConfigPath(null)} />
        : <ServiceManagement serviceId={serviceId!} versions={versions} onSelectConfig={setConfigPath} />)}
    </ServiceRecordLookup>
  );
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
  const { role, user } = useAuth();
  const navAccess: NavAccessContext = {
    accountType: typeof user?.account_type === 'string' ? user.account_type : undefined,
    teamMembers: user?.features?.team_members === true,
  };

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
  if (!canAccessPath(role, currentPath, navAccess)) {
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
        <Suspense fallback={<PageLoading />}>
          <ScreenCopyGate>{children}</ScreenCopyGate>
        </Suspense>
      </RouteLoadBoundary>
    </MainLayout>
  );
}

// A page waits for its own copy the same way it waits for its own bundle.
// The shell's strings ship with the boot payload; every screen's strings are
// fetched beside the screen (register R-060), so a route must not paint until
// they are here — a key rendering as its own name is worse than a spinner that
// lasts as long as the bundle fetch beside it. The shell around this gate —
// the rail, the identity, the profile menu — is already drawn.
//
// Bir sayfa, kendi paketini beklediği gibi kendi metnini de bekler. Kabuğun
// metni açılış yüküyle gelir; her ekranın metni o ekranın yanında getirilir
// (defter R-060). Anahtarın kendi adıyla çizilmesi, yanındaki paket kadar süren
// bir bekleme göstergesinden kötüdür.
function ScreenCopyGate({ children }: { children: ReactNode }) {
  const { screensReady, screensFailed, t } = useI18n();
  if (screensFailed) return <PageLoadFailed message={t('app.pageLoadFailed')} reloadLabel={t('app.reload')} />;
  if (!screensReady) return <PageLoading />;
  return <>{children}</>;
}

function PageLoadFailed({ message, reloadLabel }: { message: string; reloadLabel: string }) {
  return (
    <div className="mx-auto flex min-h-64 max-w-lg flex-col items-center justify-center gap-4 rounded-xl border border-border bg-surface p-8 text-center">
      <p className="text-sm text-fg-muted">{message}</p>
      <button
        type="button"
        className="rounded-lg bg-primary px-4 py-2 text-sm font-medium text-white hover:bg-primary-hover"
        onClick={() => window.location.reload()}
      >
        {reloadLabel}
      </button>
    </div>
  );
}

function PageLoading() {
  return (
    <div className="flex min-h-64 items-center justify-center">
      <Spinner />
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

    return <PageLoadFailed message={this.props.message} reloadLabel={this.props.reloadLabel} />;
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
        <Route path="/setup" element={<ServerSetup />} />
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
//
// A page that is already mounted is replaced only by a KNOWN negative: a
// confirmed 401, or a sign-out. A session or readiness answer that is merely
// unknown (PANEL_STARTING, AUTH_STATUS_UNAVAILABLE, a failed read) keeps the
// pages mounted and unreachable behind AccessHold until the server answers. The
// full recovery page is for a load on which the application cannot start at all.
//
// Açık bir sayfayı yalnızca BİLİNEN olumsuz sonuç değiştirir: doğrulanmış 401 ya
// da çıkış. Yalnızca bilinmeyen oturum ya da hazır olma yanıtı sayfaları bağlı ve
// erişilmez tutar; tam kurtarma sayfası uygulamanın hiç başlayamadığı yükleme içindir.
function AuthGate() {
  const { user, state, checking, unanswered, afterAnswer, failure, generation: authGenerationRef, retry, transitionAuthentication, markUnavailable } = usePanelSession();
  const [observationRecovery, setObservationRecovery] = useState(false);
  const endSession = useCallback(() => transitionAuthentication(null), [transitionAuthentication]);
  // The identity whose pages are mounted, whether the owner signed out, and
  // whether the session ended under a mounted page. Derived while rendering so
  // that the pages and their hold never disagree for a frame.
  const mounted = useRef<CurrentUser | null>(null);
  const signingOut = useRef(false);
  const sessionEnded = useRef(false);
  if (state === 'ready' && user) {
    mounted.current = user;
    sessionEnded.current = false;
  } else if (state === 'unauthenticated') {
    if (mounted.current) sessionEnded.current = !signingOut.current;
    mounted.current = null;
    signingOut.current = false;
  } else if (user && mounted.current && user.username !== mounted.current.username) {
    mounted.current = null;
  }
  const signOut = useCallback(() => {
    signingOut.current = true;
    transitionAuthentication(null);
  }, [transitionAuthentication]);
  useLayoutEffect(() => {
    // Pause the optional tracker while the independent status surface owns access.
    // This preserves the exact saved operation; it does not end the session.
    publishSystemUpdateAuthentication(state === 'ready' && !observationRecovery && user?.effective_role === 'admin');
  }, [state, user, observationRecovery]);
  useEffect(() => () => publishSystemUpdateAuthentication(false), []);

  // Watch every API response; a 401 means the session is gone, so return
  // to the login screen instead of showing broken pages.
  // Her API yanıtını izle; bir 401 oturumun gittiği anlamına gelir, bozuk
  // sayfalar göstermek yerine giriş ekranına dön.
  useEffect(() => {
    const originalFetch = window.fetch;
    window.fetch = async (...args) => {
      const requestGeneration = authGenerationRef.current;
      // One identity per user action on every state-changing call (D-029).
      // Durum değiştiren her çağrıda kullanıcı eylemi başına tek kimlik.
      const res = await sendIdentified(originalFetch, args[0], args[1]);
      const url = typeof args[0] === 'string' ? args[0] : (args[0] as Request).url;
      if (res.status === 401
        && url.includes('/api/')
        && !url.includes('/auth/login')
        && shouldApplyUnauthorizedResponse(requestGeneration, authGenerationRef.current)) {
        transitionAuthentication(null);
      }
      if ((res.status === 403 || res.status === 503) && url.includes('/api/')
        && shouldApplyUnauthorizedResponse(requestGeneration, authGenerationRef.current)) {
        void res.clone().json().then(problem => {
          if (!shouldApplyUnauthorizedResponse(requestGeneration, authGenerationRef.current)) return;
          // The access route is what that event makes the gate read. Its own
          // refusal is the gate's answer already and must not ask for another read.
          if (['license_required', 'LICENSE_VERIFICATION_UNAVAILABLE', 'LICENSE_STATUS_UNAVAILABLE'].includes(problem.code)
            && !url.includes('/api/v1/license/access')) {
            window.dispatchEvent(new Event('celikpanel:license-locked'));
          }
          if (problem.code === 'AUTH_STATUS_UNAVAILABLE') markUnavailable(true);
          if (problem.code === 'panel_starting' || problem.code === 'PANEL_STARTING') markUnavailable();
        }).catch(() => {});
      }
      return res;
    };
    return () => { window.fetch = originalFetch; };
  }, [transitionAuthentication, markUnavailable, authGenerationRef]);

  // The router keeps the address while the sign-in form is shown, so signing in
  // again opens the same route. What was typed on that page is not kept.
  if (state === 'unauthenticated') return <Login onSuccess={transitionAuthentication} sessionEnded={sessionEnded.current} />;
  const known = state === 'auth_unavailable' ? null : user;
  // Never depends on two state updates landing in one render: without a verified
  // identity in hand, the pages that are mounted stay the ones that are shown.
  const shown = state === 'ready' && user ? user : mounted.current;
  // First read still in flight: nothing has failed yet, so nothing is reported as
  // failed, and RecoveryAccess draws only the page background before the quiet time.
  const cause = state === 'checking' ? 'checking' : state === 'auth_unavailable' || !user ? 'auth' : state === 'starting' ? 'starting' : 'availability';
  // A read that has not answered, also after its own time limit, is still the wait (ninth native record, cell 2),
  // also when an earlier read had answered (then the earlier failure is only "last known"): only an answer is drawn
  // as a failure, and then with what was read.
  if (!shown) return <RecoveryAccess user={known} cause={unanswered ? 'checking' : cause} checking={checking} failure={failure}
    afterAnswer={afterAnswer} onRetry={() => void retry()} onUnauthorized={endSession} />;

  return (
    <AccessHold active={state !== 'ready'} cause={cause === 'checking' ? 'availability' : cause} checking={checking}
      user={known} onRetry={() => void retry()} onUnauthorized={endSession}>
    <AuthProvider key={shown.username} user={shown} onLogout={signOut}>
        <LicenseOnboarding onRecoveryChange={setObservationRecovery} suspended={state !== 'ready'}>
        <Suspense fallback={<PageLoading />}>
          <ComponentOperationProvider>
            <ServerSetupGate><AppRoutes /></ServerSetupGate>
          </ComponentOperationProvider>
        </Suspense>
        </LicenseOnboarding>
    </AuthProvider>
    </AccessHold>
  );
}

// loading: the interface is still being fetched. That is a wait, not a failure,
// so nothing is drawn before the quiet time, and after it the page says what is
// awaited (a read, or with session and readiness confirmed, the interface)
// until a read or the fetch has actually failed (seventh native record, cell 5).
function StandaloneRecovery({ loading = false }: { loading?: boolean }) {
  const { user, state, checking, unanswered, afterAnswer, failure, retry, transitionAuthentication } = usePanelSession();
  const endSession = useCallback(() => transitionAuthentication(null), [transitionAuthentication]);
  if (state === 'unauthenticated') return <Login onSuccess={transitionAuthentication} />;
  const cause = unanswered || state === 'checking' ? 'checking' : state === 'auth_unavailable' ? 'auth' : !user ? 'auth'
    : !loading ? 'bundle' : state === 'starting' ? 'starting' : state === 'availability_unavailable' ? 'availability' : 'loading';
  return <RecoveryAccess user={state === 'auth_unavailable' ? null : user} cause={cause} checking={checking} failure={failure}
    afterAnswer={afterAnswer} onRetry={() => void retry()} onUnauthorized={endSession} />;
}

class RecoveryBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() { return this.state.failed ? <StandaloneRecovery /> : this.props.children; }
}

function App() {
  return (
    <RecoveryBoundary>
      <Suspense fallback={<StandaloneRecovery loading />}>
        <SystemUpdateOperationProvider>
          <BrowserRouter>
            <AuthGate />
          </BrowserRouter>
        </SystemUpdateOperationProvider>
      </Suspense>
    </RecoveryBoundary>
  );
}

export default App;
