package repositories

import (
	"context"
	"github.com/alicelik/celikpanel/internal/core"
)

// UserRepository defines operations for User entity
type UserRepository interface {
	Create(ctx context.Context, user *core.User) error
	GetByID(ctx context.Context, id int) (*core.User, error)
	GetByUsername(ctx context.Context, username string) (*core.User, error)
	GetByEmail(ctx context.Context, email string) (*core.User, error)
	Update(ctx context.Context, user *core.User) error
	Delete(ctx context.Context, id int) error
	List(ctx context.Context) ([]*core.User, error)
}

// SubscriptionRepository defines operations for Subscription entity
type SubscriptionRepository interface {
	Create(ctx context.Context, sub *core.Subscription) error
	GetByID(ctx context.Context, id int) (*core.Subscription, error)
	GetByOwnerID(ctx context.Context, ownerID int) ([]*core.Subscription, error)
	Update(ctx context.Context, sub *core.Subscription) error
	Delete(ctx context.Context, id int) error
	List(ctx context.Context) ([]*core.Subscription, error)
}

// DomainRepository defines operations for Domain entity
type DomainRepository interface {
	Create(ctx context.Context, domain *core.Domain) error
	GetByID(ctx context.Context, id int) (*core.Domain, error)
	GetByName(ctx context.Context, name string) (*core.Domain, error)
	GetBySubscriptionID(ctx context.Context, subID int) ([]*core.Domain, error)
	Update(ctx context.Context, domain *core.Domain) error
	Delete(ctx context.Context, id int) error
	List(ctx context.Context) ([]*core.Domain, error)
}

// Database Management v2 Repositories

// DatabaseServerRepository defines operations for database server management
type DatabaseServerRepository interface {
	Create(ctx context.Context, server *core.DatabaseServer) error
	GetByID(ctx context.Context, id int) (*core.DatabaseServer, error)
	ListBySubscription(ctx context.Context, subscriptionID int) ([]*core.DatabaseServer, error)
	ListByType(ctx context.Context, subscriptionID int, serverType string) ([]*core.DatabaseServer, error)
	Update(ctx context.Context, server *core.DatabaseServer) error
	Delete(ctx context.Context, id int) error
}

// DatabaseUserRepository defines operations for database user management
type DatabaseUserRepository interface {
	Create(ctx context.Context, user *core.DatabaseUser) error
	GetByID(ctx context.Context, id int) (*core.DatabaseUser, error)
	GetByUsername(ctx context.Context, serverID int, username string) (*core.DatabaseUser, error)
	ListByServer(ctx context.Context, serverID int) ([]*core.DatabaseUser, error)
	Update(ctx context.Context, user *core.DatabaseUser) error
	Delete(ctx context.Context, id int) error
}

// DatabaseV2Repository defines operations for database management (v2 schema)
type DatabaseV2Repository interface {
	Create(ctx context.Context, db *core.DatabaseV2) error
	GetByID(ctx context.Context, id int) (*core.DatabaseV2, error)
	ListByServer(ctx context.Context, serverID int) ([]*core.DatabaseV2, error)
	Update(ctx context.Context, db *core.DatabaseV2) error
	Delete(ctx context.Context, id int) error
}

// DatabaseGrantRepository defines operations for database grant management
type DatabaseGrantRepository interface {
	Grant(ctx context.Context, grant *core.DatabaseGrant) error
	Revoke(ctx context.Context, databaseID, userID int) error
	ListByDatabase(ctx context.Context, databaseID int) ([]*core.DatabaseGrant, error)
	ListByUser(ctx context.Context, userID int) ([]*core.DatabaseGrant, error)
	GetByID(ctx context.Context, id int) (*core.DatabaseGrant, error)
	Delete(ctx context.Context, id int) error
}

// SiteRepository defines operations for Site entity
type SiteRepository interface {
	Create(ctx context.Context, site *core.Site) error
	GetByID(ctx context.Context, id int) (*core.Site, error)
	GetByDomainID(ctx context.Context, domainID int) ([]*core.Site, error)
	Update(ctx context.Context, site *core.Site) error
	Delete(ctx context.Context, id int) error
	List(ctx context.Context) ([]*core.Site, error)
}
