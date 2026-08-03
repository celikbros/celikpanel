package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func frontendSourceFile(t *testing.T, elements ...string) string {
	t.Helper()
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(testFile), "..", ".."))
	path := filepath.Join(append([]string{root}, elements...)...)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func TestRootErrorBoundaryCoversApplicationShell(t *testing.T) {
	mainSource := frontendSourceFile(t, "web", "src", "main.tsx")
	boundarySource := frontendSourceFile(
		t,
		"web",
		"src",
		"components",
		"RootErrorBoundary.tsx",
	)

	for _, required := range []string{
		`import { RootErrorBoundary } from './components/RootErrorBoundary'`,
		`<RootErrorBoundary>`,
		`<ThemeProvider>`,
		`<I18nProvider>`,
		`<App />`,
		`</I18nProvider>`,
		`</ThemeProvider>`,
		`</RootErrorBoundary>`,
	} {
		if !strings.Contains(mainSource, required) {
			t.Fatalf("application root is missing %q", required)
		}
	}

	rootOpen := strings.Index(mainSource, `<RootErrorBoundary>`)
	themeOpen := strings.Index(mainSource, `<ThemeProvider>`)
	i18nOpen := strings.Index(mainSource, `<I18nProvider>`)
	i18nClose := strings.Index(mainSource, `</I18nProvider>`)
	themeClose := strings.Index(mainSource, `</ThemeProvider>`)
	rootClose := strings.Index(mainSource, `</RootErrorBoundary>`)
	if !(rootOpen < themeOpen && themeOpen < i18nOpen &&
		i18nOpen < i18nClose && i18nClose < themeClose && themeClose < rootClose) {
		t.Fatal("root error boundary must wrap the theme, i18n, authentication, login, and layout tree")
	}

	for _, required := range []string{
		`getDerivedStateFromError`,
		`componentDidCatch`,
		`CelikPanel beklenmeyen bir hatayla karşılaştı.`,
		`CelikPanel encountered an unexpected error.`,
		`Yeniden yükle / Reload`,
		`window.location.reload()`,
		`role="alert"`,
	} {
		if !strings.Contains(boundarySource, required) {
			t.Fatalf("root error fallback is missing %q", required)
		}
	}
}
