-- Subdomains: a subdomain (blog.example.com) is a normal hosted site — own
-- document root, nginx vhost and PHP pool — but it does NOT get its own DNS
-- zone. Instead an A record is added to the PARENT domain's existing zone, so
-- the subdomain resolves without a separate delegation. parent_domain_id links
-- the child to that parent; NULL means a top-level domain (the default).
--
-- Subdomain'ler: bir subdomain (blog.example.com) normal barındırılan bir
-- sitedir — kendi belge kökü, nginx vhost'u ve PHP havuzu — ama KENDİ DNS
-- zone'unu ALMAZ. Bunun yerine ANA domain'in var olan zone'una bir A kaydı
-- eklenir; böylece subdomain ayrı bir devir olmadan çözülür. parent_domain_id
-- çocuğu o ana domain'e bağlar; NULL tepe-seviye domain demektir (varsayılan).
ALTER TABLE domains ADD COLUMN parent_domain_id INTEGER REFERENCES domains(id);

CREATE INDEX idx_domains_parent ON domains(parent_domain_id);
