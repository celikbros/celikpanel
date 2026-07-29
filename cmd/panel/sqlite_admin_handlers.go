package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/systemsqlite"
)

const (
	systemSQLiteReleaseTimeout  = 5 * time.Second
	systemSQLiteAuditTimeout    = 2 * time.Second
	systemSQLiteMaxSnapshotSize = int64(2 << 30)
	systemSQLiteDownloadBodyMax = int64(4096)
)

func (p *Panel) handleSystemSQLiteDatabases(w http.ResponseWriter, r *http.Request) {
	if !p.requireSystemSQLiteAdmin(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	request := systemsqlite.ListRequest{ProtocolVersion: systemsqlite.ProtocolVersion}
	var response systemsqlite.ListResponse
	if err := p.agentClient.CallContext(r.Context(), "Agent.ListSystemSQLiteDatabases", &request, &response); err != nil {
		writeAgentError(w, err, "")
		return
	}
	if !response.Success || response.Error != "" {
		writeAgentError(w, nil, response.Error)
		return
	}
	if response.Databases == nil {
		response.Databases = []systemsqlite.DatabaseInfo{}
	}
	for index := range response.Databases {
		if !knownSystemSQLiteDatabase(response.Databases[index].ID) ||
			!safeSystemSQLitePathHint(response.Databases[index].PathHint) {
			writeServerError(w, errors.New("agent returned an unknown system SQLite database ID"))
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"databases": response.Databases})
}

func safeSystemSQLitePathHint(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 256 {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	for _, component := range strings.FieldsFunc(value, func(character rune) bool {
		return character == '/' || character == '\\'
	}) {
		if strings.TrimSpace(component) == ".." {
			return false
		}
	}
	return !filepath.IsAbs(value) &&
		filepath.VolumeName(value) == "" &&
		!hasSystemSQLiteWindowsVolume(value) &&
		!strings.HasPrefix(value, "/") &&
		!strings.HasPrefix(value, `\`)
}

func hasSystemSQLiteWindowsVolume(value string) bool {
	if len(value) < 2 || value[1] != ':' {
		return false
	}
	first := value[0]
	return first >= 'A' && first <= 'Z' || first >= 'a' && first <= 'z'
}

func (p *Panel) handleSystemSQLiteDatabaseAction(w http.ResponseWriter, r *http.Request) {
	if !p.requireSystemSQLiteAdmin(w, r) {
		return
	}

	databaseID, action, ok := parseSystemSQLiteActionPath(r.URL.Path)
	if !ok || !knownSystemSQLiteDatabase(databaseID) {
		writeClientError(w, http.StatusNotFound, "system database action not found")
		return
	}
	switch action {
	case "check", "snapshot", "snapshot-download", "optimize":
	default:
		writeClientError(w, http.StatusNotFound, "system database action not found")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	switch action {
	case "check":
		p.handleSystemSQLiteCheck(w, r, databaseID)
	case "snapshot":
		p.handleSystemSQLiteSnapshot(w, r, databaseID)
	case "snapshot-download":
		p.handleSystemSQLiteSnapshotDownload(w, r, databaseID)
	case "optimize":
		p.handleSystemSQLiteOptimize(w, r, databaseID)
	}
}

func (p *Panel) requireSystemSQLiteAdmin(w http.ResponseWriter, r *http.Request) bool {
	caller := currentCaller(r)
	if caller != nil && caller.Role == roleAdmin {
		return true
	}
	if p.db != nil {
		p.audit(r, "system_sqlite.access.denied", "system_database", 0)
	}
	writeClientError(w, http.StatusForbidden, "administrator access required")
	return false
}

func parseSystemSQLiteActionPath(requestPath string) (databaseID, action string, ok bool) {
	const prefix = "/api/v1/system-databases/"
	if !strings.HasPrefix(requestPath, prefix) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(requestPath, prefix), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func knownSystemSQLiteDatabase(databaseID string) bool {
	switch databaseID {
	case systemsqlite.DatabasePanel,
		systemsqlite.DatabasePowerDNS,
		systemsqlite.DatabaseRoundcube,
		systemsqlite.DatabaseComponentCatalog:
		return true
	default:
		return false
	}
}

func (p *Panel) handleSystemSQLiteCheck(w http.ResponseWriter, r *http.Request, databaseID string) {
	request := systemsqlite.DatabaseRequest{
		ProtocolVersion: systemsqlite.ProtocolVersion,
		DatabaseID:      databaseID,
	}
	var response systemsqlite.CheckResponse
	if err := p.agentClient.CallContext(r.Context(), "Agent.CheckSystemSQLiteDatabase", &request, &response); err != nil {
		p.auditSystemSQLiteResult(r, databaseID, "check", "failed")
		writeAgentError(w, err, "")
		return
	}
	if !response.Success || response.Error != "" {
		p.auditSystemSQLiteResult(r, databaseID, "check", "failed")
		writeAgentError(w, nil, response.Error)
		return
	}
	if response.Check.DatabaseID != databaseID {
		p.auditSystemSQLiteResult(r, databaseID, "check", "failed")
		writeServerError(w, errors.New("agent returned a mismatched system SQLite check result"))
		return
	}

	response.Error = ""
	auditResult := "completed"
	if !response.Check.IntegrityOK || !response.Check.ForeignKeysOK {
		auditResult = "warning"
	}
	p.auditSystemSQLiteResult(r, databaseID, "check", auditResult)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (p *Panel) handleSystemSQLiteOptimize(w http.ResponseWriter, r *http.Request, databaseID string) {
	request := systemsqlite.DatabaseRequest{
		ProtocolVersion: systemsqlite.ProtocolVersion,
		DatabaseID:      databaseID,
	}
	var response systemsqlite.OptimizeResponse
	if err := p.agentClient.CallContext(r.Context(), "Agent.OptimizeSystemSQLiteDatabase", &request, &response); err != nil {
		p.auditSystemSQLiteResult(r, databaseID, "optimize", "failed")
		writeAgentError(w, err, "")
		return
	}
	if !response.Success || response.Error != "" {
		p.auditSystemSQLiteResult(r, databaseID, "optimize", "failed")
		writeAgentError(w, nil, response.Error)
		return
	}
	if response.Result.DatabaseID != databaseID {
		p.auditSystemSQLiteResult(r, databaseID, "optimize", "failed")
		writeServerError(w, errors.New("agent returned a mismatched system SQLite optimize result"))
		return
	}

	response.Error = ""
	p.auditSystemSQLiteResult(r, databaseID, "optimize", "completed")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (p *Panel) handleSystemSQLiteSnapshot(w http.ResponseWriter, r *http.Request, databaseID string) {
	p.auditSystemSQLiteResult(r, databaseID, "snapshot", "requested")
	request := systemsqlite.DatabaseRequest{
		ProtocolVersion: systemsqlite.ProtocolVersion,
		DatabaseID:      databaseID,
	}
	var response systemsqlite.SnapshotResponse
	if err := p.agentClient.CallContext(r.Context(), "Agent.CreateSystemSQLiteSnapshot", &request, &response); err != nil {
		p.auditSystemSQLiteResult(r, databaseID, "snapshot", "failed")
		writeAgentError(w, err, "")
		return
	}
	if !response.Success || response.Error != "" {
		p.auditSystemSQLiteResult(r, databaseID, "snapshot", "failed")
		writeAgentError(w, nil, response.Error)
		return
	}
	snapshot := response.Snapshot
	releaseSnapshot := snapshot.Token != ""
	if snapshot.Token != "" {
		defer func() {
			if releaseSnapshot {
				p.releaseSystemSQLiteSnapshot(r, databaseID, snapshot.Token)
			}
		}()
	}
	expectedSHA256, digestErr := decodeSystemSQLiteSHA256(snapshot.SHA256)
	if snapshot.DatabaseID != databaseID ||
		!validSystemSQLiteSnapshotToken(snapshot.Token) ||
		snapshot.SizeBytes <= 0 ||
		snapshot.SizeBytes > systemSQLiteMaxSnapshotSize ||
		digestErr != nil {
		p.auditSystemSQLiteResult(r, databaseID, "snapshot", "failed")
		writeServerError(w, errors.New("agent returned invalid system SQLite snapshot metadata"))
		return
	}

	if err := p.verifySystemSQLiteSnapshot(r.Context(), databaseID, snapshot.Token, snapshot.SizeBytes, expectedSHA256); err != nil {
		p.auditSystemSQLiteResult(r, databaseID, "snapshot", "failed")
		writeServerError(w, err)
		return
	}

	releaseSnapshot = false
	p.auditSystemSQLiteResult(r, databaseID, "snapshot", "prepared")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"download_token": snapshot.Token,
		"size_bytes":     snapshot.SizeBytes,
	})
}

func (p *Panel) handleSystemSQLiteSnapshotDownload(w http.ResponseWriter, r *http.Request, databaseID string) {
	p.auditSystemSQLiteResult(r, databaseID, "snapshot_download", "requested")
	r.Body = http.MaxBytesReader(w, r.Body, systemSQLiteDownloadBodyMax)
	if err := r.ParseForm(); err != nil {
		p.auditSystemSQLiteResult(r, databaseID, "snapshot_download", "failed")
		writeClientError(w, http.StatusBadRequest, "invalid snapshot download request")
		return
	}
	token := r.PostForm.Get("download_token")
	if !validSystemSQLiteSnapshotToken(token) {
		p.auditSystemSQLiteResult(r, databaseID, "snapshot_download", "failed")
		writeClientError(w, http.StatusBadRequest, "invalid snapshot download token")
		return
	}
	defer p.releaseSystemSQLiteSnapshot(r, databaseID, token)

	firstChunk, err := p.readSystemSQLiteSnapshotChunk(r.Context(), token, 0)
	if err != nil {
		p.auditSystemSQLiteResult(r, databaseID, "snapshot_download", "failed")
		writeServerError(w, errors.New("system SQLite snapshot download failed"))
		return
	}
	snapshotSize := firstChunk.SizeBytes
	if snapshotSize <= 0 || snapshotSize > systemSQLiteMaxSnapshotSize {
		p.auditSystemSQLiteResult(r, databaseID, "snapshot_download", "failed")
		writeServerError(w, errors.New("agent returned invalid system SQLite snapshot metadata"))
		return
	}
	if err := validateSystemSQLiteSnapshotChunk(firstChunk, databaseID, 0, snapshotSize); err != nil {
		p.auditSystemSQLiteResult(r, databaseID, "snapshot_download", "failed")
		writeServerError(w, err)
		return
	}

	filename := fmt.Sprintf("celikpanel-%s-%s.sqlite3", databaseID, time.Now().UTC().Format("20060102T150405Z"))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("Content-Length", strconv.FormatInt(snapshotSize, 10))
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	chunk := firstChunk
	for {
		if len(chunk.Data) > 0 {
			written, writeErr := w.Write(chunk.Data)
			if writeErr != nil || written != len(chunk.Data) {
				p.auditSystemSQLiteResult(r, databaseID, "snapshot_download", "failed")
				if writeErr == nil {
					writeErr = io.ErrShortWrite
				}
				log.Printf("system SQLite snapshot stream stopped at %d: %v", chunk.NextOffset-int64(len(chunk.Data)), writeErr)
				return
			}
		}
		if chunk.EOF {
			p.auditSystemSQLiteResult(r, databaseID, "snapshot_download", "completed")
			return
		}
		offset := chunk.NextOffset
		chunk, err = p.readSystemSQLiteSnapshotChunk(r.Context(), token, offset)
		if err != nil {
			p.auditSystemSQLiteResult(r, databaseID, "snapshot_download", "failed")
			log.Printf("system SQLite snapshot stream stopped at %d: %v", offset, err)
			return
		}
		if err := validateSystemSQLiteSnapshotChunk(chunk, databaseID, offset, snapshotSize); err != nil {
			p.auditSystemSQLiteResult(r, databaseID, "snapshot_download", "failed")
			log.Printf("system SQLite snapshot stream validation failed at %d: %v", offset, err)
			return
		}
	}
}

// verifySystemSQLiteSnapshot performs a bounded first pass before HTTP headers are committed.
// verifySystemSQLiteSnapshot, HTTP başlıkları kesinleşmeden önce sınırlı bir ilk geçiş yapar.
func (p *Panel) verifySystemSQLiteSnapshot(ctx context.Context, databaseID, token string, expectedSize int64, expectedSHA256 []byte) error {
	hasher := sha256.New()
	offset := int64(0)
	for {
		chunk, err := p.readSystemSQLiteSnapshotChunk(ctx, token, offset)
		if err != nil {
			return errors.New("system SQLite snapshot verification failed")
		}
		if err := validateSystemSQLiteSnapshotChunk(chunk, databaseID, offset, expectedSize); err != nil {
			return err
		}
		if len(chunk.Data) > 0 {
			_, _ = hasher.Write(chunk.Data)
		}
		offset = chunk.NextOffset
		if chunk.EOF {
			break
		}
	}
	if subtle.ConstantTimeCompare(hasher.Sum(nil), expectedSHA256) != 1 {
		return errors.New("system SQLite snapshot verification failed")
	}
	return nil
}

func decodeSystemSQLiteSHA256(value string) ([]byte, error) {
	if len(value) != sha256.Size*2 {
		return nil, errors.New("invalid system SQLite snapshot SHA-256 length")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return nil, errors.New("invalid system SQLite snapshot SHA-256")
	}
	return decoded, nil
}

func validSystemSQLiteSnapshotToken(token string) bool {
	if len(token) != sha256.Size*2 {
		return false
	}
	for _, character := range token {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

func (p *Panel) readSystemSQLiteSnapshotChunk(
	ctx context.Context,
	token string,
	offset int64,
) (systemsqlite.ReadSnapshotChunkResponse, error) {
	request := systemsqlite.ReadSnapshotChunkRequest{
		ProtocolVersion: systemsqlite.ProtocolVersion,
		Token:           token,
		Offset:          offset,
		MaxBytes:        systemsqlite.MaxChunkSize,
	}
	var response systemsqlite.ReadSnapshotChunkResponse
	if err := p.agentClient.CallContext(ctx, "Agent.ReadSystemSQLiteSnapshotChunk", &request, &response); err != nil {
		return systemsqlite.ReadSnapshotChunkResponse{}, err
	}
	if !response.Success || response.Error != "" {
		if response.Error == "" {
			response.Error = "agent refused the system SQLite snapshot chunk"
		}
		return systemsqlite.ReadSnapshotChunkResponse{}, errors.New(response.Error)
	}
	return response, nil
}

func validateSystemSQLiteSnapshotChunk(
	chunk systemsqlite.ReadSnapshotChunkResponse,
	expectedDatabaseID string,
	requestOffset int64,
	expectedSize int64,
) error {
	if !knownSystemSQLiteDatabase(expectedDatabaseID) || chunk.DatabaseID != expectedDatabaseID {
		return errors.New("system SQLite snapshot database mismatch")
	}
	if requestOffset < 0 || expectedSize < 0 || requestOffset > expectedSize {
		return errors.New("invalid system SQLite snapshot bounds")
	}
	if len(chunk.Data) > systemsqlite.MaxChunkSize {
		return errors.New("system SQLite snapshot chunk exceeds the maximum size")
	}
	expectedNext := requestOffset + int64(len(chunk.Data))
	if expectedNext < requestOffset || chunk.NextOffset != expectedNext {
		return errors.New("system SQLite snapshot chunk offset mismatch")
	}
	if chunk.SizeBytes != expectedSize || chunk.NextOffset > expectedSize {
		return errors.New("system SQLite snapshot size mismatch")
	}
	if chunk.EOF {
		if chunk.NextOffset != expectedSize {
			return errors.New("system SQLite snapshot ended before the declared size")
		}
		return nil
	}
	if len(chunk.Data) == 0 || chunk.NextOffset >= expectedSize {
		return errors.New("system SQLite snapshot returned an empty non-final chunk")
	}
	return nil
}

func (p *Panel) releaseSystemSQLiteSnapshot(r *http.Request, databaseID, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), systemSQLiteReleaseTimeout)
	defer cancel()
	request := systemsqlite.ReleaseSnapshotRequest{
		ProtocolVersion: systemsqlite.ProtocolVersion,
		Token:           token,
	}
	var response systemsqlite.ReleaseSnapshotResponse
	if err := p.agentClient.CallContext(ctx, "Agent.ReleaseSystemSQLiteSnapshot", &request, &response); err != nil {
		p.auditSystemSQLiteResult(r, databaseID, "snapshot_release", "failed")
		log.Printf("system SQLite snapshot release failed: %v", err)
		return
	}
	if !response.Success || !response.Released || response.Error != "" {
		p.auditSystemSQLiteResult(r, databaseID, "snapshot_release", "failed")
		if response.Error != "" {
			log.Printf("system SQLite snapshot release refused: %s", response.Error)
		}
	}
}

func (p *Panel) auditSystemSQLiteResult(r *http.Request, databaseID, operation, result string) {
	if p.db == nil {
		return
	}
	auditRequest := r
	if r.Context().Err() != nil {
		ctx, cancel := context.WithTimeout(context.Background(), systemSQLiteAuditTimeout)
		defer cancel()
		if caller := currentCaller(r); caller != nil {
			ctx = context.WithValue(ctx, callerKey, caller)
		}
		auditRequest = r.Clone(ctx)
	}
	p.audit(auditRequest, "system_sqlite."+operation+"."+result+":"+databaseID, "system_database", 0)
}
