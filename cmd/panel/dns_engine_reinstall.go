package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// A reinstall writes no engine-switch snapshot, and that is on purpose rather
// than for want of one. A switch snapshot exists to move authority: the schema
// requires it to name a source engine, a different target, and the next epoch,
// and the singleton's triggers refuse to attach anything else. A reinstall
// moves no authority — same engine, same epoch, same topology, same zones — so
// there is nothing for that row to record and no transition for it to guard.
// What the operation does change lives entirely on the host, and the host keeps
// its own durable record of it: the agent's mutation ledger, the switch
// journal, and the ownership receipts. The panel takes the same durable host
// lease every other privileged mutation takes, so a panel that dies mid-repair
// is recovered by the same startup path, against durable authority that never
// moved.
//
// Yeniden kurulum motor geçiş anlık görüntüsü yazmaz; bu, bir tane
// bulunamadığından değil, bilerek böyledir. Geçiş anlık görüntüsü yetkiyi
// taşımak için vardır: şema onun bir kaynak motoru, farklı bir hedefi ve bir
// sonraki çağı adlandırmasını ister; tekil durumun tetikleyicileri de başka bir
// şeyin bağlanmasını reddeder. Yeniden kurulum yetkiyi taşımaz — aynı motor,
// aynı çağ, aynı topoloji, aynı bölgeler — dolayısıyla o satırın kaydedeceği
// bir şey ve koruyacağı bir geçiş yoktur. İşlemin değiştirdiği şey tümüyle
// sunucuda yaşar ve sunucu bunun kalıcı kaydını kendi tutar: agent'ın mutasyon
// defteri, geçiş günlüğü ve sahiplik makbuzları. Panel, diğer her ayrıcalıklı
// mutasyonun aldığı kalıcı sunucu kirasının aynısını alır; onarımın ortasında
// ölen bir panel de aynı açılış yoluyla, hiç yer değiştirmemiş kalıcı yetkiye
// karşı kurtarılır.
func (p *Panel) commitDNSEngineReinstall(
	w http.ResponseWriter,
	actor dnsEngineAuditActor,
	request dnsEngineSwitchRequest,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) {
	if !mutationpayload.ReinstallsActiveDNSEngine(manifest) ||
		manifest.TargetEngine != request.TargetEngine ||
		manifest.SourceEngine != request.ExpectedSource.engine() {
		writeClientError(w, http.StatusConflict,
			"DNS engine state changed after preview; review the change again")
		return
	}
	ownerID, err := newServiceOperationID()
	if err != nil {
		writeServerError(w, err)
		return
	}
	p.auditDNSEngineReinstallBounded(actor, "accepted", request.RequestID, manifest)
	workerCtx, cancel := context.WithTimeout(
		context.Background(), dnsEngineSwitchTimeout,
	)
	defer cancel()
	if err := p.executeDNSEngineReinstall(
		workerCtx, request.RequestID, ownerID, manifest,
	); err != nil {
		p.auditDNSEngineReinstallBounded(
			actor, "failed", request.RequestID, manifest,
		)
		logDNSEngineAgentRejection(request.RequestID, err)
		log.Printf(
			"DNS engine reinstall %s did not finalize: %v",
			request.RequestID, err,
		)
		var held *agentMutationHeldError
		var followup *dnsEngineMutationAppliedFollowupError
		switch {
		case errors.As(err, &held):
			writeDNSEngineMutationsHeld(w, held.Hold)
		case errors.As(err, &followup):
			writeDNSEngineChangeAppliedRefreshRequired(w)
		case !mutationTerminalUncertain(err):
			// Nothing was taken away: the durable authority named this engine
			// at this epoch before the attempt and still does, because a
			// reinstall never writes it. Say the change was not committed
			// rather than leaving the operator to wonder what the panel now
			// believes.
			//
			// Elinden alınan bir şey yok: kalıcı yetki, denemeden önce bu
			// motoru bu çağda adlandırıyordu ve hâlâ adlandırıyor; çünkü
			// yeniden kurulum onu hiç yazmaz. Operatörü panelin artık neye
			// inandığını merak eder hâlde bırakmak yerine, değişikliğin
			// işlenmediğini söyle.
			p.auditDNSEngineReinstallBounded(
				actor, "change_not_committed", request.RequestID, manifest,
			)
			writeDNSEngineChangeNotCommitted(w, err)
		default:
			p.auditDNSEngineReinstallBounded(
				actor, "uncertain", request.RequestID, manifest,
			)
			writeDNSEngineStateUnverified(w)
		}
		return
	}
	p.auditDNSEngineReinstallBounded(
		actor, "succeeded", request.RequestID, manifest,
	)
	firewallErr := p.syncFirewallLocked(workerCtx)
	_, scanErr := p.scanManagedServices(workerCtx)
	if firewallErr != nil || scanErr != nil {
		log.Printf(
			"DNS engine reinstall %s committed with pending follow-up: firewall=%v scan=%v",
			request.RequestID, firewallErr, scanErr,
		)
		p.auditDNSEngineReinstallBounded(
			actor, "post_commit.pending", request.RequestID, manifest,
		)
		writeDNSEnginePostCommitFailed(w, dnsEnginePostCommitResult{
			FirewallErr: firewallErr, ScanErr: scanErr,
		})
		return
	}
	p.auditDNSEngineReinstallBounded(
		actor, "post_commit.completed", request.RequestID, manifest,
	)
	finalSnapshot, err := p.dnsEngineSnapshot(workerCtx)
	if err != nil {
		log.Printf("DNS engine reinstall completed but final state response failed: %v", err)
		writeDNSEngineChangeAppliedRefreshRequired(w)
		return
	}
	_ = json.NewEncoder(w).Encode(finalSnapshot)
}

func (p *Panel) executeDNSEngineReinstall(
	ctx context.Context,
	requestID, ownerID string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	agentRequest := dnsEngineSwitchRequestForManifest(manifest)
	var response transport.SwitchDNSEngineV1Response
	op := serviceOperation{
		RequestID: requestID, Kind: dnsEngineSwitchKind,
		ServiceID:   string(manifest.TargetEngine),
		PackageName: manifest.Qualifier,
	}
	err := p.withStandaloneAgentMutationIdentity(
		ctx, op, ownerID,
		func(callCtx context.Context, binding agentMutationBinding) error {
			agentRequest.ServiceMutationBinding = binding
			if err := p.callAgentContext(
				callCtx, "Agent.SwitchDNSEngineV1", &agentRequest, &response,
			); err != nil {
				return err
			}
			if response.Error != "" {
				return newDNSEngineAgentRejectedError(response.Error)
			}
			if !response.Applied ||
				response.ActiveEngine != manifest.TargetEngine ||
				response.ActiveEpoch != manifest.TargetEpoch ||
				response.AppliedZones != len(manifest.Zones) {
				return errors.New("agent did not confirm the exact DNS engine reinstall")
			}
			return nil
		},
	)
	if err != nil {
		return err
	}
	if err := p.verifyDNSEngineRuntimeTarget(ctx, manifest.TargetEngine); err != nil {
		return &dnsEngineMutationAppliedFollowupError{err: err}
	}
	if err := p.finalizeDNSEngineReinstallSuccess(
		ctx, requestID, ownerID, manifest,
	); err != nil {
		return &dnsEngineMutationAppliedFollowupError{err: err}
	}
	return nil
}

// finalizeDNSEngineReinstallSuccess records what the reinstall actually
// published. The engine and epoch are unchanged, so every zone qualifier is the
// one already stored against this engine: for a host whose zones were all
// applied the rows are rewritten identically. The work that matters is the zone
// the old host never managed to apply — after the reinstall it is served, and
// the ledger must stop calling it pending.
//
// finalizeDNSEngineReinstallSuccess, yeniden kurulumun gerçekten ne
// yayımladığını kaydeder. Motor ve çağ değişmediğinden her bölge niteleyicisi bu
// motora karşı zaten saklanan niteleyicidir: bölgelerinin tümü uygulanmış bir
// sunucuda satırlar birebir aynı biçimde yeniden yazılır. Asıl önemli olan iş,
// eski sunucunun uygulamayı başaramadığı bölgedir — yeniden kurulumdan sonra o
// bölge sunulur ve defter ona artık "bekliyor" dememelidir.
func (p *Panel) finalizeDNSEngineReinstallSuccess(
	ctx context.Context,
	requestID, ownerID string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state, err := readDNSEngineDBState(ctx, tx)
	if err != nil {
		return err
	}
	if state.CurrentSwitchID != "" ||
		state.ActiveEngine != manifest.TargetEngine ||
		state.EngineEpoch != manifest.TargetEpoch ||
		state.Revision != manifest.SourceRevision ||
		state.Topology != manifest.Topology {
		return errors.New(
			"DNS engine authority changed during the reinstall",
		)
	}
	for _, zone := range manifest.Zones {
		action := "sync"
		if zone.Delete {
			action = "delete"
		}
		application, err := tx.ExecContext(ctx, `
			INSERT INTO dns_zone_engine_applications (
			  zone_name, engine, engine_epoch, applied_generation,
			  applied_action, applied_zone_type, qualifier,
			  mutation_request_id, mutation_owner_id, switch_id, revision
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, 1)
			ON CONFLICT(zone_name, engine) DO UPDATE SET
			  engine_epoch = excluded.engine_epoch,
			  applied_generation = excluded.applied_generation,
			  applied_action = excluded.applied_action,
			  applied_zone_type = excluded.applied_zone_type,
			  qualifier = excluded.qualifier,
			  mutation_request_id = excluded.mutation_request_id,
			  mutation_owner_id = excluded.mutation_owner_id,
			  switch_id = excluded.switch_id,
			  revision = dns_zone_engine_applications.revision + 1,
			  applied_at = datetime('now'), updated_at = datetime('now')`,
			zone.Domain, manifest.TargetEngine, manifest.TargetEpoch,
			zone.DesiredGeneration, action, zone.ZoneType, zone.ZoneQualifier,
			requestID, ownerID,
		)
		if err != nil {
			return err
		}
		if err := requireExactRows(
			application, 1,
			"DNS engine reinstall zone application was not exact",
		); err != nil {
			return err
		}
		applied, err := tx.ExecContext(ctx, `
			UPDATE dns_zone_sync_state
			SET applied_generation = desired_generation, status = 'applied',
			    last_error = NULL, updated_at = datetime('now')
			WHERE zone_name = ? AND desired_generation = ?
			  AND desired_action = ? AND desired_zone_type = ?`,
			zone.Domain, zone.DesiredGeneration, action, zone.ZoneType,
		)
		if err != nil {
			return err
		}
		if err := requireExactRows(
			applied, 1,
			"frozen DNS zone state changed before reinstall finalization",
		); err != nil {
			return err
		}
		if zone.Delete {
			// A deletion this engine already applied has no marker left to
			// retire; republishing the same tombstone must not invent one to
			// delete. Zero or one, never two.
			//
			// Bu motorun zaten uyguladığı bir silmenin emekliye ayrılacak
			// işareti kalmamıştır; aynı mezar taşını yeniden yayımlamak
			// silinecek bir işaret uydurmamalıdır. Sıfır ya da bir, asla iki.
			retired, err := tx.ExecContext(ctx, `
				DELETE FROM dns_zone_deletion_markers WHERE zone_name = ?`,
				zone.Domain,
			)
			if err != nil {
				return fmt.Errorf(
					"retire reinstalled DNS engine deletion marker: %w", err,
				)
			}
			changed, err := retired.RowsAffected()
			if err != nil {
				return err
			}
			if changed > 1 {
				return errors.New(
					"reinstalled DNS engine deletion marker was not unique",
				)
			}
		}
	}
	return tx.Commit()
}

func (p *Panel) auditDNSEngineReinstallBounded(
	actor dnsEngineAuditActor,
	outcome, requestID string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) {
	ctx, cancel := context.WithTimeout(
		context.Background(), dnsEnginePostCommitAuditTimeout,
	)
	defer cancel()
	p.auditDNSEngineAction(ctx, actor, fmt.Sprintf(
		"dns.engine.reinstall.%s request=%s engine=%s epoch=%d action=%s mode=%s",
		outcome, requestID, manifest.TargetEngine, manifest.TargetEpoch,
		dnsEngineActionReinstall, manifest.Mode,
	))
}
