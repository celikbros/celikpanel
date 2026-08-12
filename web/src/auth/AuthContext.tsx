import { createContext, useContext, type ReactNode } from 'react';
import type { CurrentUser } from '../lib/api';

// The signed-in user (with role) is provided once at the top of the tree so
// the shell, navigation and route guards all render from the same source.
// When backend authorization (ROLES.md) lands, capabilities will ride
// alongside role here.
//
// Giriş yapan kullanıcı (rolüyle) ağacın tepesinde bir kez sağlanır; böylece
// kabuk, navigasyon ve rota koruyucuları aynı kaynaktan render eder. Backend
// yetkilendirmesi (ROLES.md) geldiğinde, yetkiler burada rolün yanında yer
// alacak.
export type Role = 'admin' | 'reseller' | 'customer' | 'additional_user';

const ROLES: readonly Role[] = ['admin', 'reseller', 'customer', 'additional_user'];

export function normalizeRole(role: string): Role {
    if (ROLES.some((candidate) => candidate === role)) {
        return role as Role;
    }

    // Authentication succeeded, but the server returned a role this client
    // cannot authorize. Failing here keeps an unknown role from inheriting a
    // customer's navigation and route access by accident.
    throw new Error('The authenticated user has an unsupported role.');
}

interface AuthContextValue {
    user: CurrentUser;
    role: Role;
    logout: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({
    user,
    onLogout,
    children,
}: {
    user: CurrentUser;
    onLogout: () => void;
    children: ReactNode;
}) {
    const role = normalizeRole(user.effective_role);

    return (
        <AuthContext.Provider value={{ user, role, logout: onLogout }}>
            {children}
        </AuthContext.Provider>
    );
}

export function useAuth(): AuthContextValue {
    const ctx = useContext(AuthContext);
    if (!ctx) throw new Error('useAuth must be used within AuthProvider');
    return ctx;
}
