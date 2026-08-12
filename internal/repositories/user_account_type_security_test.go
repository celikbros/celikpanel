package repositories_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/repositories"
)

func newAccountTypeUserRepositoryFixture(t *testing.T) (*repositories.PostgresUserRepository, *paneldb.SQLiteDB) {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open account type repository database: %v", err)
	}
	t.Cleanup(database.Close)
	return repositories.NewPostgresUserRepository(database.GetDB()), database
}

func createCustomerAccount(t *testing.T, repository *repositories.PostgresUserRepository, suffix string) *core.User {
	t.Helper()
	user := &core.User{
		Username:     "owner-" + suffix,
		PasswordHash: "hash",
		Email:        fmt.Sprintf("owner-%s@example.test", suffix),
		Role:         "customer",
		Status:       "active",
	}
	if err := repository.Create(context.Background(), user); err != nil {
		t.Fatalf("create customer account: %v", err)
	}
	return user
}

func insertAdditionalUserDirect(t *testing.T, database *paneldb.SQLiteDB, ownerID int, suffix string) int {
	t.Helper()
	result, err := database.GetDB().Exec(`
		INSERT INTO users (username, password_hash, email, role, parent_id, status, account_type)
		VALUES (?, 'hash', ?, 'customer', ?, 'active', 'additional_user')`,
		"member-"+suffix, fmt.Sprintf("member-%s@example.test", suffix), ownerID)
	if err != nil {
		t.Fatalf("insert additional user: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("additional user id: %v", err)
	}
	return int(id)
}

func assertAdditionalUser(t *testing.T, user *core.User, memberID, ownerID int) {
	t.Helper()
	if user.ID != memberID {
		t.Fatalf("user ID = %d, want %d", user.ID, memberID)
	}
	if user.AccountType != core.AccountTypeAdditionalUser {
		t.Fatalf("account type = %q, want %q", user.AccountType, core.AccountTypeAdditionalUser)
	}
	if user.ParentID == nil || *user.ParentID != ownerID {
		t.Fatalf("parent ID = %v, want %d", user.ParentID, ownerID)
	}
	if role := user.EffectiveRole(); role != core.EffectiveRoleAdditionalUser {
		t.Fatalf("effective role = %q, want %q", role, core.EffectiveRoleAdditionalUser)
	}
}

func TestUserRepositoryCreateDefaultsAndReadsAccountType(t *testing.T) {
	repository, database := newAccountTypeUserRepositoryFixture(t)
	user := createCustomerAccount(t, repository, "default")

	if user.AccountType != core.AccountTypeAccount {
		t.Fatalf("created account type = %q, want %q", user.AccountType, core.AccountTypeAccount)
	}
	var storedAccountType string
	if err := database.GetDB().QueryRow(`SELECT account_type FROM users WHERE id = ?`, user.ID).Scan(&storedAccountType); err != nil {
		t.Fatal(err)
	}
	if storedAccountType != string(core.AccountTypeAccount) {
		t.Fatalf("stored account type = %q, want %q", storedAccountType, core.AccountTypeAccount)
	}

	got, err := repository.GetByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.AccountType != core.AccountTypeAccount || got.EffectiveRole() != "customer" {
		t.Fatalf("read account type/effective role = %q/%q, want account/customer", got.AccountType, got.EffectiveRole())
	}
}

func TestUserRepositoryGenericCreateRejectsNonAccountMarkersWithoutInsert(t *testing.T) {
	repository, database := newAccountTypeUserRepositoryFixture(t)

	for index, accountType := range []core.AccountType{core.AccountTypeAdditionalUser, "unknown"} {
		user := &core.User{
			Username:     fmt.Sprintf("rejected-%d", index),
			PasswordHash: "hash",
			Email:        fmt.Sprintf("rejected-%d@example.test", index),
			Role:         "customer",
			Status:       "active",
			AccountType:  accountType,
		}
		if err := repository.Create(context.Background(), user); err == nil {
			t.Fatalf("Create accepted account type %q", accountType)
		}
	}

	var count int
	if err := database.GetDB().QueryRow(`SELECT COUNT(*) FROM users WHERE username LIKE 'rejected-%'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("generic Create inserted %d rejected users, want 0", count)
	}
}

func TestUserRepositoryGetAndListScanAdditionalUserIdentity(t *testing.T) {
	repository, database := newAccountTypeUserRepositoryFixture(t)
	owner := createCustomerAccount(t, repository, "scan")
	memberID := insertAdditionalUserDirect(t, database, owner.ID, "scan")

	getters := []struct {
		name string
		get  func() (*core.User, error)
	}{
		{name: "id", get: func() (*core.User, error) { return repository.GetByID(context.Background(), memberID) }},
		{name: "username", get: func() (*core.User, error) { return repository.GetByUsername(context.Background(), "member-scan") }},
		{name: "email", get: func() (*core.User, error) {
			return repository.GetByEmail(context.Background(), "member-scan@example.test")
		}},
	}
	for _, getter := range getters {
		t.Run(getter.name, func(t *testing.T) {
			user, err := getter.get()
			if err != nil {
				t.Fatalf("get additional user: %v", err)
			}
			assertAdditionalUser(t, user, memberID, owner.ID)
		})
	}

	users, err := repository.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, user := range users {
		if user.ID == memberID {
			assertAdditionalUser(t, user, memberID, owner.ID)
			return
		}
	}
	t.Fatalf("additional user %d missing from List", memberID)
}

func TestUserRepositoryUpdatesPreserveAdditionalUserMarker(t *testing.T) {
	repository, database := newAccountTypeUserRepositoryFixture(t)
	owner := createCustomerAccount(t, repository, "update")
	memberID := insertAdditionalUserDirect(t, database, owner.ID, "update")

	member, err := repository.GetByID(context.Background(), memberID)
	if err != nil {
		t.Fatal(err)
	}
	member.Email = "member-update-once@example.test"
	member.AccountType = core.AccountTypeAccount
	if err := repository.Update(context.Background(), member); err != nil {
		t.Fatalf("Update: %v", err)
	}
	afterUpdate, err := repository.GetByID(context.Background(), memberID)
	if err != nil {
		t.Fatal(err)
	}
	assertAdditionalUser(t, afterUpdate, memberID, owner.ID)

	afterUpdate.Email = "member-update-twice@example.test"
	afterUpdate.AccountType = core.AccountTypeAccount
	if err := repository.UpdateAndRevokeSessions(context.Background(), afterUpdate); err != nil {
		t.Fatalf("UpdateAndRevokeSessions: %v", err)
	}
	afterRevocation, err := repository.GetByID(context.Background(), memberID)
	if err != nil {
		t.Fatal(err)
	}
	assertAdditionalUser(t, afterRevocation, memberID, owner.ID)
}

func TestUserRepositorySuspendingCustomerRevokesOnlyAdditionalUserSessions(t *testing.T) {
	repository, database := newAccountTypeUserRepositoryFixture(t)
	owner := createCustomerAccount(t, repository, "suspend-owner")
	memberID := insertAdditionalUserDirect(t, database, owner.ID, "suspend-owner")
	otherOwner := createCustomerAccount(t, repository, "other-owner")
	otherMemberID := insertAdditionalUserDirect(t, database, otherOwner.ID, "other-owner")

	reseller := &core.User{
		Username:     "unrelated-reseller",
		PasswordHash: "hash",
		Email:        "unrelated-reseller@example.test",
		Role:         "reseller",
		Status:       "active",
	}
	if err := repository.Create(context.Background(), reseller); err != nil {
		t.Fatalf("create reseller: %v", err)
	}
	resellerID := reseller.ID
	customerChild := &core.User{
		Username:     "reseller-customer",
		PasswordHash: "hash",
		Email:        "reseller-customer@example.test",
		Role:         "customer",
		ParentID:     &resellerID,
		Status:       "active",
	}
	if err := repository.Create(context.Background(), customerChild); err != nil {
		t.Fatalf("create reseller customer: %v", err)
	}

	sessionUsers := []int{owner.ID, memberID, otherOwner.ID, otherMemberID, reseller.ID, customerChild.ID}
	for index, userID := range sessionUsers {
		if _, err := database.GetDB().Exec(`
			INSERT INTO sessions (token_hash, user_id, expires_at)
			VALUES (?, ?, '2099-01-01T00:00:00Z')
		`, fmt.Sprintf("suspension-session-%d", index), userID); err != nil {
			t.Fatalf("insert session for user %d: %v", userID, err)
		}
	}

	owner.Status = "suspended"
	if err := repository.UpdateAndRevokeSessions(context.Background(), owner); err != nil {
		t.Fatalf("suspend customer: %v", err)
	}

	expectations := []struct {
		name         string
		userID       int
		wantSessions int
		wantEpoch    int
	}{
		{name: "owner", userID: owner.ID, wantSessions: 0, wantEpoch: 1},
		{name: "owned additional user", userID: memberID, wantSessions: 0, wantEpoch: 1},
		{name: "other owner", userID: otherOwner.ID, wantSessions: 1, wantEpoch: 0},
		{name: "other additional user", userID: otherMemberID, wantSessions: 1, wantEpoch: 0},
		{name: "reseller", userID: reseller.ID, wantSessions: 1, wantEpoch: 0},
		{name: "reseller customer", userID: customerChild.ID, wantSessions: 1, wantEpoch: 0},
	}
	for _, expectation := range expectations {
		t.Run(expectation.name, func(t *testing.T) {
			var sessions, epoch int
			if err := database.GetDB().QueryRow(`
				SELECT
					(SELECT COUNT(*) FROM sessions WHERE user_id = users.id),
					auth_epoch
				FROM users
				WHERE id = ?
			`, expectation.userID).Scan(&sessions, &epoch); err != nil {
				t.Fatal(err)
			}
			if sessions != expectation.wantSessions || epoch != expectation.wantEpoch {
				t.Fatalf("sessions/epoch = %d/%d, want %d/%d",
					sessions, epoch, expectation.wantSessions, expectation.wantEpoch)
			}
		})
	}
}
