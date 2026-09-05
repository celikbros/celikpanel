package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
)

// R-053. The operator's whole experience of this defect is the response body,
// so the response body is what is asserted: a refusal the engine explained
// answers 409 with an instruction and a code the screen can branch on, and a
// failure nobody can name still answers the way it always did.
func TestWriteDatabaseEngineError(t *testing.T) {
	tests := []struct {
		name        string
		server      *core.DatabaseServer
		err         error
		wantStatus  int
		wantCode    string
		wantSubstr  string
		wantNoLeak  string
		wantGeneric bool
	}{
		{
			name:       "MariaDB refusing the panel's empty root password",
			server:     &core.DatabaseServer{TypeName: "MariaDB"},
			err:        errors.New(`MariaDB connection failed: ERROR 1045 (28000): Access denied for user 'root'@'localhost' (using password: NO)`),
			wantStatus: http.StatusConflict,
			wantCode:   errCodeDatabaseEngineCredentialRefused,
			wantSubstr: "unix socket",
			wantNoLeak: "1045",
		},
		{
			name:       "PostgreSQL refusing the postgres role",
			server:     &core.DatabaseServer{TypeName: "PostgreSQL"},
			err:        errors.New(`pq: password authentication failed for user "postgres"`),
			wantStatus: http.StatusConflict,
			wantCode:   errCodeDatabaseEngineCredentialRefused,
			wantSubstr: "postgres role",
			wantNoLeak: "pq:",
		},
		{
			name:       "an engine that is not running",
			server:     &core.DatabaseServer{TypeName: "MariaDB"},
			err:        errors.New(`dial tcp 127.0.0.1:3306: connect: connection refused`),
			wantStatus: http.StatusConflict,
			wantCode:   errCodeDatabaseEngineUnreachable,
			wantSubstr: "Start the engine",
			wantNoLeak: "127.0.0.1",
		},
		{
			name:        "a failure nobody can name keeps the ordinary path",
			server:      &core.DatabaseServer{TypeName: "MariaDB"},
			err:         errors.New("invalid database name: identifier contains a quote"),
			wantStatus:  http.StatusInternalServerError,
			wantCode:    errCodeInternal,
			wantGeneric: true,
		},
		{
			name:        "an engine with no recognised type keeps the ordinary path",
			server:      nil,
			err:         errors.New("some other failure"),
			wantStatus:  http.StatusInternalServerError,
			wantCode:    errCodeInternal,
			wantGeneric: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeDatabaseEngineError(recorder, tc.server, tc.err)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.wantStatus)
			}
			var body apiErrorBody
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
			}
			if body.Code != tc.wantCode {
				t.Fatalf("code = %q, want %q", body.Code, tc.wantCode)
			}
			if tc.wantGeneric {
				if body.Error != "internal server error" {
					t.Fatalf("message = %q, want the generic one", body.Error)
				}
				return
			}
			if !strings.Contains(body.Error, tc.wantSubstr) {
				t.Fatalf("message %q does not contain %q", body.Error, tc.wantSubstr)
			}
			// The engine's own words are logged, never returned.
			if tc.wantNoLeak != "" && strings.Contains(body.Error, tc.wantNoLeak) {
				t.Fatalf("message %q leaked the engine's own text %q", body.Error, tc.wantNoLeak)
			}
		})
	}
}

// A second registration of the same address is a conflict with a remedy, not
// an unread constraint error.
func TestIsDuplicateDatabaseServerAddress(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{
			"sqlite unique index",
			errors.New("UNIQUE constraint failed: database_servers.subscription_id, database_servers.host, database_servers.port"),
			true,
		},
		{
			"postgres unique index",
			errors.New(`pq: duplicate key value violates unique constraint "database_servers_address_key"`),
			true,
		},
		{"anything else", errors.New("disk I/O error"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDuplicateDatabaseServerAddress(tc.err); got != tc.want {
				t.Fatalf("duplicate = %v, want %v", got, tc.want)
			}
		})
	}
}
