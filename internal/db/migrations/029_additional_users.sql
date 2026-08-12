-- 029: Additional customer users and scoped team permissions.
--
-- Existing users are full accounts. Additional users keep the stored customer
-- role for compatibility, while account_type distinguishes their restricted
-- identity. Permission values are deliberately closed in the database as well
-- as in Go so unknown capabilities cannot silently become access grants.

ALTER TABLE users ADD COLUMN account_type TEXT NOT NULL DEFAULT 'account'
    CHECK (account_type IN ('account', 'additional_user'));

CREATE INDEX idx_users_parent_account_type
    ON users(parent_id, account_type);

CREATE TABLE additional_user_subscription_permissions (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    subscription_id INTEGER NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    capability TEXT NOT NULL CHECK (capability IN (
        'files', 'databases', 'mail', 'dns', 'ssl', 'cron', 'backups', 'php', 'statistics'
    )),
    mode TEXT NOT NULL CHECK (mode IN ('view', 'manage')),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (user_id, subscription_id, capability)
);

CREATE INDEX idx_additional_user_subscription_permissions_scope
    ON additional_user_subscription_permissions(subscription_id, user_id);

CREATE TABLE additional_user_domain_permissions (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    capability TEXT NOT NULL CHECK (capability IN (
        'files', 'databases', 'mail', 'dns', 'ssl', 'cron', 'backups', 'php', 'statistics'
    )),
    mode TEXT NOT NULL CHECK (mode IN ('view', 'manage')),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (user_id, domain_id, capability)
);

CREATE INDEX idx_additional_user_domain_permissions_scope
    ON additional_user_domain_permissions(domain_id, user_id);

-- The marker is an immutable identity boundary. Converting an existing account
-- in place could retain sessions and ownership that were never granted to an
-- additional user (or promote a restricted identity back into a full account).
CREATE TRIGGER validate_user_account_type_immutable
BEFORE UPDATE OF account_type ON users
WHEN NEW.account_type <> OLD.account_type
BEGIN
    SELECT RAISE(ABORT, 'account_type is immutable');
END;

CREATE TRIGGER validate_additional_user_identity_insert
BEFORE INSERT ON users
WHEN NEW.account_type = 'additional_user'
BEGIN
    SELECT CASE
        WHEN NEW.role <> 'customer'
        THEN RAISE(ABORT, 'additional user role must be customer')
    END;
    SELECT CASE
        WHEN NEW.status NOT IN ('active', 'suspended')
        THEN RAISE(ABORT, 'invalid additional user status')
    END;
    SELECT CASE
        WHEN NEW.parent_id IS NULL OR NOT EXISTS (
            SELECT 1
            FROM users AS owner
            WHERE owner.id = NEW.parent_id
              AND owner.role = 'customer'
              AND owner.account_type = 'account'
              AND owner.status = 'active'
        )
        THEN RAISE(ABORT, 'additional user requires an active customer account owner')
    END;
END;

CREATE TRIGGER validate_additional_user_identity_update
BEFORE UPDATE OF role, parent_id, status ON users
WHEN NEW.account_type = 'additional_user'
BEGIN
    SELECT CASE
        WHEN NEW.role <> 'customer'
        THEN RAISE(ABORT, 'additional user role must be customer')
    END;
    SELECT CASE
        WHEN NEW.status NOT IN ('active', 'suspended')
        THEN RAISE(ABORT, 'invalid additional user status')
    END;
    SELECT CASE
        WHEN NEW.parent_id IS NULL OR NOT EXISTS (
            SELECT 1
            FROM users AS owner
            WHERE owner.id = NEW.parent_id
              AND owner.role = 'customer'
              AND owner.account_type = 'account'
              AND owner.status = 'active'
        )
        THEN RAISE(ABORT, 'additional user requires an active customer account owner')
    END;
END;

-- An owner with restricted identity must never acquire an unrestricted
-- subscription through a direct SQL write.
CREATE TRIGGER reject_additional_user_subscription_owner_insert
BEFORE INSERT ON subscriptions
WHEN EXISTS (
    SELECT 1 FROM users
    WHERE id = NEW.owner_id AND account_type = 'additional_user'
)
BEGIN
    SELECT RAISE(ABORT, 'additional users cannot own subscriptions');
END;

CREATE TRIGGER reject_additional_user_subscription_owner_update
BEFORE UPDATE OF owner_id ON subscriptions
WHEN EXISTS (
    SELECT 1 FROM users
    WHERE id = NEW.owner_id AND account_type = 'additional_user'
)
BEGIN
    SELECT RAISE(ABORT, 'additional users cannot own subscriptions');
END;

-- Grant validation intentionally includes both the member and owner lifecycle.
-- Suspended identities retain their stored grants but cannot receive or retarget
-- grants until both sides of the relationship are active again.
CREATE TRIGGER validate_additional_user_subscription_permission_insert
BEFORE INSERT ON additional_user_subscription_permissions
WHEN NOT EXISTS (
    SELECT 1
    FROM users AS member
    JOIN users AS owner ON owner.id = member.parent_id
    JOIN subscriptions AS subscription ON subscription.owner_id = owner.id
    WHERE member.id = NEW.user_id
      AND member.role = 'customer'
      AND member.account_type = 'additional_user'
      AND member.status = 'active'
      AND owner.role = 'customer'
      AND owner.account_type = 'account'
      AND owner.status = 'active'
      AND subscription.id = NEW.subscription_id
)
BEGIN
    SELECT RAISE(ABORT, 'additional-user permission crosses tenancy boundary');
END;

CREATE TRIGGER validate_additional_user_subscription_permission_update
BEFORE UPDATE ON additional_user_subscription_permissions
WHEN NOT EXISTS (
    SELECT 1
    FROM users AS member
    JOIN users AS owner ON owner.id = member.parent_id
    JOIN subscriptions AS subscription ON subscription.owner_id = owner.id
    WHERE member.id = NEW.user_id
      AND member.role = 'customer'
      AND member.account_type = 'additional_user'
      AND member.status = 'active'
      AND owner.role = 'customer'
      AND owner.account_type = 'account'
      AND owner.status = 'active'
      AND subscription.id = NEW.subscription_id
)
BEGIN
    SELECT RAISE(ABORT, 'additional-user permission crosses tenancy boundary');
END;

CREATE TRIGGER validate_additional_user_domain_permission_insert
BEFORE INSERT ON additional_user_domain_permissions
WHEN NOT EXISTS (
    SELECT 1
    FROM users AS member
    JOIN users AS owner ON owner.id = member.parent_id
    JOIN subscriptions AS subscription ON subscription.owner_id = owner.id
    JOIN domains AS domain ON domain.subscription_id = subscription.id
    WHERE member.id = NEW.user_id
      AND member.role = 'customer'
      AND member.account_type = 'additional_user'
      AND member.status = 'active'
      AND owner.role = 'customer'
      AND owner.account_type = 'account'
      AND owner.status = 'active'
      AND domain.id = NEW.domain_id
)
BEGIN
    SELECT RAISE(ABORT, 'additional-user permission crosses tenancy boundary');
END;

CREATE TRIGGER validate_additional_user_domain_permission_update
BEFORE UPDATE ON additional_user_domain_permissions
WHEN NOT EXISTS (
    SELECT 1
    FROM users AS member
    JOIN users AS owner ON owner.id = member.parent_id
    JOIN subscriptions AS subscription ON subscription.owner_id = owner.id
    JOIN domains AS domain ON domain.subscription_id = subscription.id
    WHERE member.id = NEW.user_id
      AND member.role = 'customer'
      AND member.account_type = 'additional_user'
      AND member.status = 'active'
      AND owner.role = 'customer'
      AND owner.account_type = 'account'
      AND owner.status = 'active'
      AND domain.id = NEW.domain_id
)
BEGIN
    SELECT RAISE(ABORT, 'additional-user permission crosses tenancy boundary');
END;

-- Scope ownership is pinned while grants exist. These guards close the gap
-- where an otherwise valid grant could become cross-tenant after an UPDATE to
-- one of the rows it derives tenancy from.
CREATE TRIGGER guard_subscription_owner_with_additional_user_grants
BEFORE UPDATE OF owner_id ON subscriptions
WHEN NEW.owner_id <> OLD.owner_id
  AND EXISTS (
      SELECT 1
      FROM additional_user_subscription_permissions AS permission
      JOIN users AS member ON member.id = permission.user_id
      WHERE permission.subscription_id = OLD.id
        AND member.parent_id <> NEW.owner_id
  )
BEGIN
    SELECT RAISE(ABORT, 'subscription owner conflicts with additional-user grants');
END;

CREATE TRIGGER guard_subscription_owner_with_additional_user_domain_grants
BEFORE UPDATE OF owner_id ON subscriptions
WHEN NEW.owner_id <> OLD.owner_id
  AND EXISTS (
      SELECT 1
      FROM domains AS domain
      JOIN additional_user_domain_permissions AS permission ON permission.domain_id = domain.id
      JOIN users AS member ON member.id = permission.user_id
      WHERE domain.subscription_id = OLD.id
        AND member.parent_id <> NEW.owner_id
  )
BEGIN
    SELECT RAISE(ABORT, 'subscription owner conflicts with additional-user grants');
END;

CREATE TRIGGER guard_domain_subscription_with_additional_user_grants
BEFORE UPDATE OF subscription_id ON domains
WHEN NEW.subscription_id <> OLD.subscription_id
  AND EXISTS (
      SELECT 1
      FROM additional_user_domain_permissions AS permission
      JOIN users AS member ON member.id = permission.user_id
      JOIN subscriptions AS subscription ON subscription.id = NEW.subscription_id
      WHERE permission.domain_id = OLD.id
        AND member.parent_id <> subscription.owner_id
  )
BEGIN
    SELECT RAISE(ABORT, 'domain subscription conflicts with additional-user grants');
END;

CREATE TRIGGER guard_additional_user_owner_with_granted_scope
BEFORE UPDATE OF parent_id ON users
WHEN NEW.account_type = 'additional_user'
  AND NEW.parent_id <> OLD.parent_id
  AND (
      EXISTS (
          SELECT 1
          FROM additional_user_subscription_permissions AS permission
          JOIN subscriptions AS subscription ON subscription.id = permission.subscription_id
          WHERE permission.user_id = OLD.id
            AND subscription.owner_id <> NEW.parent_id
      )
      OR EXISTS (
          SELECT 1
          FROM additional_user_domain_permissions AS permission
          JOIN domains AS domain ON domain.id = permission.domain_id
          JOIN subscriptions AS subscription ON subscription.id = domain.subscription_id
          WHERE permission.user_id = OLD.id
            AND subscription.owner_id <> NEW.parent_id
      )
  )
BEGIN
    SELECT RAISE(ABORT, 'additional-user owner conflicts with granted scope');
END;

-- A customer account with children must remain a customer account. Suspension
-- is allowed and is handled at authorization/grant-mutation time.
CREATE TRIGGER guard_additional_user_owner_role
BEFORE UPDATE OF role ON users
WHEN NEW.role <> 'customer'
  AND EXISTS (
      SELECT 1
      FROM users AS member
      WHERE member.parent_id = OLD.id
        AND member.account_type = 'additional_user'
  )
BEGIN
    SELECT RAISE(ABORT, 'customer account role conflicts with additional users');
END;
