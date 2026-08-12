package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// panelFirewallMu makes every panel-side Status→desired ports→Apply sequence
// indivisible. Without it, a service sync that read "on" could race a manual
// "off" and unexpectedly recreate the table after the operator disabled it.
//
// panelFirewallMu, panel tarafındaki Durum→istenen portlar→Uygula dizisini
// bölünmez yapar. Onsuz "açık" okuyan servis eşitlemesi elle "kapat" ile
// yarışıp operatör kapattıktan sonra tabloyu sürprizce yeniden oluşturabilirdi.
var panelFirewallMu sync.Mutex

var errFirewallNoEngine = errors.New("firewall engine is not installed")

var errFirewallNestedMutation = errors.New("firewall apply requires a fresh direct mutation lease")

// Firewall, panel side. The panel decides policy — which ports should be open
// — and the agent enforces it in nftables. The desired set is derived, never
// hand-typed: the panel's own port plus the firewall ports of every installed
// service. SSH is added by the agent from live listeners, so it is never
// possible to lock the box. Whenever a service is installed or removed, the
// firewall re-syncs, so the open ports always track the running services.
//
// Güvenlik duvarı, panel tarafı. Politikayı panel belirler (hangi portlar
// açık olmalı), agent nftables'ta uygular. İstenen küme türetilir, elle
// yazılmaz: panelin kendi portu + kurulu her servisin güvenlik-duvarı
// portları. SSH'ı agent canlı dinleyicilerden ekler; kutuyu kilitlemek asla
// mümkün değil. Bir servis kurulunca/kaldırılınca güvenlik duvarı yeniden
// senkronlanır; açık portlar daima koşan servisleri izler.

// panelPort returns the TCP port the panel itself listens on, so the firewall
// never closes the door it is administered through.
// panelPort, panelin dinlediği TCP portunu döndürür; güvenlik duvarı,
// yönetildiği kapıyı asla kapatmaz.
func panelPort() int {
	addr := listenAddr() // ":2083" or "1.2.3.4:2083"
	if i := strings.LastIndexByte(addr, ':'); i >= 0 {
		if n, err := strconv.Atoi(addr[i+1:]); err == nil {
			return n
		}
	}
	return 2083
}

// desiredFirewallPorts computes the ports that must be open: the panel port
// plus every installed service's declared ports. Local-only services (empty
// FirewallPorts) contribute nothing — exactly the point.
// desiredFirewallPorts, açık olması gereken portları hesaplar: panel portu +
// kurulu her servisin bildirdiği portlar. Yalnız-yerel servisler katkı vermez.
func (p *Panel) desiredFirewallPorts(extraTCP ...int) (tcp []int, udp []int, err error) {
	tcp = append(tcp, panelPort())
	tcp = append(tcp, extraTCP...)

	// Let's Encrypt renewal answers HTTP-01 on :80. A panel carrying a real
	// CA certificate with the firewall blocking 80 renews nothing — the
	// certificate dies silently in 90 days. Caught live (Jul 17): issuance
	// worked only because the firewall was still off that day.
	// Let's Encrypt yenilemesi HTTP-01'i :80'de cevaplar. Gerçek CA
	// sertifikası taşıyan bir panelde firewall 80'i keserse hiçbir şey
	// yenilenmez — sertifika 90 günde sessizce ölür. Canlıda yakalandı
	// (17 Tem): sertifika alınabildi çünkü o gün firewall henüz kapalıydı.
	if cert := currentPanelCert(); cert.HTTPSEnabled && !cert.SelfSigned {
		tcp = append(tcp, 80)
	}

	var installed []string
	if err := p.callAgent("Agent.InstalledServiceIDsStrict", &transport.Empty{}, &installed); err != nil {
		return nil, nil, fmt.Errorf("discover installed services for firewall policy: %w", err)
	}
	installedSet := map[string]bool{}
	for _, id := range installed {
		installedSet[id] = true
	}
	for i := range core.ManagedServices {
		m := &core.ManagedServices[i]
		if !installedSet[m.ID] {
			continue
		}
		for _, fp := range m.FirewallPorts {
			if fp.Proto == "udp" {
				udp = append(udp, fp.Port)
			} else {
				tcp = append(tcp, fp.Port)
			}
		}
	}
	return tcp, udp, nil
}

// syncFirewall re-applies the firewall to match the installed service set,
// but only when the firewall is already enabled (never turns it on by
// surprise). Called after install/uninstall.
// syncFirewall, güvenlik duvarını kurulu servis kümesiyle eşleşecek şekilde
// yeniden uygular — yalnız zaten etkinken (asla sürpriz açmaz). Kurulum/
// kaldırma sonrası çağrılır.
func (p *Panel) syncFirewall(ctx context.Context) error {
	p.serviceMutationMu.Lock()
	defer p.serviceMutationMu.Unlock()
	return p.syncFirewallWithExtraTCPLocked(ctx)
}

// syncFirewallLocked is the composite/uninstall entry point. Its caller owns
// serviceMutationMu and this helper takes only the component lock, preserving
// the global -> firewall lock order.
func (p *Panel) syncFirewallLocked(ctx context.Context) error {
	return p.syncFirewallWithExtraTCPLocked(ctx)
}

// syncFirewallWithExtraTCP temporarily extends the derived live policy for
// an operation that must become reachable before its durable state exists.
// The panel certificate HTTP-01 preflight is the current caller: on a fresh
// self-signed installation, :80 must be open before certbot can prove the
// hostname. Disabled firewalls remain disabled.
func (p *Panel) syncFirewallWithExtraTCP(ctx context.Context, extraTCP ...int) error {
	p.serviceMutationMu.Lock()
	defer p.serviceMutationMu.Unlock()
	return p.syncFirewallWithExtraTCPLocked(ctx, extraTCP...)
}

func (p *Panel) syncFirewallWithExtraTCPLocked(ctx context.Context, extraTCP ...int) error {
	if _, err := panelMutationBinding(ctx); err == nil {
		return errFirewallNestedMutation
	}
	panelFirewallMu.Lock()
	defer panelFirewallMu.Unlock()

	// Freeze the complete caller-controlled policy before BeginServiceMutation.
	// The qualifier stored in the durable job therefore commits to exactly the
	// same detached slices later sent to ApplyFirewallV2.
	var st FirewallStatusResp
	if err := p.callAgentContext(ctx, "Agent.FirewallStatus", &transport.Empty{}, &st); err != nil {
		return fmt.Errorf("read firewall status: %w", err)
	}
	if st.Error != "" {
		return fmt.Errorf("read firewall status: %s", st.Error)
	}
	if !st.Enabled {
		return nil
	}
	tcp, udp, err := p.desiredFirewallPorts(extraTCP...)
	if err != nil {
		return err
	}
	commitment, err := mutationpayload.CanonicalFirewallApply(true, false, tcp, udp)
	if err != nil {
		return fmt.Errorf("canonicalize firewall policy: %w", err)
	}
	out, err := p.applyCanonicalFirewallV2(ctx, "firewall_sync", commitment)
	if err != nil {
		return fmt.Errorf("apply firewall policy: %w", err)
	}
	if out.Error != "" {
		return fmt.Errorf("apply firewall policy: %s", out.Error)
	}
	return nil
}

type applyFirewallReq = transport.ApplyFirewallRequest

// Must mirror the agent's FirewallStatusResponse field-for-field: net/rpc
// (gob) transfers by exported field NAME, so a field missing here is silently
// dropped in transit. EngineAvailable was added to the agent but not here —
// that is why the panel never saw "nftables not installed" and the UI kept
// offering "Turn on" against a missing engine.
// Agent'ın FirewallStatusResponse'unu alan alan yansıtmalı: net/rpc (gob)
// dışa açık alan ADIYLA taşır; burada eksik bir alan sessizce yolda düşer.
// EngineAvailable agent'a eklenmiş ama buraya eklenmemişti — panelin
// 'nftables kurulu değil'i asla görmemesinin ve motor yokken 'Turn on'
// sunmaya devam etmesinin nedeni buydu.
type FirewallStatusResp = transport.FirewallStatusResponse

func (p *Panel) readFirewallStatus() (FirewallStatusResp, error) {
	panelFirewallMu.Lock()
	defer panelFirewallMu.Unlock()
	var st FirewallStatusResp
	if err := p.callAgent("Agent.FirewallStatus", &transport.Empty{}, &st); err != nil {
		return st, err
	}
	return st, nil
}

// applyFirewallSetting is the explicit operator path. persist is true only for
// Save for reboot or explicit disable, never for live enable/background sync.
//
// applyFirewallSetting açık operatör yoludur. persist yalnız Save for reboot
// veya açık kapatmada doğrudur; canlı açma/arka plan eşitlemesinde asla değil.
func (p *Panel) applyFirewallSetting(enabled, persist bool) (FirewallStatusResp, error) {
	return p.applyFirewallSettingContext(context.Background(), enabled, persist)
}

type firewallAgentResponseError struct {
	message string
}

func (e *firewallAgentResponseError) Error() string {
	return e.message
}

func (p *Panel) applyFirewallSettingContext(
	ctx context.Context,
	enabled, persist bool,
) (FirewallStatusResp, error) {
	// Non-HTTP callers acquire the process-wide mutation lock here. Production
	// HTTP admission uses beginServiceMutation and calls the locked helper below
	// so the invariant is always serviceMutationMu -> panelFirewallMu.
	p.serviceMutationMu.Lock()
	defer p.serviceMutationMu.Unlock()
	return p.applyFirewallSettingContextLocked(ctx, enabled, persist)
}

func (p *Panel) applyFirewallSettingContextLocked(
	ctx context.Context,
	enabled, persist bool,
) (FirewallStatusResp, error) {
	if _, err := panelMutationBinding(ctx); err == nil {
		return FirewallStatusResp{}, errFirewallNestedMutation
	}
	panelFirewallMu.Lock()
	defer panelFirewallMu.Unlock()

	var tcp, udp []int
	if enabled {
		var cur FirewallStatusResp
		if err := p.callAgentContext(ctx, "Agent.FirewallStatus", &transport.Empty{}, &cur); err != nil {
			return FirewallStatusResp{}, err
		}
		if cur.Error != "" {
			return FirewallStatusResp{Error: cur.Error}, nil
		}
		if !cur.EngineAvailable {
			return FirewallStatusResp{}, errFirewallNoEngine
		}
		var err error
		tcp, udp, err = p.desiredFirewallPorts()
		if err != nil {
			return FirewallStatusResp{}, err
		}
	}
	commitment, err := mutationpayload.CanonicalFirewallApply(enabled, persist, tcp, udp)
	if err != nil {
		return FirewallStatusResp{}, fmt.Errorf("canonicalize firewall policy: %w", err)
	}
	st, err := p.applyCanonicalFirewallV2(ctx, "firewall_apply", commitment)
	var responseErr *firewallAgentResponseError
	if errors.As(err, &responseErr) {
		return st, nil
	}
	return st, err
}

// applyCanonicalFirewallV2 opens a fresh direct lease for an already frozen
// firewall payload. Callers must hold panelFirewallMu from discovery through
// this function so another local firewall decision cannot overtake the
// commitment. A bound context is deliberately rejected: direct firewall jobs
// never borrow a package/profile/certificate mutation identity.
func (p *Panel) applyCanonicalFirewallV2(
	ctx context.Context,
	kind string,
	commitment mutationpayload.FirewallApplyCommitment,
) (FirewallStatusResp, error) {
	return p.applyCanonicalFirewallV2Identity(ctx, kind, commitment, "", "")
}

func (p *Panel) applyCanonicalFirewallV2Identity(
	ctx context.Context,
	kind string,
	commitment mutationpayload.FirewallApplyCommitment,
	requestID, ownerID string,
) (FirewallStatusResp, error) {
	if _, err := panelMutationBinding(ctx); err == nil {
		return FirewallStatusResp{}, errFirewallNestedMutation
	}
	if kind != "firewall_apply" && kind != "firewall_sync" {
		return FirewallStatusResp{}, errors.New("invalid direct firewall mutation kind")
	}
	canonical, err := mutationpayload.CanonicalFirewallApply(
		commitment.Enabled,
		commitment.Persist,
		commitment.TCPPorts,
		commitment.UDPPorts,
	)
	if err != nil {
		return FirewallStatusResp{}, fmt.Errorf("validate canonical firewall payload: %w", err)
	}
	if !mutationpayload.ValidFirewallApplyQualifier(commitment.Qualifier) ||
		canonical.Qualifier != commitment.Qualifier {
		return FirewallStatusResp{}, errors.New("invalid canonical firewall qualifier")
	}
	commitment = canonical

	var response FirewallStatusResp
	rpcResponseObserved := false
	call := func(callCtx context.Context, binding agentMutationBinding) error {
		request := applyFirewallReq{
			ServiceMutationBinding: binding,
			Enabled:                commitment.Enabled,
			Persist:                commitment.Persist,
			TCPPorts:               append([]int(nil), commitment.TCPPorts...),
			UDPPorts:               append([]int(nil), commitment.UDPPorts...),
		}
		if err := p.callAgentContext(
			callCtx,
			"Agent.ApplyFirewallV2",
			&request,
			&response,
		); err != nil {
			return err
		}
		rpcResponseObserved = true
		if response.Error != "" {
			return &firewallAgentResponseError{message: response.Error}
		}
		return nil
	}
	err = nil
	if requestID == "" && ownerID == "" {
		err = p.withStandaloneAgentMutation(
			ctx, kind, "nftables", commitment.Qualifier, call,
		)
	} else {
		err = p.withStandaloneAgentMutationIdentity(
			ctx,
			serviceOperation{
				RequestID:   requestID,
				Kind:        kind,
				ServiceID:   "nftables",
				PackageName: commitment.Qualifier,
			},
			ownerID,
			call,
		)
	}
	if err == nil && !rpcResponseObserved {
		// The agent may have durably committed success before the RPC response
		// was lost. withStandaloneAgentMutationIdentity proved the exact
		// terminal receipt; refresh only the display/status fields here.
		if statusErr := p.callAgentContext(
			ctx, "Agent.FirewallStatus", &transport.Empty{}, &response,
		); statusErr != nil {
			return response, fmt.Errorf("read committed firewall status: %w", statusErr)
		}
		if response.Error != "" {
			return response, fmt.Errorf("read committed firewall status: %s", response.Error)
		}
		if response.Enabled != commitment.Enabled {
			return response, errors.New("committed firewall status does not match the exact payload")
		}
	}
	return response, err
}

// syncFirewallForServiceOperation derives and freezes the desired live policy,
// persists the exact child identity in the active panel row, and only then
// begins the direct firewall_sync job. A persisted phase without an active
// child is intentional recoverable desired work after a process crash.
func (p *Panel) syncFirewallForServiceOperation(
	ctx context.Context,
	op serviceOperation,
) error {
	if !serviceOperationNeedsFirewallSync(op) {
		return errors.New("service operation does not require firewall synchronization")
	}
	if _, err := panelMutationBinding(ctx); err == nil {
		return errFirewallNestedMutation
	}
	panelFirewallMu.Lock()
	defer panelFirewallMu.Unlock()

	var status FirewallStatusResp
	if err := p.callAgentContext(ctx, "Agent.FirewallStatus", &transport.Empty{}, &status); err != nil {
		return fmt.Errorf("read firewall status: %w", err)
	}
	if status.Error != "" {
		return fmt.Errorf("read firewall status: %s", status.Error)
	}
	if !status.Enabled {
		return p.updateServiceOperationPhase(ctx, op.ID, serviceOperationFirewallPhase(op))
	}
	tcp, udp, err := p.desiredFirewallPorts()
	if err != nil {
		return err
	}
	commitment, err := mutationpayload.CanonicalFirewallApply(true, false, tcp, udp)
	if err != nil {
		return fmt.Errorf("canonicalize firewall policy: %w", err)
	}
	requestID, err := newServiceOperationID()
	if err != nil {
		return fmt.Errorf("create firewall child request identity: %w", err)
	}
	ownerID, err := newServiceOperationID()
	if err != nil {
		return fmt.Errorf("create firewall child owner identity: %w", err)
	}
	phase, err := encodeFirewallChildPhase(firewallChildIdentity{
		RequestID: requestID,
		OwnerID:   ownerID,
		Qualifier: commitment.Qualifier,
	})
	if err != nil {
		return err
	}
	if err := p.updateServiceOperationPhase(ctx, op.ID, phase); err != nil {
		return err
	}
	response, err := p.applyCanonicalFirewallV2Identity(
		ctx, "firewall_sync", commitment, requestID, ownerID,
	)
	if err != nil {
		return err
	}
	if response.Error != "" {
		return &firewallAgentResponseError{message: response.Error}
	}
	return nil
}

// handleFirewall: GET status; POST {enabled} turns on/off and the explicit
// {action:"save_for_reboot"} persists an already-live policy (admin-only).
// handleFirewall: GET durum; POST {enabled} açar/kapatır, açık
// {action:"save_for_reboot"} ise canlı politikayı kalıcılaştırır (yalnız admin).
func (p *Panel) handleFirewall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if c := currentCaller(r); c == nil || c.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "admin only")
		return
	}
	switch r.Method {
	case http.MethodGet:
		st, err := p.readFirewallStatus()
		if err != nil {
			writeAgentError(w, err, "firewall")
			return
		}
		json.NewEncoder(w).Encode(st)

	case http.MethodPost:
		var req struct {
			Action  string `json:"action"`
			Enabled *bool  `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		saveForReboot := req.Action == "save_for_reboot"
		if req.Action != "" && !saveForReboot {
			writeClientError(w, http.StatusBadRequest, "invalid firewall action")
			return
		}
		if !saveForReboot && req.Enabled == nil {
			writeClientError(w, http.StatusBadRequest, "enabled is required")
			return
		}
		releaseMutation, busy := p.beginServiceMutation(w, r)
		if busy {
			return
		}
		defer releaseMutation()
		enabled := saveForReboot || *req.Enabled
		if saveForReboot {
			current, statusErr := p.readFirewallStatus()
			if statusErr != nil {
				writeAgentError(w, statusErr, "firewall")
				return
			}
			if !current.Enabled {
				writeClientError(w, http.StatusConflict, "firewall must be enabled before it can be saved for reboot")
				return
			}
		}
		persist := saveForReboot || !enabled
		st, err := p.applyFirewallSettingContextLocked(r.Context(), enabled, persist)
		if errors.Is(err, errFirewallNoEngine) {
			writeCodedError(w, http.StatusConflict, errCodeFirewallNoEngine,
				"the firewall engine (nftables) is not installed — install it from Services first", "/services")
			return
		}
		if err != nil {
			if saveForReboot {
				p.audit(r, "firewall.persistence.enable.failed — "+auditReason(err.Error()), "", 0)
			}
			writeAgentError(w, err, "firewall")
			return
		}
		if st.Error != "" {
			if saveForReboot {
				p.audit(r, "firewall.persistence.enable.failed — "+auditReason(st.Error), "", 0)
			}
			writeClientError(w, http.StatusConflict, st.Error)
			return
		}
		action := "firewall.off"
		if saveForReboot {
			action = "firewall.persistence.enable"
		} else if enabled {
			action = "firewall.on"
		}
		p.audit(r, action, "", 0)
		json.NewEncoder(w).Encode(st)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
