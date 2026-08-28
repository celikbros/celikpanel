export interface RouterHistoryPort {
  href: string;
  origin: string;
  state: unknown;
  replaceState: (state: unknown, title: string, url: string) => void;
}

export interface RouterHistoryReplacement {
  state: unknown;
  url: string;
  pathname: string;
  search: string;
  hash: string;
}

export function replaceRouterHistoryURL(
  history: RouterHistoryPort,
  to: string,
): RouterHistoryReplacement | null {
  try {
    const target = new URL(to, history.href);
    if (target.origin !== history.origin) return null;
    const url = (target.pathname || '/') + target.search + target.hash;
    history.replaceState(history.state, '', url);
    return {
      state: history.state,
      url,
      pathname: target.pathname || '/',
      search: target.search,
      hash: target.hash,
    };
  } catch {
    return null;
  }
}
