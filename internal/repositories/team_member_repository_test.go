package repositories_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/repositories"
)

func createTeamMemberScope(t *testing.T, database *paneldb.SQLiteDB, ownerID int, suffix string) (int, int) {
	t.Helper()
	result, err := database.GetDB().Exec(`INSERT INTO subscriptions (owner_id, name) VALUES (?, ?)`, ownerID, "subscription-"+suffix)
	if err != nil {
		t.Fatalf("create subscription scope: %v", err)
	}
	subscriptionID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("subscription scope ID: %v", err)
	}
	result, err = database.GetDB().Exec(`INSERT INTO domains (subscription_id, name) VALUES (?, ?)`, subscriptionID, "domain-"+suffix+".example.test")
	if err != nil {
		t.Fatalf("create domain scope: %v", err)
	}
	domainID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("domain scope ID: %v", err)
	}
	return int(subscriptionID), int(domainID)
}

func emptyTeamMemberAccess() core.TeamMemberAccess {
	return core.TeamMemberAccess{
		SubscriptionPermissions: []core.TeamSubscriptionPermission{},
		DomainPermissions:       []core.TeamDomainPermission{},
	}
}

func createRepositoryTeamMember(t *testing.T, repository *repositories.TeamMemberRepository, ownerID int, suffix string, access core.TeamMemberAccess) *core.TeamMember {
	t.Helper()
	member, err := repository.Create(context.Background(), ownerID, repositories.TeamMemberCreate{
		Username:     "member-" + suffix,
		PasswordHash: "hash-" + suffix,
		Email:        fmt.Sprintf("member-%s@example.test", suffix),
		Status:       "active",
		Access:       access,
	})
	if err != nil {
		t.Fatalf("create team member: %v", err)
	}
	return member
}

func insertTeamMemberSession(t *testing.T, database *paneldb.SQLiteDB, memberID int, token string) {
	t.Helper()
	if _, err := database.GetDB().Exec(`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, '2099-01-01T00:00:00Z')`, token, memberID); err != nil {
		t.Fatalf("insert team member session: %v", err)
	}
}

func teamMemberSecurityState(t *testing.T, database *paneldb.SQLiteDB, memberID int) (int64, int) {
	t.Helper()
	var epoch int64
	if err := database.GetDB().QueryRow(`SELECT auth_epoch FROM users WHERE id = ?`, memberID).Scan(&epoch); err != nil {
		t.Fatalf("read team member auth epoch: %v", err)
	}
	var sessions int
	if err := database.GetDB().QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, memberID).Scan(&sessions); err != nil {
		t.Fatalf("count team member sessions: %v", err)
	}
	return epoch, sessions
}

func TestTeamMemberRepositoryCRUDAndSecurityRevocation(t *testing.T) {
	users, database := newAccountTypeUserRepositoryFixture(t)
	owner := createCustomerAccount(t, users, "team-crud")
	subscriptionID, domainID := createTeamMemberScope(t, database, owner.ID, "team-crud")
	repository := repositories.NewTeamMemberRepository(database.GetDB())
	access := core.TeamMemberAccess{
		SubscriptionPermissions: []core.TeamSubscriptionPermission{{
			SubscriptionID: subscriptionID,
			Capability:     core.TeamCapabilityFiles,
			Mode:           core.TeamPermissionView,
		}},
		DomainPermissions: []core.TeamDomainPermission{{
			DomainID:   domainID,
			Capability: core.TeamCapabilityDNS,
			Mode:       core.TeamPermissionManage,
		}},
	}
	member := createRepositoryTeamMember(t, repository, owner.ID, "team-crud", access)
	if member.OwnerID != owner.ID || member.Status != "active" {
		t.Fatalf("created member owner/status = %d/%q, want %d/active", member.OwnerID, member.Status, owner.ID)
	}
	if got := member.Access.SubscriptionPermissions; len(got) != 1 || got[0].SubscriptionName != "subscription-team-crud" {
		t.Fatalf("created subscription permissions = %#v", got)
	}
	if got := member.Access.DomainPermissions; len(got) != 1 || got[0].DomainName != "domain-team-crud.example.test" {
		t.Fatalf("created domain permissions = %#v", got)
	}

	listed, err := repository.ListByOwner(context.Background(), owner.ID)
	if err != nil || len(listed) != 1 || listed[0].ID != member.ID {
		t.Fatalf("ListByOwner = %#v, %v", listed, err)
	}
	got, err := repository.GetByOwner(context.Background(), owner.ID, member.ID)
	if err != nil || got.ID != member.ID {
		t.Fatalf("GetByOwner = %#v, %v", got, err)
	}

	insertTeamMemberSession(t, database, member.ID, "identity-session")
	epoch, sessions := teamMemberSecurityState(t, database, member.ID)
	newEmail := "member-team-crud-renamed@example.test"
	got, revoked, err := repository.Update(context.Background(), owner.ID, member.ID, repositories.TeamMemberUpdate{Email: &newEmail})
	if err != nil || revoked || got.Email != newEmail {
		t.Fatalf("identity-only Update = %#v, revoked=%v, err=%v", got, revoked, err)
	}
	if afterEpoch, afterSessions := teamMemberSecurityState(t, database, member.ID); afterEpoch != epoch || afterSessions != sessions {
		t.Fatalf("identity-only state = epoch %d sessions %d, want %d/%d", afterEpoch, afterSessions, epoch, sessions)
	}

	// A round-trip response contains display names. Normalization strips those
	// derived fields, so submitting the same grants must remain a no-op.
	sameAccess := got.Access
	_, revoked, err = repository.Update(context.Background(), owner.ID, member.ID, repositories.TeamMemberUpdate{Access: &sameAccess})
	if err != nil || revoked {
		t.Fatalf("same-access Update revoked=%v err=%v", revoked, err)
	}
	if afterEpoch, afterSessions := teamMemberSecurityState(t, database, member.ID); afterEpoch != epoch || afterSessions != sessions {
		t.Fatalf("same-access state = epoch %d sessions %d, want %d/%d", afterEpoch, afterSessions, epoch, sessions)
	}

	replacement := core.TeamMemberAccess{
		SubscriptionPermissions: []core.TeamSubscriptionPermission{{
			SubscriptionID: subscriptionID,
			Capability:     core.TeamCapabilityBackups,
			Mode:           core.TeamPermissionManage,
		}},
		DomainPermissions: []core.TeamDomainPermission{},
	}
	got, revoked, err = repository.Update(context.Background(), owner.ID, member.ID, repositories.TeamMemberUpdate{Access: &replacement})
	if err != nil || !revoked {
		t.Fatalf("grant replacement revoked=%v err=%v", revoked, err)
	}
	if len(got.Access.SubscriptionPermissions) != 1 || got.Access.SubscriptionPermissions[0].Capability != core.TeamCapabilityBackups || len(got.Access.DomainPermissions) != 0 {
		t.Fatalf("replaced access = %#v", got.Access)
	}
	epoch++
	if afterEpoch, afterSessions := teamMemberSecurityState(t, database, member.ID); afterEpoch != epoch || afterSessions != 0 {
		t.Fatalf("grant replacement state = epoch %d sessions %d, want %d/0", afterEpoch, afterSessions, epoch)
	}

	insertTeamMemberSession(t, database, member.ID, "password-session")
	passwordHash := "replacement-password-hash"
	_, revoked, err = repository.Update(context.Background(), owner.ID, member.ID, repositories.TeamMemberUpdate{PasswordHash: &passwordHash})
	if err != nil || !revoked {
		t.Fatalf("password Update revoked=%v err=%v", revoked, err)
	}
	epoch++
	if afterEpoch, afterSessions := teamMemberSecurityState(t, database, member.ID); afterEpoch != epoch || afterSessions != 0 {
		t.Fatalf("password state = epoch %d sessions %d, want %d/0", afterEpoch, afterSessions, epoch)
	}

	insertTeamMemberSession(t, database, member.ID, "status-session")
	status := "suspended"
	_, revoked, err = repository.Update(context.Background(), owner.ID, member.ID, repositories.TeamMemberUpdate{Status: &status})
	if err != nil || !revoked {
		t.Fatalf("status Update revoked=%v err=%v", revoked, err)
	}
	epoch++
	if afterEpoch, afterSessions := teamMemberSecurityState(t, database, member.ID); afterEpoch != epoch || afterSessions != 0 {
		t.Fatalf("status state = epoch %d sessions %d, want %d/0", afterEpoch, afterSessions, epoch)
	}

	insertTeamMemberSession(t, database, member.ID, "delete-session")
	deleted, err := repository.Delete(context.Background(), owner.ID, member.ID)
	if err != nil || deleted.ID != member.ID {
		t.Fatalf("Delete = %#v, %v", deleted, err)
	}
	if _, err := repository.GetByOwner(context.Background(), owner.ID, member.ID); !errors.Is(err, repositories.ErrTeamMemberNotFound) {
		t.Fatalf("GetByOwner after delete error = %v", err)
	}
	for _, table := range []string{"users", "sessions", "additional_user_subscription_permissions", "additional_user_domain_permissions"} {
		var count int
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s = ?", table, map[bool]string{true: "id", false: "user_id"}[table == "users"])
		if err := database.GetDB().QueryRow(query, member.ID).Scan(&count); err != nil {
			t.Fatalf("count %s after delete: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows after delete = %d, want 0", table, count)
		}
	}
}

func TestTeamMemberRepositoryStagesSuspendedGrantWritesAtomically(t *testing.T) {
	users, database := newAccountTypeUserRepositoryFixture(t)
	owner := createCustomerAccount(t, users, "suspended-grants")
	subscriptionID, domainID := createTeamMemberScope(t, database, owner.ID, "suspended-grants")
	repository := repositories.NewTeamMemberRepository(database.GetDB())
	initialAccess := core.TeamMemberAccess{
		SubscriptionPermissions: []core.TeamSubscriptionPermission{{
			SubscriptionID: subscriptionID,
			Capability:     core.TeamCapabilityFiles,
			Mode:           core.TeamPermissionView,
		}},
		DomainPermissions: []core.TeamDomainPermission{{
			DomainID:   domainID,
			Capability: core.TeamCapabilityDNS,
			Mode:       core.TeamPermissionManage,
		}},
	}

	member, err := repository.Create(context.Background(), owner.ID, repositories.TeamMemberCreate{
		Username:     "member-suspended-grants",
		PasswordHash: "hash-suspended-grants",
		Email:        "member-suspended-grants@example.test",
		Status:       "suspended",
		Access:       initialAccess,
	})
	if err != nil {
		t.Fatalf("create suspended member with grants: %v", err)
	}
	if member.Status != "suspended" ||
		len(member.Access.SubscriptionPermissions) != 1 ||
		member.Access.SubscriptionPermissions[0].Capability != core.TeamCapabilityFiles ||
		len(member.Access.DomainPermissions) != 1 ||
		member.Access.DomainPermissions[0].Capability != core.TeamCapabilityDNS {
		t.Fatalf("created suspended member = %#v", member)
	}
	if epoch, sessions := teamMemberSecurityState(t, database, member.ID); epoch != 0 || sessions != 0 {
		t.Fatalf("created suspended security state = epoch %d sessions %d, want 0/0", epoch, sessions)
	}

	insertTeamMemberSession(t, database, member.ID, "suspended-grant-edit-session")
	replacement := core.TeamMemberAccess{
		SubscriptionPermissions: []core.TeamSubscriptionPermission{{
			SubscriptionID: subscriptionID,
			Capability:     core.TeamCapabilityBackups,
			Mode:           core.TeamPermissionManage,
		}},
		DomainPermissions: []core.TeamDomainPermission{{
			DomainID:   domainID,
			Capability: core.TeamCapabilitySSL,
			Mode:       core.TeamPermissionView,
		}},
	}
	updated, revoked, err := repository.Update(
		context.Background(),
		owner.ID,
		member.ID,
		repositories.TeamMemberUpdate{Access: &replacement},
	)
	if err != nil || !revoked {
		t.Fatalf("edit suspended grants revoked=%v err=%v", revoked, err)
	}
	if updated.Status != "suspended" ||
		len(updated.Access.SubscriptionPermissions) != 1 ||
		updated.Access.SubscriptionPermissions[0].Capability != core.TeamCapabilityBackups ||
		len(updated.Access.DomainPermissions) != 1 ||
		updated.Access.DomainPermissions[0].Capability != core.TeamCapabilitySSL {
		t.Fatalf("updated suspended member = %#v", updated)
	}
	if epoch, sessions := teamMemberSecurityState(t, database, member.ID); epoch != 1 || sessions != 0 {
		t.Fatalf("updated suspended security state = epoch %d sessions %d, want 1/0", epoch, sessions)
	}
}

func TestTeamMemberRepositoryRejectsInvalidAndForeignAccessAtomically(t *testing.T) {
	users, database := newAccountTypeUserRepositoryFixture(t)
	owner := createCustomerAccount(t, users, "team-valid")
	foreignOwner := createCustomerAccount(t, users, "team-foreign")
	ownedSubscriptionID, _ := createTeamMemberScope(t, database, owner.ID, "team-valid")
	foreignSubscriptionID, _ := createTeamMemberScope(t, database, foreignOwner.ID, "team-foreign")
	repository := repositories.NewTeamMemberRepository(database.GetDB())

	invalid := []core.TeamMemberAccess{
		{},
		{SubscriptionPermissions: []core.TeamSubscriptionPermission{{SubscriptionID: ownedSubscriptionID, Capability: "unknown", Mode: core.TeamPermissionView}}, DomainPermissions: []core.TeamDomainPermission{}},
		{SubscriptionPermissions: []core.TeamSubscriptionPermission{{SubscriptionID: ownedSubscriptionID, Capability: core.TeamCapabilityFiles, Mode: "owner"}}, DomainPermissions: []core.TeamDomainPermission{}},
		{SubscriptionPermissions: []core.TeamSubscriptionPermission{
			{SubscriptionID: ownedSubscriptionID, Capability: core.TeamCapabilityFiles, Mode: core.TeamPermissionView},
			{SubscriptionID: ownedSubscriptionID, Capability: core.TeamCapabilityFiles, Mode: core.TeamPermissionManage},
		}, DomainPermissions: []core.TeamDomainPermission{}},
	}
	for index, access := range invalid {
		_, err := repository.Create(context.Background(), owner.ID, repositories.TeamMemberCreate{
			Username:     fmt.Sprintf("invalid-member-%d", index),
			PasswordHash: "hash",
			Email:        fmt.Sprintf("invalid-member-%d@example.test", index),
			Status:       "active",
			Access:       access,
		})
		if !errors.Is(err, repositories.ErrInvalidTeamPermission) {
			t.Fatalf("invalid access %d error = %v", index, err)
		}
	}
	foreignAccess := core.TeamMemberAccess{
		SubscriptionPermissions: []core.TeamSubscriptionPermission{{SubscriptionID: foreignSubscriptionID, Capability: core.TeamCapabilityFiles, Mode: core.TeamPermissionView}},
		DomainPermissions:       []core.TeamDomainPermission{},
	}
	if _, err := repository.Create(context.Background(), owner.ID, repositories.TeamMemberCreate{
		Username: "foreign-member", PasswordHash: "hash", Email: "foreign-member@example.test", Status: "active", Access: foreignAccess,
	}); !errors.Is(err, repositories.ErrTeamMemberForeignScope) {
		t.Fatalf("foreign create error = %v", err)
	}
	var rejectedCount int
	if err := database.GetDB().QueryRow(`SELECT COUNT(*) FROM users WHERE username LIKE 'invalid-member-%' OR username = 'foreign-member'`).Scan(&rejectedCount); err != nil {
		t.Fatal(err)
	}
	if rejectedCount != 0 {
		t.Fatalf("rejected creates inserted %d users", rejectedCount)
	}

	originalAccess := core.TeamMemberAccess{
		SubscriptionPermissions: []core.TeamSubscriptionPermission{{SubscriptionID: ownedSubscriptionID, Capability: core.TeamCapabilityFiles, Mode: core.TeamPermissionView}},
		DomainPermissions:       []core.TeamDomainPermission{},
	}
	member := createRepositoryTeamMember(t, repository, owner.ID, "rollback", originalAccess)
	insertTeamMemberSession(t, database, member.ID, "rollback-session")
	epoch, sessions := teamMemberSecurityState(t, database, member.ID)
	changedEmail := "rollback-changed@example.test"
	if _, _, err := repository.Update(context.Background(), owner.ID, member.ID, repositories.TeamMemberUpdate{Email: &changedEmail, Access: &foreignAccess}); !errors.Is(err, repositories.ErrTeamMemberForeignScope) {
		t.Fatalf("foreign update error = %v", err)
	}
	after, err := repository.GetByOwner(context.Background(), owner.ID, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Email != member.Email || len(after.Access.SubscriptionPermissions) != 1 || after.Access.SubscriptionPermissions[0].SubscriptionID != ownedSubscriptionID {
		t.Fatalf("failed update partially changed member: %#v", after)
	}
	if afterEpoch, afterSessions := teamMemberSecurityState(t, database, member.ID); afterEpoch != epoch || afterSessions != sessions {
		t.Fatalf("failed update state = epoch %d sessions %d, want %d/%d", afterEpoch, afterSessions, epoch, sessions)
	}
}

func TestTeamMemberRepositoryOwnershipStatusAndConflictsFailClosed(t *testing.T) {
	users, database := newAccountTypeUserRepositoryFixture(t)
	owner := createCustomerAccount(t, users, "owner-a")
	otherOwner := createCustomerAccount(t, users, "owner-b")
	repository := repositories.NewTeamMemberRepository(database.GetDB())
	member := createRepositoryTeamMember(t, repository, owner.ID, "owned", emptyTeamMemberAccess())

	if _, err := repository.GetByOwner(context.Background(), otherOwner.ID, member.ID); !errors.Is(err, repositories.ErrTeamMemberNotFound) {
		t.Fatalf("foreign GetByOwner error = %v", err)
	}
	foreignEmail := "foreign-update@example.test"
	if _, _, err := repository.Update(context.Background(), otherOwner.ID, member.ID, repositories.TeamMemberUpdate{Email: &foreignEmail}); !errors.Is(err, repositories.ErrTeamMemberNotFound) {
		t.Fatalf("foreign Update error = %v", err)
	}
	if _, err := repository.Delete(context.Background(), otherOwner.ID, member.ID); !errors.Is(err, repositories.ErrTeamMemberNotFound) {
		t.Fatalf("foreign Delete error = %v", err)
	}

	if _, err := repository.Create(context.Background(), owner.ID, repositories.TeamMemberCreate{
		Username: "member-owned", PasswordHash: "hash", Email: "other@example.test", Status: "active", Access: emptyTeamMemberAccess(),
	}); !errors.Is(err, repositories.ErrTeamMemberConflict) {
		t.Fatalf("duplicate username error = %v", err)
	}

	if _, err := database.GetDB().Exec(`UPDATE users SET status = 'suspended' WHERE id = ?`, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Create(context.Background(), owner.ID, repositories.TeamMemberCreate{
		Username: "member-suspended-owner", PasswordHash: "hash", Email: "suspended-owner@example.test", Status: "active", Access: emptyTeamMemberAccess(),
	}); !errors.Is(err, repositories.ErrTeamMemberOwnerNotFound) {
		t.Fatalf("create for suspended owner error = %v", err)
	}
	if _, _, err := repository.Update(context.Background(), owner.ID, member.ID, repositories.TeamMemberUpdate{Email: &foreignEmail}); !errors.Is(err, repositories.ErrTeamMemberOwnerNotFound) {
		t.Fatalf("update for suspended owner error = %v", err)
	}
	if listed, err := repository.ListByOwner(context.Background(), owner.ID); err != nil || len(listed) != 1 {
		t.Fatalf("list for suspended owner = %#v, %v", listed, err)
	}
	if _, err := repository.Delete(context.Background(), owner.ID, member.ID); err != nil {
		t.Fatalf("delete for suspended owner: %v", err)
	}
}

func TestTeamMemberRepositoryRejectsStoredForeignScopeFailClosed(t *testing.T) {
	users, database := newAccountTypeUserRepositoryFixture(t)
	owner := createCustomerAccount(t, users, `stored-owner`)
	foreignOwner := createCustomerAccount(t, users, `stored-foreign-owner`)
	repository := repositories.NewTeamMemberRepository(database.GetDB())
	member := createRepositoryTeamMember(t, repository, owner.ID, `stored-foreign`, emptyTeamMemberAccess())
	foreignSubscriptionID, _ := createTeamMemberScope(t, database, foreignOwner.ID, `stored-foreign`)

	if _, err := database.GetDB().Exec(`DROP TRIGGER validate_additional_user_subscription_permission_insert`); err != nil {
		t.Fatalf(`drop ownership validation trigger: %v`, err)
	}
	if _, err := database.GetDB().Exec(`
		INSERT INTO additional_user_subscription_permissions
			(user_id, subscription_id, capability, mode)
		VALUES (?, ?, 'files', 'view')`,
		member.ID, foreignSubscriptionID,
	); err != nil {
		t.Fatalf(`insert corrupt foreign grant: %v`, err)
	}

	if _, err := repository.GetByOwner(context.Background(), owner.ID, member.ID); !errors.Is(err, repositories.ErrTeamMemberForeignScope) {
		t.Fatalf(`GetByOwner corrupt foreign grant error = %v`, err)
	}
	if _, err := repository.ListByOwner(context.Background(), owner.ID); !errors.Is(err, repositories.ErrTeamMemberForeignScope) {
		t.Fatalf(`ListByOwner corrupt foreign grant error = %v`, err)
	}
}
