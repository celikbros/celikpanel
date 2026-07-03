package repositories

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/alicelik/celikpanel/internal/core"
)

type PostgresDomainRepository struct {
	db *sql.DB
}

func NewPostgresDomainRepository(db *sql.DB) *PostgresDomainRepository {
	return &PostgresDomainRepository{db: db}
}

func (r *PostgresDomainRepository) Create(ctx context.Context, domain *core.Domain) error {
	query := `
		INSERT INTO domains (subscription_id, name, ip_address_id, is_temporary, temporary_suffix, status)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	result, err := r.db.ExecContext(ctx, query,
		domain.SubscriptionID, domain.Name, 1, 0, nil, domain.Status)
	if err != nil {
		return err
	}
	
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	
	// Fetch back to populate defaults (created_at)
	return r.db.QueryRowContext(ctx, "SELECT id, created_at, updated_at FROM domains WHERE id = ?", id).
		Scan(&domain.ID, &domain.CreatedAt, &domain.UpdatedAt)
}

func (r *PostgresDomainRepository) GetByID(ctx context.Context, id int) (*core.Domain, error) {
	domain := &core.Domain{}
	query := `
		SELECT id, subscription_id, name, dns_zone_id, status, created_at, updated_at,
		       ip_address_id, is_temporary, temporary_suffix
		FROM domains WHERE id = ?
	`
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&domain.ID, &domain.SubscriptionID, &domain.Name, &domain.DNSZoneID, &domain.Status,
		&domain.CreatedAt, &domain.UpdatedAt, &domain.IPAddressID, &domain.IsTemporary, &domain.TemporarySuffix,
	)
	if err != nil {
		return nil, fmt.Errorf("domain not found: %v", err)
	}
	return domain, nil
}

func (r *PostgresDomainRepository) GetByName(ctx context.Context, name string) (*core.Domain, error) {
	domain := &core.Domain{}
	query := `
		SELECT id, subscription_id, name, dns_zone_id, status, created_at, updated_at,
		       ip_address_id, is_temporary, temporary_suffix
		FROM domains WHERE name = ?
	`
	err := r.db.QueryRowContext(ctx, query, name).Scan(
		&domain.ID, &domain.SubscriptionID, &domain.Name, &domain.DNSZoneID, &domain.Status,
		&domain.CreatedAt, &domain.UpdatedAt, &domain.IPAddressID, &domain.IsTemporary, &domain.TemporarySuffix,
	)
	if err != nil {
		return nil, fmt.Errorf("domain not found: %v", err)
	}
	return domain, nil
}

func (r *PostgresDomainRepository) GetBySubscriptionID(ctx context.Context, subID int) ([]*core.Domain, error) {
	query := `
		SELECT id, subscription_id, name, dns_zone_id, status, created_at, updated_at,
		       ip_address_id, is_temporary, temporary_suffix
		FROM domains WHERE subscription_id = ? ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, subID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var domains []*core.Domain
	for rows.Next() {
		domain := &core.Domain{}
		err := rows.Scan(&domain.ID, &domain.SubscriptionID, &domain.Name, &domain.DNSZoneID, &domain.Status,
			&domain.CreatedAt, &domain.UpdatedAt, &domain.IPAddressID, &domain.IsTemporary, &domain.TemporarySuffix)
		if err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	return domains, nil
}

func (r *PostgresDomainRepository) Update(ctx context.Context, domain *core.Domain) error {
	query := `
		UPDATE domains
		SET name = ?, status = ?, updated_at = datetime('now')
		WHERE id = ?
	`
	_, err := r.db.ExecContext(ctx, query, domain.Name, domain.Status, domain.ID)
	return err
}

func (r *PostgresDomainRepository) Delete(ctx context.Context, id int) error {
	query := `DELETE FROM domains WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

func (r *PostgresDomainRepository) List(ctx context.Context) ([]*core.Domain, error) {
	query := `
		SELECT id, subscription_id, name, dns_zone_id, status, created_at, updated_at,
		       ip_address_id, is_temporary, temporary_suffix
		FROM domains ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var domains []*core.Domain
	for rows.Next() {
		domain := &core.Domain{}
		err := rows.Scan(&domain.ID, &domain.SubscriptionID, &domain.Name, &domain.DNSZoneID, &domain.Status,
			&domain.CreatedAt, &domain.UpdatedAt, &domain.IPAddressID, &domain.IsTemporary, &domain.TemporarySuffix)
		if err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	return domains, nil
}
