package transport

// DNS publication failure reasons are typed causes of a verified, local
// publication failure (the change was not applied or was rolled back). Unlike
// the peer pending codes they are not propagation states: the exact operation
// failed and the same accepted change is retried by the owner's retry action.
// The reason names a cause the Panel can explain; raw tool output never
// crosses the wire in it.
//
// DNS yayımlama hata nedenleri, doğrulanmış yerel bir yayımlama hatasının
// (değişiklik uygulanmadı ya da geri alındı) türlü nedenleridir. Eş bekleme
// kodlarının aksine yayılım durumu değildir: tam işlem başarısız oldu ve aynı
// kabul edilmiş değişiklik sahibin yeniden deneme eylemiyle tekrarlanır. Ham
// araç çıktısı bu alanla kabloyu geçmez.
const (
	// DNSPublicationFailureBINDRNDCUnavailable: the product could not ask the
	// local named about zone state because rndc has no usable key or the
	// control channel refused it (missing or unreadable rndc.key, refused
	// authentication, nothing listening on the control port).
	DNSPublicationFailureBINDRNDCUnavailable = "bind_rndc_unavailable"
)

func ValidDNSPublicationFailureReason(reason string) bool {
	switch reason {
	case DNSPublicationFailureBINDRNDCUnavailable:
		return true
	default:
		return false
	}
}
