import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const appSource = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8');
const routerSource = readFileSync(new URL('../src/router.tsx', import.meta.url), 'utf8');

test('the continuously mounted router wraps auth and every operation provider', () => {
  const appStart = appSource.indexOf('function App()');
  const rootTree = appSource.slice(appStart);
  const systemProviderStart = rootTree.indexOf('<SystemUpdateOperationProvider>');
  const routerStart = rootTree.indexOf('<BrowserRouter>');
  const authGate = rootTree.indexOf('<AuthGate />');
  const routerEnd = rootTree.indexOf('</BrowserRouter>');
  const systemProviderEnd = rootTree.indexOf('</SystemUpdateOperationProvider>');

  const authGateStart = appSource.indexOf('function AuthGate()');
  const authGateEnd = appSource.indexOf('function App()', authGateStart);
  const authenticatedTree = appSource.slice(authGateStart, authGateEnd);
  const providerStart = authenticatedTree.indexOf('<ComponentOperationProvider>');
  const providerEnd = authenticatedTree.indexOf('</ComponentOperationProvider>');

  assert.ok(appStart >= 0, 'App must exist');
  assert.ok(authGateStart >= 0, 'AuthGate must exist');
  assert.ok(systemProviderStart >= 0, 'SystemUpdateOperationProvider must exist');
  assert.ok(routerStart > systemProviderStart, 'the router must stay inside the system-update provider');
  assert.ok(authGate > routerStart, 'the router must stay mounted while AuthGate changes authentication views');
  assert.ok(routerEnd > authGate, 'the router must close after AuthGate');
  assert.ok(systemProviderEnd > routerEnd, 'the system-update provider must close after the router');
  assert.ok(providerStart >= 0, 'ComponentOperationProvider must exist');
  assert.ok(providerEnd > providerStart, 'ComponentOperationProvider must be closed');
  assert.doesNotMatch(authenticatedTree, /<BrowserRouter>/, 'AuthGate must not remount the router');
});

test('AppRoutes does not create a nested router', () => {
  const routesStart = appSource.indexOf('function AppRoutes()');
  const routesEnd = appSource.indexOf('function AuthGate()', routesStart);
  const routesSource = appSource.slice(routesStart, routesEnd);

  assert.ok(routesStart >= 0 && routesEnd > routesStart, 'AppRoutes source must be found');
  assert.doesNotMatch(routesSource, /<BrowserRouter>/);
});

test('declarative authorization redirects retry after the system navigation blocker releases', () => {
  assert.match(routerSource, /const navigationBlockerListeners = new Set/);
  assert.match(routerSource, /if \(navigationBlocker\?\.current\) return false/);
  const navigate = routerSource.slice(routerSource.indexOf('export function Navigate'), routerSource.indexOf('export interface RouteProps'));
  assert.match(navigate, /navigationBlockerListeners\.add\(attempt\)/);
  assert.match(navigate, /if \(!completed && navigate\(to, \{ replace \}\)\) completed = true/);
  assert.match(navigate, /navigationBlockerListeners\.delete\(attempt\)/);
  const blocker = routerSource.slice(routerSource.indexOf('export function useNavigationBlocker'), routerSource.indexOf('export function useParams'));
  assert.match(blocker, /useLayoutEffect/);
  assert.doesNotMatch(blocker, /useEffect/, 'an initial active marker must register its blocker before passive redirects run');
  assert.ok(blocker.indexOf('navigationBlockers.add(blocked)')
    < blocker.indexOf('for (const listener of navigationBlockerListeners) listener()'));
  assert.match(blocker, /for \(const listener of navigationBlockerListeners\) listener\(\)/);
  const link = routerSource.slice(routerSource.indexOf('export function Link'));
  assert.doesNotMatch(link, /navigationBlockerListeners\.add/, 'ordinary link clicks must never be queued');
});

test('blocked unmanaged popstate restores the exact accepted entry without moving the history cursor', () => {
  const pop = routerSource.slice(routerSource.indexOf('const onPopState'), routerSource.indexOf('window.addEventListener', routerSource.indexOf('const onPopState')));
  assert.match(pop, /const accepted = acceptedEntryRef\.current/);
  assert.match(pop, /window\.history\.replaceState\(accepted\.state, '', accepted\.url\)/);
  const restored = pop.indexOf('window.history.replaceState(accepted.state');
  const blockedReturn = pop.indexOf('return;', restored);
  const acceptedCursorMutation = pop.indexOf('historyIndexRef.current = target.index');
  assert.ok(restored >= 0 && blockedReturn > restored && acceptedCursorMutation > blockedReturn);
});

test('trusted URL cleanup synchronizes history, accepted entry and router location', () => {
  assert.match(routerSource, /export function replaceCurrentRouterURL/);
  const replacement = routerSource.slice(
    routerSource.indexOf('const replaceCurrentURL'),
    routerSource.indexOf('const value = useMemo'),
  );
  assert.match(replacement, /replaceRouterHistoryURL/);
  assert.match(replacement, /acceptedEntryRef\.current = \{ state: replaced\.state, url: replaced\.url \}/);
  assert.match(replacement, /setLocation\(browserLocation\(\)\)/);
  assert.match(replacement, /useLayoutEffect/);
  assert.doesNotMatch(replacement, /navigationBlocker/);
});
