import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import test from 'node:test';
import React from 'react';
import TestRenderer, { act } from 'react-test-renderer';
import ts from 'typescript';

const require = createRequire(import.meta.url);
const reactURL = pathToFileURL(require.resolve('react')).href;
const jsxRuntimeURL = pathToFileURL(require.resolve('react/jsx-runtime')).href;
const moduleURL = source => 'data:text/javascript;base64,' + Buffer.from(source).toString('base64');

function compileURL(relativePath, imports) {
    const source = readFileSync(new URL(relativePath, import.meta.url), 'utf8');
    const compiled = ts.transpileModule(source, {
        compilerOptions: {
            module: ts.ModuleKind.ES2022,
            target: ts.ScriptTarget.ES2020,
            jsx: ts.JsxEmit.ReactJSX,
        },
    }).outputText;
    return moduleURL(compiled.replace(/from ['"]([^'"]+)['"]/g, (_match, path) => {
        const target = path === 'react' ? reactURL
            : path === 'react/jsx-runtime' ? jsxRuntimeURL : imports[path];
        assert.ok(target, 'unmapped mounted-shell import: ' + path);
        return `from '${target}'`;
    }));
}

const stubURL = moduleURL(`
    import React from '${reactURL}';
    const Icon = props => React.createElement('i', props);
    export const LogOut = Icon, ChevronDown = Icon, Menu = Icon, KeyRound = Icon, UserCheck = Icon;
    export const BrandMark = Icon;
    export const ThemeSwitcher = () => React.createElement('button', null, 'Theme');
    export const SkinSwitcher = () => React.createElement('button', null, 'Skin');
    export const LanguageSwitcher = () => React.createElement('button', null, 'Language');
    export const ChangePasswordModal = () => null;
    export const Button = ({ children, ...props }) => React.createElement('button', props, children);
    export const Spinner = () => React.createElement('span', null, 'Loading');
    export const useAuth = () => globalThis.setupShellFixture.auth;
    export const useI18n = () => ({ t: key => key, screensReady: true, screensFailed: false });
    export const useLocation = () => ({ pathname: '/setup' });
    export const Navigate = props => React.createElement('redirect', props);
    export const Link = ({ to, children, ...props }) => React.createElement('a', { ...props, href: to }, children);
    export const DesktopPageHeaderTargetContext = React.createContext(null);
    export const navGroups = [{ id: 'hosting', labelKey: 'nav.hosting' }];
    export const navItemsForRole = () => [
        { id: 'domains', group: 'hosting', labelKey: 'nav.domains', icon: Icon, countKey: 'domains' },
        { id: 'services', group: 'hosting', labelKey: 'nav.services', icon: Icon, countKey: 'services' },
    ];
    export const api = {
        getServices: async () => {
            globalThis.setupShellFixture.serviceReads++;
            return { components: [{ id: 'nginx', is_installed: true }] };
        },
        logout: async () => {},
    };
    export const publishComponentCensus = result => globalThis.setupShellFixture.censusPublished.push(result);
    export const useComponentCensus = () => 1;
`);
const layoutURL = compileURL('../src/components/Layout.tsx', Object.fromEntries([
    './BrandMark', 'lucide-react', '../lib/api', '../auth/AuthContext', '../i18n',
    '../nav', '../lib/componentCensus', './ThemeSwitcher', './SkinSwitcher',
    './LanguageSwitcher', './ChangePasswordModal', './pageHeaderSlot',
].map(path => [path, stubURL])));
const setupModelURL = compileURL('../src/lib/serverSetup.ts', {});
const setupGateURL = compileURL('../src/components/ServerSetupGate.tsx', {
    '../auth/AuthContext': stubURL,
    '../i18n': stubURL,
    '../router': stubURL,
    '../lib/serverSetup': setupModelURL,
    './Layout': layoutURL,
    './ChangePasswordModal': stubURL,
    './ui': stubURL,
});
const { Layout } = await import(layoutURL);
const { ServerSetupShell } = await import(setupGateURL);
const { ServerSetupSteps } = await import(compileURL('../src/components/ServerSetupSteps.tsx', { '../i18n': stubURL }));

const runtime = {
    version: 'v9.8.7-server-fixture',
    commit: '1234567890abcdef1234567890abcdef12345678',
    agent_commit: '1234567890abcdef1234567890abcdef12345678',
    agent_matches: true,
    hostname: 'boston.example.test',
    ipv4: '192.0.2.20',
};

function textOf(node) {
    if (node == null || typeof node === 'boolean') return '';
    if (typeof node === 'string' || typeof node === 'number') return String(node);
    if (Array.isArray(node)) return node.map(textOf).join(' ');
    return textOf(node.children);
}

async function withMounted(element, check, response = runtime) {
    const previousFetch = globalThis.fetch;
    const previousFixture = globalThis.setupShellFixture;
    const fixture = {
        auth: { role: 'admin', user: { username: 'admin' }, logout() {} },
        requests: [], serviceReads: 0, censusPublished: [],
    };
    globalThis.setupShellFixture = fixture;
    globalThis.fetch = async (input, options) => {
        const path = String(input);
        fixture.requests.push({ path, options });
        if (path === '/api/v1/panel/version') return Response.json(response);
        if (path === '/api/v1/domains') return Response.json([{ name: 'one.example' }, { name: 'two.example' }]);
        throw new Error('unexpected shell request: ' + path);
    };
    let renderer;
    try {
        await act(async () => {
            renderer = TestRenderer.create(element);
            await Promise.resolve();
            await Promise.resolve();
            await Promise.resolve();
        });
        await check(renderer, fixture);
    } finally {
        if (renderer) await act(async () => renderer.unmount());
        globalThis.fetch = previousFetch;
        if (previousFixture === undefined) delete globalThis.setupShellFixture;
        else globalThis.setupShellFixture = previousFixture;
    }
}

test('mounted setup shell preserves server identity and build without ordinary navigation or census requests', async () => {
    const navigation = React.createElement(ServerSetupSteps, {
        steps: ['purpose', 'components', 'access', 'review', 'progress'], current: 'review',
    });
    await withMounted(React.createElement(ServerSetupShell, { navigation },
        React.createElement('h1', null, 'Reviewed setup plan')), async (renderer, fixture) => {
        const root = renderer.root;
        for (const placement of ['sidebar', 'mobile']) {
            const identity = root.findByProps({ 'data-server-identity': placement });
            assert.ok(textOf(identity).includes(runtime.hostname));
            assert.ok(textOf(identity).includes(runtime.ipv4));
        }
        const mobileBuild = root.findByProps({ 'data-setup-build': true });
        assert.ok(textOf(mobileBuild).includes(runtime.version));
        assert.ok(textOf(mobileBuild).includes(runtime.commit.slice(0, 12)));
        const main = root.findByType('main');
        assert.equal(main.findAllByProps({ 'data-setup-build': true }).length, 0,
            'the narrow-screen build footer remains outside scrolling setup content');
        assert.ok(textOf(root.findByType('aside')).includes(runtime.version));
        assert.equal(root.findAllByProps({ 'aria-current': 'step' }).length, 1);
        assert.ok(textOf(root.findByProps({ 'aria-current': 'step' })).includes('setup.step.review'));
        assert.equal(root.findAllByType('button').some(button => /nav\.(domains|services)/.test(textOf(button))), false);
        assert.equal(root.findAllByProps({ 'aria-label': 'Menu' }).length, 0);
        assert.equal(fixture.serviceReads, 0);
        assert.equal(fixture.censusPublished.length, 0);
        assert.deepEqual(fixture.requests.map(request => request.path), ['/api/v1/panel/version']);
        assert.equal(fixture.requests[0].options.cache, 'no-store');

        const links = root.findAllByType('a').map(link => link.props.href);
        for (const recovery of ['panel', 'dns', 'updates']) {
            assert.ok(links.includes('/settings?section=' + recovery), 'missing recovery link: ' + recovery);
        }
        assert.equal(links.some(href => href === '/services' || href.startsWith('/services/')), false);
        assert.equal(links.some(href => href === '/domains' || href.startsWith('/domains/')), false);
    });
});

test('mounted regular shell retains domain and component navigation and census loading', async () => {
    const navigated = [];
    await withMounted(React.createElement(Layout, {
        currentPage: 'domains', onPageChange: id => navigated.push(id),
    }, React.createElement('h1', null, 'Domains')), async (renderer, fixture) => {
        const buttons = renderer.root.findAllByType('button');
        const domains = buttons.find(button => textOf(button).includes('nav.domains'));
        const services = buttons.find(button => textOf(button).includes('nav.services'));
        assert.ok(domains);
        assert.ok(services);
        assert.ok(textOf(domains).includes('2'), 'domain count remains present');
        assert.ok(textOf(services).includes('1'), 'component census remains present');
        await act(async () => services.props.onClick());
        assert.deepEqual(navigated, ['services']);
        assert.equal(fixture.serviceReads, 1);
        assert.equal(fixture.censusPublished.length, 1);
        assert.deepEqual(fixture.requests.map(request => request.path).sort(), [
            '/api/v1/domains', '/api/v1/panel/version',
        ]);
        assert.equal(renderer.root.findAllByProps({ 'data-setup-build': true }).length, 0);
        assert.equal(renderer.root.findAllByProps({ 'data-setup-navigation': true }).length, 0);
    });
});

test('mounted setup shell does not invent identity or version from an invalid runtime response', async () => {
    await withMounted(React.createElement(ServerSetupShell, null,
        React.createElement('h1', null, 'Setup')), async (renderer, fixture) => {
        assert.equal(renderer.root.findAll(node => node.props['data-server-identity'] !== undefined).length, 0);
        const output = textOf(renderer.toJSON());
        assert.equal(output.includes(runtime.version), false);
        assert.equal(output.includes(runtime.hostname), false);
        assert.equal(output.includes(runtime.ipv4), false);
        assert.equal(fixture.serviceReads, 0);
        assert.equal(renderer.root.findAllByType('main').length, 1);
    }, { ...runtime, agent_matches: 'unverified' });
});
