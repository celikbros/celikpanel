import {
  Children,
  createContext,
  isValidElement,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type AnchorHTMLAttributes,
  type MouseEvent,
  type ReactElement,
  type ReactNode,
} from 'react';
import { matchRoute, type RouteParams } from './router-core';

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

type NavigateFunction = (to: string, options?: NavigateOptions) => void;

interface RouterContextValue {
  location: LocationState;
  navigate: NavigateFunction;
}

const RouterContext = createContext<RouterContextValue | null>(null);
const ParamsContext = createContext<RouteParams>({});

function browserLocation(): LocationState {
  return {
    pathname: window.location.pathname || '/',
    search: window.location.search,
    hash: window.location.hash,
    state: window.history.state,
  };
}

function requireRouter(): RouterContextValue {
  const router = useContext(RouterContext);
  if (!router) throw new Error('router hook used outside BrowserRouter');
  return router;
}

export function BrowserRouter({ children }: { children: ReactNode }) {
  const [location, setLocation] = useState<LocationState>(browserLocation);

  useEffect(() => {
    const onPopState = () => setLocation(browserLocation());
    window.addEventListener('popstate', onPopState);
    return () => window.removeEventListener('popstate', onPopState);
  }, []);

  const navigate = useCallback<NavigateFunction>((to, options = {}) => {
    const target = new URL(to, window.location.href);
    if (target.origin !== window.location.origin) {
      throw new Error('cross-origin navigation is not allowed');
    }
    const next = `${target.pathname}${target.search}${target.hash}`;
    if (options.replace) window.history.replaceState(options.state ?? null, '', next);
    else window.history.pushState(options.state ?? null, '', next);
    setLocation(browserLocation());
  }, []);

  const value = useMemo(() => ({ location, navigate }), [location, navigate]);
  return <RouterContext.Provider value={value}>{children}</RouterContext.Provider>;
}

export function useNavigate(): NavigateFunction {
  return requireRouter().navigate;
}

export function useLocation(): LocationState {
  return requireRouter().location;
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
  useEffect(() => navigate(to, { replace }), [navigate, replace, to]);
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
