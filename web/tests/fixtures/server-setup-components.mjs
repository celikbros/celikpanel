// A deliberately small catalogue still exercises transitive dependencies,
// reciprocal mail dependencies, mandatory protection and conflicting providers.
export function setupCatalogFixture() {
    const row = (id, name, category, dependencies = [], extra = {}) => ({
        id, name, category, dependencies, conflicts: [], supported: true, installed: false, ...extra,
    });
    return {
        version: 1,
        inventory_state: 'ready',
        presets: {
            web: ['nginx', 'php-fpm', 'mariadb'],
            web_mail: ['nginx', 'php-fpm', 'mariadb', 'postfix', 'dovecot', 'roundcube', 'rspamd'],
            application: ['nginx', 'node', 'mariadb'],
            dns: [],
            custom: [],
        },
        required_components: ['nftables', 'certbot'],
        components: [
            row('nginx', 'Nginx', 'web'),
            row('php-fpm', 'PHP-FPM', 'web'),
            row('node', 'Node.js', 'web', ['nginx']),
            row('mariadb', 'MariaDB', 'database'),
            row('postgresql', 'PostgreSQL', 'database'),
            row('phpmyadmin', 'phpMyAdmin', 'database', ['mariadb', 'nginx', 'php-fpm']),
            row('postfix', 'Postfix', 'email', ['dovecot']),
            row('dovecot', 'Dovecot', 'email', ['postfix']),
            row('roundcube', 'Roundcube', 'email', ['postfix', 'dovecot', 'nginx', 'php-fpm']),
            row('rspamd', 'Rspamd', 'email', ['postfix']),
            row('redis', 'Redis', 'cache', [], { conflicts: ['valkey'] }),
            row('valkey', 'Valkey', 'cache', [], { conflicts: ['redis'] }),
            row('clamav', 'ClamAV', 'security', [], { supported: false, reason: 'server_setup_service_unsupported:clamav' }),
            row('nftables', 'Firewall', 'security', [], { installed: true }),
            row('certbot', 'Certbot', 'security'),
        ],
    };
}
