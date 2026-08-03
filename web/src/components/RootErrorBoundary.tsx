import { Component, type ErrorInfo, type ReactNode } from 'react'

interface RootErrorBoundaryProps {
  children: ReactNode
}

interface RootErrorBoundaryState {
  failed: boolean
}

/**
 * Last-resort application boundary. It deliberately has no dependency on the
 * theme or translation providers because failures in those providers must also
 * leave the administrator with a visible recovery action.
 */
export class RootErrorBoundary extends Component<
  RootErrorBoundaryProps,
  RootErrorBoundaryState
> {
  state: RootErrorBoundaryState = { failed: false }

  static getDerivedStateFromError(): RootErrorBoundaryState {
    return { failed: true }
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error('Uncaught CelikPanel application error', error, errorInfo)
  }

  private reload = () => {
    window.location.reload()
  }

  render() {
    if (!this.state.failed) {
      return this.props.children
    }

    return (
      <main className="flex min-h-screen items-center justify-center bg-surface-2 px-6 py-12 text-fg">
        <section
          className="w-full max-w-lg rounded-2xl border border-border bg-surface p-8 text-center shadow-card"
          role="alert"
          aria-live="assertive"
        >
          <div className="mx-auto mb-5 flex h-12 w-12 items-center justify-center rounded-xl bg-danger/10 text-2xl font-bold text-danger">
            !
          </div>
          <h1 className="text-xl font-semibold">
            CelikPanel beklenmeyen bir hatayla karşılaştı.
          </h1>
          <p className="mt-2 text-base font-medium">
            CelikPanel encountered an unexpected error.
          </p>
          <p className="mt-4 text-sm leading-6 text-fg-muted">
            Sayfayı yeniden yükleyerek güvenli biçimde tekrar deneyin. / Reload the page to
            try again safely.
          </p>
          <button
            type="button"
            className="mt-6 rounded-lg bg-primary px-5 py-2.5 text-sm font-semibold text-primary-fg transition-colors hover:bg-primary-hover focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2"
            onClick={this.reload}
          >
            Yeniden yükle / Reload
          </button>
        </section>
      </main>
    )
  }
}
