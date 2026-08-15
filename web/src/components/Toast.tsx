import { useState, useEffect } from 'react';
import { CheckCircle, XCircle, Info, AlertTriangle, X } from 'lucide-react';

type ToastType = 'success' | 'error' | 'info' | 'warning';

interface Toast {
    id: number;
    type: ToastType;
    message: string;
}

let toastId = 0;
const toastListeners: ((toast: Toast) => void)[] = [];

export function showToast(type: ToastType, message: string) {
    const toast: Toast = {
        id: toastId++,
        type,
        message,
    };
    toastListeners.forEach(listener => listener(toast));
}

export function ToastContainer() {
    const [toasts, setToasts] = useState<Toast[]>([]);

    useEffect(() => {
        const listener = (toast: Toast) => {
            setToasts(prev => [...prev, toast]);
            setTimeout(() => {
                setToasts(prev => prev.filter(t => t.id !== toast.id));
            }, 5000);
        };
        toastListeners.push(listener);
        return () => {
            const index = toastListeners.indexOf(listener);
            if (index > -1) toastListeners.splice(index, 1);
        };
    }, []);

    const removeToast = (id: number) => {
        setToasts(prev => prev.filter(t => t.id !== id));
    };

    const getIcon = (type: ToastType) => {
        switch (type) {
            case 'success': return <CheckCircle className="w-5 h-5" />;
            case 'error': return <XCircle className="w-5 h-5" />;
            case 'warning': return <AlertTriangle className="w-5 h-5" />;
            case 'info': return <Info className="w-5 h-5" />;
        }
    };

    const getStyles = (type: ToastType) => {
        switch (type) {
            case 'success': return 'bg-success/90 border-success text-success-fg';
            case 'error': return 'bg-danger/90 border-danger text-danger-fg';
            case 'warning': return 'bg-warning/90 border-warning text-warning-fg';
            case 'info': return 'bg-primary/90 border-primary text-primary-fg';
        }
    };

    return (
        <div className="fixed top-4 right-4 z-50 space-y-2 max-w-md">
            {toasts.map(toast => (
                <div
                    key={toast.id}
                    role={toast.type === 'error' ? 'alert' : 'status'}
                    aria-live={toast.type === 'error' ? 'assertive' : 'polite'}
                    aria-atomic={true}
                    className={`flex items-center gap-3 p-4 rounded-lg border backdrop-blur-sm shadow-lg animate-slide-in ${getStyles(toast.type)}`}
                >
                    {getIcon(toast.type)}
                    <p className="flex-1 text-sm font-medium">{toast.message}</p>
                    <button
                        type={'button'}
                        aria-label={'Dismiss notification'}
                        onClick={() => removeToast(toast.id)}
                        className={'rounded p-1 text-inherit transition-colors hover:bg-black/15 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-current'}
                    >
                        <X className="w-4 h-4" />
                    </button>
                </div>
            ))}
        </div>
    );
}
