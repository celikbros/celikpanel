# Role-aware post-install guidance

The dashboard guides the signed-in role through real panel actions. Preserve
the existing navy/white design, role guards and backend mutation workflow.

- Administrator: configure the DNS topology and nameserver identity, activate
  BIND or PowerDNS, then inspect DNS readiness. DNS remains the first hosting
  prerequisite. Fresh runtime and identity evidence are required for completion.
- Firewall persistence, panel HTTPS, account protection and the server audit
  remain independently accessible; they never wait behind DNS completion.
- Website and mail are separate optional next tasks. Unattempted mail blocked
  by absent DNS is not an operational incident. Attempted, partial and installed
  mail remain observable. A website certificate cannot complete panel HTTPS.
- Reseller: customer management and hosted domains. Customer: connect a domain,
  publish content, enable site HTTPS and arrange backups through domain tools.
  Additional user: only the authorization-filtered assigned domains; explain
  missing access without exposing server controls or account actions.
- Guidance is bilingual, has direct destinations and can be collapsed without
  marking configuration complete. No host configuration runs automatically.

Also repair the Settings system-databases tab being mounted under the hidden DNS
tabpanel. Validate role isolation, stale/failed observations, DNS ordering,
optional mail and panel/site certificate separation, then desktop/mobile TR/EN.
