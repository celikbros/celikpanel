package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	paneldb "github.com/alicelik/celikpanel/internal/db"
)

// D-029. These tests drive the guard with a counting handler in place of the
// real ones; request_identity_routes_test.go drives the eight real handlers.
// D-029. Bu testler korumayı gerçek işleyiciler yerine sayan bir işleyiciyle
// sürer.

const (
	// Not secret-bearing / secret-bearing protected routes used as carriers.
	requestIdentityPlainPath  = "/api/v1/import/cpanel/apply"
	requestIdentitySecretPath = "/api/v1/vpn/peers"
)

func newRequestIdentityTestGuard(t *testing.T) (*requestIdentityGuard, *sql.DB) {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(database.Close)
	return newRequestIdentityGuard(database.GetDB()), database.GetDB()
}

func requestIdentityTestID(n int) string {
	return fmt.Sprintf("%032x", n)
}

func requestIdentityTestRequest(path, body, id string, caller *Caller) *http.Request {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if id != "" {
		request.Header.Set(requestIdentityHeader, id)
	}
	if caller != nil {
		request = request.WithContext(context.WithValue(request.Context(), callerKey, caller))
	}
	return request
}

func requestIdentityTestAdmin() *Caller {
	return &Caller{ID: 7, Role: roleAdmin}
}

type requestIdentityStoredRow struct {
	status   string
	code     int
	retained int
	body     []byte
	actor    int
	route    string
}

func readRequestIdentityRow(t *testing.T, db *sql.DB, id string) requestIdentityStoredRow {
	t.Helper()
	var row requestIdentityStoredRow
	if err := db.QueryRow(`
		SELECT status, response_status, response_retained, response_body, actor_user_id, route
		FROM request_identities WHERE id = ?`, id).Scan(
		&row.status, &row.code, &row.retained, &row.body, &row.actor, &row.route); err != nil {
		t.Fatalf("read request identity %s: %v", id, err)
	}
	return row
}

func decodeRequestIdentityRefusal(t *testing.T, recorder *httptest.ResponseRecorder) apiErrorBody {
	t.Helper()
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("refusal is not the coded error shape: %v: %s", err, recorder.Body.String())
	}
	return body
}

// The handler runs once; the replay is answered from the row, byte for byte.
// İşleyici bir kez çalışır; yineleme satırdan, baytı baytına yanıtlanır.
func TestRequestIdentityFirstArrivalRunsAndReplayIsAnsweredFromTheRow(t *testing.T) {
	guard, db := newRequestIdentityTestGuard(t)
	var runs atomic.Int32
	handler := guard.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runs.Add(1)
		if got := requestIdentityFromContext(r.Context()); got != requestIdentityTestID(1) {
			t.Errorf("handler context carries identity %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(w, `{"run":%d}`, runs.Load())
	}))

	var answers []string
	for attempt := 0; attempt < 3; attempt++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, requestIdentityTestRequest(
			requestIdentityPlainPath, `{"a":1}`, requestIdentityTestID(1), requestIdentityTestAdmin()))
		if recorder.Code != http.StatusCreated {
			t.Fatalf("attempt %d: status=%d body=%s", attempt, recorder.Code, recorder.Body.String())
		}
		if got := recorder.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("attempt %d: content type %q", attempt, got)
		}
		if got := recorder.Header().Get(requestIdentityHeader); got != requestIdentityTestID(1) {
			t.Fatalf("attempt %d: answer carries identity %q", attempt, got)
		}
		if replayed := recorder.Header().Get(requestIdentityReplayedHeader); (attempt > 0) != (replayed == "1") {
			t.Fatalf("attempt %d: replayed header %q", attempt, replayed)
		}
		answers = append(answers, recorder.Body.String())
	}
	if runs.Load() != 1 {
		t.Fatalf("handler ran %d times, want once", runs.Load())
	}
	if answers[0] != `{"run":1}` || answers[1] != answers[0] || answers[2] != answers[0] {
		t.Fatalf("answers differ: %q", answers)
	}
	row := readRequestIdentityRow(t, db, requestIdentityTestID(1))
	if row.status != requestIdentityDone || row.code != http.StatusCreated || row.retained != 1 ||
		row.actor != 7 || row.route != requestIdentityRouteImportApply.pattern {
		t.Fatalf("row=%+v", row)
	}
	var expires, created int64
	if err := db.QueryRow(`SELECT created_at, expires_at FROM request_identities WHERE id = ?`,
		requestIdentityTestID(1)).Scan(&created, &expires); err != nil {
		t.Fatal(err)
	}
	if expires-created != int64((24 * time.Hour).Seconds()) {
		t.Fatalf("row lives %d seconds, want 24 hours", expires-created)
	}
}

// The request body itself is never stored: the row holds only its SHA-256.
// İstek gövdesi asla saklanmaz: satır yalnız SHA-256'sını tutar.
func TestRequestIdentityNeverStoresTheRequestBody(t *testing.T) {
	guard, db := newRequestIdentityTestGuard(t)
	handler := guard.wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	const secret = "eab-hmac-key-that-must-not-be-stored"
	handler.ServeHTTP(httptest.NewRecorder(), requestIdentityTestRequest(
		requestIdentityPlainPath, `{"eab_hmac_key":"`+secret+`"}`, requestIdentityTestID(2), requestIdentityTestAdmin()))
	rows, err := db.Query(`SELECT * FROM request_identities`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		for i, value := range values {
			if strings.Contains(fmt.Sprintf("%s", value), secret) {
				t.Fatalf("column %s holds the request body", columns[i])
			}
		}
	}
}

// A page loaded before this release cannot run a protected route.
// Bu sürümden önce yüklenmiş bir sayfa korunan bir rotayı çalıştıramaz.
func TestRequestIdentityIsRequiredOnProtectedRoutesAndIgnoredElsewhere(t *testing.T) {
	guard, db := newRequestIdentityTestGuard(t)
	var runs atomic.Int32
	handler := guard.wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		runs.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, path := range []string{
		"/api/v1/domains/3/backups/restore",
		"/api/v1/import/cpanel/apply",
		"/api/v1/domains/3/ssl/letsencrypt",
		"/api/v1/domains/3/backups",
		"/api/v1/database-servers/4/admin-account",
		"/api/v1/vpn/peers",
		"/api/v1/database-servers/4/databases",
		"/api/v1/domains/3/databases",
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, requestIdentityTestRequest(path, `{}`, "", requestIdentityTestAdmin()))
		if recorder.Code != http.StatusPreconditionRequired {
			t.Fatalf("%s without the header: status=%d", path, recorder.Code)
		}
		if body := decodeRequestIdentityRefusal(t, recorder); body.Code != errCodeRequestIDRequired || body.Error != requestIDRequiredMessage {
			t.Fatalf("%s: refusal=%+v", path, body)
		}
		for _, malformed := range []string{"abc", strings.Repeat("A", 32), strings.Repeat("g", 32), strings.Repeat("a", 33)} {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, requestIdentityTestRequest(path, `{}`, malformed, requestIdentityTestAdmin()))
			if recorder.Code != http.StatusBadRequest || decodeRequestIdentityRefusal(t, recorder).Code != errCodeRequestIDRequired {
				t.Fatalf("%s with identity %q: status=%d body=%s", path, malformed, recorder.Code, recorder.Body.String())
			}
		}
	}
	if runs.Load() != 0 {
		t.Fatalf("a refused request reached the handler %d times", runs.Load())
	}

	// A protected route without a verified caller never reaches the handler.
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, requestIdentityTestRequest(requestIdentityPlainPath, `{}`, requestIdentityTestID(3), nil))
	if recorder.Code != http.StatusUnauthorized || runs.Load() != 0 {
		t.Fatalf("no caller: status=%d runs=%d", recorder.Code, runs.Load())
	}

	// Every other route, and every other method on these paths, behaves as
	// before: no header is asked for and none is recorded.
	passThrough := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/domains/3/backups", nil),
		httptest.NewRequest(http.MethodDelete, "/api/v1/domains/3/backups?name=x", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/database-servers/4/admin-account", nil),
		httptest.NewRequest(http.MethodDelete, "/api/v1/database-servers/4/admin-account", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/vpn/peers", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/vpn/peers/9/ack", strings.NewReader(`{}`)),
		httptest.NewRequest(http.MethodPost, "/api/v1/import/cpanel/inspect", strings.NewReader(`{}`)),
		httptest.NewRequest(http.MethodPost, "/api/v1/domains/3/ssl/upload", strings.NewReader(`{}`)),
		httptest.NewRequest(http.MethodPut, "/api/v1/domains/3/backups/schedule", strings.NewReader(`{}`)),
		httptest.NewRequest(http.MethodPost, "/api/v1/domains/create", strings.NewReader(`{}`)),
	}
	for index, request := range passThrough {
		if index%2 == 0 {
			request.Header.Set(requestIdentityHeader, requestIdentityTestID(100+index))
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("%s %s: status=%d", request.Method, request.URL.Path, recorder.Code)
		}
	}
	if int(runs.Load()) != len(passThrough) {
		t.Fatalf("unprotected routes reached the handler %d times, want %d", runs.Load(), len(passThrough))
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_identities`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("%d rows were recorded for refused or unprotected requests", rows)
	}
}

// The same identity with another body, path or actor is refused and changes
// nothing; the original row is left as it was.
// Aynı kimlik başka gövde, yol ya da kullanıcıyla reddedilir.
func TestRequestIdentityReusedForADifferentRequestIsRefused(t *testing.T) {
	guard, db := newRequestIdentityTestGuard(t)
	var runs atomic.Int32
	handler := guard.wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		runs.Add(1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	id := requestIdentityTestID(4)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, requestIdentityTestRequest("/api/v1/domains/3/backups", `{"type":"files"}`, id, requestIdentityTestAdmin()))
	if first.Code != http.StatusOK {
		t.Fatalf("first: %d", first.Code)
	}

	other := &Caller{ID: 8, Role: roleAdmin}
	withQuery := requestIdentityTestRequest("/api/v1/domains/3/backups?x=1", `{"type":"files"}`, id, requestIdentityTestAdmin())
	for name, request := range map[string]*http.Request{
		"different body":   requestIdentityTestRequest("/api/v1/domains/3/backups", `{"type":"full"}`, id, requestIdentityTestAdmin()),
		"different domain": requestIdentityTestRequest("/api/v1/domains/5/backups", `{"type":"files"}`, id, requestIdentityTestAdmin()),
		"different route":  requestIdentityTestRequest("/api/v1/domains/3/backups/restore", `{"type":"files"}`, id, requestIdentityTestAdmin()),
		"different query":  withQuery,
		"different actor":  requestIdentityTestRequest("/api/v1/domains/3/backups", `{"type":"files"}`, id, other),
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		body := decodeRequestIdentityRefusal(t, recorder)
		if recorder.Code != http.StatusConflict || body.Code != errCodeRequestIDReused || body.Vars["request_id"] != id {
			t.Fatalf("%s: status=%d body=%s", name, recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), `"ok"`) {
			t.Fatalf("%s: the first request's answer was shown to a different request", name)
		}
	}
	if runs.Load() != 1 {
		t.Fatalf("handler ran %d times", runs.Load())
	}
	if row := readRequestIdentityRow(t, db, id); row.status != requestIdentityDone || row.actor != 7 {
		t.Fatalf("row was changed by a refused reuse: %+v", row)
	}
}

// A replay that arrives while the first is running waits for it and gets its
// answer; when the wait runs out it is told the request is still running, with
// the same identity, and the handler is not started again.
// İlki sürerken gelen yineleme onu bekler ve onun yanıtını alır; bekleme
// dolarsa isteğin sürdüğü, aynı kimlikle söylenir.
func TestRequestIdentityReplayWhileRunningWaitsThenSaysInProgress(t *testing.T) {
	guard, _ := newRequestIdentityTestGuard(t)
	guard.wait = 30 * time.Millisecond
	var runs atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	handler := guard.wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		runs.Add(1)
		close(entered)
		<-release
		_, _ = w.Write([]byte(`{"finished":true}`))
	}))
	id := requestIdentityTestID(5)
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, requestIdentityTestRequest(requestIdentityPlainPath, `{}`, id, requestIdentityTestAdmin()))
		firstDone <- recorder
	}()
	<-entered

	early := httptest.NewRecorder()
	handler.ServeHTTP(early, requestIdentityTestRequest(requestIdentityPlainPath, `{}`, id, requestIdentityTestAdmin()))
	body := decodeRequestIdentityRefusal(t, early)
	if early.Code != http.StatusConflict || body.Code != errCodeRequestInProgress ||
		body.Vars["request_id"] != id || early.Header().Get(requestIdentityHeader) != id {
		t.Fatalf("in-flight replay: status=%d body=%s", early.Code, early.Body.String())
	}

	// A replay that is still waiting when the first one ends gets its answer.
	guard.wait = 30 * time.Second
	waiting := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, requestIdentityTestRequest(requestIdentityPlainPath, `{}`, id, requestIdentityTestAdmin()))
		waiting <- recorder
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)
	first := <-firstDone
	replay := <-waiting
	if first.Code != http.StatusOK || replay.Code != http.StatusOK || first.Body.String() != replay.Body.String() {
		t.Fatalf("first=%d %q replay=%d %q", first.Code, first.Body.String(), replay.Code, replay.Body.String())
	}
	if runs.Load() != 1 {
		t.Fatalf("handler ran %d times", runs.Load())
	}
}

// Rows a previous process left running are marked at start. Their replay is
// told the outcome is unknown and the handler is never run for them again.
// Önceki sürecin `running` bıraktığı satırlar açılışta işaretlenir; yinelemeye
// sonucun bilinmediği söylenir ve işleyici onlar için bir daha çalışmaz.
func TestRequestIdentityRunningRowsAreInterruptedAtStartAndNeverReExecuted(t *testing.T) {
	guard, db := newRequestIdentityTestGuard(t)
	now := time.Now().Unix()
	hash := requestIdentityHash(http.MethodPost, requestIdentityPlainPath, "", []byte(`{}`))
	for _, id := range []string{requestIdentityTestID(6), requestIdentityTestID(7)} {
		if _, err := db.Exec(`
			INSERT INTO request_identities (id, actor_user_id, method, route, request_sha256, status, created_at, expires_at)
			VALUES (?, 7, 'POST', ?, ?, 'running', ?, ?)`,
			id, requestIdentityRouteImportApply.pattern, hash, now, now+3600); err != nil {
			t.Fatal(err)
		}
	}
	var runs atomic.Int32
	handler := guard.wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { runs.Add(1) }))

	// Without the start-up pass: a running row no process runs is still
	// answered as unknown, and marked.
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, requestIdentityTestRequest(requestIdentityPlainPath, `{}`, requestIdentityTestID(6), requestIdentityTestAdmin()))
	if body := decodeRequestIdentityRefusal(t, recorder); recorder.Code != http.StatusConflict ||
		body.Code != errCodeRequestOutcomeUnknown || body.Error != requestOutcomeUnknownMessage {
		t.Fatalf("orphan running row: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if row := readRequestIdentityRow(t, db, requestIdentityTestID(6)); row.status != requestIdentityInterrupted {
		t.Fatalf("orphan row status=%q", row.status)
	}

	marked, err := guard.markInterruptedAtStart(context.Background())
	if err != nil || marked != 1 {
		t.Fatalf("marked=%d err=%v", marked, err)
	}
	if row := readRequestIdentityRow(t, db, requestIdentityTestID(7)); row.status != requestIdentityInterrupted {
		t.Fatalf("row status=%q", row.status)
	}
	for attempt := 0; attempt < 2; attempt++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, requestIdentityTestRequest(requestIdentityPlainPath, `{}`, requestIdentityTestID(7), requestIdentityTestAdmin()))
		if recorder.Code != http.StatusConflict || decodeRequestIdentityRefusal(t, recorder).Code != errCodeRequestOutcomeUnknown {
			t.Fatalf("interrupted replay: status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
	if runs.Load() != 0 {
		t.Fatalf("an interrupted request was executed %d times", runs.Load())
	}
}

// An answer larger than 64 KiB is not kept: the replay is told the change was
// made and that its result is not retained.
// 64 KiB'tan büyük yanıt saklanmaz.
func TestRequestIdentityOversizeAnswerKeepsOnlyItsStatus(t *testing.T) {
	guard, db := newRequestIdentityTestGuard(t)
	large := strings.Repeat("x", requestIdentityMaxStoredBody+1)
	exact := strings.Repeat("y", requestIdentityMaxStoredBody)
	handler := guard.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == requestIdentityPlainPath {
			_, _ = w.Write([]byte(large))
			return
		}
		_, _ = w.Write([]byte(exact))
	}))

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, requestIdentityTestRequest(requestIdentityPlainPath, `{}`, requestIdentityTestID(8), requestIdentityTestAdmin()))
	if first.Code != http.StatusOK || first.Body.Len() != len(large) {
		t.Fatalf("first arrival did not get the whole answer: %d bytes", first.Body.Len())
	}
	if row := readRequestIdentityRow(t, db, requestIdentityTestID(8)); row.retained != 0 || row.body != nil || row.code != http.StatusOK {
		t.Fatalf("oversize row=%+v", row)
	}
	replay := httptest.NewRecorder()
	handler.ServeHTTP(replay, requestIdentityTestRequest(requestIdentityPlainPath, `{}`, requestIdentityTestID(8), requestIdentityTestAdmin()))
	body := decodeRequestIdentityRefusal(t, replay)
	if replay.Code != http.StatusConflict || body.Code != errCodeRequestResultNotKept || body.Reason != "" ||
		body.Error != requestResultNotKeptMessage {
		t.Fatalf("oversize replay: status=%d body=%s", replay.Code, replay.Body.String())
	}

	// Exactly 64 KiB is kept.
	handler.ServeHTTP(httptest.NewRecorder(), requestIdentityTestRequest("/api/v1/domains/3/backups", `{}`, requestIdentityTestID(9), requestIdentityTestAdmin()))
	if row := readRequestIdentityRow(t, db, requestIdentityTestID(9)); row.retained != 1 || len(row.body) != requestIdentityMaxStoredBody {
		t.Fatalf("64 KiB row retained=%d bytes=%d", row.retained, len(row.body))
	}
}

// A route whose answer carries a one-time secret stores the status only; so
// does a handler that says its answer carries one. A failed first attempt is
// reported as failed, not as made.
// Yanıtı tek seferlik gizli bilgi taşıyan rota yalnız durum kodunu saklar.
func TestRequestIdentitySecretAnswersAreNeverStored(t *testing.T) {
	guard, db := newRequestIdentityTestGuard(t)
	const secret = "PrivateKey = one-time-secret"
	handler := guard.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.RawQuery, "fail"):
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"engine said: ` + secret + `"}`))
			return
		case r.URL.Path == "/api/v1/database-servers/4/databases":
			doNotRetainRequestAnswer(r.Context())
		}
		_, _ = w.Write([]byte(`{"client_config":"` + secret + `"}`))
	}))

	for index, target := range []string{requestIdentitySecretPath, "/api/v1/database-servers/4/admin-account", "/api/v1/database-servers/4/databases"} {
		id := requestIdentityTestID(20 + index)
		first := httptest.NewRecorder()
		handler.ServeHTTP(first, requestIdentityTestRequest(target, `{}`, id, requestIdentityTestAdmin()))
		if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), secret) {
			t.Fatalf("%s: the first arrival must get the one-time answer: %d %s", target, first.Code, first.Body.String())
		}
		row := readRequestIdentityRow(t, db, id)
		if row.status != requestIdentityDone || row.retained != 0 || row.body != nil || row.code != http.StatusOK {
			t.Fatalf("%s: row=%+v", target, row)
		}
		replay := httptest.NewRecorder()
		handler.ServeHTTP(replay, requestIdentityTestRequest(target, `{}`, id, requestIdentityTestAdmin()))
		body := decodeRequestIdentityRefusal(t, replay)
		if replay.Code != http.StatusConflict || body.Code != errCodeRequestResultNotKept || body.Reason != "" {
			t.Fatalf("%s replay: status=%d body=%s", target, replay.Code, replay.Body.String())
		}
		if strings.Contains(replay.Body.String(), secret) {
			t.Fatalf("%s: the replay carried the secret", target)
		}
	}

	failed := requestIdentityTestID(30)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, requestIdentityTestRequest(requestIdentitySecretPath+"?fail=1", `{}`, failed, requestIdentityTestAdmin()))
	if first.Code != http.StatusConflict {
		t.Fatalf("failed first arrival: %d", first.Code)
	}
	replay := httptest.NewRecorder()
	handler.ServeHTTP(replay, requestIdentityTestRequest(requestIdentitySecretPath+"?fail=1", `{}`, failed, requestIdentityTestAdmin()))
	body := decodeRequestIdentityRefusal(t, replay)
	if replay.Code != http.StatusConflict || body.Code != errCodeRequestResultNotKept ||
		body.Reason != requestResultNotKeptReasonFail || body.Error != requestFailedResultNotKeptMessage {
		t.Fatalf("failed replay: status=%d body=%s", replay.Code, replay.Body.String())
	}
	if strings.Contains(replay.Body.String(), secret) {
		t.Fatal("the replay of a failed secret-bearing request carried the first answer")
	}

	var stored int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_identities WHERE response_body IS NOT NULL OR response_content_type != ''`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 0 {
		t.Fatalf("%d secret-bearing answers were stored", stored)
	}
}

// Expired rows are removed by the sweep; a row still in its 24 hours stays.
// An expired identity the sweep has not reached does not block a new request.
// Süresi dolan satırlar süpürülür.
func TestRequestIdentityExpirySweep(t *testing.T) {
	guard, db := newRequestIdentityTestGuard(t)
	clock := time.Unix(1_800_000_000, 0)
	guard.now = func() time.Time { return clock }
	var runs atomic.Int32
	handler := guard.wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"run":%d}`, runs.Add(1))
	}))
	send := func(id string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, requestIdentityTestRequest(requestIdentityPlainPath, `{}`, id, requestIdentityTestAdmin()))
		return recorder
	}
	old, young := requestIdentityTestID(40), requestIdentityTestID(41)
	send(old)
	clock = clock.Add(23 * time.Hour)
	send(young)

	if removed, err := guard.sweepExpired(context.Background()); err != nil || removed != 0 {
		t.Fatalf("early sweep removed=%d err=%v", removed, err)
	}
	if got := send(old).Body.String(); got != `{"run":1}` {
		t.Fatalf("replay inside 24 hours: %q", got)
	}

	clock = clock.Add(time.Hour)
	// Not swept yet, but expired: the identity starts a new request.
	if got := send(old).Body.String(); got != `{"run":3}` {
		t.Fatalf("expired identity: %q", got)
	}
	clock = clock.Add(23 * time.Hour)
	removed, err := guard.sweepExpired(context.Background())
	if err != nil || removed != 1 {
		t.Fatalf("sweep removed=%d err=%v", removed, err)
	}
	var left []string
	rows, err := db.Query(`SELECT id FROM request_identities`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		left = append(left, id)
	}
	if len(left) != 1 || left[0] != old {
		t.Fatalf("rows after sweep: %v", left)
	}
}

// A handler that panics has an unknown outcome. The row says so, and so does
// every answer for that identity; the handler is not run again.
// Panik yapan işleyicinin sonucu bilinmez; satır ve her yanıt bunu söyler.
func TestRequestIdentityHandlerPanicLeavesATruthfulRow(t *testing.T) {
	guard, db := newRequestIdentityTestGuard(t)
	var runs atomic.Int32
	handler := guard.wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		runs.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"half":"written"}`))
		panic("handler failed after it changed something")
	}))
	id := requestIdentityTestID(50)
	for attempt := 0; attempt < 2; attempt++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, requestIdentityTestRequest(requestIdentityPlainPath, `{}`, id, requestIdentityTestAdmin()))
		body := decodeRequestIdentityRefusal(t, recorder)
		if recorder.Code != http.StatusConflict || body.Code != errCodeRequestOutcomeUnknown {
			t.Fatalf("attempt %d: status=%d body=%s", attempt, recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "half") {
			t.Fatalf("attempt %d: a half-written answer was sent", attempt)
		}
	}
	if runs.Load() != 1 {
		t.Fatalf("handler ran %d times", runs.Load())
	}
	row := readRequestIdentityRow(t, db, id)
	if row.status != requestIdentityInterrupted || row.retained != 0 || row.body != nil {
		t.Fatalf("row=%+v", row)
	}
	guard.mu.Lock()
	inFlight := len(guard.inFlight)
	guard.mu.Unlock()
	if inFlight != 0 {
		t.Fatalf("%d requests still counted as running", inFlight)
	}
}

// The client goes away while the handler is working. The handler's context is
// not cancelled, the handler finishes, its outcome is recorded, and the replay
// is answered from that record.
// İstemci, işleyici çalışırken gider. İşleyicinin bağlamı iptal edilmez,
// işleyici bitirir, sonucu kaydedilir ve yineleme o kayıttan yanıtlanır.
func TestRequestIdentityHandlerOutlivesTheConnection(t *testing.T) {
	guard, db := newRequestIdentityTestGuard(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var contextError atomic.Value
	var deadlineSet atomic.Bool
	var runs atomic.Int32
	handler := guard.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runs.Add(1)
		close(entered)
		<-release
		if err := r.Context().Err(); err != nil {
			contextError.Store(err)
		}
		if deadline, ok := r.Context().Deadline(); ok && time.Until(deadline) > time.Hour {
			deadlineSet.Store(true)
		}
		// The caller is still known after the connection has gone.
		if caller := currentCaller(r); caller == nil || caller.ID != 7 {
			t.Errorf("caller lost on the detached context: %+v", caller)
		}
		_, _ = w.Write([]byte(`{"recorded":true}`))
	}))

	id := requestIdentityTestID(60)
	connection, hangUp := context.WithCancel(context.Background())
	request := requestIdentityTestRequest(requestIdentityPlainPath, `{"domain":"example.test"}`, id, requestIdentityTestAdmin())
	request = request.WithContext(context.WithValue(connection, callerKey, requestIdentityTestAdmin()))
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}()
	<-entered
	hangUp()
	if row := readRequestIdentityRow(t, db, id); row.status != requestIdentityRunning {
		t.Fatalf("row while the handler works: %+v", row)
	}
	close(release)
	<-finished

	if err := contextError.Load(); err != nil {
		t.Fatalf("the handler's context was cancelled with the connection: %v", err)
	}
	if !deadlineSet.Load() {
		t.Fatal("the detached context has no server-side time limit of the route")
	}
	row := readRequestIdentityRow(t, db, id)
	if row.status != requestIdentityDone || row.code != http.StatusOK || string(row.body) != `{"recorded":true}` {
		t.Fatalf("row after the client left: %+v", row)
	}
	replay := httptest.NewRecorder()
	handler.ServeHTTP(replay, requestIdentityTestRequest(requestIdentityPlainPath, `{"domain":"example.test"}`, id, requestIdentityTestAdmin()))
	if replay.Code != http.StatusOK || replay.Body.String() != `{"recorded":true}` || runs.Load() != 1 {
		t.Fatalf("replay after the client left: %d %q runs=%d", replay.Code, replay.Body.String(), runs.Load())
	}
}

// Many arrivals of one identity at once: one run, one answer for all.
// Tek kimliğin aynı anda çok gelişi: tek çalışma, hepsine tek yanıt.
func TestRequestIdentityConcurrentArrivalsRunOnce(t *testing.T) {
	guard, _ := newRequestIdentityTestGuard(t)
	var runs atomic.Int32
	handler := guard.wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		run := runs.Add(1)
		time.Sleep(5 * time.Millisecond)
		_, _ = fmt.Fprintf(w, `{"run":%d}`, run)
	}))
	const arrivals = 12
	id := requestIdentityTestID(70)
	answers := make([]string, arrivals)
	codes := make([]int, arrivals)
	var group sync.WaitGroup
	start := make(chan struct{})
	for index := 0; index < arrivals; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, requestIdentityTestRequest(requestIdentityPlainPath, `{}`, id, requestIdentityTestAdmin()))
			answers[index], codes[index] = recorder.Body.String(), recorder.Code
		}(index)
	}
	close(start)
	group.Wait()
	if runs.Load() != 1 {
		t.Fatalf("handler ran %d times", runs.Load())
	}
	for index := range answers {
		if codes[index] != http.StatusOK || answers[index] != `{"run":1}` {
			t.Fatalf("arrival %d: %d %q", index, codes[index], answers[index])
		}
	}
}

// The handler never starts when its row cannot be written.
// Satırı yazılamayan işleyici hiç başlamaz.
func TestRequestIdentityHandlerDoesNotRunWithoutItsRow(t *testing.T) {
	guard, db := newRequestIdentityTestGuard(t)
	if _, err := db.Exec(`DROP TABLE request_identities`); err != nil {
		t.Fatal(err)
	}
	var runs atomic.Int32
	handler := guard.wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { runs.Add(1) }))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, requestIdentityTestRequest(requestIdentityPlainPath, `{}`, requestIdentityTestID(80), requestIdentityTestAdmin()))
	if recorder.Code != http.StatusInternalServerError || runs.Load() != 0 {
		t.Fatalf("status=%d runs=%d", recorder.Code, runs.Load())
	}
}

func TestKeyedLocksSerialisePerKeyAndForgetIdleKeys(t *testing.T) {
	var locks keyedLocks
	releaseOne := locks.lock(1)
	// Another key is not held up.
	locks.lock(2)()
	acquired := make(chan struct{})
	go func() {
		release := locks.lock(1)
		close(acquired)
		release()
	}()
	select {
	case <-acquired:
		t.Fatal("the same key was locked twice at once")
	case <-time.After(20 * time.Millisecond):
	}
	releaseOne()
	<-acquired
	// Wait for the goroutine's release to settle before counting.
	locks.lock(1)()
	locks.mu.Lock()
	defer locks.mu.Unlock()
	if len(locks.entries) != 0 {
		t.Fatalf("%d idle keys are still held in memory", len(locks.entries))
	}
}
