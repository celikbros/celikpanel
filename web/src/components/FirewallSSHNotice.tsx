import { ShieldAlert } from 'lucide-react';
import { useI18n } from '../i18n';

// Turning the firewall on keeps every proven SSH port open so the operator can
// never be locked out of their own server. On a server that has no SSH service
// at all there is no door to lock, and the product used to refuse anyway with
// one sentence and no way forward. It now says which of three things is true
// and, on the only one an operator may knowingly accept, asks for that consent
// in its own words.
//
// Güvenlik duvarını açmak, kanıtlanmış her SSH portunu açık tutar; böylece
// operatör kendi sunucusunun dışında asla kalmaz. Hiç SSH servisi olmayan bir
// sunucuda kilitlenecek bir kapı yoktur; ürün eskiden yine de tek bir cümleyle
// ve ileri gidecek bir yol bırakmadan reddediyordu. Artık üç durumdan hangisi
// doğruysa onu söylüyor ve operatörün bilerek kabul edebileceği tek durumda bu
// onayı kendi sözleriyle istiyor.

export const FIREWALL_SSH_REASONS = [
    'no_ssh_service',
    'ssh_not_listening',
    'discovery_failed',
] as const;

export type FirewallSSHReason = (typeof FIREWALL_SSH_REASONS)[number];

const REASON_SET: ReadonlySet<string> = new Set(FIREWALL_SSH_REASONS);

// A reason the panel does not know is no reason at all. A newer server must
// never make an older screen invent a state it cannot describe.
// Panelin bilmediği bir neden, hiç neden değildir. Yeni bir sunucu, eski bir
// ekrana tarif edemediği bir durumu asla uydurtmamalıdır.
export function readFirewallSSHReason(value: unknown): FirewallSSHReason | null {
    if (typeof value !== 'string' || !REASON_SET.has(value)) return null;
    return value as FirewallSSHReason;
}

// The state line the firewall banner carries. A server without SSH is a fact
// about the server, not a fault, so it reads in the muted voice; the other two
// are things the operator has to deal with and read in the warning voice.
// Güvenlik duvarı bandının taşıdığı durum satırı. SSH'sız bir sunucu, bir arıza
// değil sunucu hakkında bir olgudur; bu yüzden sessiz sesle okunur. Diğer ikisi
// operatörün ilgilenmesi gereken şeylerdir ve uyarı sesiyle okunur.
export function FirewallSSHReasonLine({ reason }: { reason: FirewallSSHReason }) {
    const { t } = useI18n();
    return (
        <p
            className={`mt-1 text-xs leading-5 ${
                reason === 'no_ssh_service' ? 'text-fg-muted' : 'text-warning'
            }`}
        >
            {t(`firewall.ssh.${reason}.state` as Parameters<typeof t>[0])}
        </p>
    );
}

// The acknowledgement itself. It is its own consent, never folded into the
// confirmation the operator already gave by opening this dialog: a click meant
// for "turn the firewall on" must not stand in for "I accept that this server
// has no SSH way back in".
// Onayın kendisi. Kendi başına bir rızadır; operatörün bu pencereyi açarken
// verdiği onayın içine asla katlanmaz: "güvenlik duvarını aç" için yapılan bir
// tıklama, "bu sunucuda geri dönecek bir SSH yolu olmadığını kabul ediyorum"un
// yerine geçmemelidir.
export function FirewallNoSSHAcknowledgement({
    id,
    checked,
    disabled,
    onChange,
}: {
    id: string;
    checked: boolean;
    disabled?: boolean;
    onChange: (value: boolean) => void;
}) {
    const { t } = useI18n();
    return (
        <section className="mb-4 rounded-lg border border-warning/40 bg-warning/10 p-3">
            <div className="flex items-start gap-2">
                <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                <div className="min-w-0">
                    <p className="text-sm font-semibold text-fg">
                        {t('firewall.ssh.no_ssh_service.title')}
                    </p>
                    <p id={`${id}-body`} className="mt-1 text-xs leading-5 text-fg-muted">
                        {t('firewall.ssh.no_ssh_service.body')}
                    </p>
                </div>
            </div>
            {/* The acknowledgement sits on the same ground as what it accepts,
                separated by a hairline, exactly as the DNS takeover's own
                acknowledgement does. A nested panel would read as a second
                subject. / Onay, kabul ettiği şeyle aynı zeminde, bir saç
                çizgisiyle ayrılarak durur; tıpkı DNS devralmasının kendi onayı
                gibi. İç içe bir panel ikinci bir konu gibi okunurdu. */}
            <label
                className="mt-3 flex cursor-pointer items-start gap-2 border-t border-warning/30 pt-3"
                htmlFor={id}
            >
                <input
                    id={id}
                    type="checkbox"
                    checked={checked}
                    disabled={disabled}
                    aria-describedby={`${id}-body`}
                    onChange={(event) => onChange(event.target.checked)}
                    className="mt-0.5 h-4 w-4 shrink-0 accent-primary"
                />
                <span className="text-xs leading-5 text-fg">
                    {t('firewall.ssh.no_ssh_service.acknowledgement')}
                </span>
            </label>
        </section>
    );
}
