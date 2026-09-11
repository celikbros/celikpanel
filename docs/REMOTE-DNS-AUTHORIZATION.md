# Remote DNS implementation authorization

*11 September 2026 · English / Türkçe*

The assistant explicitly asked whether it may develop and locally test the
credential-bearing connection to existing CelikPanel DNS infrastructure. The
user answered: **“herşeyi onaylıyorum devam et”** (“I approve everything; continue”).

This resolves the earlier automatic-review request for separate authorization.
Continue the implementation and local acceptance tests described in
[the security contract](REMOTE-DNS-SECURITY.md), including scoped credentials,
enrollment, publication, revocation and recovery. Do not ask for this permission
again. The reply does not revoke the standing instruction that the user starts
all installed-panel updates inside CelikPanel. Source implementation is separate
from real server enrollment or release installation.

Asistan, mevcut CelikPanel DNS altyapısına kimlik bilgisi taşıyan bağlantının
kodunu geliştirip yerel testlerle doğrulamak için açık onay istedi. Kullanıcı
**“herşeyi onaylıyorum devam et”** yanıtıyla onayladı. Önceki otomatik incelemenin
istediği ayrı yetkilendirme böylece sağlandı; aynı izin tekrar sorulmaz.
[Güvenlik sözleşmesindeki](REMOTE-DNS-SECURITY.tr.md) sınırlı yetki, eşleştirme,
yayın, iptal ve kurtarma kodu ile yerel testlere devam edilir. Kurulu panellerin
güncellemesini kullanıcının panel içinden başlatması kuralı geçerlidir.
