import {
  Children,
  createContext,
  isValidElement,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type AnchorHTMLAttributes,
  type MouseEvent,
  type ReactElement,
  type ReactNode,
} from 'react';
import { matchRoute, type RouteParams } from './router-core';
import { replaceRouterHistoryURL } from './router-history';

export { matchRoute } from './router-core';

interface LocationState {
  pathname: string;
  search: string;
  hash: string;
  state: unknown;
}

interface NavigateOptions {
  replace?: boolean;
  state?: unknown;
}

type NavigateFunction = (to: string, options?: NavigateOptions) => boolean;

interface RouterContextValue {
  location: LocationState;
  navigate: NavigateFunction;
}

const RouterContext = createContext<RouterContextValue | null>(null);
const ParamsContext = createContext<RouteParams>({});
const ROUTER_STATE_KEY = '__celikpanel_router_v1';
const navigationBlockers = new Set<{ readonly current: boolean }>();
const navigationBlockerListeners = new Set<() => void>();
type CurrentURLReplacer = (to: string) => boolean;
let currentURLReplacer: CurrentURLReplacer | null = null;
const navigationBlocker = {
  get current(): boolean {
    for (const blocker of navigationBlockers) {
      if (blocker.current) return true;
    }
    return false;
  },
};

export function isNavigationBlocked(): boolean {
  return navigationBlocker.current;
}

interface ManagedHistoryState {
  index: number;
  value: unknown;
}

function managedHistoryState(value: unknown): ManagedHistoryState | null {
  const managed = (value as Record<string, ManagedHistoryState> | null)?.[ROUTER_STATE_KEY];
  return managed && Number.isSafeInteger(managed.index) ? managed : null;
}

function wrappedHistoryState(index: number, value: unknown) {
  return { [ROUTER_STATE_KEY]: { index, value } };
}

function currentBrowserURL() {
  return `${window.location.pathname || '/'}${window.location.search}${window.location.hash}`;
}

function browserLocation(): LocationState {
  const managed = managedHistoryState(window.history.state);
  return {
    pathname: window.location.pathname || '/',
    search: window.location.search,
    hash: window.location.hash,
    state: managed ? managed.value : window.history.state,
  };
}

function requireRouter(): RouterContextValue {
  const router = useContext(RouterContext);
  if (!router) throw new Error('router hook used outside BrowserRouter');
  return router;
}

export function replaceCurrentRouterURL(to: string): boolean {
  return currentURLReplacer?.(to) ?? false;
}

export function BrowserRouter({ children }: { children: ReactNode }) {
  const historyIndexRef = useRef(0);
  const acceptedEntryRef = useRef({
    state: window.history.state,
    url: currentBrowserURL(),
  });
  const [location, setLocation] = useState<LocationState>(() => {
    const managed = managedHistoryState(window.history.state);
    historyIndexRef.current = managed?.index ?? 0;
    if (!managed) {
      window.history.replaceState(
        wrappedHistoryState(historyIndexRef.current, window.history.state),
        '',
      );
    }
    acceptedEntryRef.current.state = window.history.state;
    return browserLocation();
  });
  useEffect(() => {
    const onPopState = () => {
      const target = managedHistoryState(window.history.state);
      if (navigationBlocker?.current) {
        if (target && target.index !== historyIndexRef.current) {
          window.history.go(historyIndexRef.current - target.index);
        } else {
          const accepted = acceptedEntryRef.current;
          window.history.replaceState(accepted.state, '', accepted.url);
        }
        return;
      }
      if (target) historyIndexRef.current = target.index;
      acceptedEntryRef.current = {
        state: window.history.state,
        url: currentBrowserURL(),
      };
      setLocation(browserLocation());
    };
    window.addEventListener('popstate', onPopState);
    return () => window.removeEventListener('popstate', onPopState);
  }, []);

  const navigate = useCallback<NavigateFunction>((to, options = {}) => {
    if (navigationBlocker?.current) return false;
    const target = new URL(to, window.location.href);
    if (target.origin !== window.location.origin) {
      throw new Error('cross-origin navigation is not allowed');
    }
    const next = `${target.pathname}${target.search}${target.hash}`;
    const index = options.replace ? historyIndexRef.current : historyIndexRef.current + 1;
    const state = wrappedHistoryState(index, options.state ?? null);
    if (options.replace) window.history.replaceState(state, '', next);
    else window.history.pushState(state, '', next);
    historyIndexRef.current = index;
    acceptedEntryRef.current = { state, url: next };
    setLocation(browserLocation());
    return true;
  }, []);

  const replaceCurrentURL = useCallback<CurrentURLReplacer>((to) => {
    const replaced = replaceRouterHistoryURL({
      href: window.location.href,
      origin: window.location.origin,
      state: window.history.state,
      replaceState: (state, title, url) => window.history.replaceState(state, title, url),
    }, to);
    if (!replaced) return false;
    acceptedEntryRef.current = { state: replaced.state, url: replaced.url };
    setLocation(browserLocation());
    return true;
  }, []);

  useLayoutEffect(() => {
    currentURLReplacer = replaceCurrentURL;
    return () => {
      if (currentURLReplacer === replaceCurrentURL) currentURLReplacer = null;
    };
  }, [replaceCurrentURL]);

  const value = useMemo(() => ({ location, navigate }), [location, navigate]);
  return <RouterContext.Provider value={value}>{children}</RouterContext.Provider>;
}

export function useNavigate(): NavigateFunction {
  return requireRouter().navigate;
}

export function useLocation(): LocationState {
  return requireRouter().location;
}

export function useNavigationBlocker(blocked: { readonly current: boolean }): void {
  useLayoutEffect(() => {
    navigationBlockers.add(blocked);
    return () => {
      navigationBlockers.delete(blocked);
    };
  }, [blocked]);
  useLayoutEffect(() => {
    for (const listener of navigationBlockerListeners) listener();
  }, [blocked.current]);
}

export function useParams(): RouteParams {
  return useContext(ParamsContext);
}

export function useSearchParams(): [
  URLSearchParams,
  (next: URLSearchParams, options?: NavigateOptions) => void,
] {
  const { location, navigate } = requireRouter();
  const params = useMemo(() => new URLSearchParams(location.search), [location.search]);
  const setParams = useCallback((next: URLSearchParams, options: NavigateOptions = {}) => {
    const query = next.toString();
    navigate(`${location.pathname}${query ? `?${query}` : ''}${location.hash}`, {
      ...options,
      state: location.state,
    });
  }, [location.hash, location.pathname, location.state, navigate]);
  return [params, setParams];
}

export function Navigate({ to, replace = false }: { to: string; replace?: boolean }) {
  const navigate = useNavigate();
  useEffect(() => {
    let completed = false;
    const attempt = () => {
      if (!completed && navigate(to, { replace })) completed = true;
    };
    navigationBlockerListeners.add(attempt);
    attempt();
    return () => {
      navigationBlockerListeners.delete(attempt);
    };
  }, [navigate, replace, to]);
  return null;
}

export interface RouteProps {
  path: string;
  element: ReactElement;
}

export function Route(_props: RouteProps) {
  return null;
}

export function Routes({ children }: { children: ReactNode }) {
  const { pathname } = useLocation();
  let fallback: ReactElement<RouteProps> | null = null;

  for (const child of Children.toArray(children)) {
    if (!isValidElement<RouteProps>(child) || child.type !== Route) continue;
    if (child.props.path === '*') {
      fallback = child;
      continue;
    }
    const params = matchRoute(child.props.path, pathname);
    if (params) {
      return <ParamsContext.Provider value={params}>{child.props.element}</ParamsContext.Provider>;
    }
  }

  return fallback
    ? <ParamsContext.Provider value={{}}>{fallback.props.element}</ParamsContext.Provider>
    : null;
}

interface LinkProps extends Omit<AnchorHTMLAttributes<HTMLAnchorElement>, 'href'> {
  to: string;
}

export function Link({ to, onClick, target, children, ...rest }: LinkProps) {
  const navigate = useNavigate();
  const handleClick = (event: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(event);
    if (
      event.defaultPrevented ||
      event.button !== 0 ||
      event.metaKey ||
      event.ctrlKey ||
      event.shiftKey ||
      event.altKey ||
      (target && target !== '_self')
    ) return;
    event.preventDefault();
    navigate(to);
  };
  return <a {...rest} href={to} target={target} onClick={handleClick}>{children}</a>;
}
