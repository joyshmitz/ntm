package handoff

import (
	"context"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/agentmail"
)

type releaseCall struct {
	projectKey string
	agentName  string
	paths      []string
}

type renewCall struct {
	projectKey    string
	agentName     string
	extendSeconds int
}

type fakeTransferClient struct {
	reserveCalls []agentmail.FileReservationOptions
	releaseCalls []releaseCall
	renewCalls   []renewCall
	reserveFn    func(opts agentmail.FileReservationOptions) (*agentmail.ReservationResult, error)
}

func (f *fakeTransferClient) ReservePaths(ctx context.Context, opts agentmail.FileReservationOptions) (*agentmail.ReservationResult, error) {
	f.reserveCalls = append(f.reserveCalls, opts)
	if f.reserveFn != nil {
		return f.reserveFn(opts)
	}
	var granted []agentmail.FileReservation
	for _, p := range opts.Paths {
		granted = append(granted, agentmail.FileReservation{PathPattern: p})
	}
	return &agentmail.ReservationResult{Granted: granted}, nil
}

func (f *fakeTransferClient) ReleaseReservations(ctx context.Context, projectKey, agentName string, paths []string, ids []int) error {
	f.releaseCalls = append(f.releaseCalls, releaseCall{
		projectKey: projectKey,
		agentName:  agentName,
		paths:      paths,
	})
	return nil
}

func (f *fakeTransferClient) RenewReservations(ctx context.Context, projectKey, agentName string, extendSeconds int) error {
	f.renewCalls = append(f.renewCalls, renewCall{
		projectKey:    projectKey,
		agentName:     agentName,
		extendSeconds: extendSeconds,
	})
	return nil
}

func TestTransferReservationsSuccess(t *testing.T) {
	client := &fakeTransferClient{}
	opts := TransferReservationsOptions{
		ProjectKey: "proj",
		FromAgent:  "old",
		ToAgent:    "new",
		Reservations: []ReservationSnapshot{
			{PathPattern: "internal/a.go", Exclusive: true},
			{PathPattern: "internal/b.go", Exclusive: false},
		},
		TTLSeconds: 120,
	}

	result, err := TransferReservations(context.Background(), client, opts)
	if err != nil {
		t.Fatalf("TransferReservations error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if len(client.releaseCalls) != 1 {
		t.Fatalf("expected 1 release call, got %d", len(client.releaseCalls))
	}
	if len(client.reserveCalls) != 2 {
		t.Fatalf("expected 2 reserve calls (exclusive+shared), got %d", len(client.reserveCalls))
	}
	if len(result.GrantedPaths) != 2 {
		t.Fatalf("expected 2 granted paths, got %d", len(result.GrantedPaths))
	}
}

func TestTransferReservationsConflictRollback(t *testing.T) {
	client := &fakeTransferClient{}
	callCount := 0
	client.reserveFn = func(opts agentmail.FileReservationOptions) (*agentmail.ReservationResult, error) {
		callCount++
		if opts.AgentName == "new" {
			conflict := agentmail.ReservationConflict{Path: "internal/a.go", Holders: []string{"someone"}}
			res := &agentmail.ReservationResult{
				Granted:   []agentmail.FileReservation{{PathPattern: "internal/a.go"}},
				Conflicts: []agentmail.ReservationConflict{conflict},
			}
			return res, agentmail.ErrReservationConflict
		}
		// rollback for old agent
		return &agentmail.ReservationResult{
			Granted: []agentmail.FileReservation{{PathPattern: "internal/a.go"}},
		}, nil
	}

	opts := TransferReservationsOptions{
		ProjectKey: "proj",
		FromAgent:  "old",
		ToAgent:    "new",
		Reservations: []ReservationSnapshot{
			{PathPattern: "internal/a.go", Exclusive: true},
		},
		TTLSeconds:  60,
		GracePeriod: 0,
	}

	result, err := TransferReservations(context.Background(), client, opts)
	if err == nil {
		t.Fatalf("expected conflict error")
	}
	if result == nil || len(result.Conflicts) == 0 {
		t.Fatalf("expected conflicts in result")
	}
	if len(client.releaseCalls) < 2 {
		t.Fatalf("expected release calls for old and partial grants")
	}
	if !result.RolledBack {
		t.Fatalf("expected rollback to succeed")
	}
}

func TestTransferReservationsSameAgentRefresh(t *testing.T) {
	client := &fakeTransferClient{}
	opts := TransferReservationsOptions{
		ProjectKey: "proj",
		FromAgent:  "same",
		ToAgent:    "same",
		Reservations: []ReservationSnapshot{
			{PathPattern: "internal/a.go", Exclusive: true},
		},
		TTLSeconds: 90,
	}

	result, err := TransferReservations(context.Background(), client, opts)
	if err != nil {
		t.Fatalf("TransferReservations error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got %s", result.Error)
	}
	if len(client.renewCalls) != 1 {
		t.Fatalf("expected 1 renew call, got %d", len(client.renewCalls))
	}
	if len(client.releaseCalls) != 0 || len(client.reserveCalls) != 0 {
		t.Fatalf("expected no release/reserve calls for same-agent refresh")
	}
}
