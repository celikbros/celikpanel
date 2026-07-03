package main

import (
	"log"
	"net/http"
)

// HTTP handlers must never hand internal error text to the client: those
// strings carry SQL fragments, filesystem paths and agent internals that
// help an attacker and help no one else. The full error goes to the log;
// the client gets a status-appropriate generic message.
//
// HTTP işleyicileri iç hata metnini asla istemciye vermemelidir: bu
// metinler SQL parçaları, dosya sistemi yolları ve agent iç detayları
// taşır; bunlar yalnızca saldırganın işine yarar. Tam hata log'a gider;
// istemci, duruma uygun genel bir mesaj alır.

// writeServerError logs the internal error and returns a generic 500.
// writeServerError iç hatayı log'lar ve genel bir 500 döndürür.
func writeServerError(w http.ResponseWriter, err error) {
	if err != nil {
		log.Printf("[500] %v", err)
	}
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

// writeClientError returns a 4xx with a safe, explicit message. The
// message must be developer-authored, never derived from an internal
// error value.
// writeClientError güvenli, açık bir mesajla bir 4xx döndürür. Mesaj
// geliştirici tarafından yazılmalı, asla bir iç hata değerinden
// türetilmemelidir.
func writeClientError(w http.ResponseWriter, status int, message string) {
	http.Error(w, message, status)
}

// writeAgentError handles a failed agent RPC. The agent's error string may
// carry command output and paths, so it is logged, never returned; the
// transport error (if any) is logged too, and the client gets a generic
// 500.
// writeAgentError, başarısız bir agent RPC'sini ele alır. Agent'ın hata
// metni komut çıktısı ve yollar taşıyabilir; bu yüzden log'lanır, asla
// döndürülmez; taşıma hatası (varsa) da log'lanır ve istemci genel bir
// 500 alır.
func writeAgentError(w http.ResponseWriter, err error, agentDetail string) {
	if agentDetail != "" {
		log.Printf("[500][agent] %s", agentDetail)
	}
	writeServerError(w, err)
}
