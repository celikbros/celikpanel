package main

import (
	"sync"
	"time"
)

// durableMutationRPCFixture gives focused handler fakes the same begin,
// heartbeat and finish contract required by production privileged RPCs.
// durableMutationRPCFixture, odaklı handler fake'lerine üretimdeki ayrıcalıklı
// RPC'lerin zorunlu tuttuğu begin, heartbeat ve finish sözleşmesini verir.
type durableMutationRPCFixture struct {
	mu     sync.Mutex
	active string
	jobs   map[string]*ServiceOperationMutationJob
}

func (f *durableMutationRPCFixture) BeginServiceMutation(
	req *ServiceOperationMutationBeginRequest,
	resp *ServiceOperationMutationResponse,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.jobs == nil {
		f.jobs = make(map[string]*ServiceOperationMutationJob)
	}
	if active := f.jobs[f.active]; active != nil && agentMutationActive(active.Status) {
		resp.Error = "another service mutation owns the host lease"
		resp.Job = cloneServiceOperationMutationJob(active)
		return nil
	}
	now := time.Now().UTC()
	job := &ServiceOperationMutationJob{
		RequestID: req.RequestID, OwnerID: req.OwnerID, Kind: req.Kind,
		Target: req.Target, PackageName: req.PackageName,
		Status: agentMutationRunning, Phase: "starting", Attempt: 1,
		LeaseExpiresAt: now.Add(time.Minute), DeadlineAt: now.Add(time.Hour),
	}
	f.jobs[req.RequestID] = job
	f.active = req.RequestID
	resp.Job = cloneServiceOperationMutationJob(job)
	return nil
}

func (f *durableMutationRPCFixture) HeartbeatServiceMutation(
	req *ServiceOperationMutationHeartbeatRequest,
	resp *ServiceOperationMutationResponse,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	job := f.jobs[req.RequestID]
	if job == nil || job.OwnerID != req.OwnerID || job.Status != agentMutationRunning {
		resp.Error = "service mutation lease is not running"
		resp.Job = cloneServiceOperationMutationJob(job)
		return nil
	}
	if req.Phase != "" {
		job.Phase = req.Phase
	}
	job.LeaseExpiresAt = time.Now().UTC().Add(time.Minute)
	resp.Job = cloneServiceOperationMutationJob(job)
	return nil
}

func (f *durableMutationRPCFixture) FinishServiceMutation(
	req *ServiceOperationMutationFinishRequest,
	resp *ServiceOperationMutationResponse,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	job := f.jobs[req.RequestID]
	if job == nil || job.OwnerID != req.OwnerID {
		resp.Error = "service mutation owner mismatch"
		resp.Job = cloneServiceOperationMutationJob(job)
		return nil
	}
	if req.Success {
		job.Status = agentMutationSucceeded
		identity := agentMutationIdentity{
			RequestID:   job.RequestID,
			OwnerID:     job.OwnerID,
			Kind:        job.Kind,
			Target:      job.Target,
			PackageName: job.PackageName,
		}
		phase, required, err := payloadBoundMutationPublishedPhase(identity)
		if required && err == nil {
			job.Phase = phase
		} else {
			job.Phase = "completed"
		}
	} else {
		job.Status = agentMutationFailed
		job.Phase = "failed"
		job.ErrorCode = req.FailureCode
		job.ErrorMessage = req.Message
	}
	if f.active == req.RequestID {
		f.active = ""
	}
	resp.Job = cloneServiceOperationMutationJob(job)
	return nil
}
