# Web dependency security

## React Router

CelikPanel is a client-rendered Vite application. It uses `BrowserRouter` and
does not enable React Server Components, framework mode, server actions,
`createRequestHandler`, server-side rendering, or React Router's server
deserialization paths.

`react-router-dom` is pinned to `7.18.2`. This release closes the redirect,
client-navigation, manifest, SSR, and deserialization advisories that affect
older releases. As of 2026-07-29, `npm audit --omit=dev` still reports
GHSA-qwww-vcr4-c8h2 for React Router versions through `8.2.0`. The advisory is
specific to RSC/server-action request processing, which is not present in this
application.

The finding is therefore not reachable in the deployed CelikPanel web
architecture. It must be reviewed again before any of the following changes:

- enabling React Server Components or React Router framework/server mode;
- adding server actions or a React Router request handler;
- introducing server-side rendering or prerendered redirects;
- accepting serialized React Router server payloads.

Do not use `npm audit fix --force` to silence this report. At the time of this
decision it proposes a version with client-visible open-redirect and XSS
advisories. Upgrade only after a release fixes the RSC advisory without
reintroducing reachable client-side findings.

## Verification

For every web release:

1. run `npm ci`;
2. run `npm run build`;
3. run `npm audit --omit=dev`;
4. confirm the application still contains none of the server/RSC APIs listed
   above;
5. record any changed advisory range in the release review.
