export type RouteParams = Record<string, string | undefined>;

function normalizePath(pathname: string): string {
  if (pathname.length > 1 && pathname.endsWith('/')) return pathname.slice(0, -1);
  return pathname || '/';
}

export function matchRoute(pattern: string, pathname: string): RouteParams | null {
  if (pattern === '*') return {};
  const wanted = normalizePath(pattern).split('/').filter(Boolean);
  const actual = normalizePath(pathname).split('/').filter(Boolean);
  if (wanted.length !== actual.length) return null;

  const params: RouteParams = {};
  for (let index = 0; index < wanted.length; index += 1) {
    const segment = wanted[index];
    if (segment.startsWith(':')) {
      try {
        params[segment.slice(1)] = decodeURIComponent(actual[index]);
      } catch {
        return null;
      }
    } else if (segment !== actual[index]) {
      return null;
    }
  }
  return params;
}
