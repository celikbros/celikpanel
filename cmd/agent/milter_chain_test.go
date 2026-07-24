package main

import "testing"

// The milter chain decides whether outgoing mail is signed and incoming mail is
// filtered. Two live failures are pinned here so neither can come back:
//
//  1. DKIM used to write `smtpd_milters = <opendkim>` outright. A spam filter
//     wired afterwards replaced it, or was replaced by it — last writer wins,
//     no error, mail quietly unsigned or unfiltered.
//  2. Rspamd was installed and "Running" on Boston while `smtpd_milters` was
//     EMPTY (24 Jul): the daemon was up and not one message went through it.
//
// Milter zinciri, çıkan postanın imzalanıp gelen postanın süzülüp
// süzülmeyeceğine karar verir. İkisi de geri dönmesin diye iki canlı arıza
// buraya sabitlenmiştir:
//
//  1. DKIM doğrudan `smtpd_milters = <opendkim>` yazıyordu. Sonradan bağlanan
//     bir spam filtresi onu siliyor ya da onun tarafından siliniyordu — son
//     yazan kazanır, hata yok, posta sessizce imzasız ya da süzgeçsiz.
//  2. Boston'da Rspamd kurulu ve "Çalışıyor"ken `smtpd_milters` BOŞtu
//     (24 Tem): daemon ayaktaydı ve içinden tek bir ileti geçmiyordu.
func TestComposeMilterChain(t *testing.T) {
	cases := []struct {
		name         string
		dkim         bool
		spam         string
		wantIncoming string
		wantOutgoing string
	}{
		{
			// The regression: both present must COMPOSE, DKIM first.
			// Asıl gerileme: ikisi de varsa BİRLEŞMELİ, önce DKIM.
			name:         "dkim ve spam birlikte",
			dkim:         true,
			spam:         rspamdMilter,
			wantIncoming: opendkimMilter + ", " + rspamdMilter,
			wantOutgoing: opendkimMilter,
		},
		{
			// A spam filter alone must still reach incoming mail — installed
			// and unwired was the exact bug.
			// Tek başına spam filtresi de gelen postaya ulaşmalı — kurulu ama
			// bağlanmamış olması tam da o hataydı.
			name:         "yalniz spam filtresi",
			spam:         rspamdMilter,
			wantIncoming: rspamdMilter,
			wantOutgoing: "",
		},
		{
			name:         "yalniz dkim",
			dkim:         true,
			wantIncoming: opendkimMilter,
			wantOutgoing: opendkimMilter,
		},
		{
			// Nothing installed: both lists empty, never a stale endpoint.
			// Hiçbiri kurulu değil: iki liste de boş, asla bayat bir uç değil.
			name: "hicbiri yok",
		},
		{
			// spamassMilterEndpoint returns "" when it cannot determine the
			// socket. A missing line is honest; a guessed one is a mail server
			// talking to nothing.
			// Soketi saptayamayınca spamassMilterEndpoint "" döner. Eksik satır
			// dürüsttür; tahmin edilmişi, hiçliğe konuşan bir posta sunucusudur.
			name:         "spam ucu bilinmiyor",
			dkim:         true,
			spam:         "",
			wantIncoming: opendkimMilter,
			wantOutgoing: opendkimMilter,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in, out := composeMilterChain(c.dkim, c.spam)
			if in != c.wantIncoming {
				t.Errorf("smtpd_milters = %q, want %q", in, c.wantIncoming)
			}
			if out != c.wantOutgoing {
				t.Errorf("non_smtpd_milters = %q, want %q", out, c.wantOutgoing)
			}
		})
	}
}

// Spam filtering must never touch mail WE send: it costs CPU on every message
// the panel's own sites generate, and a false positive on our own notification
// mail is a bounce nobody can explain.
// Spam süzme, BİZİM gönderdiğimiz postaya asla dokunmamalı: panelin kendi
// sitelerinin ürettiği her iletide CPU harcar ve kendi bildirim postamızda bir
// yanlış pozitif, kimsenin açıklayamayacağı bir geri dönüştür.
func TestSpamFilterNeverScansOutgoing(t *testing.T) {
	_, outgoing := composeMilterChain(true, rspamdMilter)
	if outgoing != opendkimMilter {
		t.Errorf("non_smtpd_milters = %q — only DKIM belongs on outgoing mail", outgoing)
	}
}
