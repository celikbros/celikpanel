#!/usr/bin/env python3
"""Stub-API preview server: serves web/dist with realistic fake API responses
so any page can be rendered/screenshotted without a live backend.

Kullanım / Usage:
    cd web && npm run build
    PORT=8199 python3 tools/dev-preview/preview-server.py            # dolu sunucu / busy server
    PORT=8199 FIREWALL=on python3 tools/dev-preview/preview-server.py
    PORT=8198 FRESH=1 python3 tools/dev-preview/preview-server.py    # taze sunucu / fresh server

Kural / Rule: stub HER ZAMAN gerçek API şemasını taklit eder (tipler dahil —
örn. capabilities.mail_server BOOL'dur). Sadakatsiz stub gerçek hatayı
maskeler; bu bir kez yaşandı (Dashboard posta adımı).
"""
import http.server, json, os, sys, mimetypes, datetime

DIST = os.environ.get('DIST', os.path.join(os.path.dirname(__file__), '..', '..', 'web', 'dist'))
PORT = int(os.environ.get('PORT', '8199'))
FIREWALL_ON = os.environ.get('FIREWALL', 'off') == 'on'
mimetypes.add_type('font/woff2', '.woff2')

now = datetime.datetime.now(datetime.timezone.utc).isoformat()

def svc(id, name, desc, icon, cat, installed, status='inactive (dead)', versions=None, **kw):
    return {
        'id': id, 'name': name, 'description': desc, 'icon': icon, 'category': cat,
        'versions': versions or ['default'], 'status': status, 'is_installed': installed,
        'config_files': [], **kw,
    }

SERVICES = [
    svc('nginx', 'Nginx', 'Reverse Proxy Server', '🟩', 'web', True, 'active (running)', ['1.26.3']),
    svc('apache', 'Apache', 'Web Server', '🪶', 'web', False, conflict_with='nginx'),
    svc('php-fpm', 'PHP-FPM', 'PHP FastCGI Process Manager', '🐘', 'web', True, 'active (running)', ['8.4']),
    svc('mariadb', 'MariaDB', 'Database Server', '🦭', 'database', True, 'inactive (dead)', ['11.4.5']),
    svc('postgresql', 'PostgreSQL', 'Database Server', '🐬', 'database', False),
    svc('phpmyadmin', 'phpMyAdmin', 'MariaDB/MySQL web admin tool', '🧰', 'database', False, requires_missing=['web-server']),
    svc('phppgadmin', 'phpPgAdmin', 'PostgreSQL web admin tool', '🧪', 'database', False, requires_missing=['postgresql']),
    svc('postfix', 'Postfix', 'SMTP Server', '📮', 'email', True, 'active (running)', ['3.9.1']),
    svc('dovecot', 'Dovecot', 'IMAP/POP3 Server', '📥', 'email', True, 'active (running)', ['2.4.4']),
    svc('spamassassin', 'SpamAssassin', 'Spam Filter', '🛡️', 'email', False),
    svc('pdns', 'PowerDNS', 'Authoritative DNS Server', '🌐', 'dns', True, 'active (running)', ['4.9.2'],
        config_files=[{'path': '/etc/powerdns/pdns.conf'}]),
    svc('bind', 'BIND', 'DNS Server', '📡', 'dns', False, conflict_with='pdns'),
    svc('clamav', 'ClamAV', 'Antivirus scanner', '🦠', 'security', False),
    svc('proftpd', 'ProFTPD', 'FTP Server', '📁', 'ftp', False),
]

API = {
    '/api/v1/auth/me': {'username': 'admin', 'role': 'admin'},
    # mail_server is a BOOL in the real API — keep the stub faithful.
    '/api/v1/hosting/capabilities': {
        'web_server': 'nginx', 'php_versions': ['8.4'], 'dns_server': 'pdns',
        'mail_server': True, 'database_servers': ['mariadb'], 'db_tools': [],
    },
    '/api/v1/subscriptions': {'subscriptions': [
        {'id': 1, 'name': 'Admin Subscription', 'owner': 'admin', 'usage': {
            'disk_used_bytes': 3_400_000_000, 'disk_limit_bytes': 10_737_418_240,
            'domains': 3, 'domains_limit': 10, 'databases': 2, 'databases_limit': 10,
            'mail_accounts': 4, 'mail_limit': 20,
        }},
    ]},
    '/api/v1/domains': [
        {'id': 1, 'domain_name': 'celikpanel.cloud', 'php_version': '8.4', 'ssl_enabled': True,
         'status': 'active', 'project_type': 'php', 'created_at': now, 'disk_usage': 1_270_000_000, 'bandwidth': 5_100_000_000},
        {'id': 2, 'domain_name': 'ornek-site.com', 'php_version': '', 'ssl_enabled': False,
         'status': 'active', 'project_type': 'static', 'created_at': now, 'disk_usage': 42_000_000, 'bandwidth': 310_000_000},
        {'id': 3, 'domain_name': 'blog.celikpanel.cloud', 'php_version': '8.4', 'ssl_enabled': True,
         'status': 'active', 'project_type': 'php', 'created_at': now, 'disk_usage': 610_000_000, 'bandwidth': 1_200_000_000, 'parent_id': 1},
    ],
    '/api/v1/database-servers': [
        {'id': 1, 'type_id': 1, 'type_name': 'mariadb', 'type_icon': '🦭', 'name': 'MariaDB',
         'version': '11.4.5', 'host': '127.0.0.1', 'port': 3306, 'is_default': True, 'status': 'active', 'created_at': now},
        {'id': 2, 'type_id': 2, 'type_name': 'postgresql', 'type_icon': '🐬', 'name': 'PostgreSQL',
         'version': '16.4', 'host': '127.0.0.1', 'port': 5432, 'is_default': False, 'status': 'active', 'created_at': now},
    ],
    '/api/v1/database-servers/1/databases': [
        {'id': 1, 'name': 'wp_celikpanel', 'users': ['wp_celik'], 'created_at': now},
        {'id': 2, 'name': 'blog_db', 'users': ['blog_user', 'wp_celik'], 'created_at': now},
    ],
    '/api/v1/database-servers/1/users': [
        {'id': 1, 'username': 'wp_celik', 'databases': ['wp_celikpanel', 'blog_db'], 'created_at': now},
        {'id': 2, 'username': 'blog_user', 'databases': ['blog_db'], 'created_at': now},
    ],
    '/api/v1/database-servers/2/databases': [],
    '/api/v1/database-servers/2/users': [],
    '/api/v1/users': {'users': [
        {'id': 1, 'username': 'admin', 'email': 'admin@celikpanel.cloud', 'role': 'admin', 'status': 'active',
         'subscriptions': 1, 'domains': 3, 'created_at': now},
        {'id': 2, 'username': 'bayi1', 'email': 'bayi@ornek.com', 'role': 'reseller', 'status': 'active',
         'parent_name': 'admin', 'subscriptions': 2, 'domains': 5, 'created_at': now},
        {'id': 3, 'username': 'musteri1', 'email': 'musteri@ornek.com', 'role': 'customer', 'status': 'suspended',
         'parent_name': 'bayi1', 'subscriptions': 1, 'domains': 1, 'created_at': now},
    ]},
    '/api/v1/plans': {'plans': [
        {'id': 1, 'name': 'Starter', 'max_domains': 1, 'max_databases': 2, 'max_email_accounts': 5,
         'disk_quota_mb': 5120, 'bandwidth_quota_mb': 51200, 'subscribers': 2},
        {'id': 2, 'name': 'Pro', 'max_domains': 10, 'max_databases': 20, 'max_email_accounts': 50,
         'disk_quota_mb': 20480, 'bandwidth_quota_mb': 204800, 'subscribers': 1},
    ]},
    '/api/v1/managed-services': {'scanned_at': now, 'services': SERVICES},
    '/api/v1/firewall': (
        {'enabled': True, 'tcp_ports': [22, 2083, 80, 443, 25], 'udp_ports': [53], 'ssh_ports': [22]}
        if FIREWALL_ON else
        {'enabled': False, 'tcp_ports': [], 'udp_ports': [], 'ssh_ports': [22]}
    ),
    '/api/v1/vpn/status': {'installed': True, 'running': True, 'endpoint': '203.0.113.10', 'port': 51820, 'peer_count': 2},
    '/api/v1/vpn/peers': {'peers': [
        {'id': 1, 'name': 'ali-laptop', 'ip': '10.8.0.2', 'last_handshake': 1783000000, 'rx_bytes': 182_000_000, 'tx_bytes': 1_270_000_000, 'subscription': ''},
        {'id': 2, 'name': 'ofis-pc', 'ip': '10.8.0.3', 'last_handshake': 0, 'rx_bytes': 0, 'tx_bytes': 0, 'subscription': 'Admin Subscription'},
    ]},
    '/api/v1/products': {'products': [
        {'id': 1, 'name': 'WireGuard VPN', 'description': 'Abonelige ozel VPN erisimi', 'monthly_price_cents': 500},
        {'id': 2, 'name': 'Dedicated IP', 'description': 'Abonelige ozel IPv4 adresi', 'monthly_price_cents': 300},
    ], 'held': [1]},
    '/api/v1/audit-logs': {'entries': [
        {'id': 1, 'username': 'admin', 'action': 'service.install nginx', 'resource_type': 'service', 'resource_id': 1, 'ip_address': '203.0.113.20', 'created_at': '2026-07-10 12:41:03'},
        {'id': 2, 'username': 'admin', 'action': 'firewall.enable', 'resource_type': 'firewall', 'resource_id': None, 'ip_address': '203.0.113.20', 'created_at': '2026-07-10 12:39:41'},
        {'id': 3, 'username': 'bayi1', 'action': 'login.success', 'resource_type': 'session', 'resource_id': None, 'ip_address': '198.51.100.4', 'created_at': '2026-07-10 11:02:19'},
    ]},
    '/api/v1/auth/2fa/status': {'enabled': False},
    '/api/v1/panel/certificate': {'status': 'self-signed', 'domain': '', 'expires_at': None},
    '/api/v1/config': {'Content': 'launch=gsqlite3\ngsqlite3-dnssec=yes\ngsqlite3-database=/var/lib/powerdns/pdns.sqlite3\nlocal-address=0.0.0.0, ::\nlocal-port=53\n', 'Parsed': ''},
    '/api/v1/system/stats': {
        'hostname': 'server.celikpanel.cloud', 'os': 'Debian GNU/Linux 13',
        'uptime_seconds': 47 * 86400 + 7200, 'cpu_percent': 23, 'cpu_cores': 8,
        'load_avg': [1.84, 1.21, 0.94],
        'mem_used_bytes': int(19.6 * 1024**3), 'mem_total_bytes': 32 * 1024**3,
        'disk_used_bytes': 414 * 1024**3, 'disk_total_bytes': 512 * 1024**3,
    },
    '/api/v1/dashboard': {
        'databases': 2, 'mail_accounts': 4,
        'expiring_certs': [
            {'domain_name': 'celikpanel.cloud', 'days_left': 5},
            {'domain_name': 'blog.celikpanel.cloud', 'days_left': 19},
        ],
    },
}

# FRESH=1: brand-new server — nothing installed, no domains, journey mode.
if os.environ.get('FRESH') == '1':
    for s in SERVICES:
        s['is_installed'] = False
        s['status'] = 'inactive (dead)'
        s['versions'] = ['default']
    API['/api/v1/domains'] = []
    API['/api/v1/hosting/capabilities'] = {
        'web_server': '', 'php_versions': [], 'dns_server': '',
        'mail_server': False, 'database_servers': [], 'db_tools': [],
    }
    API['/api/v1/dashboard'] = {'databases': 0, 'mail_accounts': 0, 'expiring_certs': []}
    API['/api/v1/audit-logs'] = {'entries': []}
    API['/api/v1/users'] = {'users': [
        {'id': 1, 'username': 'admin', 'email': 'a@b.c', 'role': 'admin', 'status': 'active',
         'subscriptions': 0, 'domains': 0, 'created_at': now},
    ]}

# The databases page speaks /api/v2 — mirror those keys.
for k in [k for k in list(API) if k.startswith('/api/v1/database-servers')]:
    API[k.replace('/api/v1/', '/api/v2/')] = API[k]

class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def _json(self, obj, code=200):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path = self.path.split('?')[0]
        if path in API:
            return self._json(API[path])
        if path.startswith('/api/'):
            return self._json({'error': 'stub: not implemented'}, 404)
        fs = os.path.join(DIST, path.lstrip('/'))
        if not os.path.isfile(fs):
            fs = os.path.join(DIST, 'index.html')  # SPA fallback
        ctype = mimetypes.guess_type(fs)[0] or 'application/octet-stream'
        with open(fs, 'rb') as f:
            data = f.read()
        self.send_response(200)
        self.send_header('Content-Type', ctype)
        self.send_header('Content-Length', str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_POST(self):
        self._json({'error': 'stub'}, 404)

print(f'stub panel on :{PORT} (firewall={"on" if FIREWALL_ON else "off"}, fresh={os.environ.get("FRESH", "0")})', file=sys.stderr)
http.server.ThreadingHTTPServer(('127.0.0.1', PORT), H).serve_forever()
