import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const serviceList = readFileSync(new URL('../src/components/ServiceList.tsx', import.meta.url), 'utf8');
const english = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');
const turkish = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');

test('installed conflicts take precedence over install availability badges', () => {
  const branch = serviceList.slice(
    serviceList.indexOf('{!s.is_installed ? ('),
    serviceList.indexOf(') : s.requires_missing', serviceList.indexOf('{!s.is_installed ? (')),
  );

  assert.ok(branch.indexOf('s.conflict_with ? (') >= 0);
  assert.ok(branch.indexOf('s.conflict_with ? (') < branch.indexOf('s.not_offered ? ('));
});

test('integration blocks are distinct from missing distro packages', () => {
  assert.match(serviceList, /not_offered_kind === 'integration'/);
  assert.match(serviceList, /services\.integrationPending/);
  assert.match(serviceList, /services\.notOffered/);
});

test('vsftpd points operators to the built-in encrypted SFTP path', () => {
  assert.match(serviceList, /s\.id === 'vsftpd'/);
  assert.match(serviceList, /services\.useBuiltInSFTP/);
  assert.match(serviceList, /onClick=\{\(\) => navigate\('\/domains'\)\}/);
  assert.match(serviceList, /type="button"/);
  assert.match(english, /'services\.useBuiltInSFTP': 'Use built-in SFTP'/);
  assert.match(turkish, /'services\.useBuiltInSFTP': 'Yerleşik SFTP’yi kullanın'/);
});

test('installed Roundcube exposes the durable repair path under fail-closed gates', () => {
  const installedBranchStart = serviceList.indexOf(') : (', serviceList.indexOf('{!s.is_installed ? ('));
  const installedBranch = serviceList.slice(installedBranchStart, serviceList.indexOf('</fieldset>', installedBranchStart));
  const roundcubeStart = installedBranch.indexOf(`s.kind === 'tool' && s.id === 'roundcube'`);

  assert.ok(roundcubeStart >= 0, 'installed tool controls must include an explicit Roundcube repair action');
  const repairAction = installedBranch.slice(roundcubeStart, installedBranch.indexOf('</button>', roundcubeStart));
  assert.match(repairAction, /startInstall\(\{/);
  assert.match(repairAction, /serviceId: s\.id/);
  assert.match(repairAction, /s\.repair_package \? \{ package: s\.repair_package \} : \{\}/);
  assert.match(repairAction, /disabled=\{mutationControlsDisabled \|\| busy === s\.id \|\| !s\.repair_available\}/);
  assert.match(repairAction, /t\('services\.repairWebmail'\)/);
  assert.match(english, /'services\.repairWebmail': 'Repair webmail'/);
  assert.match(turkish, /'services\.repairWebmail': 'Web postayı onar'/);
});

test('partial webmail uninstall keeps an idempotent cleanup retry reachable', () => {
  const loadStart = serviceList.indexOf('const loadServices = async');
  const loadFlow = serviceList.slice(loadStart, serviceList.indexOf('const scan = async', loadStart));
  const uninstallStart = serviceList.indexOf('const doUninstall = async');
  const uninstallFlow = serviceList.slice(uninstallStart, serviceList.indexOf('const handleAction', uninstallStart));
  const partialCode = uninstallFlow.indexOf(`error.code === 'WEBMAIL_UNINSTALL_PARTIAL'`);
  const partialEnd = uninstallFlow.indexOf('// The removal and fresh scan succeeded', partialCode);
  const partialBranch = uninstallFlow.slice(partialCode, partialEnd);

  assert.ok(partialCode >= 0, 'webmail partial-success code must be handled explicitly');
  assert.ok(partialEnd > partialCode, 'the cleanup retry branch must stay isolated from ordinary partial-success handling');
  assert.match(partialBranch, /setUninstallError\(error\)/);
  assert.match(partialBranch, /setRetryCleanup\(true\)/);
  assert.match(serviceList, /const \[cleanupAttempt, setCleanupAttempt\] = useState<'initial' \| 'retry' \| null>\(null\)/);
  assert.match(loadFlow, /\{ markUnverifiedOnFailure = true \}/);
  assert.match(loadFlow, /if \(markUnverifiedOnFailure\) markStateUnverified\(\)/);

  const attemptDecision = uninstallFlow.indexOf('const nextCleanupAttempt = retryCleanup');
  const retryDecision = uninstallFlow.indexOf(`? (uninstallError ? 'retry' : 'initial')`, attemptDecision);
  const applyAttempt = uninstallFlow.indexOf('setCleanupAttempt(nextCleanupAttempt);', retryDecision);
  const clearPriorError = uninstallFlow.indexOf('setUninstallError(null);', applyAttempt);
  const uninstallRequest = uninstallFlow.indexOf(`fetch('/api/v1/service/uninstall'`, clearPriorError);
  assert.ok(attemptDecision >= 0, 'each cleanup request must capture an explicit attempt kind');
  assert.ok(retryDecision > attemptDecision, 'a retry must be identified from the visible pre-request error');
  assert.ok(applyAttempt > retryDecision, 'the captured attempt kind must be published before request state changes');
  assert.ok(clearPriorError > applyAttempt, 'the request error may only be cleared after the attempt kind is captured');
  assert.ok(uninstallRequest > clearPriorError, 'the stable attempt label must be in place before the request starts');

  const initialAfterPartial = partialBranch.indexOf(`if (nextCleanupAttempt === null) setCleanupAttempt('initial')`);
  const awaitedLoad = partialBranch.indexOf('const verified = await loadServices({ markUnverifiedOnFailure: false })');
  const failedLoad = partialBranch.indexOf('if (!verified)', awaitedLoad);
  const closeOnFailure = partialBranch.indexOf('closeUninstallDialog();', failedLoad);
  const lockOnFailure = partialBranch.indexOf('markStateUnverified();', closeOnFailure);
  const returnOnFailure = partialBranch.indexOf('return;', lockOnFailure);
  const partialError = partialBranch.indexOf('setUninstallError(error);', returnOnFailure);
  const partialRetryMode = partialBranch.indexOf('setRetryCleanup(true);', partialError);
  const partialToast = partialBranch.indexOf(`showToast('error', apiErrorText(error, t, 'services.actionFailed'));`, partialRetryMode);
  assert.ok(initialAfterPartial >= 0, 'an ordinary uninstall entering cleanup must remain an initial attempt');
  assert.doesNotMatch(partialBranch, /setCleanupAttempt\('retry'\)/);
  assert.doesNotMatch(partialBranch, /setCleanupAttempt\(null\)/);
  assert.ok(awaitedLoad > initialAfterPartial, 'verified state reload must be awaited before dialog-directed partial copy is exposed');
  assert.ok(failedLoad > awaitedLoad, 'cleanup retry must keep its dialog when the verified reload succeeds');
  assert.ok(closeOnFailure > failedLoad, 'a failed reload must close and reset the cleanup dialog');
  assert.ok(lockOnFailure > closeOnFailure, 'the dialog must close before the fail-closed Rescan lock is enabled');
  assert.ok(returnOnFailure > lockOnFailure, 'a failed reload must leave before exposing stale dialog guidance');
  assert.ok(partialError > returnOnFailure, 'the partial banner may only be shown after a verified reload keeps the dialog reachable');
  assert.ok(partialRetryMode > partialError, 'Retry mode may only be exposed with the verified partial banner');
  assert.ok(partialToast > partialRetryMode, 'dialog-directed toast copy may only be shown after verified Retry state is reachable');
  assert.doesNotMatch(
    partialBranch.slice(awaitedLoad, returnOnFailure),
    /setUninstallError\(error\)|setRetryCleanup\(true\)|showToast\('error', apiErrorText/,
    'the reload-failure path must not expose stale instructions that point back to a closed dialog',
  );
  assert.doesNotMatch(partialBranch.slice(awaitedLoad, failedLoad), /closeUninstallDialog|setUninstallTarget\(null\)/);
  assert.doesNotMatch(partialBranch, /void loadServices|\.then\(/);
  assert.doesNotMatch(partialBranch, /setBusy\(/);
  assert.ok(uninstallFlow.indexOf('} finally {', partialCode) > lockOnFailure + partialCode);
  const finallyStart = uninstallFlow.indexOf('} finally {', partialCode);
  const clearBusy = uninstallFlow.indexOf('setBusy(null);', finallyStart);
  const resetAttempt = uninstallFlow.indexOf('setCleanupAttempt(null);', clearBusy);
  assert.ok(clearBusy > finallyStart, 'busy state must be held through the request and verified reload');
  assert.ok(resetAttempt > clearBusy, 'the explicit attempt kind may only reset when the request leaves busy state');

  const catchStart = uninstallFlow.indexOf('} catch {');
  const catchEnd = uninstallFlow.indexOf('} finally {', catchStart);
  const catchBranch = uninstallFlow.slice(catchStart, catchEnd);
  const closeOnException = catchBranch.indexOf('closeUninstallDialog();');
  const lockOnException = catchBranch.indexOf('markStateUnverified();');
  assert.ok(catchStart >= 0 && catchEnd > catchStart, 'the uninstall exception path must remain explicit');
  assert.ok(closeOnException >= 0, 'an uninstall exception must close and reset the stale retry dialog');
  assert.ok(lockOnException > closeOnException, 'the exception path must reset the dialog before enabling the fail-closed lock');

  const ordinaryPartialStart = uninstallFlow.indexOf(`error.code === 'FIREWALL_SYNC_FAILED'`);
  const ordinaryPartialEnd = uninstallFlow.indexOf('setUninstallError(error)', ordinaryPartialStart);
  const ordinaryPartialBranch = uninstallFlow.slice(ordinaryPartialStart, ordinaryPartialEnd);
  assert.match(ordinaryPartialBranch, /closeUninstallDialog\(\)/);
  assert.match(ordinaryPartialBranch, /await loadServices\(\)/);

  const openStart = serviceList.indexOf('const openUninstallDialog');
  const openHelper = serviceList.slice(openStart, serviceList.indexOf('const closeUninstallDialog', openStart));
  const closeStart = serviceList.indexOf('const closeUninstallDialog');
  const closeHelper = serviceList.slice(closeStart, serviceList.indexOf('const doUninstall', closeStart));
  assert.match(openHelper, /setUninstallError\(null\)/);
  assert.match(openHelper, /setRetryCleanup\(false\)/);
  assert.match(openHelper, /setCleanupAttempt\(null\)/);
  assert.match(closeHelper, /setUninstallTarget\(null\)/);
  assert.match(closeHelper, /setUninstallError\(null\)/);
  assert.match(closeHelper, /setRetryCleanup\(false\)/);
  assert.match(closeHelper, /setCleanupAttempt\(null\)/);
  assert.match(uninstallFlow, /closeUninstallDialog\(\);\s+showToast\(\s*'success'/);

  const cleanupSuccessChoice = uninstallFlow.indexOf('nextCleanupAttempt !== null', uninstallRequest);
  const cleanupSuccessCopy = uninstallFlow.indexOf(`t('services.webmailCleanupCompleted')`, cleanupSuccessChoice);
  const ordinarySuccessCopy = uninstallFlow.indexOf(`t('services.uninstalled', { name: service.name })`, cleanupSuccessCopy);
  const successToast = uninstallFlow.lastIndexOf('showToast(', cleanupSuccessChoice);
  const closeBeforeSuccess = uninstallFlow.lastIndexOf('closeUninstallDialog();', successToast);
  assert.ok(cleanupSuccessChoice >= 0, 'successful cleanup requests must branch on the captured attempt kind');
  assert.ok(cleanupSuccessCopy > cleanupSuccessChoice, 'initial and retry cleanup success must use dedicated cleanup-complete copy');
  assert.ok(ordinarySuccessCopy > cleanupSuccessCopy, 'ordinary uninstall success must retain the existing uninstalled copy');
  assert.ok(successToast >= 0 && closeBeforeSuccess >= 0 && closeBeforeSuccess < successToast, 'the successful request must close the dialog before its truthful toast');
  assert.match(english, /'services\.webmailCleanupCompleted': 'Roundcube webmail cleanup completed\.'/);
  assert.match(turkish, /'services\.webmailCleanupCompleted': 'Roundcube web posta temizliği tamamlandı\.'/);

  const dialogStart = serviceList.indexOf('{uninstallTarget && (');
  const dialog = serviceList.slice(dialogStart, serviceList.indexOf('</fieldset>', dialogStart));
  assert.match(dialog, /error=\{uninstallError\}/);
  assert.match(dialog, /retryCleanup=\{retryCleanup\}/);
  assert.match(dialog, /cleanupAttempt=\{cleanupAttempt\}/);
  assert.match(dialog, /onConfirm=\{\(\) => doUninstall\(uninstallTarget\)\}/);
  assert.match(uninstallFlow, /fetch\('\/api\/v1\/service\/uninstall'/);
  assert.match(uninstallFlow, /JSON\.stringify\(\{ service_id: service\.id \}\)/);
  assert.match(serviceList, /error \? 'services\.retryWebmailCleanup' : 'services\.cleanupWebmail'/);
  const dialogComponentStart = serviceList.indexOf('function UninstallServiceDialog');
  const dialogComponent = serviceList.slice(dialogComponentStart, serviceList.indexOf('// Firewall status', dialogComponentStart));
  assert.match(dialogComponent, /cleanupAttempt === 'retry' \? 'services\.retryingWebmailCleanup' : 'services\.cleaningWebmail'/);
  assert.doesNotMatch(dialogComponent, /error \? 'services\.retryingWebmailCleanup'/);
  assert.match(english, /'services\.retryWebmailCleanup': 'Retry webmail cleanup'/);
  assert.match(turkish, /'services\.retryWebmailCleanup': 'Web posta temizliğini yeniden dene'/);
  assert.match(english, /Use Retry webmail cleanup in this dialog/);
  const englishPartial = english.match(/'err\.WEBMAIL_UNINSTALL_PARTIAL': '([^']+)'/)?.[1] ?? '';
  const turkishPartial = turkish.match(/'err\.WEBMAIL_UNINSTALL_PARTIAL': '([^']+)'/)?.[1] ?? '';
  assert.match(englishPartial, /Roundcube is no longer detected, but its uninstall cleanup and finalization could not be fully verified\./);
  assert.match(turkishPartial, /Roundcube artık algılanmıyor ancak kaldırma temizliği ve sonlandırması tam olarak doğrulanamadı\./);
  assert.doesNotMatch(englishPartial, /proxy|socket/i);
  assert.doesNotMatch(turkishPartial, /proxy|soket/i);
});

test('not-installed Roundcube keeps a fail-closed cleanup path across reloads', () => {
  const notInstalledStart = serviceList.indexOf('{!s.is_installed ? (');
  const cleanupStart = serviceList.indexOf(`{s.id === 'roundcube' && (`, notInstalledStart);
  const installedStart = serviceList.indexOf('\n                                                    ) : (\n                                                    <>', cleanupStart);
  const cleanupAction = serviceList.slice(cleanupStart, serviceList.indexOf('</button>', cleanupStart));

  assert.ok(notInstalledStart >= 0, 'the not-installed controls branch must remain explicit');
  assert.ok(cleanupStart > notInstalledStart, 'not-installed Roundcube must expose a separate cleanup action');
  assert.ok(installedStart > cleanupStart, 'cleanup must live in the not-installed branch');
  assert.match(cleanupAction, /openWebmailCleanupDialog\(s\)/);
  assert.match(cleanupAction, /disabled=\{mutationControlsDisabled \|\| busy === s\.id\}/);
  assert.match(cleanupAction, /t\('services\.cleanupWebmail'\)/);
  assert.doesNotMatch(cleanupAction, /repair_available|not_offered|requires_missing|conflict_with|startInstall|setInstallTarget/);

  const helperStart = serviceList.indexOf('const openWebmailCleanupDialog');
  const cleanupHelper = serviceList.slice(helperStart, serviceList.indexOf('const closeUninstallDialog', helperStart));
  assert.match(cleanupHelper, /setUninstallTarget\(service\)/);
  assert.match(cleanupHelper, /setUninstallError\(null\)/);
  assert.match(cleanupHelper, /setRetryCleanup\(true\)/);
  assert.match(cleanupHelper, /setCleanupAttempt\(null\)/);

  const uninstallStart = serviceList.indexOf('const doUninstall = async');
  const uninstallFlow = serviceList.slice(uninstallStart, serviceList.indexOf('const handleAction', uninstallStart));
  assert.match(uninstallFlow, /if \(stateUnverified\)/);
  assert.match(uninstallFlow, /fetch\('\/api\/v1\/service\/uninstall'/);
  assert.match(uninstallFlow, /JSON\.stringify\(\{ service_id: service\.id \}\)/);

  const dialogStart = serviceList.indexOf('function UninstallServiceDialog');
  const dialog = serviceList.slice(dialogStart, serviceList.indexOf('// Firewall status', dialogStart));
  assert.match(dialog, /retryCleanup\s+\? t\('services\.cleanupWebmailTitle'\)/);
  assert.match(dialog, /t\(retryCleanup \? 'services\.cleanupWebmailWarn' : 'services\.uninstallWarn'\)/);
  assert.match(dialog, /!retryCleanup && \(/);
  assert.match(dialog, /error \? 'services\.retryWebmailCleanup' : 'services\.cleanupWebmail'/);

  const englishCleanup = english.match(/'services\.cleanupWebmailWarn': '([^']+)'/)?.[1] ?? '';
  const turkishCleanup = turkish.match(/'services\.cleanupWebmailWarn': '([^']+)'/)?.[1] ?? '';
  assert.match(englishCleanup, /permanently removes leftover Roundcube application files/i);
  assert.match(englishCleanup, /Roundcube-local settings/);
  assert.match(englishCleanup, /address-book database/);
  assert.match(englishCleanup, /does not remove email mailboxes or Maildir data/i);
  assert.match(englishCleanup, /does not install Roundcube/i);
  assert.match(turkishCleanup, /Roundcube uygulama dosyalarını/);
  assert.match(turkishCleanup, /adres defteri veritabanı/);
  assert.match(turkishCleanup, /posta kutularını veya Maildir verilerini silmez/);
  assert.match(turkishCleanup, /Roundcube’u kurmaz/);
});

test('state refresh failure copy never claims the operation completed', () => {
  const englishRefresh = english.match(/'err\.SERVICE_STATE_REFRESH_FAILED': '([^']+)'/)?.[1] ?? '';
  const turkishRefresh = turkish.match(/'err\.SERVICE_STATE_REFRESH_FAILED': '([^']+)'/)?.[1] ?? '';

  assert.match(englishRefresh, /operation outcome and current service state could not be verified/i);
  assert.match(englishRefresh, /Run Rescan/);
  assert.doesNotMatch(englishRefresh, /completed|succeeded|removed/i);
  assert.match(turkishRefresh, /İşlemin sonucu ve servisin güncel durumu doğrulanamadı/);
  assert.match(turkishRefresh, /Yeniden Tara/);
  assert.doesNotMatch(turkishRefresh, /tamamlandı|başarılı|kaldırıldı/i);
});
