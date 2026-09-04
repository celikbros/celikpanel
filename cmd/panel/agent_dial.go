package main

import (
	"context"
	"errors"
	"log"
	"net/rpc"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The panel cannot serve without the agent, so it must eventually give up and
// let systemd restart it. What it must not do is give up on the FIRST attempt.
//
// systemd's After= orders execution, not readiness: the agent unit is started
// before the panel, but its socket appears only after it has loaded its ledger
// and reconciled interrupted work. An agent restarted by an update, or one
// taking a few seconds to finish startup recovery, is a normal transient — and
// exiting on it produced a second flapping unit beside the first, each restart
// re-running the whole startup sequence for a socket that was seconds away.
//
// A bounded, backing-off wait turns that into a patient start. If the agent is
// genuinely absent the panel still exits, with a message that says how long it
// waited — but it no longer exits because it was thirty milliseconds early.
//
// Panel, agent olmadan hizmet veremez; dolayısıyla sonunda vazgeçip systemd'nin
// kendisini yeniden başlatmasına izin vermelidir. Yapmaması gereken şey İLK
// denemede vazgeçmektir.
//
// systemd'nin After= ayarı çalıştırmayı sıralar, hazır olmayı değil: agent
// unit'i panelden önce başlatılır ama soketi ancak defterini yükleyip yarım
// kalmış işleri uzlaştırdıktan sonra belirir. Bir güncellemenin yeniden
// başlattığı ya da açılış kurtarmasını bitirmesi birkaç saniye süren bir agent
// normal ve geçicidir — ve bunun üzerine çıkmak, birincinin yanına çırpınan
// ikinci bir birim koyuyordu; her yeniden başlatma, saniyeler uzaktaki bir
// soket için bütün açılış dizisini yeniden koşturuyordu.
//
// Sınırlı ve artan beklemeli bir deneme bunu sabırlı bir başlangıca çevirir.
// Agent gerçekten yoksa panel yine çıkar — ama ne kadar beklediğini söyleyen bir
// mesajla, ve artık otuz milisaniye erken davrandığı için çıkmaz.
const (
	agentDialTotalWait   = 90 * time.Second
	agentDialFirstPause  = 500 * time.Millisecond
	agentDialMaxPause    = 8 * time.Second
	agentDialAttemptWait = 10 * time.Second
)

// connectAgentPatiently dials the agent socket until it answers or the total
// wait elapses, whichever comes first. It reports the last error and how long
// it waited so the journal line is diagnostic rather than a bare refusal.
// connectAgentPatiently, agent soketini yanıt verene ya da toplam bekleme
// dolana kadar dener. Son hatayı ve ne kadar beklediğini bildirir; böylece
// günlük satırı çıplak bir ret değil, teşhis olur.
func connectAgentPatiently(
	ctx context.Context,
	dial func(context.Context) (*rpc.Client, error),
	now func() time.Time,
	sleep func(time.Duration),
) (*rpc.Client, time.Duration, error) {
	if dial == nil {
		return nil, 0, errors.New("agent dial function is required")
	}
	if now == nil {
		now = time.Now
	}
	if sleep == nil {
		sleep = time.Sleep
	}

	started := now()
	pause := agentDialFirstPause
	var lastErr error
	announced := false

	for {
		attemptCtx, cancel := context.WithTimeout(ctx, agentDialAttemptWait)
		client, err := dial(attemptCtx)
		cancel()
		if err == nil {
			return client, now().Sub(started), nil
		}
		lastErr = err

		waited := now().Sub(started)
		if waited+pause >= agentDialTotalWait || ctx.Err() != nil {
			return nil, waited, lastErr
		}
		// One line, not one per attempt: a retry loop that narrates itself is
		// noise, but a silent 90-second pause looks like a hang.
		// Deneme başına değil, tek satır: kendini anlatan bir döngü gürültüdür
		// ama sessiz 90 saniyelik bir duraklama takılma gibi görünür.
		if !announced {
			log.Printf(
				"Agent socket is not answering yet; waiting up to %s for it: %v",
				agentDialTotalWait, err,
			)
			announced = true
		}
		sleep(pause)
		if pause < agentDialMaxPause {
			pause *= 2
			if pause > agentDialMaxPause {
				pause = agentDialMaxPause
			}
		}
	}
}

// dialAgentOnce adapts the production connector to the injectable signature.
// dialAgentOnce, üretim bağlayıcısını enjekte edilebilir imzaya uyarlar.
func dialAgentOnce(ctx context.Context) (*rpc.Client, error) {
	return transport.ConnectAgentContext(ctx)
}
