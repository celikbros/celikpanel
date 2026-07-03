package services

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/alicelik/celikpanel/internal/core"
)

type IPManager struct {
	db *sql.DB
}

func NewIPManager(db *sql.DB) *IPManager {
	return &IPManager{db: db}
}

// ListAvailableIPs returns all active IP addresses
func (m *IPManager) ListAvailableIPs(ctx context.Context) ([]*core.IPAddress, error) {
	query := `
		SELECT id, address, type, is_shared, is_primary, status, created_at
		FROM ip_addresses
		WHERE status = 'active'
		ORDER BY is_primary DESC, is_shared DESC
	`
	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ips []*core.IPAddress
	for rows.Next() {
		ip := &core.IPAddress{}
		err := rows.Scan(&ip.ID, &ip.Address, &ip.Type, &ip.IsShared, &ip.IsPrimary, &ip.Status, &ip.CreatedAt)
		if err != nil {
			return nil, err
		}
		ips = append(ips, ip)
	}
	return ips, nil
}

// GetPrimaryIP returns the server's primary IP address
func (m *IPManager) GetPrimaryIP(ctx context.Context) (*core.IPAddress, error) {
	ip := &core.IPAddress{}
	query := `
		SELECT id, address, type, is_shared, is_primary, status, created_at
		FROM ip_addresses
		WHERE is_primary = 1
		LIMIT 1
	`
	err := m.db.QueryRowContext(ctx, query).Scan(
		&ip.ID, &ip.Address, &ip.Type, &ip.IsShared, &ip.IsPrimary, &ip.Status, &ip.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("primary IP not found: %v", err)
	}
	return ip, nil
}

// AssignIP assigns an IP address to a domain
func (m *IPManager) AssignIP(ctx context.Context, domainID int, ipID int) error {
	query := `UPDATE domains SET ip_address_id = ? WHERE id = ?`
	_, err := m.db.ExecContext(ctx, query, ipID, domainID)
	return err
}

// AddIP adds a new IP address to the pool
func (m *IPManager) AddIP(ctx context.Context, address string, ipType string, isShared bool) error {
	query := `
		INSERT INTO ip_addresses (address, type, is_shared)
		VALUES (?, ?, ?)
	`
	sharedVal := 0
	if isShared {
		sharedVal = 1
	}
	_, err := m.db.ExecContext(ctx, query, address, ipType, sharedVal)
	return err
}
