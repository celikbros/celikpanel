# Alpha66 setup guidance validation

Local acceptance recorded September 11, 2026 before the release metadata bump.
The PR and tag workflows validate the final release source separately.

| Check | Result | Evidence |
|---|---|---|
| Go setup suite | PASS | `go-setup.log` |
| Go setup race suite | PASS | `go-setup-race.log` |
| Frontend runtime/contracts | 357 passed | `web-tests.log` |
| TypeScript, production build, bundle budget | PASS | `web-build.log` |
| TR/EN at 1440 and 390 px | 4 scenarios passed | `browser-results.json`, `browser.log` |

Each browser scenario starts with an undecided legacy server, chooses manual
mode, reloads Dashboard, reopens setup through Settings, and chooses the wizard.
Only two preference writes occur; no setup-start or DNS-pairing calls occur.
No JavaScript errors or horizontal overflow were observed. The mocked statistics
endpoint intentionally returns 503; the console messages are fixture behavior.

`browser-fixture.cjs` records the executed local Chrome fixture, including its
machine-specific browser/dependency paths. Screenshots show the Turkish mobile
choice and wizard, plus English desktop Settings. This is not evidence of a
production server installation or a newly executed service profile.

Türkçe: Manuel tercihin yenilemeden sonra korunması, Ayarlar'dan sihirbaza dönüş
ve Türkçe/İngilizce mobil/masaüstü görünümü doğrulandı. Denemeler yerel taklit
API'lerle yapıldı; kurulu paneller güncellenmedi veya yapılandırılmadı.
