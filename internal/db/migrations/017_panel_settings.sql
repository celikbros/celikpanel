-- Server-level settings the panel needs but cannot derive: things that are a
-- CHOICE about this installation rather than a fact about the machine.
--
-- The first of them is the nameserver pair, and it exists because of a real
-- logic error the operator caught (25 Jul): the zone template wrote
-- `ns1.<domain>` / `ns2.<domain>` into EVERY zone. That made each hosted
-- domain its own nameserver, so biovision.health would have had to register
-- glue for ns1.biovision.health — which is the "vanity nameserver" premium
-- feature, not the default. A hosting server has ONE nameserver pair
-- (ns1.celikhost.com / ns2.celikhost.com), registered once with glue, and
-- every hosted domain simply delegates to those names.
--
-- Panelin ihtiyaç duyduğu ama türetemeyeceği sunucu düzeyi ayarlar: makine
-- hakkında bir olgu değil, bu kurulum hakkında bir SEÇİM olan şeyler.
--
-- İlki ad sunucusu çiftidir ve operatörün yakaladığı gerçek bir mantık hatası
-- yüzünden vardır (25 Tem): zone şablonu HER zone'a `ns1.<alanadı>` /
-- `ns2.<alanadı>` yazıyordu. Bu, barındırılan her alan adını kendi ad
-- sunucusu yapıyordu; yani biovision.health'in ns1.biovision.health için glue
-- kaydettirmesi gerekecekti — ki bu, varsayılan değil "vanity nameserver"
-- adı verilen ek özelliktir. Bir barındırma sunucusunun TEK bir ad sunucusu
-- çifti vardır (ns1.celikhost.com / ns2.celikhost.com), bir kez glue ile
-- kaydedilir ve barındırılan her alan adı yalnızca o adlara devreder.

CREATE TABLE IF NOT EXISTS panel_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
