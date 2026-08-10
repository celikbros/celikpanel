package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
)

var (
	ErrTeamMemberNotFound      = errors.New("team member not found")
	ErrTeamMemberOwnerNotFound = errors.New("team member owner not found")
	ErrTeamMemberForeignScope  = errors.New("team member permission scope not found")
	ErrInvalidTeamPermission   = errors.New("invalid team member permission")
	ErrTeamMemberConflict      = errors.New("team member conflicts with an existing account")
)

type TeamMemberRepository struct {
	db *sql.DB
}

func NewTeamMemberRepository(db *sql.DB) *TeamMemberRepository {
	return &TeamMemberRepository{db: db}
}

type TeamMemberCreate struct {
	Username     string
	PasswordHash string
	Email        string
	Status       string
	Access       core.TeamMemberAccess
}

type TeamMemberUpdate struct {
	Username     *string
	PasswordHash *string
	Email        *string
	Status       *string
	Access       *core.TeamMemberAccess
}

type teamMemberRecord struct {
	member       core.TeamMember
	passwordHash string
	authEpoch    int64
}

type teamMemberQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (r *TeamMemberRepository) Create(ctx context.Context, ownerID int, input TeamMemberCreate) (*core.TeamMember, error) {
	access, err := normalizeTeamMemberAccess(input.Access)
	if err != nil {
		return nil, err
	}
	if !validTeamMemberStatus(input.Status) {
		return nil, ErrInvalidTeamPermission
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := requireTeamMemberOwner(ctx, tx, ownerID, true); err != nil {
		return nil, err
	}
	if err := validateTeamMemberAccessScope(ctx, tx, ownerID, access); err != nil {
		return nil, err
	}

	storedStatus := input.Status
	stageSuspendedAccess := input.Status == "suspended" &&
		(len(access.SubscriptionPermissions) != 0 || len(access.DomainPermissions) != 0)
	if stageSuspendedAccess {
		// Grant triggers deliberately accept only active identities. Keep the
		// requested suspended state private to this transaction: create an
		// active staging row, install its grants, then suspend it before commit.
		storedStatus = "active"
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO users (
			username, password_hash, email, role, account_type, parent_id, status
		) VALUES (?, ?, ?, 'customer', 'additional_user', ?, ?)
	`, input.Username, input.PasswordHash, input.Email, ownerID, storedStatus)
	if err != nil {
		return nil, classifyTeamMemberDBError(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := replaceTeamMemberAccess(ctx, tx, int(id), access); err != nil {
		return nil, classifyTeamMemberDBError(err)
	}
	if stageSuspendedAccess {
		result, err := tx.ExecContext(ctx, `
			UPDATE users
			SET status = 'suspended'
			WHERE id = ? AND parent_id = ? AND role = 'customer'
			  AND account_type = 'additional_user' AND status = 'active'
		`, id, ownerID)
		if err != nil {
			return nil, classifyTeamMemberDBError(err)
		}
		if affected, err := result.RowsAffected(); err != nil {
			return nil, err
		} else if affected != 1 {
			return nil, ErrTeamMemberNotFound
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, classifyTeamMemberDBError(err)
	}
	return r.GetByOwner(ctx, ownerID, int(id))
}

func (r *TeamMemberRepository) ListByOwner(ctx context.Context, ownerID int) ([]*core.TeamMember, error) {
	if err := requireTeamMemberOwner(ctx, r.db, ownerID, false); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, parent_id, username, password_hash, email, status,
		       auth_epoch, created_at, updated_at
		FROM users
		WHERE parent_id = ? AND role = 'customer' AND account_type = 'additional_user'
		ORDER BY username COLLATE NOCASE, id
	`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := make([]*core.TeamMember, 0)
	for rows.Next() {
		record, err := scanTeamMemberRecord(rows)
		if err != nil {
			return nil, err
		}
		if err := loadTeamMemberAccess(ctx, r.db, &record.member); err != nil {
			return nil, err
		}
		member := record.member
		members = append(members, &member)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return members, nil
}

func (r *TeamMemberRepository) GetByOwner(ctx context.Context, ownerID, memberID int) (*core.TeamMember, error) {
	if err := requireTeamMemberOwner(ctx, r.db, ownerID, false); err != nil {
		return nil, err
	}
	record, err := loadTeamMemberRecord(ctx, r.db, ownerID, memberID)
	if err != nil {
		return nil, err
	}
	if err := loadTeamMemberAccess(ctx, r.db, &record.member); err != nil {
		return nil, err
	}
	return &record.member, nil
}

// Update applies identity and grant changes atomically. The returned boolean is
// true when a security-sensitive change revoked all existing sessions.
func (r *TeamMemberRepository) Update(ctx context.Context, ownerID, memberID int, input TeamMemberUpdate) (*core.TeamMember, bool, error) {
	var normalizedAccess *core.TeamMemberAccess
	if input.Access != nil {
		access, err := normalizeTeamMemberAccess(*input.Access)
		if err != nil {
			return nil, false, err
		}
		normalizedAccess = &access
	}
	if input.Status != nil && !validTeamMemberStatus(*input.Status) {
		return nil, false, ErrInvalidTeamPermission
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if err := requireTeamMemberOwner(ctx, tx, ownerID, true); err != nil {
		return nil, false, err
	}
	record, err := loadTeamMemberRecord(ctx, tx, ownerID, memberID)
	if err != nil {
		return nil, false, err
	}
	if err := loadTeamMemberAccess(ctx, tx, &record.member); err != nil {
		return nil, false, err
	}
	currentAccess, err := normalizeTeamMemberAccess(record.member.Access)
	if err != nil {
		return nil, false, err
	}

	username := record.member.Username
	passwordHash := record.passwordHash
	email := record.member.Email
	status := record.member.Status
	if input.Username != nil {
		username = *input.Username
	}
	if input.PasswordHash != nil {
		passwordHash = *input.PasswordHash
	}
	if input.Email != nil {
		email = *input.Email
	}
	if input.Status != nil {
		status = *input.Status
	}

	accessChanged := normalizedAccess != nil && !reflect.DeepEqual(currentAccess, *normalizedAccess)
	securityChanged := input.PasswordHash != nil || status != record.member.Status || accessChanged
	if normalizedAccess != nil {
		if err := validateTeamMemberAccessScope(ctx, tx, ownerID, *normalizedAccess); err != nil {
			return nil, false, err
		}
	}
	replaceAccessBeforeFinalSuspend := accessChanged && status == "suspended"
	if replaceAccessBeforeFinalSuspend {
		// Grant triggers accept only active members. An already-suspended row is
		// activated only inside this transaction; both it and an active row that
		// is being suspended receive their grants before the final identity
		// update restores the requested suspended state.
		if record.member.Status == "suspended" {
			result, err := tx.ExecContext(ctx, `
				UPDATE users
				SET status = 'active'
				WHERE id = ? AND parent_id = ? AND role = 'customer'
				  AND account_type = 'additional_user' AND status = 'suspended'
			`, memberID, ownerID)
			if err != nil {
				return nil, false, classifyTeamMemberDBError(err)
			}
			if affected, err := result.RowsAffected(); err != nil {
				return nil, false, err
			} else if affected != 1 {
				return nil, false, ErrTeamMemberNotFound
			}
		}
		if err := replaceTeamMemberAccess(ctx, tx, memberID, *normalizedAccess); err != nil {
			return nil, false, classifyTeamMemberDBError(err)
		}
	}

	epochIncrement := 0
	if securityChanged {
		epochIncrement = 1
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE users
		SET username = ?, password_hash = ?, email = ?, status = ?,
		    auth_epoch = auth_epoch + ?, updated_at = datetime('now')
		WHERE id = ? AND parent_id = ? AND role = 'customer'
		  AND account_type = 'additional_user'
	`, username, passwordHash, email, status, epochIncrement, memberID, ownerID)
	if err != nil {
		return nil, false, classifyTeamMemberDBError(err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, false, err
	} else if affected != 1 {
		return nil, false, ErrTeamMemberNotFound
	}
	if accessChanged && !replaceAccessBeforeFinalSuspend {
		if err := replaceTeamMemberAccess(ctx, tx, memberID, *normalizedAccess); err != nil {
			return nil, false, classifyTeamMemberDBError(err)
		}
	}
	if securityChanged {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, memberID); err != nil {
			return nil, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, false, classifyTeamMemberDBError(err)
	}
	member, err := r.GetByOwner(ctx, ownerID, memberID)
	return member, securityChanged, err
}

func (r *TeamMemberRepository) Delete(ctx context.Context, ownerID, memberID int) (*core.TeamMember, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := requireTeamMemberOwner(ctx, tx, ownerID, false); err != nil {
		return nil, err
	}
	record, err := loadTeamMemberRecord(ctx, tx, ownerID, memberID)
	if err != nil {
		return nil, err
	}
	if err := loadTeamMemberAccess(ctx, tx, &record.member); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
		DELETE FROM users
		WHERE id = ? AND parent_id = ? AND role = 'customer'
		  AND account_type = 'additional_user'
	`, memberID, ownerID)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected != 1 {
		return nil, ErrTeamMemberNotFound
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &record.member, nil
}

func requireTeamMemberOwner(ctx context.Context, q teamMemberQueryer, ownerID int, requireActive bool) error {
	if ownerID <= 0 {
		return ErrTeamMemberOwnerNotFound
	}
	query := `
		SELECT 1 FROM users
		WHERE id = ? AND role = 'customer' AND account_type = 'account'
	`
	if requireActive {
		query += ` AND status = 'active'`
	}
	var exists int
	if err := q.QueryRowContext(ctx, query, ownerID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTeamMemberOwnerNotFound
		}
		return err
	}
	return nil
}

func loadTeamMemberRecord(ctx context.Context, q teamMemberQueryer, ownerID, memberID int) (*teamMemberRecord, error) {
	if memberID <= 0 {
		return nil, ErrTeamMemberNotFound
	}
	row := q.QueryRowContext(ctx, `
		SELECT id, parent_id, username, password_hash, email, status,
		       auth_epoch, created_at, updated_at
		FROM users
		WHERE id = ? AND parent_id = ? AND role = 'customer'
		  AND account_type = 'additional_user'
	`, memberID, ownerID)
	record, err := scanTeamMemberRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTeamMemberNotFound
	}
	return record, err
}

type teamMemberScanner interface {
	Scan(...any) error
}

func scanTeamMemberRecord(scanner teamMemberScanner) (*teamMemberRecord, error) {
	var record teamMemberRecord
	var createdAt, updatedAt sql.NullString
	if err := scanner.Scan(
		&record.member.ID,
		&record.member.OwnerID,
		&record.member.Username,
		&record.passwordHash,
		&record.member.Email,
		&record.member.Status,
		&record.authEpoch,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	record.member.CreatedAt = parseDBTime(createdAt)
	record.member.UpdatedAt = parseDBTime(updatedAt)
	record.member.Access = core.TeamMemberAccess{
		SubscriptionPermissions: make([]core.TeamSubscriptionPermission, 0),
		DomainPermissions:       make([]core.TeamDomainPermission, 0),
	}
	return &record, nil
}

func loadTeamMemberAccess(ctx context.Context, q teamMemberQueryer, member *core.TeamMember) error {
	member.Access.SubscriptionPermissions = make([]core.TeamSubscriptionPermission, 0)
	rows, err := q.QueryContext(ctx, `
		SELECT permission.subscription_id, subscription.name, subscription.owner_id,
		       permission.capability, permission.mode
		FROM additional_user_subscription_permissions AS permission
		JOIN subscriptions AS subscription ON subscription.id = permission.subscription_id
		WHERE permission.user_id = ?
		ORDER BY permission.subscription_id, permission.capability
	`, member.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var permission core.TeamSubscriptionPermission
		var ownerID int
		if err := rows.Scan(&permission.SubscriptionID, &permission.SubscriptionName, &ownerID, &permission.Capability, &permission.Mode); err != nil {
			rows.Close()
			return err
		}
		if ownerID != member.OwnerID {
			rows.Close()
			return ErrTeamMemberForeignScope
		}
		if !core.ValidTeamCapability(permission.Capability) || !core.ValidTeamPermissionMode(permission.Mode) {
			rows.Close()
			return ErrInvalidTeamPermission
		}
		member.Access.SubscriptionPermissions = append(member.Access.SubscriptionPermissions, permission)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	member.Access.DomainPermissions = make([]core.TeamDomainPermission, 0)
	rows, err = q.QueryContext(ctx, `
		SELECT permission.domain_id, domain.name, subscription.owner_id,
		       permission.capability, permission.mode
		FROM additional_user_domain_permissions AS permission
		JOIN domains AS domain ON domain.id = permission.domain_id
		JOIN subscriptions AS subscription ON subscription.id = domain.subscription_id
		WHERE permission.user_id = ?
		ORDER BY permission.domain_id, permission.capability
	`, member.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var permission core.TeamDomainPermission
		var ownerID int
		if err := rows.Scan(&permission.DomainID, &permission.DomainName, &ownerID, &permission.Capability, &permission.Mode); err != nil {
			return err
		}
		if ownerID != member.OwnerID {
			return ErrTeamMemberForeignScope
		}
		if !core.ValidTeamCapability(permission.Capability) || !core.ValidTeamPermissionMode(permission.Mode) {
			return ErrInvalidTeamPermission
		}
		member.Access.DomainPermissions = append(member.Access.DomainPermissions, permission)
	}
	return rows.Err()
}

func normalizeTeamMemberAccess(access core.TeamMemberAccess) (core.TeamMemberAccess, error) {
	if access.SubscriptionPermissions == nil || access.DomainPermissions == nil {
		return core.TeamMemberAccess{}, ErrInvalidTeamPermission
	}
	normalized := core.TeamMemberAccess{
		SubscriptionPermissions: make([]core.TeamSubscriptionPermission, len(access.SubscriptionPermissions)),
		DomainPermissions:       make([]core.TeamDomainPermission, len(access.DomainPermissions)),
	}
	copy(normalized.SubscriptionPermissions, access.SubscriptionPermissions)
	copy(normalized.DomainPermissions, access.DomainPermissions)
	seenSubscriptions := make(map[string]struct{}, len(normalized.SubscriptionPermissions))
	for i := range normalized.SubscriptionPermissions {
		permission := &normalized.SubscriptionPermissions[i]
		permission.SubscriptionName = ""
		if permission.SubscriptionID <= 0 || !core.ValidTeamCapability(permission.Capability) || !core.ValidTeamPermissionMode(permission.Mode) {
			return core.TeamMemberAccess{}, ErrInvalidTeamPermission
		}
		key := fmt.Sprintf("%d\x00%s", permission.SubscriptionID, permission.Capability)
		if _, exists := seenSubscriptions[key]; exists {
			return core.TeamMemberAccess{}, ErrInvalidTeamPermission
		}
		seenSubscriptions[key] = struct{}{}
	}
	seenDomains := make(map[string]struct{}, len(normalized.DomainPermissions))
	for i := range normalized.DomainPermissions {
		permission := &normalized.DomainPermissions[i]
		permission.DomainName = ""
		if permission.DomainID <= 0 || !core.ValidTeamCapability(permission.Capability) || !core.ValidTeamPermissionMode(permission.Mode) {
			return core.TeamMemberAccess{}, ErrInvalidTeamPermission
		}
		key := fmt.Sprintf("%d\x00%s", permission.DomainID, permission.Capability)
		if _, exists := seenDomains[key]; exists {
			return core.TeamMemberAccess{}, ErrInvalidTeamPermission
		}
		seenDomains[key] = struct{}{}
	}
	sort.Slice(normalized.SubscriptionPermissions, func(i, j int) bool {
		left, right := normalized.SubscriptionPermissions[i], normalized.SubscriptionPermissions[j]
		if left.SubscriptionID != right.SubscriptionID {
			return left.SubscriptionID < right.SubscriptionID
		}
		return left.Capability < right.Capability
	})
	sort.Slice(normalized.DomainPermissions, func(i, j int) bool {
		left, right := normalized.DomainPermissions[i], normalized.DomainPermissions[j]
		if left.DomainID != right.DomainID {
			return left.DomainID < right.DomainID
		}
		return left.Capability < right.Capability
	})
	return normalized, nil
}

func validateTeamMemberAccessScope(ctx context.Context, q teamMemberQueryer, ownerID int, access core.TeamMemberAccess) error {
	for _, permission := range access.SubscriptionPermissions {
		var exists int
		err := q.QueryRowContext(ctx, `SELECT 1 FROM subscriptions WHERE id = ? AND owner_id = ?`, permission.SubscriptionID, ownerID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTeamMemberForeignScope
		}
		if err != nil {
			return err
		}
	}
	for _, permission := range access.DomainPermissions {
		var exists int
		err := q.QueryRowContext(ctx, `
			SELECT 1
			FROM domains AS domain
			JOIN subscriptions AS subscription ON subscription.id = domain.subscription_id
			WHERE domain.id = ? AND subscription.owner_id = ?
		`, permission.DomainID, ownerID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTeamMemberForeignScope
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func replaceTeamMemberAccess(ctx context.Context, tx *sql.Tx, memberID int, access core.TeamMemberAccess) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM additional_user_subscription_permissions WHERE user_id = ?`, memberID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM additional_user_domain_permissions WHERE user_id = ?`, memberID); err != nil {
		return err
	}
	for _, permission := range access.SubscriptionPermissions {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO additional_user_subscription_permissions
				(user_id, subscription_id, capability, mode)
			VALUES (?, ?, ?, ?)
		`, memberID, permission.SubscriptionID, permission.Capability, permission.Mode); err != nil {
			return err
		}
	}
	for _, permission := range access.DomainPermissions {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO additional_user_domain_permissions
				(user_id, domain_id, capability, mode)
			VALUES (?, ?, ?, ?)
		`, memberID, permission.DomainID, permission.Capability, permission.Mode); err != nil {
			return err
		}
	}
	return nil
}

func validTeamMemberStatus(status string) bool {
	return status == "active" || status == "suspended"
}

func classifyTeamMemberDBError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "unique constraint") || strings.Contains(message, "users.username") || strings.Contains(message, "users.email") {
		return fmt.Errorf("%w: %v", ErrTeamMemberConflict, err)
	}
	if strings.Contains(message, "crosses tenancy boundary") || strings.Contains(message, "conflicts with granted scope") {
		return fmt.Errorf("%w: %v", ErrTeamMemberForeignScope, err)
	}
	if strings.Contains(message, "check constraint") || strings.Contains(message, "invalid additional user") {
		return fmt.Errorf("%w: %v", ErrInvalidTeamPermission, err)
	}
	return err
}
