import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const appSource = readFileSync(new URL('../src/App.tsx', import.meta.url), 'utf8');

test('component operation failures render inside the router context', () => {
  const authGateStart = appSource.indexOf('function AuthGate()');
  const authenticatedTree = appSource.slice(authGateStart);
  const providerStart = authenticatedTree.indexOf('<ComponentOperationProvider>');
  const providerEnd = authenticatedTree.indexOf('</ComponentOperationProvider>');
  const routerStart = authenticatedTree.indexOf('<BrowserRouter>');
  const routerEnd = authenticatedTree.indexOf('</BrowserRouter>');

  assert.ok(authGateStart >= 0, 'AuthGate must exist');
  assert.ok(providerStart >= 0, 'ComponentOperationProvider must exist');
  assert.ok(routerStart >= 0, 'BrowserRouter must exist in the authenticated tree');
  assert.ok(routerStart < providerStart, 'BrowserRouter must wrap ComponentOperationProvider');
  assert.ok(providerEnd > providerStart, 'ComponentOperationProvider must be closed');
  assert.ok(routerEnd > providerEnd, 'BrowserRouter must close after ComponentOperationProvider');
});

test('AppRoutes does not create a nested router', () => {
  const routesStart = appSource.indexOf('function AppRoutes()');
  const routesEnd = appSource.indexOf('function AuthGate()', routesStart);
  const routesSource = appSource.slice(routesStart, routesEnd);

  assert.ok(routesStart >= 0 && routesEnd > routesStart, 'AppRoutes source must be found');
  assert.doesNotMatch(routesSource, /<BrowserRouter>/);
});
