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
            case 'success': return 'bg-green-900/90 border-green-700 text-green-100';
            case 'error': return 'bg-red-900/90 border-red-700 text-red-100';
            case 'warning': return 'bg-yellow-900/90 border-yellow-700 text-yellow-100';
            case 'info': return 'bg-blue-900/90 border-blue-700 text-blue-100';
        }
    };

    return (
        <div className="fixed top-4 right-4 z-50 space-y-2 max-w-md">
            {toasts.map(toast => (
                <div
                    key={toast.id}
                    className={`flex items-center gap-3 p-4 rounded-lg border backdrop-blur-sm shadow-lg animate-slide-in ${getStyles(toast.type)}`}
                >
                    {getIcon(toast.type)}
                    <p className="flex-1 text-sm font-medium">{toast.message}</p>
                    <button
                        onClick={() => removeToast(toast.id)}
                        className="p-1 hover:bg-white/10 rounded transition-colors"
                    >
                        <X className="w-4 h-4" />
                    </button>
                </div>
            ))}
        </div>
    );
}
