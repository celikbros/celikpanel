package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// Every state-changing request carries one identity; a replay never runs twice
// (D-029, 10 Oct 2026).
//
// Measured in a real browser: when a connection is reset while a POST is being
// sent, Chrome sends the POST again by itself, so one click reached the Panel
// three times. A second hazard sat under it: handlers passed the connection's
// context to the Agent call and to the database writes after it, so a reset
// stopped the Panel half way while the Agent kept working, and the replay then
// ran against that torn state.
//
// The guard closes both for the routes it names. The request is identified by
// the header X-CelikPanel-Request-Id (32 lowercase hexadecimal characters, one
// per user action). The first arrival is recorded as `running` and the handler
// runs on a context that the connection cannot cancel, bounded by the route's
// own time limit. Its answer is stored and every later arrival of the same
// identity is answered from the row.
//
// Her durum değiştiren istek tek bir kimlik taşır; yineleme asla ikinci kez
// çalışmaz (D-029, 10 Eki 2026). Gerçek tarayıcıda ölçüldü: bir POST
// gönderilirken bağlantı sıfırlanırsa Chrome POST'u kendiliğinden yeniden
// gönderir; tek tıklama Panel'e üç kez ulaştı. İkinci tehlike: işleyiciler
// bağlantının bağlamını Agent çağrısına ve sonrasındaki veritabanı yazımlarına
// veriyordu; sıfırlama Panel'i yarıda durdururken Agent çalışmayı sürdürüyor,
// yineleme de bu yarım duruma karşı çalışıyordu. Bu koruma adlandırdığı
// rotalarda ikisini de kapatır.

const (
	requestIdentityHeader = "X-CelikPanel-Request-Id"
	// requestIdentityReplayedHeader marks an answer that came from the stored
	// row rather than from a run of the handler.
	requestIdentityReplayedHeader = "X-CelikPanel-Request-Replayed"

	requestIdentityRunning     = "running"
	requestIdentityDone        = "done"
	requestIdentityInterrupted = "interrupted"

	requestIdentityRetention       = 24 * time.Hour
	requestIdentityMaxStoredBody   = 64 << 10
	requestIdentityMaxRequestBody  = 1 << 20
	requestIdentityReplayWait      = 20 * time.Second
	requestIdentityBookkeepingTime = 15 * time.Second

	errCodeRequestIDRequired       = "REQUEST_ID_REQUIRED"
	errCodeRequestIDReused         = "REQUEST_ID_REUSED"
	errCodeRequestInProgress       = "REQUEST_IN_PROGRESS"
	errCodeRequestOutcomeUnknown   = "REQUEST_OUTCOME_UNKNOWN"
	errCodeRequestResultNotKept    = "REQUEST_COMPLETED_RESULT_NOT_RETAINED"
	requestResultNotKeptReasonFail = "failed"
)

// The sentences a screen shows are in web/src/i18n (err.<CODE>), in English and
// Turkish; these are their English originals for anything that reads the API
// directly. Order (D-024): what happened, what was and was not changed, the
// next action, how the work goes on. docs/OPERATION-GUIDANCE.md carries both
// languages verbatim.
// Ekranın gösterdiği cümleler web/src/i18n içindedir; bunlar, API'yi doğrudan
// okuyanlar için İngilizce asıllarıdır.
const requestIDRequiredMessage = "This page was opened before CelikPanel was updated, so the server did not accept the change and nothing was changed. " +
	"Reload the page, then make the change again. " +
	"(A client that is not the CelikPanel page sends the header X-CelikPanel-Request-Id: 32 lowercase hexadecimal characters, a new value for each action.)"

const requestIDReusedMessage = "This change was sent with an identifier the server already used for a different change, so it was not carried out. " +
	"Reload the page, then make the change again."

const requestInProgressMessage = "This change is still running on the server. It was not started a second time. " +
	"Wait a little, then reload the page to see the result; do not send it again."

const requestOutcomeUnknownMessage = "CelikPanel restarted or failed while this change was running, so it is not known whether the change was completed. " +
	"It will not be run again by itself. Reload the page and check the current state; make the change again only if it is missing."

const requestResultNotKeptMessage = "This change was already made; it was not made a second time. " +
	"Its result was shown only once and is not kept. " +
	"Reload the page to see the current state; if you still need what was shown once (a password or a configuration file), create a new one."

const requestFailedResultNotKeptMessage = "This change already ended with an error, and that answer is not kept; it was not tried a second time. " +
	"Reload the page and check the current state; make the change again only if it is missing."

// requestIdentityRoute is one protected route.
type requestIdentityRoute struct {
	// pattern is stored in the row and names the route without its IDs.
	pattern string
	// timeout bounds the handler once it no longer depends on the connection.
	timeout time.Duration
	// secret: the answer carries a one-time secret, so only its status is kept.
	secret bool
}

// The eight routes of batch 1. Each one was harmful when it ran twice; see
// D-029 for the confirmed outcome of each. The time limits follow the longest
// Agent call the handler makes, with room for the work around it.
//
// Birinci grubun sekiz rotası. Her biri iki kez çalıştığında zararlıydı. Süre
// sınırları işleyicinin yaptığı en uzun Agent çağrısını izler.
var (
	requestIdentityRouteBackupRestore = requestIdentityRoute{
		pattern: "/api/v1/domains/{id}/backups/restore", timeout: agentRPCBulkImportTimeout + 10*time.Minute,
	}
	requestIdentityRouteBackupCreate = requestIdentityRoute{
		pattern: "/api/v1/domains/{id}/backups", timeout: agentRPCBulkImportTimeout + 5*time.Minute,
	}
	requestIdentityRouteImportApply = requestIdentityRoute{
		pattern: "/api/v1/import/cpanel/apply", timeout: 2 * time.Hour,
	}
	requestIdentityRouteLetsEncrypt = requestIdentityRoute{
		pattern: "/api/v1/domains/{id}/ssl/letsencrypt", timeout: sslMutationTimeout + 5*time.Minute,
	}
	requestIdentityRouteDomainDatabase = requestIdentityRoute{
		pattern: "/api/v1/domains/{id}/databases", timeout: agentRPCDatabaseTimeout + 5*time.Minute,
	}
	requestIdentityRouteServerDatabase = requestIdentityRoute{
		pattern: "/api/v1/database-servers/{id}/databases", timeout: agentRPCDatabaseTimeout + 5*time.Minute,
	}
	requestIdentityRouteAdminAccount = requestIdentityRoute{
		pattern: "/api/v1/database-servers/{id}/admin-account", timeout: agentRPCDatabaseTimeout + 2*time.Minute,
		secret: true,
	}
	requestIdentityRouteVPNPeer = requestIdentityRoute{
		pattern: "/api/v1/vpn/peers", timeout: 10 * time.Minute, secret: true,
	}
)

// requestIdentityRouteFor names the protected route a request would reach. It
// uses the dispatcher's own strict matchers, so a path the guard does not
// recognize is a path the dispatcher does not serve either.
// requestIdentityRouteFor, isteğin ulaşacağı korunan rotayı adlandırır.
func requestIdentityRouteFor(r *http.Request) (requestIdentityRoute, bool) {
	if r.Method != http.MethodPost {
		return requestIdentityRoute{}, false
	}
	path := r.URL.Path
	switch {
	case path == "/api/v1/import/cpanel/apply":
		return requestIdentityRouteImportApply, true
	case path == "/api/v1/vpn/peers":
		return requestIdentityRouteVPNPeer, true
	case strings.HasPrefix(path, "/api/v1/domains/"):
		match, ok := matchDomainSubroute(r)
		if !ok {
			return requestIdentityRoute{}, false
		}
		switch match.kind {
		case "backup-restore":
			return requestIdentityRouteBackupRestore, true
		case "backups":
			return requestIdentityRouteBackupCreate, true
		case "ssl-letsencrypt":
			return requestIdentityRouteLetsEncrypt, true
		case "databases":
			return requestIdentityRouteDomainDatabase, true
		}
	case strings.HasPrefix(path, "/api/v1/database-servers/"):
		match, ok := matchDatabaseSubroute(r, "/api/v1/database-servers/")
		if !ok {
			return requestIdentityRoute{}, false
		}
		switch match.kind {
		case "server-databases":
			return requestIdentityRouteServerDatabase, true
		case "server-admin-account":
			return requestIdentityRouteAdminAccount, true
		}
	}
	return requestIdentityRoute{}, false
}

type requestIdentityContextKey struct{}

// requestIdentityScope travels with the detached handler context.
type requestIdentityScope struct {
	id string
	mu sync.Mutex
	// notRetained is set by a handler whose answer turned out to carry a
	// one-time secret.
	notRetained bool
}

// requestIdentityFromContext returns the identity of the request a handler is
// running for, or "" when the route is not protected.
// requestIdentityFromContext, işleyicinin uğruna çalıştığı isteğin kimliğini
// döndürür; rota korunmuyorsa "".
func requestIdentityFromContext(ctx context.Context) string {
	if scope, ok := ctx.Value(requestIdentityContextKey{}).(*requestIdentityScope); ok {
		return scope.id
	}
	return ""
}

// doNotRetainRequestAnswer is called by a handler before it writes an answer
// that carries a one-time secret. Only the status of that answer is stored.
// doNotRetainRequestAnswer, tek seferlik gizli bilgi taşıyan bir yanıt
// yazmadan önce çağrılır; o yanıtın yalnız durum kodu saklanır.
func doNotRetainRequestAnswer(ctx context.Context) {
	if scope, ok := ctx.Value(requestIdentityContextKey{}).(*requestIdentityScope); ok {
		scope.mu.Lock()
		scope.notRetained = true
		scope.mu.Unlock()
	}
}

type requestIdentityRow struct {
	id          string
	actor       int
	method      string
	route       string
	hash        string
	status      string
	code        int
	retained    bool
	contentType string
	body        []byte
}

// requestIdentityGuard owns the table and the set of requests this process is
// running. A `running` row that this process is not running was left by an
// earlier process: its outcome is unknown.
// requestIdentityGuard tabloya ve bu sürecin çalıştırdığı isteklere sahiptir.
// Bu sürecin çalıştırmadığı bir `running` satırı önceki bir süreçten kalmıştır:
// sonucu bilinmez.
type requestIdentityGuard struct {
	db   *sql.DB
	now  func() time.Time
	wait time.Duration

	mu       sync.Mutex
	inFlight map[string]chan struct{}
}

func newRequestIdentityGuard(database *sql.DB) *requestIdentityGuard {
	return &requestIdentityGuard{
		db:       database,
		now:      time.Now,
		wait:     requestIdentityReplayWait,
		inFlight: make(map[string]chan struct{}),
	}
}

func validRequestIdentity(id string) bool {
	return validServiceOperationID(id)
}

func requestIdentityHash(method, path, rawQuery string, body []byte) string {
	digest := sha256.New()
	for _, part := range []string{method, path, rawQuery} {
		_, _ = io.WriteString(digest, part)
		_, _ = digest.Write([]byte{0})
	}
	_, _ = digest.Write(body)
	return hex.EncodeToString(digest.Sum(nil))
}

func (g *requestIdentityGuard) bookkeepingContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), requestIdentityBookkeepingTime)
}

// markInterruptedAtStart turns every row a previous process left `running`
// into `interrupted`. It runs before the Panel serves application requests.
// markInterruptedAtStart, önceki sürecin `running` bıraktığı her satırı
// `interrupted` yapar. Panel uygulama isteklerine hizmet vermeden önce çalışır.
func (g *requestIdentityGuard) markInterruptedAtStart(ctx context.Context) (int64, error) {
	result, err := g.db.ExecContext(ctx, `
		UPDATE request_identities
		SET status = ?, finished_at = ?
		WHERE status = ?`,
		requestIdentityInterrupted, g.now().Unix(), requestIdentityRunning)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// sweepExpired removes rows past their 24 hours. No handler runs that long, so
// an expired row is never one this process is still running.
// sweepExpired, 24 saatini dolduran satırları siler.
func (g *requestIdentityGuard) sweepExpired(ctx context.Context) (int64, error) {
	result, err := g.db.ExecContext(ctx,
		`DELETE FROM request_identities WHERE expires_at <= ?`, g.now().Unix())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (g *requestIdentityGuard) load(ctx context.Context, id string) (requestIdentityRow, error) {
	row := requestIdentityRow{id: id}
	var retained int
	var body []byte
	err := g.db.QueryRowContext(ctx, `
		SELECT actor_user_id, method, route, request_sha256, status,
		       response_status, response_retained, response_content_type, response_body
		FROM request_identities WHERE id = ?`, id).Scan(
		&row.actor, &row.method, &row.route, &row.hash, &row.status,
		&row.code, &retained, &row.contentType, &body)
	row.retained = retained == 1
	row.body = body
	return row, err
}

// begin records the first arrival of an identity, or returns the row and the
// in-flight signal of the arrival that came before.
// begin, bir kimliğin ilk gelişini kaydeder ya da önceki gelişin satırını ve
// sürüyor işaretini döndürür.
func (g *requestIdentityGuard) begin(
	ctx context.Context, id string, actor int, method string, route requestIdentityRoute, hash string,
) (first bool, done chan struct{}, existing requestIdentityRow, err error) {
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	// An expired row that the sweep has not reached yet does not block a new
	// use of its identity.
	if _, err := g.db.ExecContext(ctx,
		`DELETE FROM request_identities WHERE id = ? AND expires_at <= ?`, id, now.Unix()); err != nil {
		return false, nil, requestIdentityRow{}, err
	}
	result, err := g.db.ExecContext(ctx, `
		INSERT INTO request_identities
			(id, actor_user_id, method, route, request_sha256, status, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`,
		id, actor, method, route.pattern, hash, requestIdentityRunning,
		now.Unix(), now.Add(requestIdentityRetention).Unix())
	if err != nil {
		return false, nil, requestIdentityRow{}, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return false, nil, requestIdentityRow{}, err
	}
	if inserted == 1 {
		done = make(chan struct{})
		g.inFlight[id] = done
		return true, done, requestIdentityRow{}, nil
	}
	existing, err = g.load(ctx, id)
	if err != nil {
		return false, nil, requestIdentityRow{}, err
	}
	return false, g.inFlight[id], existing, nil
}

// finish stores the outcome and releases every arrival waiting for it. When
// the outcome cannot be stored the row stays `running` with no process behind
// it, and the next arrival answers it as unknown, which is the truth.
// finish sonucu saklar ve onu bekleyen her gelişi serbest bırakır.
func (g *requestIdentityGuard) finish(id string, done chan struct{}, status string, code int, retained bool, contentType string, body []byte) {
	ctx, cancel := g.bookkeepingContext()
	defer cancel()
	var stored any
	retainedFlag := 0
	if retained {
		retainedFlag = 1
		stored = body
		if body == nil {
			stored = []byte{}
		}
	} else {
		contentType = ""
	}
	if _, err := g.db.ExecContext(ctx, `
		UPDATE request_identities
		SET status = ?, response_status = ?, response_retained = ?,
		    response_content_type = ?, response_body = ?, finished_at = ?
		WHERE id = ? AND status = ?`,
		status, code, retainedFlag, contentType, stored, g.now().Unix(),
		id, requestIdentityRunning); err != nil {
		log.Printf("[request-identity] %s: outcome %s (HTTP %d) could not be recorded: %v", id, status, code, err)
	}
	g.mu.Lock()
	delete(g.inFlight, id)
	g.mu.Unlock()
	close(done)
}

func (g *requestIdentityGuard) markAbandoned(id string) {
	ctx, cancel := g.bookkeepingContext()
	defer cancel()
	if _, err := g.db.ExecContext(ctx, `
		UPDATE request_identities SET status = ?, finished_at = ?
		WHERE id = ? AND status = ?`,
		requestIdentityInterrupted, g.now().Unix(), id, requestIdentityRunning); err != nil {
		log.Printf("[request-identity] %s: abandoned request could not be marked interrupted: %v", id, err)
	}
}

// requestIdentityRecorder holds the handler's answer until it is stored.
type requestIdentityRecorder struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (r *requestIdentityRecorder) Header() http.Header { return r.header }

func (r *requestIdentityRecorder) WriteHeader(code int) {
	if r.code == 0 {
		r.code = code
	}
}

func (r *requestIdentityRecorder) Write(data []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	return r.body.Write(data)
}

func writeRequestIdentityRefusal(w http.ResponseWriter, status int, code, message, reason, id string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	body := apiErrorBody{Error: message, Code: code, Reason: reason}
	if id != "" {
		w.Header().Set(requestIdentityHeader, id)
		body.Vars = map[string]string{"request_id": id}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (g *requestIdentityGuard) answerStored(w http.ResponseWriter, row requestIdentityRow) {
	switch row.status {
	case requestIdentityDone:
		if !row.retained {
			if row.code >= 200 && row.code < 300 {
				writeRequestIdentityRefusal(w, http.StatusConflict, errCodeRequestResultNotKept,
					requestResultNotKeptMessage, "", row.id)
				return
			}
			writeRequestIdentityRefusal(w, http.StatusConflict, errCodeRequestResultNotKept,
				requestFailedResultNotKeptMessage, requestResultNotKeptReasonFail, row.id)
			return
		}
		if row.contentType != "" {
			w.Header().Set("Content-Type", row.contentType)
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set(requestIdentityHeader, row.id)
		w.Header().Set(requestIdentityReplayedHeader, "1")
		w.WriteHeader(row.code)
		_, _ = w.Write(row.body)
	default:
		writeRequestIdentityRefusal(w, http.StatusConflict, errCodeRequestOutcomeUnknown,
			requestOutcomeUnknownMessage, "", row.id)
	}
}

// wrap puts the guard in front of next. Requests for any other route pass
// through untouched, with or without the header.
// wrap, korumayı next'in önüne koyar. Başka her rotanın istekleri, başlık olsun
// olmasın, dokunulmadan geçer.
func (g *requestIdentityGuard) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, protected := requestIdentityRouteFor(r)
		if !protected {
			next.ServeHTTP(w, r)
			return
		}
		caller := currentCaller(r)
		if caller == nil {
			writeCodedError(w, http.StatusUnauthorized, errCodeAuthRequired, "authentication required", "")
			return
		}
		id := r.Header.Get(requestIdentityHeader)
		if id == "" {
			// A page loaded before this release cannot run these routes
			// unprotected.
			writeRequestIdentityRefusal(w, http.StatusPreconditionRequired, errCodeRequestIDRequired,
				requestIDRequiredMessage, "", "")
			return
		}
		if !validRequestIdentity(id) {
			writeRequestIdentityRefusal(w, http.StatusBadRequest, errCodeRequestIDRequired,
				requestIDRequiredMessage, "", "")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, requestIdentityMaxRequestBody))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeClientError(w, http.StatusRequestEntityTooLarge, "request body is too large")
				return
			}
			writeClientError(w, http.StatusBadRequest, "request body could not be read")
			return
		}
		hash := requestIdentityHash(r.Method, r.URL.Path, r.URL.RawQuery, body)

		beginCtx, cancelBegin := g.bookkeepingContext()
		first, done, existing, err := g.begin(beginCtx, id, caller.ID, r.Method, route, hash)
		cancelBegin()
		if err != nil {
			// Nothing ran: the handler is never started without its row.
			writeServerError(w, err)
			return
		}
		if !first {
			g.answerReplay(w, r, existing, done, caller.ID, route, hash)
			return
		}
		g.runFirst(w, r, next, route, id, done, body)
	})
}

func (g *requestIdentityGuard) answerReplay(
	w http.ResponseWriter, r *http.Request, existing requestIdentityRow, done chan struct{},
	actor int, route requestIdentityRoute, hash string,
) {
	if existing.actor != actor || existing.hash != hash ||
		existing.method != r.Method || existing.route != route.pattern {
		writeRequestIdentityRefusal(w, http.StatusConflict, errCodeRequestIDReused,
			requestIDReusedMessage, "", existing.id)
		return
	}
	if existing.status != requestIdentityRunning {
		g.answerStored(w, existing)
		return
	}
	if done == nil {
		// `running` with no process behind it.
		g.markAbandoned(existing.id)
		existing.status = requestIdentityInterrupted
		g.answerStored(w, existing)
		return
	}
	timer := time.NewTimer(g.wait)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		writeRequestIdentityRefusal(w, http.StatusConflict, errCodeRequestInProgress,
			requestInProgressMessage, "", existing.id)
		return
	case <-r.Context().Done():
		return
	}
	loadCtx, cancel := g.bookkeepingContext()
	finished, err := g.load(loadCtx, existing.id)
	cancel()
	if err != nil {
		writeServerError(w, err)
		return
	}
	if finished.status == requestIdentityRunning {
		// The outcome could not be recorded when the first arrival ended.
		g.markAbandoned(finished.id)
		finished.status = requestIdentityInterrupted
	}
	g.answerStored(w, finished)
}

func (g *requestIdentityGuard) runFirst(
	w http.ResponseWriter, r *http.Request, next http.Handler,
	route requestIdentityRoute, id string, done chan struct{}, body []byte,
) {
	scope := &requestIdentityScope{id: id}
	// The connection cannot cancel this context: a reset must not stop the
	// Panel between the Agent's work and the record of it.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), route.timeout)
	defer cancel()
	ctx = context.WithValue(ctx, requestIdentityContextKey{}, scope)
	detached := r.WithContext(ctx)
	detached.Body = io.NopCloser(bytes.NewReader(body))
	detached.ContentLength = int64(len(body))

	recorder := &requestIdentityRecorder{header: make(http.Header)}
	panicked := true
	func() {
		defer func() {
			if !panicked {
				return
			}
			if value := recover(); value != nil {
				log.Printf("[request-identity] %s %s %s: handler panicked: %v\n%s",
					id, r.Method, route.pattern, value, debug.Stack())
			}
		}()
		next.ServeHTTP(recorder, detached)
		panicked = false
	}()

	if panicked {
		// What the handler changed before it stopped is not known.
		g.finish(id, done, requestIdentityInterrupted, 0, false, "", nil)
		writeRequestIdentityRefusal(w, http.StatusConflict, errCodeRequestOutcomeUnknown,
			requestOutcomeUnknownMessage, "", id)
		return
	}

	code := recorder.code
	if code == 0 {
		code = http.StatusOK
	}
	answer := recorder.body.Bytes()
	scope.mu.Lock()
	notRetained := scope.notRetained
	scope.mu.Unlock()
	retained := !route.secret && !notRetained && len(answer) <= requestIdentityMaxStoredBody
	g.finish(id, done, requestIdentityDone, code, retained, recorder.header.Get("Content-Type"), answer)

	header := w.Header()
	for name, values := range recorder.header {
		header[name] = append([]string(nil), values...)
	}
	header.Set(requestIdentityHeader, id)
	w.WriteHeader(code)
	_, _ = w.Write(answer)
}
