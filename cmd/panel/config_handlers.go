package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/alicelik/celikpanel/internal/transport"
)

const maxServiceConfigRequestBody = 64 << 10

func decodeServiceConfigJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxServiceConfigRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func (p *Panel) handleGetPHPConfig(w http.ResponseWriter, r *http.Request) {
	phpVersion := r.URL.Query().Get("version")
	if phpVersion == "" {
		writeClientError(w, http.StatusBadRequest, "version parameter required")
		return
	}
	req := transport.GetPHPConfigRequest{PHPVersion: phpVersion}
	var resp transport.GetPHPConfigResponse
	if err := p.callAgent("Agent.GetPHPConfig", req, &resp); err != nil {
		writeServerError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		return
	}
}

func (p *Panel) handleUpdatePHPConfig(w http.ResponseWriter, r *http.Request) {
	var req transport.UpdatePHPConfigRequest
	if err := decodeServiceConfigJSON(w, r, &req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request")
		return
	}
	var resp transport.Empty
	if err := p.callAgent("Agent.UpdatePHPConfig", req, &resp); err != nil {
		writeServerError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (p *Panel) handleGetMySQLConfig(w http.ResponseWriter, r *http.Request) {
	var resp transport.GetMySQLConfigResponse
	if err := p.callAgent("Agent.GetMySQLConfig", transport.Empty{}, &resp); err != nil {
		writeServerError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		return
	}
}

func (p *Panel) handleUpdateMySQLConfig(w http.ResponseWriter, r *http.Request) {
	var req transport.UpdateMySQLConfigRequest
	if err := decodeServiceConfigJSON(w, r, &req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request")
		return
	}
	var resp transport.Empty
	if err := p.callAgent("Agent.UpdateMySQLConfig", req, &resp); err != nil {
		writeServerError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
