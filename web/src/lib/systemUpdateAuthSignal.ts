type AuthenticationListener = (authenticated: boolean, generation: number) => void;

let currentAuthenticationState = false;
let currentAuthenticationGeneration = 0;
const authenticationListeners = new Set<AuthenticationListener>();

export function publishSystemUpdateAuthentication(authenticated: boolean): void {
    currentAuthenticationState = authenticated;
    currentAuthenticationGeneration += 1;
    for (const listener of authenticationListeners) {
        listener(authenticated, currentAuthenticationGeneration);
    }
}

export function subscribeSystemUpdateAuthentication(listener: AuthenticationListener): () => void {
    authenticationListeners.add(listener);
    listener(currentAuthenticationState, currentAuthenticationGeneration);
    return () => authenticationListeners.delete(listener);
}

export function shouldApplyUnauthorizedResponse(
    requestGeneration: number,
    currentGeneration: number,
): boolean {
    return requestGeneration === currentGeneration;
}
