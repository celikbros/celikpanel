-- CelikPanel Multi-Tenant SQLite Schema
-- Combined and converted from PostgreSQL migrations

-- 1. Users
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    email TEXT UNIQUE NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('admin', 'reseller', 'customer')),
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_email ON users(email);

-- 2. Subscriptions
CREATE TABLE subscriptions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    max_domains INTEGER DEFAULT 5,
    max_databases INTEGER DEFAULT 10,
    max_email_accounts INTEGER DEFAULT 50,
    disk_quota_mb INTEGER DEFAULT 10240,
    bandwidth_quota_mb INTEGER DEFAULT 102400,
    status TEXT DEFAULT 'active' CHECK(status IN ('active', 'suspended', 'expired')),
    expires_at TEXT,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX idx_subscriptions_owner ON subscriptions(owner_id);
CREATE INDEX idx_subscriptions_status ON subscriptions(status);

-- IP Addresses
CREATE TABLE ip_addresses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    address TEXT NOT NULL UNIQUE,
    type TEXT CHECK(type IN ('ipv4', 'ipv6')),
    is_shared INTEGER DEFAULT 1,
    is_primary INTEGER DEFAULT 0,
    status TEXT DEFAULT 'active',
    created_at TEXT DEFAULT (datetime('now'))
);

INSERT INTO ip_addresses (address, type, is_shared, is_primary) VALUES ('0.0.0.0', 'ipv4', 1, 1);

-- 3. Domains
CREATE TABLE domains (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    subscription_id INTEGER NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    name TEXT UNIQUE NOT NULL,
    dns_zone_id INTEGER,
    status TEXT DEFAULT 'active' CHECK(status IN ('active', 'suspended', 'pending')),
    ip_address_id INTEGER REFERENCES ip_addresses(id) DEFAULT 1,
    is_temporary INTEGER DEFAULT 0,
    temporary_suffix TEXT,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX idx_domains_subscription ON domains(subscription_id);
CREATE INDEX idx_domains_ip ON domains(ip_address_id);

CREATE TABLE domain_aliases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    alias TEXT NOT NULL UNIQUE,
    created_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX idx_domain_aliases_domain ON domain_aliases(domain_id);

-- 4. Sites
CREATE TABLE sites (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    document_root TEXT NOT NULL,
    nginx_config TEXT,
    web_server TEXT DEFAULT 'nginx' CHECK(web_server IN ('nginx', 'apache', 'litespeed')),
    php_version TEXT DEFAULT '8.2',
    php_fpm_socket TEXT,
    ssl_enabled INTEGER DEFAULT 0,
    ssl_cert_path TEXT,
    ssl_key_path TEXT,
    ssl_type TEXT CHECK(ssl_type IN ('letsencrypt', 'custom', 'none')) DEFAULT 'none',
    ssl_auto_redirect INTEGER DEFAULT 0,
    force_https INTEGER DEFAULT 0,
    hsts_enabled INTEGER DEFAULT 0,
    hsts_max_age INTEGER DEFAULT 31536000,
    access_method TEXT CHECK(access_method IN ('ftp', 'sftp', 'both')) DEFAULT 'sftp',
    redirect_www INTEGER DEFAULT 0,
    redirect_https INTEGER DEFAULT 0,
    custom_error_pages TEXT DEFAULT '{}',
    status TEXT DEFAULT 'active',
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX idx_sites_domain ON sites(domain_id);
CREATE INDEX idx_sites_status ON sites(status);
CREATE INDEX idx_sites_web_server ON sites(web_server);
CREATE INDEX idx_sites_ssl ON sites(ssl_type);

-- 5. Email Accounts and Forwardings
CREATE TABLE email_accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    address TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    quota_mb INTEGER DEFAULT 1024,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX idx_email_domain ON email_accounts(domain_id);

CREATE TABLE email_forwardings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    source TEXT NOT NULL UNIQUE,
    destination TEXT NOT NULL,
    created_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX idx_email_forwardings_domain ON email_forwardings(domain_id);

-- 6. FTP Accounts
CREATE TABLE ftp_accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    subscription_id INTEGER NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    home_dir TEXT NOT NULL,
    access_type TEXT CHECK(access_type IN ('ftp', 'sftp')) DEFAULT 'sftp',
    ssh_key TEXT,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX idx_ftp_subscription ON ftp_accounts(subscription_id);

-- 7. SSL Certificates
CREATE TABLE ssl_certificates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK(type IN ('letsencrypt', 'custom')),
    cert_path TEXT NOT NULL,
    key_path TEXT NOT NULL,
    chain_path TEXT,
    issuer TEXT,
    subject TEXT,
    issued_at TEXT,
    expires_at TEXT NOT NULL,
    auto_renew INTEGER DEFAULT 1,
    last_renewal_attempt TEXT,
    renewal_status TEXT,
    status TEXT DEFAULT 'active' CHECK(status IN ('active', 'expired', 'revoked')),
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX idx_ssl_certificates_domain ON ssl_certificates(domain_id);

-- 8. Audit Logs
CREATE TABLE audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    resource_type TEXT,
    resource_id INTEGER,
    ip_address TEXT,
    user_agent TEXT,
    created_at TEXT DEFAULT (datetime('now'))
);

-- 9. Service Metadata
CREATE TABLE service_metadata (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    service_name TEXT UNIQUE NOT NULL,
    service_type TEXT NOT NULL,
    version TEXT,
    config_paths TEXT,
    discovered_at TEXT DEFAULT (datetime('now')),
    last_verified TEXT DEFAULT (datetime('now')),
    is_primary INTEGER DEFAULT 1,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

-- 10. Database Management V2
CREATE TABLE database_server_types (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    display_name TEXT NOT NULL,
    default_port INTEGER,
    icon TEXT,
    supports_users INTEGER DEFAULT 1,
    supports_databases INTEGER DEFAULT 1,
    created_at TEXT DEFAULT (datetime('now'))
);

INSERT INTO database_server_types (name, display_name, default_port, icon, supports_users, supports_databases)
VALUES 
    ('postgresql', 'PostgreSQL', 5432, '🐘', 1, 1),
    ('mariadb', 'MariaDB', 3306, '🦭', 1, 1)
ON CONFLICT (name) DO NOTHING;

CREATE TABLE database_servers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    subscription_id INTEGER NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    type_id INTEGER NOT NULL REFERENCES database_server_types(id),
    name TEXT NOT NULL,
    version TEXT,
    host TEXT DEFAULT 'localhost',
    port INTEGER NOT NULL,
    is_default INTEGER DEFAULT 0,
    root_password_encrypted TEXT,
    connection_params TEXT,
    status TEXT DEFAULT 'active',
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now')),
    UNIQUE(subscription_id, host, port)
);

CREATE TABLE database_users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id INTEGER NOT NULL REFERENCES database_servers(id) ON DELETE CASCADE,
    subscription_id INTEGER NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    username TEXT NOT NULL,
    password TEXT NOT NULL,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now')),
    UNIQUE(server_id, username)
);

CREATE TABLE databases_v2 (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    server_id INTEGER NOT NULL REFERENCES database_servers(id) ON DELETE CASCADE,
    subscription_id INTEGER NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    domain_id INTEGER REFERENCES domains(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now')),
    UNIQUE(server_id, name)
);

CREATE TABLE database_user_grants (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    database_id INTEGER NOT NULL REFERENCES databases_v2(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES database_users(id) ON DELETE CASCADE,
    privileges TEXT DEFAULT 'ALL',
    created_at TEXT DEFAULT (datetime('now')),
    UNIQUE(database_id, user_id)
);

-- 11. Legacy Databases Table (if needed by some old code, better to keep it for safety)
CREATE TABLE databases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    subscription_id INTEGER NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    db_type TEXT DEFAULT 'postgresql' NOT NULL,
    db_user TEXT NOT NULL,
    db_password TEXT NOT NULL,
    host TEXT DEFAULT 'localhost',
    port INTEGER DEFAULT 5432,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now')),
    UNIQUE(subscription_id, name),
    UNIQUE(subscription_id, db_user)
);

-- 12. PowerDNS Schema
CREATE TABLE pdns_domains (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  master TEXT DEFAULT NULL,
  last_check INTEGER DEFAULT NULL,
  type TEXT NOT NULL,
  notified_serial INTEGER DEFAULT NULL,
  account TEXT DEFAULT NULL,
  options TEXT DEFAULT NULL,
  catalog TEXT DEFAULT NULL
);

CREATE TABLE pdns_records (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  domain_id INTEGER DEFAULT NULL REFERENCES pdns_domains(id) ON DELETE CASCADE,
  name TEXT DEFAULT NULL,
  type TEXT DEFAULT NULL,
  content TEXT DEFAULT NULL,
  ttl INTEGER DEFAULT NULL,
  prio INTEGER DEFAULT NULL,
  disabled INTEGER DEFAULT 0,
  ordername TEXT,
  auth INTEGER DEFAULT 1
);
CREATE INDEX rec_name_index ON pdns_records(name);

CREATE TABLE pdns_supermasters (
  ip TEXT NOT NULL,
  nameserver TEXT NOT NULL,
  account TEXT NOT NULL,
  PRIMARY KEY (ip, nameserver)
);

CREATE TABLE pdns_comments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  domain_id INTEGER NOT NULL REFERENCES pdns_domains(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  modified_at INTEGER NOT NULL,
  account TEXT DEFAULT NULL,
  comment TEXT NOT NULL
);

CREATE TABLE pdns_domainmetadata (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  domain_id INTEGER NOT NULL REFERENCES pdns_domains(id) ON DELETE CASCADE,
  kind TEXT,
  content TEXT
);

CREATE TABLE pdns_cryptokeys (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  domain_id INTEGER NOT NULL REFERENCES pdns_domains(id) ON DELETE CASCADE,
  flags INTEGER NOT NULL,
  active INTEGER,
  published INTEGER DEFAULT 1,
  content TEXT
);

CREATE TABLE pdns_tsigkeys (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT,
  algorithm TEXT,
  secret TEXT,
  UNIQUE(name, algorithm)
);

-- Default Admin
INSERT INTO users (username, email, password_hash, role) VALUES
('admin', 'admin@celikpanel.local', '$2a$10$rVQ8K5h6Z.Zg0qX7J3K3KuF7pB3vZ8mN9lD5qE0wY0kX0H0L0M0N0', 'admin');

INSERT INTO subscriptions (owner_id, name, max_domains, max_databases, status) VALUES
(1, 'Admin Subscription', 999, 999, 'active');
