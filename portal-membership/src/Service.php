<?php
declare(strict_types=1);

namespace CelikPanel\Membership;

use PDO;
use RuntimeException;
use Throwable;

final class Problem extends RuntimeException {}

/** Central state only. Never installed on a customer's panel server. */
final class Service
{
    public readonly PDO $db;
    private \Closure $clock;
    private \Closure $send;
    public function __construct(string $database, private string $signingKey, callable $send, ?callable $clock = null)
    {
        if (strlen($signingKey) !== SODIUM_CRYPTO_SIGN_SECRETKEYBYTES) {
            throw new RuntimeException('Invalid license signing key');
        }
        $this->clock = \Closure::fromCallable($clock ?? static fn(): int => time());
        $this->send = \Closure::fromCallable($send);
        $this->db = new PDO('sqlite:' . $database, null, null, [PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION]);
        $this->db->exec('PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL;');
        $this->db->exec(<<<'SQL'
CREATE TABLE IF NOT EXISTS members (
 id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE, password TEXT NOT NULL,
 verified INTEGER NOT NULL DEFAULT 0, epoch INTEGER NOT NULL DEFAULT 1, created INTEGER NOT NULL,
 accepted_terms TEXT NOT NULL DEFAULT '2026-09-09'
);
CREATE TABLE IF NOT EXISTS tokens (
 hash TEXT PRIMARY KEY, member_id INTEGER NOT NULL REFERENCES members(id),
 purpose TEXT NOT NULL CHECK(purpose IN ('verify','reset')), expires INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS licenses (
 id TEXT PRIMARY KEY, member_id INTEGER NOT NULL REFERENCES members(id),
 key_hash TEXT NOT NULL UNIQUE, key_suffix TEXT NOT NULL, created INTEGER NOT NULL,
 activated INTEGER, expires INTEGER, server_id TEXT, hostname TEXT,
 generation INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS events (
 id INTEGER PRIMARY KEY, member_id INTEGER NOT NULL REFERENCES members(id),
 license_id TEXT, kind TEXT NOT NULL, created INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS rate_limits (
 bucket TEXT PRIMARY KEY, count INTEGER NOT NULL, expires INTEGER NOT NULL
);
SQL);
        $columns=$this->db->query('PRAGMA table_info(members)')->fetchAll(PDO::FETCH_COLUMN,1);
        if (!in_array('accepted_terms',$columns,true)) {
            $this->db->exec("ALTER TABLE members ADD COLUMN accepted_terms TEXT NOT NULL DEFAULT '2026-09-09'");
        }
        $this->atomic(function (): void {
            $columns=$this->db->query('PRAGMA table_info(licenses)')->fetchAll(PDO::FETCH_COLUMN,1);
            if (!in_array('key_encrypted',$columns,true)) { $this->db->exec('ALTER TABLE licenses ADD COLUMN key_encrypted TEXT'); }
            if (!in_array('server_ip',$columns,true)) { $this->db->exec('ALTER TABLE licenses ADD COLUMN server_ip TEXT'); }
            foreach ($this->db->query('SELECT id,created FROM licenses WHERE expires IS NULL')->fetchAll(PDO::FETCH_ASSOC) as $l) {
                $this->run('UPDATE licenses SET expires=? WHERE id=?',[self::anniversary((int)$l['created']),$l['id']]);
            }
        });
    }
    public function now(): int { return ($this->clock)(); }
    public static function hash(string $secret): string { return hash('sha256', $secret); }
    private static function secret(): string { return bin2hex(random_bytes(32)); }
    private function one(string $sql, array $args): ?array
    {
        $s = $this->db->prepare($sql); $s->execute($args);
        $r = $s->fetch(PDO::FETCH_ASSOC); return $r === false ? null : $r;
    }
    private function run(string $sql, array $args): void
    {
        $s = $this->db->prepare($sql); $s->execute($args);
    }
    private function atomic(callable $fn): mixed
    {
        $this->db->exec('BEGIN IMMEDIATE');
        try { $r = $fn(); $this->db->exec('COMMIT'); return $r; }
        catch (Throwable $e) { $this->db->exec('ROLLBACK'); throw $e; }
    }
    public function limit(string $bucket, int $max, int $seconds): void
    {
        $allowed = $this->atomic(function () use ($bucket, $max, $seconds): bool {
            $now = $this->now();
            $this->run('DELETE FROM rate_limits WHERE expires<=?', [$now]);
            $this->run('INSERT INTO rate_limits(bucket,count,expires) VALUES(?,1,?) ON CONFLICT(bucket) DO UPDATE SET count=count+1',
                [self::hash($bucket), $now + $seconds]);
            return (int)$this->one('SELECT count FROM rate_limits WHERE bucket=?', [self::hash($bucket)])['count'] <= $max;
        });
        if (!$allowed) { throw new Problem('rate_limited'); }
    }
    private static function email(string $email): string
    {
        $email = strtolower(trim($email));
        if (strlen($email) > 254 || !filter_var($email, FILTER_VALIDATE_EMAIL) || preg_match('/[^\x21-\x7e]/', $email)) {
            throw new Problem('invalid_email');
        }
        return $email;
    }
    private static function password(string $password): void
    {
        if (strlen($password) < 12 || strlen($password) > 128 || str_contains($password, "\0")) {
            throw new Problem('password_length');
        }
    }
    public function register(string $email): void
    {
        $email = self::email($email);
        // The owner chooses the password only after proving email ownership.
        $hash = password_hash(self::secret(), PASSWORD_ARGON2ID);
        $this->run('INSERT INTO members(email,password,created,accepted_terms) VALUES(?,?,?,?) ON CONFLICT(email) DO NOTHING', [$email, $hash, $this->now(), '2026-09-10']);
        // Never replace the password of an unverified account on a duplicate registration.
        $this->requestToken($email, 'verify');
    }
    public function requestToken(string $email, string $purpose): void
    {
        $email = self::email($email);
        if (!in_array($purpose, ['verify','reset'], true)) { throw new Problem('invalid_request'); }
        $this->limit('mail:' . $email, 3, 3600);
        $m = $this->one('SELECT * FROM members WHERE email=?', [$email]);
        if (!$m || ($purpose === 'verify' && (int)$m['verified'] === 1)) { return; }
        $token = self::secret();
        $this->atomic(function () use ($m, $purpose, $token): void {
            $this->run('DELETE FROM tokens WHERE (member_id=? AND purpose=?) OR expires<=?', [$m['id'], $purpose, $this->now()]);
            $this->run('INSERT INTO tokens(hash,member_id,purpose,expires) VALUES(?,?,?,?)', [self::hash($token), $m['id'], $purpose, $this->now()+1800]);
        });
        // The fragment never enters HTTP access logs. The browser submits it via POST.
        ($this->send)($email, $purpose, $token);
    }
    public function consumeToken(string $token, string $purpose, string $newPassword = ''): void
    {
        if (!preg_match('/^[a-f0-9]{64}$/D', $token) || !in_array($purpose, ['verify','reset'], true)) {
            throw new Problem('invalid_token');
        }
        $passwordHash = null;
        self::password($newPassword); $passwordHash = password_hash($newPassword, PASSWORD_ARGON2ID);
        $this->atomic(function () use ($token, $purpose, $passwordHash): void {
            $t = $this->one('SELECT * FROM tokens WHERE hash=? AND purpose=? AND expires>?', [self::hash($token), $purpose, $this->now()]);
            if (!$t) { throw new Problem('invalid_token'); }
            if ($purpose === 'verify') {
                $this->run('UPDATE members SET verified=1,password=?,epoch=epoch+1 WHERE id=?', [$passwordHash,$t['member_id']]);
            } else {
                $this->run('UPDATE members SET password=?,epoch=epoch+1 WHERE id=?', [$passwordHash, $t['member_id']]);
            }
            $this->run('DELETE FROM tokens WHERE member_id=? AND purpose=?', [$t['member_id'], $purpose]);
        });
    }
    public function login(string $email, string $password): array
    {
        $email = self::email($email);
        $this->limit('login:' . $email, 10, 900);
        $m = $this->one('SELECT * FROM members WHERE email=?', [$email]);
        // A valid dummy hash keeps the expensive password check on unknown emails.
        $dummy = '$2y$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2uheWG/igi.';
        $valid = password_verify(substr($password, 0, 129), $m['password'] ?? $dummy);
        if (!$m || !$valid || strlen($password)>128) { throw new Problem('invalid_login'); }
        if (!(int)$m['verified']) { throw new Problem('verify_email'); }
        return ['id'=>(int)$m['id'], 'email'=>$m['email'], 'epoch'=>(int)$m['epoch']];
    }
    public function member(int $id, int $epoch): ?array
    {
        return $this->one('SELECT id,email,epoch FROM members WHERE id=? AND epoch=? AND verified=1', [$id,$epoch]);
    }
    private function event(int $member, ?string $license, string $kind): void
    {
        $this->run('INSERT INTO events(member_id,license_id,kind,created) VALUES(?,?,?,?)', [$member,$license,$kind,$this->now()]);
    }
    public function licenses(int $member): array
    {
        $s = $this->db->prepare('SELECT id,key_suffix,created,activated,expires,hostname,server_id,server_ip,(key_encrypted IS NOT NULL) AS key_available FROM licenses WHERE member_id=? ORDER BY created DESC,id');
        $s->execute([$member]); return $s->fetchAll(PDO::FETCH_ASSOC);
    }
    public function issue(int $member): array
    {
        $this->limit('issue:' . $member, 10, 86400);
        return $this->atomic(function () use ($member): array {
            $m=$this->one('SELECT id FROM members WHERE id=? AND verified=1',[$member]);
            if (!$m) { throw new Problem('authentication_required'); }
            $id = bin2hex(random_bytes(16)); $key = 'CPK-' . self::secret();
            $this->run('INSERT INTO licenses(id,member_id,key_hash,key_suffix,created,expires,key_encrypted) VALUES(?,?,?,?,?,?,?)', [$id,$member,self::hash($key),substr($key,-8),$this->now(),self::anniversary($this->now()),$this->encryptKey($id,$key)]);
            $this->event($member,$id,'issued');
            return ['id'=>$id,'key'=>$key];
        });
    }
    private function owned(int $member, string $id, string $password): array
    {
        $m = $this->one('SELECT password FROM members WHERE id=? AND verified=1', [$member]);
        if (!$m || strlen($password)>128 || !password_verify($password,$m['password'])) { throw new Problem('invalid_login'); }
        $l = $this->one('SELECT * FROM licenses WHERE id=? AND member_id=?', [$id,$member]);
        if (!$l) { throw new Problem('license_not_found'); }
        return $l;
    }
    // Storage encryption is domain-separated from receipt signing. Only the
    // private membership host holds the secret; no key material enters releases.
    private function storageKey(): string
    {
        return sodium_crypto_generichash('celikpanel-license-key-storage-v1',substr($this->signingKey,0,32),32);
    }
    private function encryptKey(string $id, string $key): string
    {
        $nonce=random_bytes(SODIUM_CRYPTO_SECRETBOX_NONCEBYTES);
        return base64_encode($nonce.sodium_crypto_secretbox($id.':'.$key,$nonce,$this->storageKey()));
    }
    public function reveal(int $member, string $id, string $password): array
    {
        $this->limit('key-access:'.$member,10,900);
        $l=$this->owned($member,$id,$password);
        if (!$l['key_encrypted']) { throw new Problem('key_not_saved'); }
        $raw=base64_decode($l['key_encrypted'],true);
        if ($raw===false || strlen($raw)<SODIUM_CRYPTO_SECRETBOX_NONCEBYTES+SODIUM_CRYPTO_SECRETBOX_MACBYTES) { throw new Problem('key_not_saved'); }
        $plain=sodium_crypto_secretbox_open(substr($raw,24),substr($raw,0,24),$this->storageKey());
        if ($plain===false || !str_starts_with($plain,$id.':')) { throw new Problem('key_not_saved'); }
        $key=substr($plain,strlen($id)+1);
        if (!hash_equals($l['key_hash'],self::hash($key))) { throw new Problem('key_not_saved'); }
        $this->event($member,$id,'key_viewed');
        return ['id'=>$id,'key'=>$key];
    }
    public function release(int $member, string $id, string $password): void
    {
        $this->limit('release:'.$member,5,86400);
        $this->atomic(function () use ($member,$id,$password): void {
            $this->owned($member,$id,$password);
            $this->run('UPDATE licenses SET server_id=NULL,server_ip=NULL,hostname=NULL,generation=generation+1 WHERE id=?',[$id]);
            $this->event($member,$id,'server_released');
        });
    }
    public function rotate(int $member, string $id, string $password): array
    {
        $this->limit('rotate:' . $member, 5, 86400);
        return $this->atomic(function () use ($member,$id,$password): array {
            $this->owned($member,$id,$password);
            $key = 'CPK-' . self::secret();
            $this->run('UPDATE licenses SET generation=generation+1,key_hash=?,key_suffix=?,key_encrypted=? WHERE id=?', [self::hash($key),substr($key,-8),$this->encryptKey($id,$key),$id]);
            $this->event($member,$id,'key_rotated');
            return ['id'=>$id,'key'=>$key];
        });
    }
    private static function anniversary(int $timestamp): int
    {
        $d = (new \DateTimeImmutable('@'.$timestamp))->setTimezone(new \DateTimeZone('UTC'));
        $year = (int)$d->format('Y')+1; $month = (int)$d->format('m');
        $last = (int)$d->setDate($year,$month,1)->format('t');
        return $d->setDate($year,$month,min((int)$d->format('d'),$last))->getTimestamp();
    }
    public function renew(int $member, string $id, string $password): void
    {
        $this->atomic(function () use ($member,$id,$password): void {
            $l=$this->owned($member,$id,$password);
            if (!$l['expires'] || (int)$l['expires'] > $this->now()+30*86400) { throw new Problem('renewal_not_due'); }
            // Free today; later a fulfilled order grants this same entitlement.
            $expires=self::anniversary(max($this->now(),(int)$l['expires']));
            $this->run('UPDATE licenses SET expires=? WHERE id=?',[$expires,$id]);
            $this->event($member,$id,'renewed_free');
        });
    }
    private function activationToken(array $l): string
    {
        return hash_hmac('sha256', 'activation-v1:'.$l['id'].':'.$l['server_id'].':'.$l['generation'], $this->signingKey);
    }
    private function entitlement(array $l): array
    {
        $now=$this->now();
        $payload=json_encode(['format'=>'celikpanel-license-v1','product'=>'celikpanel','license_id'=>$l['id'],
            'server_id'=>$l['server_id'],'issued_at'=>$now,'activated_at'=>(int)$l['activated'],
            'expires_at'=>(int)$l['expires'],'refresh_after'=>$now+86400,
            'offline_until'=>min($now+7*86400,(int)$l['expires'])], JSON_THROW_ON_ERROR|JSON_UNESCAPED_SLASHES);
        return ['payload'=>base64_encode($payload),'signature'=>base64_encode(sodium_crypto_sign_detached($payload,$this->signingKey)),
            'activation_token'=>$this->activationToken($l)];
    }
    private static function publicIP(string $ip): string
    {
        $raw=@inet_pton($ip);
        if ($raw===false) { throw new Problem('invalid_server_ip'); }
        if (strlen($raw)===16 && substr($raw,0,12)===str_repeat("\0",10)."\xff\xff") { $raw=substr($raw,12); }
        $ip=inet_ntop($raw);
        if (!filter_var($ip,FILTER_VALIDATE_IP,FILTER_FLAG_NO_PRIV_RANGE|FILTER_FLAG_NO_RES_RANGE)) { throw new Problem('invalid_server_ip'); }
        return $ip;
    }

    public function activate(string $key, string $server, string $hostname, string $observedIP): array
    {
        $ip=self::publicIP($observedIP);
        if (!preg_match('/^CPK-[a-f0-9]{64}$/D',$key) || !preg_match('/^[a-f0-9]{64}$/D',$server)
            || !preg_match('/^[a-zA-Z0-9][a-zA-Z0-9.-]{0,252}$/D',$hostname)) { throw new Problem('invalid_activation'); }
        return $this->atomic(function () use ($key,$server,$hostname,$ip): array {
            $l=$this->one('SELECT * FROM licenses WHERE key_hash=?',[self::hash($key)]);
            if (!$l) { throw new Problem('invalid_license'); }
            if ($l['server_ip']!==null && !hash_equals($l['server_ip'],$ip)) { throw new Problem('license_in_use'); }
            // Legacy bindings prove the old installation once, or the owner releases them from the account.
            if ($l['server_ip']===null && $l['server_id']!==null && !hash_equals($l['server_id'],$server)) { throw new Problem('license_in_use'); }
            if ($l['server_id']!==null && !hash_equals($l['server_id'],$server)) { $l['generation']=(int)$l['generation']+1; }
            if ($l['expires'] !== null && (int)$l['expires']<=$this->now()) { throw new Problem('license_expired'); }
            if ($l['activated'] === null) {
                $l['activated']=$this->now(); $l['expires'] ??= self::anniversary((int)$l['created']);
            }
            $first=$l['server_id']===null; $l['server_id']=$server; $l['hostname']=$hostname;
            $this->run('UPDATE licenses SET activated=?,expires=?,server_id=?,hostname=?,server_ip=?,generation=?,key_encrypted=? WHERE id=?',[$l['activated'],$l['expires'],$server,$hostname,$ip,$l['generation'],$this->encryptKey($l['id'],$key),$l['id']]);
            if ($first) { $this->event((int)$l['member_id'],$l['id'],'activated'); }
            return $this->entitlement($l);
        });
    }
    public function refresh(string $id, string $server, string $token, string $observedIP): array
    {
        $ip=self::publicIP($observedIP);
        return $this->atomic(function () use ($id,$server,$token,$ip): array {
            $l=$this->one('SELECT * FROM licenses WHERE id=?',[$id]);
            if (!$l || !$l['server_id'] || !hash_equals($l['server_id'],$server) || !hash_equals($this->activationToken($l),$token)) {
                throw new Problem('invalid_activation');
            }
            if ($l['server_ip']!==null && !hash_equals($l['server_ip'],$ip)) { throw new Problem('license_in_use'); }
            if ($l['server_ip']===null) {
                $this->run('UPDATE licenses SET server_ip=? WHERE id=?',[$ip,$id]);
                $this->event((int)$l['member_id'],$id,'ip_bound');
            }
            return $this->entitlement($l);
        });
    }
}
