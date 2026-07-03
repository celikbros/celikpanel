export interface ConfigFile {
    path: string;
    is_managed: boolean;
    Content?: string;
    Parsed?: string;
}

export interface Service {
    id: string;
    name: string;
    description: string;
    icon: string;
    category: string;
    versions: string[];
    status: string;
    is_installed: boolean;
    config_files: ConfigFile[];
}

export interface ConfigResponse {
    Content: string;
    Parsed: string;
}

const API_BASE = '/api/v1';

export interface CurrentUser {
    username: string;
    role: string;
    email?: string;
}

class API {
    // me returns the current user, or null when unauthenticated (401).
    // me, mevcut kullanıcıyı döndürür; kimlik doğrulanmamışsa (401) null.
    async me(): Promise<CurrentUser | null> {
        const res = await fetch(`${API_BASE}/auth/me`);
        if (res.status === 401) return null;
        if (!res.ok) throw new Error('Failed to fetch current user');
        return res.json();
    }

    // login authenticates and, on success, the server sets the session
    // cookie. Returns the user on success, throws on failure.
    // login kimlik doğrular; başarılıysa sunucu oturum çerezini ayarlar.
    async login(username: string, password: string): Promise<CurrentUser> {
        const res = await fetch(`${API_BASE}/auth/login`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username, password }),
        });
        if (!res.ok) {
            throw new Error(res.status === 401 ? 'invalid_credentials' : 'login_failed');
        }
        return res.json();
    }

    async logout(): Promise<void> {
        await fetch(`${API_BASE}/auth/logout`, { method: 'POST' });
    }

    async getServices(): Promise<Service[]> {
        const res = await fetch(`${API_BASE}/managed-services`);
        if (!res.ok) throw new Error('Failed to fetch services');
        const data = await res.json();
        return data || [];
    }

    async getConfig(path: string): Promise<ConfigResponse> {
        const res = await fetch(`${API_BASE}/config?path=${encodeURIComponent(path)}`);
        if (!res.ok) throw new Error('Failed to fetch config');
        return res.json();
    }

    async saveConfig(path: string, content: string) {
        const res = await fetch(`${API_BASE}/config?path=${encodeURIComponent(path)}`, {
            method: 'POST',
            headers: { 'Content-Type': 'text/plain' },
            body: content,
        });
        if (!res.ok) throw new Error('Failed to save config');
    }

    async serviceAction(serviceName: string, action: string) {
        const res = await fetch(`${API_BASE}/service/action`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ service_name: serviceName, action }),
        });
        if (!res.ok) throw new Error(`Failed to ${action} service`);
    }

    async getServiceStatus(name: string): Promise<{ name: string; active: boolean; pid: string }> {
        const res = await fetch(`${API_BASE}/service/status?name=${encodeURIComponent(name)}`);
        if (!res.ok) throw new Error('Failed to fetch service status');
        return res.json();
    }
}

export const api = new API();
