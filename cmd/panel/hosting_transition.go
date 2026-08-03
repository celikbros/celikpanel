package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	hostingMutationTimeout     = 12 * time.Minute
	hostingCompensationTimeout = 3 * time.Minute
)

var (
	errHostingSiteNotFound     = errors.New("hosting site not found")
	errHostingConcurrentChange = errors.New("hosting configuration changed concurrently")
	errHostingPortInUse        = errors.New("application port is already assigned")
	errHostingActivation       = errors.New("hosting activation failed and the previous configuration was restored")
	errHostingRestore          = errors.New("hosting activation failed and runtime restoration is incomplete")
)

type hostingOperationLock struct {
	token chan struct{}
	refs  int
}

var domainHostingOperationLocks = struct {
	sync.Mutex
	entries map[int]*hostingOperationLock
}{entries: make(map[int]*hostingOperationLock)}

// Application ports are a machine-wide namespace. Holding this lease across
// every transition into or out of Node prevents two domains from reserving the
// same free port, and prevents a newly allocated app from racing an old app
// that nginx has stopped using but systemd has not removed yet.
var hostingAppPortLease = func() chan struct{} {
	lease := make(chan struct{}, 1)
	lease <- struct{}{}
	return lease
}()

func lockDomainHostingOperation(ctx context.Context, domainID int) (func(), error) {
	domainHostingOperationLocks.Lock()
	entry := domainHostingOperationLocks.entries[domainID]
	if entry == nil {
		entry = &hostingOperationLock{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		domainHostingOperationLocks.entries[domainID] = entry
	}
	entry.refs++
	domainHostingOperationLocks.Unlock()

	select {
	case <-ctx.Done():
		domainHostingOperationLocks.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(domainHostingOperationLocks.entries, domainID)
		}
		domainHostingOperationLocks.Unlock()
		return nil, ctx.Err()
	case <-entry.token:
		return func() {
			entry.token <- struct{}{}
			domainHostingOperationLocks.Lock()
			entry.refs--
			if entry.refs == 0 {
				delete(domainHostingOperationLocks.entries, domainID)
			}
			domainHostingOperationLocks.Unlock()
		}, nil
	}
}

func lockHostingAppPort(ctx context.Context) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-hostingAppPortLease:
		return func() { hostingAppPortLease <- struct{}{} }, nil
	}
}

// Once authorized, a hosting mutation must finish or compensate even when the
// browser disconnects. WithoutCancel preserves request values but removes the
// client lifetime; the operation still has a reviewed hard deadline.
func hostingDurableContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(parent), hostingMutationTimeout)
}

func hostingCompensationContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), hostingCompensationTimeout)
}

type hostingRuntimeState struct {
	SiteID         int
	DomainID       int
	DomainName     string
	DocumentRoot   string
	ProjectType    string
	AppPort        sql.NullInt64
	StartCommand   sql.NullString
	RuntimeVersion sql.NullString
	ForwardTo      sql.NullString
	ForwardCode    sql.NullInt64
}

func (p *Panel) handleUpdateHosting(w http.ResponseWriter, r *http.Request, domainID int) {
	var req hostingSettings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validProjectTypes[req.ProjectType] {
		writeClientError(w, http.StatusBadRequest, "project_type must be one of php, static, node, proxy, forwarding")
		return
	}

	switch req.ProjectType {
	case "node":
		if strings.TrimSpace(req.StartCommand) == "" {
			writeClientError(w, http.StatusBadRequest, "start_command is required for node projects")
			return
		}
		if strings.TrimSpace(req.RuntimeVersion) == "" {
			writeCodedError(
				w,
				http.StatusBadRequest,
				errCodeRuntimeVersionRequired,
				"pick a Node.js version installed by the panel",
				"/services",
			)
			return
		}
		if req.AppPort < 0 || req.AppPort > 65535 ||
			(req.AppPort > 0 && req.AppPort < 1024) {
			writeClientError(w, http.StatusBadRequest, "app_port must be between 1024 and 65535")
			return
		}
	case "forwarding":
		if !strings.HasPrefix(req.ForwardTo, "http://") &&
			!strings.HasPrefix(req.ForwardTo, "https://") {
			writeClientError(w, http.StatusBadRequest, "forward_to must be an http(s) URL")
			return
		}
		if req.ForwardCode != 301 && req.ForwardCode != 302 {
			req.ForwardCode = 301
		}
	case "proxy":
		if !strings.HasPrefix(req.ForwardTo, "http://") &&
			!strings.HasPrefix(req.ForwardTo, "https://") {
			writeClientError(w, http.StatusBadRequest, "forward_to (upstream URL) is required for proxy projects")
			return
		}
	}

	ctx, cancel := hostingDurableContext(r.Context())
	defer cancel()
	appPort, err := p.transitionHosting(ctx, domainID, req)
	switch {
	case err == nil:
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":  true,
			"app_port": appPort,
		})
	case errors.Is(err, errHostingSiteNotFound):
		writeClientError(w, http.StatusNotFound, "site not found")
	case errors.Is(err, errHostingConcurrentChange):
		writeClientError(w, http.StatusConflict, "hosting settings changed; reload and try again")
	case errors.Is(err, errHostingPortInUse):
		writeClientError(w, http.StatusConflict, "the selected application port is already assigned")
	case errors.Is(err, errHostingActivation):
		log.Printf("hosting transition domain %d: %v", domainID, err)
		writeClientError(
			w,
			http.StatusConflict,
			"the hosting change could not be activated; the previous configuration was restored",
		)
	default:
		writeServerError(w, err)
	}
}

func (p *Panel) transitionHosting(
	ctx context.Context,
	domainID int,
	req hostingSettings,
) (int, error) {
	unlockDomain, err := lockDomainHostingOperation(ctx, domainID)
	if err != nil {
		return 0, err
	}
	defer unlockDomain()

	previous, err := p.loadHostingRuntimeState(ctx, domainID)
	if err != nil {
		return 0, err
	}

	var unlockPort func()
	if previous.ProjectType == "node" || req.ProjectType == "node" {
		unlockPort, err = lockHostingAppPort(ctx)
		if err != nil {
			return 0, err
		}
		defer unlockPort()
	}

	target := previous
	target.ProjectType = req.ProjectType
	target.AppPort = sql.NullInt64{}
	target.StartCommand = sql.NullString{}
	target.RuntimeVersion = sql.NullString{}
	target.ForwardTo = sql.NullString{}
	target.ForwardCode = sql.NullInt64{}

	switch req.ProjectType {
	case "node":
		port := req.AppPort
		if port == 0 && previous.ProjectType == "node" && previous.AppPort.Valid {
			port = int(previous.AppPort.Int64)
		}
		if port == 0 {
			port, err = p.allocateAppPort(ctx)
			if err != nil {
				return 0, err
			}
		}
		var conflicts int
		if err := p.db.GetDB().QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM sites
			WHERE app_port = ? AND id <> ?`, port, previous.SiteID).Scan(&conflicts); err != nil {
			return 0, err
		}
		if conflicts != 0 {
			return 0, errHostingPortInUse
		}
		target.AppPort = sql.NullInt64{Int64: int64(port), Valid: true}
		target.StartCommand = sql.NullString{String: strings.TrimSpace(req.StartCommand), Valid: true}
		target.RuntimeVersion = sql.NullString{String: strings.TrimSpace(req.RuntimeVersion), Valid: true}
	case "proxy":
		target.ForwardTo = sql.NullString{String: strings.TrimSpace(req.ForwardTo), Valid: true}
	case "forwarding":
		target.ForwardTo = sql.NullString{String: strings.TrimSpace(req.ForwardTo), Valid: true}
		target.ForwardCode = sql.NullInt64{Int64: int64(req.ForwardCode), Valid: true}
	}

	// Start or reconfigure the new application before publishing a vhost that
	// sends traffic to it. A transport failure is ambiguous because net/rpc
	// cannot cancel an already dispatched agent method, so every failure takes
	// the same compensation path.
	if target.ProjectType == "node" {
		if err := p.applyHostingApp(ctx, target); err != nil {
			return 0, p.compensateHostingFailure(err, previous, target, false)
		}
	}

	if err := p.compareAndSwapHostingState(ctx, previous, target); err != nil {
		if target.ProjectType == "node" {
			return 0, p.compensateHostingFailure(err, previous, target, false)
		}
		return 0, err
	}

	if err := p.applyVhostForDomain(ctx, domainID); err != nil {
		return 0, p.compensateHostingFailure(err, previous, target, true)
	}

	// Keep the old Node process alive until nginx no longer points at it.
	if previous.ProjectType == "node" && target.ProjectType != "node" {
		if err := p.removeHostingApp(ctx, previous.SiteID); err != nil {
			return 0, p.compensateHostingFailure(err, previous, target, true)
		}
	}

	if target.AppPort.Valid {
		return int(target.AppPort.Int64), nil
	}
	return 0, nil
}

func (p *Panel) loadHostingRuntimeState(
	ctx context.Context,
	domainID int,
) (hostingRuntimeState, error) {
	var state hostingRuntimeState
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT s.id, s.domain_id, d.name, s.document_root,
		       COALESCE(s.project_type, 'php'), s.app_port, s.start_command,
		       s.runtime_version, s.forward_to, s.forward_code
		FROM sites s
		JOIN domains d ON d.id = s.domain_id
		WHERE s.domain_id = ?`, domainID).Scan(
		&state.SiteID,
		&state.DomainID,
		&state.DomainName,
		&state.DocumentRoot,
		&state.ProjectType,
		&state.AppPort,
		&state.StartCommand,
		&state.RuntimeVersion,
		&state.ForwardTo,
		&state.ForwardCode,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return hostingRuntimeState{}, errHostingSiteNotFound
	}
	return state, err
}

func nullableHostingInt(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

func nullableHostingString(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func (p *Panel) compareAndSwapHostingState(
	ctx context.Context,
	from hostingRuntimeState,
	to hostingRuntimeState,
) error {
	result, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE sites
		SET project_type = ?, app_port = ?, start_command = ?,
		    runtime_version = ?, forward_to = ?, forward_code = ?,
		    updated_at = datetime('now')
		WHERE id = ?
		  AND COALESCE(project_type, 'php') = ?
		  AND app_port IS ?
		  AND start_command IS ?
		  AND runtime_version IS ?
		  AND forward_to IS ?
		  AND forward_code IS ?`,
		to.ProjectType,
		nullableHostingInt(to.AppPort),
		nullableHostingString(to.StartCommand),
		nullableHostingString(to.RuntimeVersion),
		nullableHostingString(to.ForwardTo),
		nullableHostingInt(to.ForwardCode),
		from.SiteID,
		from.ProjectType,
		nullableHostingInt(from.AppPort),
		nullableHostingString(from.StartCommand),
		nullableHostingString(from.RuntimeVersion),
		nullableHostingString(from.ForwardTo),
		nullableHostingInt(from.ForwardCode),
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errHostingConcurrentChange
	}
	return nil
}

func (p *Panel) applyHostingApp(ctx context.Context, state hostingRuntimeState) error {
	if state.ProjectType != "node" ||
		!state.AppPort.Valid ||
		!state.StartCommand.Valid ||
		!state.RuntimeVersion.Valid {
		return errors.New("incomplete node runtime state")
	}
	var response transport.AppApplyResponse
	err := p.callAgentContext(ctx, "Agent.ApplyAppUnit", &transport.AppApplyRequest{
		SiteID:      state.SiteID,
		Description: state.DomainName,
		WorkDir:     state.DocumentRoot,
		Command:     state.StartCommand.String,
		Port:        int(state.AppPort.Int64),
		NodeVersion: state.RuntimeVersion.String,
		RunAsUser:   "www-data",
	}, &response)
	if err != nil {
		return fmt.Errorf("apply application unit: %w", err)
	}
	if response.Error != "" {
		return fmt.Errorf("apply application unit rejected: %s", response.Error)
	}
	return nil
}

func (p *Panel) removeHostingApp(ctx context.Context, siteID int) error {
	var response transport.AppApplyResponse
	err := p.callAgentContext(
		ctx,
		"Agent.RemoveAppUnit",
		&transport.AppControlRequest{SiteID: siteID},
		&response,
	)
	if err != nil {
		return fmt.Errorf("remove application unit: %w", err)
	}
	if response.Error != "" {
		return fmt.Errorf("remove application unit rejected: %s", response.Error)
	}
	return nil
}

func (p *Panel) reconcileHostingApp(
	ctx context.Context,
	state hostingRuntimeState,
) error {
	if state.ProjectType == "node" {
		return p.applyHostingApp(ctx, state)
	}
	return p.removeHostingApp(ctx, state.SiteID)
}

func (p *Panel) compensateHostingFailure(
	cause error,
	previous hostingRuntimeState,
	target hostingRuntimeState,
	databaseApplied bool,
) error {
	ctx, cancel := hostingCompensationContext()
	defer cancel()

	desired := previous
	var restoreErrors []error
	if databaseApplied {
		if err := p.compareAndSwapHostingState(ctx, target, previous); err != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("restore database: %w", err))
			current, loadErr := p.loadHostingRuntimeState(ctx, previous.DomainID)
			if loadErr != nil {
				restoreErrors = append(restoreErrors, fmt.Errorf("load current database state: %w", loadErr))
				return fmt.Errorf(
					"%w: activation: %v; compensation: %v",
					errHostingRestore,
					cause,
					errors.Join(restoreErrors...),
				)
			}
			desired = current
		}
	} else {
		// The runtime may have changed before the compare-and-swap failed.
		// Reconcile to the database state that actually won, never to the
		// stale snapshot this request originally loaded.
		current, loadErr := p.loadHostingRuntimeState(ctx, previous.DomainID)
		if loadErr != nil {
			return fmt.Errorf(
				"%w: activation: %v; load current database state: %v",
				errHostingRestore,
				cause,
				loadErr,
			)
		}
		desired = current
	}
	if err := p.reconcileHostingApp(ctx, desired); err != nil {
		restoreErrors = append(restoreErrors, fmt.Errorf("restore application unit: %w", err))
	}
	if err := p.applyVhostForDomain(ctx, desired.DomainID); err != nil {
		restoreErrors = append(restoreErrors, fmt.Errorf("restore vhost: %w", err))
	}
	if restoreErr := errors.Join(restoreErrors...); restoreErr != nil {
		return fmt.Errorf(
			"%w: activation: %v; compensation: %v",
			errHostingRestore,
			cause,
			restoreErr,
		)
	}
	return fmt.Errorf("%w: %w", errHostingActivation, cause)
}
