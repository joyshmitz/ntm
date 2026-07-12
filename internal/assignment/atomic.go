package assignment

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/events"
)

// DispatchState records the durable boundary reached by an atomic assignment.
type DispatchState string

const (
	DispatchPending DispatchState = "pending"
	DispatchSending DispatchState = "sending"
	DispatchSent    DispatchState = "sent"
)

// ClaimState records the crash-recovery state of the external Beads claim.
type ClaimState string

const (
	ClaimPending  ClaimState = "pending"
	ClaimClaiming ClaimState = "claiming"
	ClaimClaimed  ClaimState = "claimed"
	ClaimFailed   ClaimState = "failed"
	ClaimUnknown  ClaimState = "unknown"
)

// ReservationState records the crash-recovery state of the Agent Mail lease.
type ReservationState string

const (
	ReservationPending   ReservationState = "pending"
	ReservationReserving ReservationState = "reserving"
	ReservationReserved  ReservationState = "reserved"
	ReservationFailed    ReservationState = "failed"
	ReservationUnknown   ReservationState = "unknown"
	ReservationReleased  ReservationState = "released"
)

var (
	// ErrClaimConflict means another actor owns the bead.
	ErrClaimConflict = errors.New("bead claim is owned by another actor")
	// ErrReservationRequired means the assignment intent requires Agent Mail
	// reservation, but no reservation port is available to enforce it.
	ErrReservationRequired = errors.New("file reservation is required but unavailable")
	// ErrReservationPathsRequired means a required reservation did not define
	// explicit paths and did not opt into reservation-port path discovery.
	ErrReservationPathsRequired = errors.New("file reservation is required but no reservation paths were defined")
	// ErrReservationOutcomeUnknown means a process stopped while acquiring a
	// lease and no reconciliation port could prove whether it was created.
	ErrReservationOutcomeUnknown = errors.New("file reservation outcome is unknown")
	// ErrReservationReleaseRequired means a reservation call returned durable
	// lease handles but the lease is not valid for dispatch. The handles must be
	// reconciled or released before retrying or replacing the assignment.
	ErrReservationReleaseRequired = errors.New("file reservation must be reconciled or released")
	// ErrClaimOutcomeUnknown means the durable claim barrier could not be
	// reconciled with the external tracker.
	ErrClaimOutcomeUnknown = errors.New("bead claim outcome is unknown")
	// ErrDispatchOutcomeUnknown means a prior process stopped after recording the
	// send intent but before recording the transport result. Retrying blindly
	// could duplicate the prompt.
	ErrDispatchOutcomeUnknown = errors.New("assignment dispatch outcome is unknown")
	// ErrTargetOccupied means another active assignment owns the exact pane or
	// mail delivery target. It is detected before claiming or dispatching.
	ErrTargetOccupied = errors.New("assignment target already has active work")
	// ErrTerminalAssignmentAttempt means a completed assignment generation was
	// retried with its old idempotency key. Reopened work must start a new
	// generation so it receives a new durable dispatch receipt.
	ErrTerminalAssignmentAttempt = errors.New("assignment generation is terminal; reopen the work item and use a new idempotency key")
	// ErrWorkingReplacementNotAllowed means a caller requested an atomic
	// reassignment without first establishing the exact durable handoff barrier:
	// the same bead must still be working and its old leases must be released.
	ErrWorkingReplacementNotAllowed = errors.New("atomic replacement requires the same working assignment with released leases")
)

type guaranteedNoActuationError struct {
	err error
}

type guaranteedNoReservationError struct {
	err error
}

type reservationReleaseRequiredError struct {
	err error
}

func (e *guaranteedNoActuationError) Error() string { return e.err.Error() }
func (e *guaranteedNoActuationError) Unwrap() error { return e.err }

// GuaranteeNoActuation marks a dispatch failure that is known to have happened
// before the transport attempted delivery. Only these errors are retryable.
func GuaranteeNoActuation(err error) error {
	if err == nil {
		return nil
	}
	return &guaranteedNoActuationError{err: err}
}

// IsGuaranteedNoActuation reports whether retrying cannot duplicate delivery.
func IsGuaranteedNoActuation(err error) bool {
	var target *guaranteedNoActuationError
	return errors.As(err, &target)
}

func (e *guaranteedNoReservationError) Error() string { return e.err.Error() }
func (e *guaranteedNoReservationError) Unwrap() error { return e.err }

// GuaranteeNoReservation marks a reservation failure that is known to have
// happened before the service created or renewed a lease.
func GuaranteeNoReservation(err error) error {
	if err == nil {
		return nil
	}
	return &guaranteedNoReservationError{err: err}
}

// IsGuaranteedNoReservation reports whether a failed reservation can be
// retried without first inspecting the remote service.
func IsGuaranteedNoReservation(err error) bool {
	var target *guaranteedNoReservationError
	return errors.As(err, &target)
}

func (e *reservationReleaseRequiredError) Error() string { return e.err.Error() }
func (e *reservationReleaseRequiredError) Unwrap() error { return e.err }

// RequireReservationRelease marks a reservation failure that is known to
// have created or retained an external lease. Adapters should return every
// available lease handle alongside this marker so clear can release it.
func RequireReservationRelease(err error) error {
	if err == nil {
		return &reservationReleaseRequiredError{err: ErrReservationReleaseRequired}
	}
	if IsReservationReleaseRequired(err) {
		return err
	}
	return &reservationReleaseRequiredError{err: errors.Join(ErrReservationReleaseRequired, err)}
}

// IsReservationReleaseRequired reports whether retrying Reserve would risk
// leaking or duplicating a known external lease.
func IsReservationReleaseRequired(err error) bool {
	var target *reservationReleaseRequiredError
	return errors.Is(err, ErrReservationReleaseRequired) || errors.As(err, &target)
}

// ClaimReceipt is the durable result of br's atomic claim transaction.
type ClaimReceipt struct {
	BeadID    string
	Actor     string
	Status    string
	ClaimedAt time.Time
}

// ClaimPort atomically claims a bead. Production implementations must use
// `br update <id> --claim --actor <actor> --json` rather than a read/update pair.
type ClaimPort interface {
	Claim(context.Context, string, string) (ClaimReceipt, error)
}

// ClaimReconciliationState is the result of inspecting a durable claim barrier.
type ClaimReconciliationState string

const (
	ClaimReconciliationAbsent   ClaimReconciliationState = "absent"
	ClaimReconciliationOwned    ClaimReconciliationState = "owned"
	ClaimReconciliationConflict ClaimReconciliationState = "conflict"
	ClaimReconciliationUnknown  ClaimReconciliationState = "unknown"
)

// ClaimReconciliation reports whether the intended actor already owns the
// claim. Receipt is required when State is owned.
type ClaimReconciliation struct {
	State   ClaimReconciliationState
	Receipt ClaimReceipt
}

// ClaimReconciliationPort is an optional capability implemented by claim
// adapters that can inspect tracker ownership after a crash. ClaimPort itself
// must still make repeated claims by the same actor idempotent.
type ClaimReconciliationPort interface {
	ReconcileClaim(context.Context, string, string) (ClaimReconciliation, error)
}

// ClaimFunc adapts a function to ClaimPort.
type ClaimFunc func(context.Context, string, string) (ClaimReceipt, error)

func (f ClaimFunc) Claim(ctx context.Context, beadID, actor string) (ClaimReceipt, error) {
	return f(ctx, beadID, actor)
}

// ReservationRequest contains the claim-owned handoff information needed to
// reserve files before dispatch.
type ReservationRequest struct {
	BeadID         string
	BeadTitle      string
	AgentName      string
	Target         string
	RequestedPaths []string
	TTL            time.Duration
}

// LeaseReceipt captures Agent Mail reservation metadata needed for renewal,
// release, and crash recovery.
type LeaseReceipt struct {
	AgentName      string
	Target         string
	Requested      []string
	Granted        []string
	ReservationIDs []int
	ExpiresAt      *time.Time
}

// ReservationPort reserves the edit surface after the bead is claimed and
// before the assignment is delivered.
type ReservationPort interface {
	Reserve(context.Context, ReservationRequest) (LeaseReceipt, error)
}

// ReservationReconciliationState is the result of inspecting a durable
// reservation barrier.
type ReservationReconciliationState string

const (
	ReservationReconciliationAbsent   ReservationReconciliationState = "absent"
	ReservationReconciliationReserved ReservationReconciliationState = "reserved"
	ReservationReconciliationUnknown  ReservationReconciliationState = "unknown"
)

// ReservationReconciliation reports whether the intended lease exists.
type ReservationReconciliation struct {
	State ReservationReconciliationState
	Lease LeaseReceipt
}

// ReservationReconciliationPort is an optional capability. A retry never
// repeats an ambiguous Reserve call unless this inspection proves absence.
type ReservationReconciliationPort interface {
	ReconcileReservation(context.Context, ReservationRequest, LeaseReceipt) (ReservationReconciliation, error)
}

// ReservationFunc adapts a function to ReservationPort.
type ReservationFunc func(context.Context, ReservationRequest) (LeaseReceipt, error)

func (f ReservationFunc) Reserve(ctx context.Context, req ReservationRequest) (LeaseReceipt, error) {
	return f(ctx, req)
}

// DispatchRequest is the transport-neutral assignment payload.
type DispatchRequest struct {
	BeadID         string
	BeadTitle      string
	Target         string
	Pane           int
	AgentType      string
	AgentName      string
	Prompt         string
	IdempotencyKey string
}

// DispatchReceipt identifies the completed transport operation.
type DispatchReceipt struct {
	DeliveryID string
	Duration   time.Duration
}

// DispatchPort delivers an already-claimed, already-reserved assignment.
type DispatchPort interface {
	Dispatch(context.Context, DispatchRequest) (DispatchReceipt, error)
}

// DispatchFunc adapts a function to DispatchPort.
type DispatchFunc func(context.Context, DispatchRequest) (DispatchReceipt, error)

func (f DispatchFunc) Dispatch(ctx context.Context, req DispatchRequest) (DispatchReceipt, error) {
	return f(ctx, req)
}

// PromptPreflightResult separates the configured transport payload from the
// sanitized payload that may be written to the durable assignment ledger.
type PromptPreflightResult struct {
	DispatchPrompt string
	DurablePrompt  string
}

// PromptPreflightPort validates and redacts the final target-specific prompt.
// Implementations must not actuate transport or mutate external state.
type PromptPreflightPort interface {
	Preflight(context.Context, DispatchRequest) (PromptPreflightResult, error)
}

// PromptPreflightFunc adapts a function to PromptPreflightPort.
type PromptPreflightFunc func(context.Context, DispatchRequest) (PromptPreflightResult, error)

func (f PromptPreflightFunc) Preflight(ctx context.Context, req DispatchRequest) (PromptPreflightResult, error) {
	return f(ctx, req)
}

// WorkItemStatusPort proves that terminal work was reopened in the external
// tracker before a new assignment generation replaces its durable receipt.
type WorkItemStatusPort interface {
	WorkItemStatus(context.Context, string) (string, error)
}

// WorkItemStatusFunc adapts a function to WorkItemStatusPort.
type WorkItemStatusFunc func(context.Context, string) (string, error)

func (f WorkItemStatusFunc) WorkItemStatus(ctx context.Context, beadID string) (string, error) {
	return f(ctx, beadID)
}

type nonTerminalClaimContextKey struct{}

// WithNonTerminalClaimGuard marks a tracker claim that must compare-and-set
// only a non-terminal work item. The marker survives crash recovery through
// Assignment.ClaimRequiresNonTerminal and is consumed by Beads adapters.
func WithNonTerminalClaimGuard(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, nonTerminalClaimContextKey{}, true)
}

// NonTerminalClaimGuardRequired reports whether a claim adapter must refuse a
// closed or tombstoned work item instead of allowing a generic claim to reopen
// it as a side effect.
func NonTerminalClaimGuardRequired(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	required, _ := ctx.Value(nonTerminalClaimContextKey{}).(bool)
	return required
}

// AtomicRequest describes one claim-reserve-dispatch transaction.
type AtomicRequest struct {
	BeadID    string
	BeadTitle string
	Target    string
	// OccupancyKey is the stable physical pane or recipient identity used for
	// locking and exclusion. Adapters should pass tmux pane IDs; raw selector
	// spellings belong in Target. Empty falls back to Target.
	OccupancyKey   string
	Pane           int
	AgentType      string
	AgentName      string
	Actor          string
	Prompt         string
	IdempotencyKey string
	// IntentSHA256 is populated by AtomicCoordinator from the original prompt
	// before preflight. Callers must not use it to bypass that calculation.
	IntentSHA256 string
	// RecoveredIntentSHA256 may carry the exact checksum from an existing same-key
	// durable row when the original unredacted prompt is intentionally unavailable.
	RecoveredIntentSHA256 string
	// RequireReservation fails closed before a new claim or dispatch when the
	// reservation port is unavailable. RequestedPaths also implies this flag.
	RequireReservation        bool
	AllowReservationDiscovery bool
	RequestedPaths            []string
	ReservationTTL            time.Duration
	// ReplaceWorkingAssignment starts a new atomic generation in place of the
	// same bead's working generation. It is accepted only after clear has
	// durably reached ClearStateLeasesReleased. Exact retries of the newly
	// persisted idempotency key remain ordinary recovery attempts.
	ReplaceWorkingAssignment bool
	claimRequiresNonTerminal bool
}

// AtomicResult reports the durable state reached by Execute.
type AtomicResult struct {
	Assignment *Assignment
	Claim      ClaimReceipt
	Lease      LeaseReceipt
	Dispatch   DispatchReceipt
	Sent       bool
	Replayed   bool
	Recovered  bool
}

// AtomicCoordinator owns the single claim-before-reserve-before-send boundary.
type AtomicCoordinator struct {
	store      *AssignmentStore
	claimer    ClaimPort
	reserver   ReservationPort
	dispatcher DispatchPort
	preflight  PromptPreflightPort
	workStatus WorkItemStatusPort
	now        func() time.Time
}

// NewAtomicCoordinator creates an assignment coordinator with injectable
// external ports. The reservation port may be nil when no paths are known.
func NewAtomicCoordinator(store *AssignmentStore, claimer ClaimPort, reserver ReservationPort, dispatcher DispatchPort, preflight ...PromptPreflightPort) *AtomicCoordinator {
	coordinator := &AtomicCoordinator{
		store:      store,
		claimer:    claimer,
		reserver:   reserver,
		dispatcher: dispatcher,
		now:        func() time.Time { return time.Now().UTC() },
	}
	if len(preflight) > 0 {
		coordinator.preflight = preflight[0]
	}
	return coordinator
}

// WithWorkItemStatusPort requires external reopen proof for terminal-to-new
// assignment generations. Production adapters should always configure it.
func (c *AtomicCoordinator) WithWorkItemStatusPort(port WorkItemStatusPort) *AtomicCoordinator {
	c.workStatus = port
	return c
}

// AssignmentIdempotencyKey returns a deterministic digest for callers that
// already have a stable external request identity. New independent attempts
// must use NewAssignmentIdempotencyKey so Beads does not treat them as the same
// actor retry.
func AssignmentIdempotencyKey(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(strings.TrimSpace(part)))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// NewAssignmentIdempotencyKey generates the unique identity of one assignment
// attempt. Persist this value and reuse it only when recovering that attempt.
func NewAssignmentIdempotencyKey() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate assignment idempotency key: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func PromptSHA256(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

// DispatchDeliveryID identifies one durable transport generation. Exact
// retries reuse the idempotency key and therefore the receipt; independent or
// reopened generations receive a different ID even on the same pane/protocol.
func DispatchDeliveryID(target, protocol, idempotencyKey string) string {
	return fmt.Sprintf("%s/%s/%s", strings.TrimSpace(target), strings.TrimSpace(protocol), strings.TrimSpace(idempotencyKey))
}

func normalizeOccupancyKey(target, occupancyKey string) string {
	if normalized := strings.TrimSpace(occupancyKey); normalized != "" {
		return normalized
	}
	return strings.TrimSpace(target)
}

// StableClaimActor makes independent assignment intents distinct Beads actors
// while keeping a retry of the same idempotency key stable and human-readable.
func StableClaimActor(baseActor, idempotencyKey string) string {
	baseActor = strings.TrimSpace(baseActor)
	if baseActor == "" {
		baseActor = "ntm"
	}
	key := strings.TrimSpace(idempotencyKey)
	if len(key) > 12 {
		key = key[:12]
	}
	if key == "" {
		key = AssignmentIdempotencyKey(baseActor)[:12]
	}
	suffix := "/ntm-" + key
	if strings.HasSuffix(baseActor, suffix) {
		return baseActor
	}
	return baseActor + suffix
}

// Execute claims the bead, persists the claim, reserves files, persists the
// lease, and only then dispatches. Same-key completed retries are replayed
// without external side effects.
func (c *AtomicCoordinator) Execute(ctx context.Context, req AtomicRequest) (AtomicResult, error) {
	var result AtomicResult
	if len(req.RequestedPaths) > 0 {
		req.RequireReservation = true
	}
	req.OccupancyKey = normalizeOccupancyKey(req.Target, req.OccupancyKey)
	// A generic `br --claim` can reopen terminal work as a side effect. Every
	// assignment generation must use the guarded nonterminal compare-and-set,
	// including the first generation when no local ledger row exists yet.
	req.claimRequiresNonTerminal = true
	if err := validateAtomicRequest(req); err != nil {
		return result, err
	}
	if c.store == nil || c.claimer == nil || c.dispatcher == nil {
		return result, errors.New("atomic assignment coordinator is not fully configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	rawPrompt := req.Prompt
	req.IntentSHA256 = PromptSHA256(rawPrompt)
	operationUnlock, err := acquireAtomicBeadOperationLock(ctx, c.store.path, req.BeadID)
	if err != nil {
		return result, fmt.Errorf("lock atomic assignment %s: %w", req.BeadID, err)
	}
	defer operationUnlock()
	targetUnlock, err := acquireAtomicTargetOperationLock(ctx, c.store.path, req.OccupancyKey)
	if err != nil {
		return result, fmt.Errorf("lock atomic assignment target %s: %w", req.Target, err)
	}
	defer targetUnlock()
	if err := c.store.LoadStrict(); err != nil {
		return result, fmt.Errorf("refresh atomic assignment %s: %w", req.BeadID, err)
	}
	if occupied := activeAssignmentForTarget(c.store.List(), req.BeadID, req.OccupancyKey, req.Pane); occupied != nil {
		result.Assignment = occupied
		return result, fmt.Errorf("%w: %s is owned by bead %s", ErrTargetOccupied, req.Target, occupied.BeadID)
	}

	prior := c.store.Get(req.BeadID)
	actor := StableClaimActor(req.Actor, req.IdempotencyKey)
	if prior != nil && prior.IdempotencyKey == req.IdempotencyKey && strings.TrimSpace(prior.ClaimActor) != "" {
		// The durable row is authoritative during same-key crash recovery. A
		// replacement generation intentionally reuses its predecessor's actor,
		// whose suffix may belong to an older idempotency key; stabilizing that
		// value again would invent a different tracker owner and deadlock retry.
		actor = prior.ClaimActor
	}
	replacementStart := false
	if req.ReplaceWorkingAssignment {
		if prior == nil {
			return result, fmt.Errorf("%w: no durable assignment exists for %s", ErrWorkingReplacementNotAllowed, req.BeadID)
		}
		if prior.IdempotencyKey != req.IdempotencyKey {
			if err := validateWorkingReplacement(prior, req); err != nil {
				result.Assignment = prior
				return result, err
			}
			actor, err = workingReplacementClaimActor(prior)
			if err != nil {
				result.Assignment = prior
				return result, err
			}
			replacementStart = true
			if strings.TrimSpace(req.BeadTitle) == "" {
				req.BeadTitle = prior.BeadTitle
			}
		} else if strings.TrimSpace(prior.ClaimActor) != "" {
			// A retry of a replacement generation must retain the actor copied
			// from the old generation rather than derive one from the new agent.
			actor = prior.ClaimActor
		}
	}
	if recoveredIntent := strings.TrimSpace(req.RecoveredIntentSHA256); recoveredIntent != "" {
		if prior == nil || prior.IdempotencyKey != req.IdempotencyKey {
			return result, errors.New("recovered intent checksum requires an existing same-key assignment")
		}
		storedIntent := strings.TrimSpace(prior.IntentSHA256)
		if storedIntent == "" {
			storedIntent = strings.TrimSpace(prior.PromptSHA256)
		}
		if storedIntent == "" || recoveredIntent != storedIntent {
			return result, errors.New("recovered intent checksum does not match the durable assignment")
		}
		req.IntentSHA256 = storedIntent
	}
	if prior != nil {
		if prior.ClearState != ClearStateNone && !replacementStart {
			result.Assignment = prior
			return result, fmt.Errorf("%w: %s is awaiting reservation release", ErrClaimConflict, req.BeadID)
		}
		if prior.IdempotencyKey == req.IdempotencyKey && prior.ClaimActor == actor {
			if isTerminalAssignmentStatus(prior.Status) {
				result.Assignment = prior
				return result, fmt.Errorf("%w: %s", ErrTerminalAssignmentAttempt, req.BeadID)
			}
			if !matchesAtomicRawIntent(prior, req) {
				result.Assignment = prior
				return result, fmt.Errorf("idempotency key %s was reused for a different assignment intent", req.IdempotencyKey)
			}
			switch prior.DispatchState {
			case DispatchSent:
				result.Assignment = prior
				result.Sent = true
				result.Replayed = true
				result.Dispatch = DispatchReceipt{DeliveryID: prior.DispatchReceiptID, Duration: prior.DispatchDuration}
				return result, nil
			case DispatchSending:
				result.Assignment = prior
				return result, ErrDispatchOutcomeUnknown
			}
			result.Recovered = true
		} else if prior.IdempotencyKey != "" && !replacementStart {
			switch prior.Status {
			case StatusClaiming, StatusClaimed, StatusAssigned, StatusWorking:
				result.Assignment = prior
				return result, fmt.Errorf("%w: %s is recorded for %s", ErrClaimConflict, req.BeadID, prior.ClaimActor)
			}
		}
		if isTerminalAssignmentStatus(prior.Status) && prior.IdempotencyKey != req.IdempotencyKey {
			if assignmentHasUnresolvedReservation(prior) {
				result.Assignment = prior
				return result, fmt.Errorf("%w: reconcile and clear assignment %s before starting a new generation", ErrReservationReleaseRequired, req.BeadID)
			}
			if c.workStatus == nil {
				result.Assignment = prior
				return result, fmt.Errorf("%w: cannot prove bead %s was reopened", ErrTerminalAssignmentAttempt, req.BeadID)
			}
			trackerStatus, statusErr := c.workStatus.WorkItemStatus(ctx, req.BeadID)
			if statusErr != nil {
				result.Assignment = prior
				return result, fmt.Errorf("%w: verify bead %s reopen status: %v", ErrTerminalAssignmentAttempt, req.BeadID, statusErr)
			}
			if !workItemStatusAllowsNewGeneration(trackerStatus) {
				result.Assignment = prior
				return result, fmt.Errorf("%w: bead %s tracker status is %q", ErrTerminalAssignmentAttempt, req.BeadID, trackerStatus)
			}
			// `br reopen` retains the assignee. Reuse that actor for the new
			// generation so the atomic claim remains valid, while the new key and
			// ledger record still distinguish the new dispatch attempt.
			if strings.TrimSpace(prior.ClaimActor) != "" {
				actor = prior.ClaimActor
			}
			req.claimRequiresNonTerminal = true
		}
	}

	durablePrompt := rawPrompt
	if c.preflight != nil {
		preflightResult, preflightErr := c.preflight.Preflight(ctx, DispatchRequest{
			BeadID: req.BeadID, BeadTitle: req.BeadTitle, Target: req.Target, Pane: req.Pane,
			AgentType: req.AgentType, AgentName: req.AgentName, Prompt: rawPrompt, IdempotencyKey: req.IdempotencyKey,
		})
		if preflightErr != nil {
			return result, fmt.Errorf("preflight assignment prompt for %s: %w", req.BeadID, preflightErr)
		}
		if strings.TrimSpace(preflightResult.DispatchPrompt) == "" || strings.TrimSpace(preflightResult.DurablePrompt) == "" {
			return result, errors.New("assignment prompt preflight returned an empty payload")
		}
		req.Prompt = preflightResult.DispatchPrompt
		durablePrompt = preflightResult.DurablePrompt
	}
	persistedReq := req
	persistedReq.Prompt = durablePrompt
	if prior != nil && prior.IdempotencyKey == req.IdempotencyKey && prior.ClaimActor == actor && !matchesAtomicIntent(prior, persistedReq) {
		result.Assignment = prior
		return result, fmt.Errorf("idempotency key %s was reused for a different durable assignment intent", req.IdempotencyKey)
	}
	if req.RequireReservation && c.reserver == nil && !replacementStart {
		switch {
		case prior != nil && prior.IdempotencyKey == req.IdempotencyKey && reservationOutcomeAmbiguous(prior):
			return result, ErrReservationOutcomeUnknown
		case prior == nil || prior.IdempotencyKey != req.IdempotencyKey || reservationNeedsRefresh(prior, c.now()):
			return result, ErrReservationRequired
		}
	}

	recorded, err := c.store.RecordAtomicIntent(persistedReq, actor, c.now())
	if err != nil {
		return result, err
	}
	result.Assignment = recorded

	claim, recorded, err := c.ensureClaim(ctx, persistedReq, actor, recorded)
	if err != nil {
		result.Assignment = c.store.Get(req.BeadID)
		return result, err
	}
	result.Claim = claim
	result.Assignment = recorded

	lease, recorded, err := c.ensureReservation(ctx, req, recorded)
	result.Lease = lease
	result.Assignment = recorded
	if err != nil {
		return result, err
	}

	if err := c.store.RecordAtomicDispatchStarted(req.BeadID, req.IdempotencyKey, c.now()); err != nil {
		return result, err
	}
	dispatch, dispatchErr := c.dispatcher.Dispatch(ctx, DispatchRequest{
		BeadID:         req.BeadID,
		BeadTitle:      req.BeadTitle,
		Target:         req.Target,
		Pane:           req.Pane,
		AgentType:      req.AgentType,
		AgentName:      req.AgentName,
		Prompt:         req.Prompt,
		IdempotencyKey: req.IdempotencyKey,
	})
	result.Dispatch = dispatch
	if dispatchErr != nil {
		if IsGuaranteedNoActuation(dispatchErr) {
			if persistErr := c.store.RecordAtomicDispatchFailed(req.BeadID, req.IdempotencyKey, dispatchErr); persistErr != nil {
				return result, errors.Join(dispatchErr, persistErr)
			}
			result.Assignment = c.store.Get(req.BeadID)
			return result, fmt.Errorf("dispatch %s: %w", req.BeadID, dispatchErr)
		}
		result.Assignment = c.store.Get(req.BeadID)
		return result, fmt.Errorf("dispatch %s: %w", req.BeadID, errors.Join(ErrDispatchOutcomeUnknown, dispatchErr))
	}
	if err := c.store.RecordAtomicDispatchSent(req.BeadID, req.IdempotencyKey, durablePrompt, dispatch, c.now()); err != nil {
		return result, err
	}
	result.Assignment = c.store.Get(req.BeadID)
	result.Sent = true
	return result, nil
}

func workItemStatusAllowsNewGeneration(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "open", "in_progress":
		return true
	default:
		return false
	}
}

func validateWorkingReplacement(prior *Assignment, req AtomicRequest) error {
	if prior == nil || prior.BeadID != req.BeadID {
		return fmt.Errorf("%w: no matching durable assignment exists for %s", ErrWorkingReplacementNotAllowed, req.BeadID)
	}
	if prior.Status != StatusWorking {
		return fmt.Errorf("%w: %s is %s, expected %s", ErrWorkingReplacementNotAllowed, req.BeadID, prior.Status, StatusWorking)
	}
	if prior.ClearState != ClearStateLeasesReleased {
		return fmt.Errorf("%w: %s clear state is %q, expected %q", ErrWorkingReplacementNotAllowed, req.BeadID, prior.ClearState, ClearStateLeasesReleased)
	}
	if prior.DispatchState == DispatchSending {
		return fmt.Errorf("%w: %s has an unknown dispatch outcome", ErrWorkingReplacementNotAllowed, req.BeadID)
	}
	if assignmentHasUnresolvedReservation(prior) {
		return fmt.Errorf("%w: %s still has unresolved reservation metadata", ErrWorkingReplacementNotAllowed, req.BeadID)
	}
	if strings.TrimSpace(prior.IdempotencyKey) == strings.TrimSpace(req.IdempotencyKey) {
		return fmt.Errorf("%w: replacement generation must use a new idempotency key", ErrWorkingReplacementNotAllowed)
	}
	return nil
}

func workingReplacementClaimActor(prior *Assignment) (string, error) {
	if prior == nil {
		return "", ErrWorkingReplacementNotAllowed
	}
	if actor := strings.TrimSpace(prior.ClaimActor); actor != "" {
		return actor, nil
	}
	// Pre-atomic ledgers did not persist ClaimActor. Their Agent Mail identity
	// is the only conservative claim-owner identity available for an idempotent
	// guarded claim; never derive a fresh actor from the replacement target.
	if actor := strings.TrimSpace(prior.AgentName); actor != "" {
		return actor, nil
	}
	return "", fmt.Errorf("%w: %s has no durable claim actor", ErrWorkingReplacementNotAllowed, prior.BeadID)
}

func (c *AtomicCoordinator) ensureClaim(ctx context.Context, req AtomicRequest, actor string, recorded *Assignment) (ClaimReceipt, *Assignment, error) {
	if req.claimRequiresNonTerminal || (recorded != nil && recorded.ClaimRequiresNonTerminal) {
		ctx = WithNonTerminalClaimGuard(ctx)
	}
	state := effectiveClaimState(recorded)
	if state == ClaimClaimed {
		return claimFromAssignment(recorded), recorded, nil
	}
	if state == ClaimFailed {
		return ClaimReceipt{}, recorded, fmt.Errorf("claim %s: %w", req.BeadID, ErrClaimConflict)
	}

	if state == ClaimClaiming || state == ClaimUnknown {
		if reconciler, ok := c.claimer.(ClaimReconciliationPort); ok {
			reconciliation, reconcileErr := reconciler.ReconcileClaim(ctx, req.BeadID, actor)
			if reconcileErr != nil {
				unknownErr := errors.Join(ErrClaimOutcomeUnknown, reconcileErr)
				if persistErr := c.store.RecordAtomicClaimUncertain(req.BeadID, req.IdempotencyKey, unknownErr); persistErr != nil {
					return ClaimReceipt{}, recorded, errors.Join(unknownErr, persistErr)
				}
				return ClaimReceipt{}, c.store.Get(req.BeadID), fmt.Errorf("reconcile claim %s: %w", req.BeadID, unknownErr)
			}
			switch reconciliation.State {
			case ClaimReconciliationOwned:
				if receiptErr := validateClaimReceipt(reconciliation.Receipt, req.BeadID, actor); receiptErr != nil {
					unknownErr := errors.Join(ErrClaimOutcomeUnknown, receiptErr)
					if persistErr := c.store.RecordAtomicClaimUncertain(req.BeadID, req.IdempotencyKey, unknownErr); persistErr != nil {
						return ClaimReceipt{}, recorded, errors.Join(unknownErr, persistErr)
					}
					return ClaimReceipt{}, c.store.Get(req.BeadID), unknownErr
				}
				claim := normalizeClaimReceipt(reconciliation.Receipt, req.BeadID, actor, c.now())
				stored, err := c.store.RecordAtomicClaim(req, claim)
				return claim, stored, err
			case ClaimReconciliationConflict:
				conflictErr := fmt.Errorf("%w: %s is owned by another actor", ErrClaimConflict, req.BeadID)
				if persistErr := c.store.RecordAtomicClaimUncertain(req.BeadID, req.IdempotencyKey, conflictErr); persistErr != nil {
					return ClaimReceipt{}, recorded, errors.Join(conflictErr, persistErr)
				}
				return ClaimReceipt{}, c.store.Get(req.BeadID), conflictErr
			case ClaimReconciliationAbsent:
				// It is now safe to retry the claim below.
			case ClaimReconciliationUnknown, "":
				unknownErr := ErrClaimOutcomeUnknown
				if persistErr := c.store.RecordAtomicClaimUncertain(req.BeadID, req.IdempotencyKey, unknownErr); persistErr != nil {
					return ClaimReceipt{}, recorded, errors.Join(unknownErr, persistErr)
				}
				return ClaimReceipt{}, c.store.Get(req.BeadID), unknownErr
			default:
				return ClaimReceipt{}, recorded, fmt.Errorf("reconcile claim %s: invalid state %q", req.BeadID, reconciliation.State)
			}
		}
	}

	if _, err := c.store.RecordAtomicClaimStarted(req.BeadID, req.IdempotencyKey, c.now()); err != nil {
		return ClaimReceipt{}, recorded, err
	}
	claim, claimErr := c.claimer.Claim(ctx, req.BeadID, actor)
	if claimErr != nil {
		if persistErr := c.store.RecordAtomicClaimUncertain(req.BeadID, req.IdempotencyKey, claimErr); persistErr != nil {
			return ClaimReceipt{}, c.store.Get(req.BeadID), errors.Join(claimErr, persistErr)
		}
		return ClaimReceipt{}, c.store.Get(req.BeadID), fmt.Errorf("claim %s: %w", req.BeadID, claimErr)
	}
	if receiptErr := validateClaimReceipt(claim, req.BeadID, actor); receiptErr != nil {
		unknownErr := errors.Join(ErrClaimOutcomeUnknown, receiptErr)
		if persistErr := c.store.RecordAtomicClaimUncertain(req.BeadID, req.IdempotencyKey, unknownErr); persistErr != nil {
			return ClaimReceipt{}, c.store.Get(req.BeadID), errors.Join(unknownErr, persistErr)
		}
		return ClaimReceipt{}, c.store.Get(req.BeadID), fmt.Errorf("claim %s: %w", req.BeadID, unknownErr)
	}
	claim = normalizeClaimReceipt(claim, req.BeadID, actor, c.now())
	stored, err := c.store.RecordAtomicClaim(req, claim)
	return claim, stored, err
}

func (c *AtomicCoordinator) ensureReservation(ctx context.Context, req AtomicRequest, recorded *Assignment) (LeaseReceipt, *Assignment, error) {
	if recorded == nil {
		return LeaseReceipt{}, nil, errors.New("atomic assignment disappeared before reservation")
	}
	reservationReq := ReservationRequest{
		BeadID:         req.BeadID,
		BeadTitle:      req.BeadTitle,
		AgentName:      req.AgentName,
		Target:         req.OccupancyKey,
		RequestedPaths: append([]string(nil), req.RequestedPaths...),
		TTL:            req.ReservationTTL,
	}
	if strings.TrimSpace(reservationReq.BeadTitle) == "" {
		reservationReq.BeadTitle = recorded.BeadTitle
	}
	if len(reservationReq.RequestedPaths) == 0 {
		if len(recorded.ReservationRequested) > 0 {
			reservationReq.RequestedPaths = append([]string(nil), recorded.ReservationRequested...)
		} else if len(recorded.ReservationInputPaths) > 0 {
			reservationReq.RequestedPaths = append([]string(nil), recorded.ReservationInputPaths...)
		}
	}
	state := effectiveReservationState(recorded)
	lease := leaseFromAssignment(recorded)

	if state == ReservationReserved {
		if recorded.ReservationError != "" {
			return lease, recorded, fmt.Errorf("reserve files for %s: %w", req.BeadID, RequireReservationRelease(errors.New(recorded.ReservationError)))
		}
		if !reservationExpired(recorded, c.now()) {
			return lease, recorded, nil
		}
	}

	if state == ReservationReserving || state == ReservationUnknown {
		reconciler, ok := c.reserver.(ReservationReconciliationPort)
		if !ok {
			return lease, recorded, ErrReservationOutcomeUnknown
		}
		reconciliation, reconcileErr := reconciler.ReconcileReservation(ctx, reservationReq, lease)
		if reconcileErr != nil {
			unknownErr := errors.Join(ErrReservationOutcomeUnknown, reconcileErr)
			if persistErr := c.store.RecordAtomicReservation(req.BeadID, req.IdempotencyKey, ReservationUnknown, lease, unknownErr); persistErr != nil {
				return lease, recorded, errors.Join(unknownErr, persistErr)
			}
			return lease, c.store.Get(req.BeadID), fmt.Errorf("reconcile reservation for %s: %w", req.BeadID, unknownErr)
		}
		switch reconciliation.State {
		case ReservationReconciliationReserved:
			lease = reconciliation.Lease
			validationErr := validateLeaseReceipt(reservationReq, lease)
			if validationErr == nil && req.RequireReservation {
				validationErr = validateRequiredLease(req, lease)
			}
			reservationState, reservationErr := classifyValidatedReservation(lease, validationErr)
			if persistErr := c.store.RecordAtomicReservation(req.BeadID, req.IdempotencyKey, reservationState, lease, reservationErr); persistErr != nil {
				return lease, recorded, persistErr
			}
			recorded = c.store.Get(req.BeadID)
			if reservationErr != nil {
				return lease, recorded, fmt.Errorf("reserve files for %s: %w", req.BeadID, reservationErr)
			}
			return lease, recorded, nil
		case ReservationReconciliationAbsent:
			if persistErr := c.store.RecordAtomicReservation(req.BeadID, req.IdempotencyKey, ReservationFailed, LeaseReceipt{}, nil); persistErr != nil {
				return lease, recorded, persistErr
			}
			recorded = c.store.Get(req.BeadID)
		case ReservationReconciliationUnknown, "":
			unknownLease := reconciliation.Lease
			if !leaseHasReservationHandles(unknownLease) {
				unknownLease = lease
			}
			if persistErr := c.store.RecordAtomicReservation(req.BeadID, req.IdempotencyKey, ReservationUnknown, unknownLease, ErrReservationOutcomeUnknown); persistErr != nil {
				return lease, recorded, errors.Join(ErrReservationOutcomeUnknown, persistErr)
			}
			return unknownLease, c.store.Get(req.BeadID), ErrReservationOutcomeUnknown
		default:
			return lease, recorded, fmt.Errorf("reconcile reservation for %s: invalid state %q", req.BeadID, reconciliation.State)
		}
	}

	if c.reserver == nil {
		if req.RequireReservation {
			return lease, recorded, ErrReservationRequired
		}
		return lease, recorded, nil
	}
	if err := c.store.RecordAtomicReservationStarted(req.BeadID, req.IdempotencyKey, c.now()); err != nil {
		return lease, recorded, err
	}
	lease, reserveErr := c.reserver.Reserve(ctx, reservationReq)
	if reserveErr != nil {
		reservationState, returnedErr := classifyReservationFailure(lease, reserveErr)
		if persistErr := c.store.RecordAtomicReservation(req.BeadID, req.IdempotencyKey, reservationState, lease, returnedErr); persistErr != nil {
			return lease, c.store.Get(req.BeadID), errors.Join(returnedErr, persistErr)
		}
		return lease, c.store.Get(req.BeadID), fmt.Errorf("reserve files for %s: %w", req.BeadID, returnedErr)
	}
	validationErr := validateLeaseReceipt(reservationReq, lease)
	if validationErr == nil && req.RequireReservation {
		validationErr = validateRequiredLease(req, lease)
	}
	reservationState, reservationErr := classifyValidatedReservation(lease, validationErr)
	if persistErr := c.store.RecordAtomicReservation(req.BeadID, req.IdempotencyKey, reservationState, lease, reservationErr); persistErr != nil {
		return lease, c.store.Get(req.BeadID), persistErr
	}
	recorded = c.store.Get(req.BeadID)
	if reservationErr != nil {
		return lease, recorded, fmt.Errorf("reserve files for %s: %w", req.BeadID, reservationErr)
	}
	return lease, recorded, nil
}

func normalizeClaimReceipt(claim ClaimReceipt, beadID, actor string, now time.Time) ClaimReceipt {
	claim.BeadID = beadID
	claim.Actor = actor
	if claim.ClaimedAt.IsZero() {
		claim.ClaimedAt = now
	}
	return claim
}

func validateClaimReceipt(claim ClaimReceipt, beadID, actor string) error {
	if actual := strings.TrimSpace(claim.BeadID); actual != "" && actual != beadID {
		return fmt.Errorf("claim receipt bead mismatch: got %s, want %s", actual, beadID)
	}
	if actual := strings.TrimSpace(claim.Actor); actual != "" && actual != actor {
		return fmt.Errorf("claim receipt actor mismatch: got %s, want %s", actual, actor)
	}
	return nil
}

func claimFromAssignment(a *Assignment) ClaimReceipt {
	if a == nil {
		return ClaimReceipt{}
	}
	claim := ClaimReceipt{BeadID: a.BeadID, Actor: a.ClaimActor, Status: a.ClaimStatus}
	if a.ClaimedAt != nil {
		claim.ClaimedAt = *a.ClaimedAt
	}
	return claim
}

func validateAtomicRequest(req AtomicRequest) error {
	switch {
	case strings.TrimSpace(req.BeadID) == "":
		return errors.New("bead ID is required")
	case strings.TrimSpace(req.IdempotencyKey) == "":
		return errors.New("idempotency key is required")
	case strings.TrimSpace(req.Target) == "":
		return errors.New("assignment target is required")
	case strings.TrimSpace(req.Prompt) == "":
		return errors.New("assignment prompt is required")
	case req.RequireReservation && len(req.RequestedPaths) == 0 && !req.AllowReservationDiscovery:
		return ErrReservationPathsRequired
	}
	for _, path := range req.RequestedPaths {
		if strings.TrimSpace(path) == "" {
			return errors.New("reservation paths cannot be empty")
		}
	}
	return nil
}

func activeAssignmentForTarget(assignments []*Assignment, beadID, occupancyKey string, pane int) *Assignment {
	occupancyKey = strings.TrimSpace(occupancyKey)
	for _, candidate := range assignments {
		if candidate == nil || candidate.BeadID == beadID {
			continue
		}
		if !assignmentOccupiesTarget(candidate, occupancyKey, pane) {
			continue
		}
		if candidate.DispatchState == DispatchSending || candidate.ClearState != ClearStateNone {
			return candidate
		}
		switch candidate.Status {
		case StatusClaiming, StatusClaimed, StatusAssigned, StatusWorking:
			return candidate
		}
	}
	return nil
}

func assignmentOccupiesTarget(candidate *Assignment, occupancyKey string, pane int) bool {
	if candidate == nil {
		return false
	}
	canonicalKey := strings.TrimSpace(candidate.OccupancyKey)
	comparisonKey := canonicalKey
	if comparisonKey == "" {
		comparisonKey = strings.TrimSpace(candidate.DispatchTarget)
	}
	if comparisonKey != "" && comparisonKey == occupancyKey {
		return true
	}
	// Rows written before canonical pane IDs were durable cannot distinguish
	// duplicate window-local indexes. Conservatively reserve every physical pane
	// with that local index until the legacy row is cleared or migrated.
	return !isPhysicalPaneOccupancyKey(canonicalKey) && candidate.Pane == pane
}

func isPhysicalPaneOccupancyKey(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[0] != '%' {
		return false
	}
	for _, ch := range value[1:] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func validateRequiredLease(req AtomicRequest, lease LeaseReceipt) error {
	expected := req.RequestedPaths
	if req.AllowReservationDiscovery {
		expected = lease.Requested
	}
	if len(expected) == 0 {
		return ErrReservationPathsRequired
	}
	granted := make(map[string]struct{}, len(lease.Granted))
	for _, path := range lease.Granted {
		granted[strings.TrimSpace(path)] = struct{}{}
	}
	missing := make([]string, 0)
	for _, path := range expected {
		path = strings.TrimSpace(path)
		if _, ok := granted[path]; !ok {
			missing = append(missing, path)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("required file reservations were not granted: %s", strings.Join(missing, ", "))
	}
	return nil
}

func validateLeaseReceipt(req ReservationRequest, lease LeaseReceipt) error {
	if strings.TrimSpace(lease.AgentName) != strings.TrimSpace(req.AgentName) {
		return fmt.Errorf("reservation receipt agent mismatch: got %q, want %q", lease.AgentName, req.AgentName)
	}
	if strings.TrimSpace(lease.Target) != strings.TrimSpace(req.Target) {
		return fmt.Errorf("reservation receipt target mismatch: got %q, want %q", lease.Target, req.Target)
	}
	return nil
}

func leaseHasReservationHandles(lease LeaseReceipt) bool {
	return len(lease.ReservationIDs) > 0 || len(lease.Granted) > 0
}

func assignmentHasReservationHandles(a *Assignment) bool {
	return a != nil && (len(a.ReservationIDs) > 0 || len(a.ReservedPaths) > 0)
}

func assignmentHasUnresolvedReservation(a *Assignment) bool {
	if a == nil {
		return false
	}
	if assignmentHasReservationHandles(a) {
		return true
	}
	switch effectiveReservationState(a) {
	case ReservationReserving, ReservationUnknown:
		return true
	case ReservationReserved:
		return !a.ReservationCompleted || strings.TrimSpace(a.ReservationError) != ""
	default:
		return false
	}
}

// ReservationOutcomeNeedsReconciliation reports whether stored handles alone
// are insufficient to prove the complete external lease set. Cleanup callers
// must enumerate the original requested paths before releasing these outcomes.
func ReservationOutcomeNeedsReconciliation(a *Assignment) bool {
	if a == nil || !a.ReservationRequired {
		return false
	}
	hasHandles := assignmentHasReservationHandles(a)
	switch effectiveReservationState(a) {
	case ReservationReleased:
		return false
	case ReservationPending:
		return a.ReservationAttempts > 0 || hasHandles
	case ReservationFailed:
		return hasHandles
	case ReservationReserved:
		return !a.ReservationCompleted || strings.TrimSpace(a.ReservationError) != ""
	case ReservationReserving, ReservationUnknown:
		return true
	default:
		return a.ReservationAttempts > 0 || hasHandles || strings.TrimSpace(a.ReservationError) != ""
	}
}

func classifyReservationFailure(lease LeaseReceipt, reservationErr error) (ReservationState, error) {
	if leaseHasReservationHandles(lease) || IsReservationReleaseRequired(reservationErr) {
		return ReservationReserved, RequireReservationRelease(reservationErr)
	}
	if IsGuaranteedNoReservation(reservationErr) {
		return ReservationFailed, reservationErr
	}
	return ReservationUnknown, errors.Join(ErrReservationOutcomeUnknown, reservationErr)
}

func classifyValidatedReservation(lease LeaseReceipt, validationErr error) (ReservationState, error) {
	if validationErr == nil {
		return ReservationReserved, nil
	}
	if leaseHasReservationHandles(lease) {
		return ReservationReserved, RequireReservationRelease(validationErr)
	}
	// A successful reservation response with no durable handles cannot leave a
	// releasable lease. Validation failures such as an empty grant are therefore
	// known, retryable failures rather than ambiguous outcomes.
	return ReservationFailed, GuaranteeNoReservation(validationErr)
}

func reservationNeedsRefresh(a *Assignment, now time.Time) bool {
	if a == nil {
		return true
	}
	if effectiveReservationState(a) == ReservationReserved {
		return reservationExpired(a, now)
	}
	return true
}

func reservationExpired(a *Assignment, now time.Time) bool {
	return a != nil && a.ReservationExpiresAt != nil && !a.ReservationExpiresAt.After(now)
}

func reservationOutcomeAmbiguous(a *Assignment) bool {
	state := effectiveReservationState(a)
	return state == ReservationReserving || state == ReservationUnknown
}

func effectiveClaimState(a *Assignment) ClaimState {
	if a == nil {
		return ClaimPending
	}
	if a.ClaimState != "" {
		return a.ClaimState
	}
	if a.ClaimedAt != nil || a.ClaimStatus != "" || a.Status == StatusClaimed || a.Status == StatusAssigned || a.Status == StatusWorking {
		return ClaimClaimed
	}
	return ClaimPending
}

func effectiveReservationState(a *Assignment) ReservationState {
	if a == nil {
		return ReservationPending
	}
	if a.ReservationState != "" {
		return a.ReservationState
	}
	if a.ReservationCompleted || len(a.ReservationIDs) > 0 || len(a.ReservedPaths) > 0 {
		return ReservationReserved
	}
	return ReservationPending
}

func matchesAtomicRawIntent(a *Assignment, req AtomicRequest) bool {
	if a == nil {
		return false
	}
	intentSHA256 := req.IntentSHA256
	if intentSHA256 == "" {
		intentSHA256 = PromptSHA256(req.Prompt)
	}
	storedIntentSHA256 := a.IntentSHA256
	if storedIntentSHA256 == "" {
		storedIntentSHA256 = a.PromptSHA256
	}
	storedOccupancyKey := a.OccupancyKey
	if storedOccupancyKey == "" {
		storedOccupancyKey = a.DispatchTarget
	}
	return a.DispatchTarget == req.Target && storedOccupancyKey == normalizeOccupancyKey(req.Target, req.OccupancyKey) && a.Pane == req.Pane &&
		a.AgentName == req.AgentName && a.AgentType == req.AgentType &&
		storedIntentSHA256 == intentSHA256 &&
		a.ReservationRequired == req.RequireReservation &&
		a.ReservationDiscovery == req.AllowReservationDiscovery &&
		stringSlicesEqual(a.ReservationInputPaths, req.RequestedPaths)
}

func matchesAtomicIntent(a *Assignment, req AtomicRequest) bool {
	return matchesAtomicRawIntent(a, req) && a.PromptSHA256 == PromptSHA256(req.Prompt)
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func leaseFromAssignment(a *Assignment) LeaseReceipt {
	if a == nil {
		return LeaseReceipt{}
	}
	return LeaseReceipt{
		AgentName:      a.ReservationAgent,
		Target:         a.ReservationTarget,
		Requested:      append([]string(nil), a.ReservationRequested...),
		Granted:        append([]string(nil), a.ReservedPaths...),
		ReservationIDs: append([]int(nil), a.ReservationIDs...),
		ExpiresAt:      cloneTimePtr(a.ReservationExpiresAt),
	}
}

// RecordAtomicIntent creates the durable transaction identity before any
// external operation. A different live idempotency key is never overwritten.
func (s *AssignmentStore) RecordAtomicIntent(req AtomicRequest, actor string, createdAt time.Time) (*Assignment, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	existing := s.Assignments[req.BeadID]
	replacementStart := req.ReplaceWorkingAssignment && existing != nil && existing.IdempotencyKey != req.IdempotencyKey
	if req.ReplaceWorkingAssignment && existing == nil {
		return nil, fmt.Errorf("%w: no durable assignment exists for %s", ErrWorkingReplacementNotAllowed, req.BeadID)
	}
	if replacementStart {
		if err := validateWorkingReplacement(existing, req); err != nil {
			return nil, err
		}
		expectedActor, err := workingReplacementClaimActor(existing)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(actor) != expectedActor {
			return nil, fmt.Errorf("%w: replacement claim actor changed from %s to %s", ErrClaimConflict, expectedActor, actor)
		}
		req.claimRequiresNonTerminal = true
	}
	if existing != nil && existing.ClearState != ClearStateNone && !replacementStart {
		return nil, fmt.Errorf("%w: %s is awaiting reservation release", ErrClaimConflict, req.BeadID)
	}
	if existing != nil && existing.IdempotencyKey != "" && existing.IdempotencyKey != req.IdempotencyKey && !replacementStart {
		switch existing.Status {
		case StatusClaiming, StatusClaimed, StatusAssigned, StatusWorking:
			return nil, fmt.Errorf("%w: %s is recorded for %s", ErrClaimConflict, req.BeadID, existing.ClaimActor)
		}
		if isTerminalAssignmentStatus(existing.Status) && assignmentHasUnresolvedReservation(existing) {
			return nil, fmt.Errorf("%w: reconcile and clear assignment %s before starting a new generation", ErrReservationReleaseRequired, req.BeadID)
		}
	}
	if existing != nil && existing.IdempotencyKey == req.IdempotencyKey {
		if existing.ClaimActor != "" && existing.ClaimActor != actor {
			return nil, fmt.Errorf("%w: idempotency key actor changed from %s to %s", ErrClaimConflict, existing.ClaimActor, actor)
		}
		if !matchesAtomicIntent(existing, req) {
			return nil, fmt.Errorf("idempotency key %s was reused for a different assignment intent", req.IdempotencyKey)
		}
		if req.claimRequiresNonTerminal && !existing.ClaimRequiresNonTerminal {
			existing.ClaimRequiresNonTerminal = true
			if err := s.saveLocked(); err != nil {
				return nil, err
			}
		}
		return cloneAssignment(existing), nil
	}

	assignment := &Assignment{
		BeadID:                   req.BeadID,
		BeadTitle:                req.BeadTitle,
		Pane:                     req.Pane,
		AgentType:                req.AgentType,
		AgentName:                req.AgentName,
		Status:                   StatusClaiming,
		AssignedAt:               createdAt,
		IdempotencyKey:           req.IdempotencyKey,
		ClaimActor:               actor,
		ClaimState:               ClaimPending,
		ClaimRequiresNonTerminal: req.claimRequiresNonTerminal,
		ReservationRequired:      req.RequireReservation,
		ReservationDiscovery:     req.AllowReservationDiscovery,
		ReservationInputPaths:    append([]string(nil), req.RequestedPaths...),
		ReservationState:         ReservationPending,
		DispatchState:            DispatchPending,
		DispatchTarget:           req.Target,
		OccupancyKey:             normalizeOccupancyKey(req.Target, req.OccupancyKey),
		PromptSHA256:             PromptSHA256(req.Prompt),
		IntentSHA256:             req.IntentSHA256,
		PendingPrompt:            req.Prompt,
	}
	previous := existing
	if replacementStart {
		assignment.RetryCount = existing.RetryCount
	}
	s.Assignments[req.BeadID] = assignment
	if previous != nil {
		if s.replace == nil {
			s.replace = make(map[string]struct{})
		}
		s.replace[req.BeadID] = struct{}{}
	}
	if err := s.saveLocked(); err != nil {
		var concurrentMutation *ConcurrentMutationError
		if !errors.As(err, &concurrentMutation) {
			if previous == nil {
				delete(s.Assignments, req.BeadID)
			} else {
				s.Assignments[req.BeadID] = previous
			}
			delete(s.replace, req.BeadID)
		}
		return nil, err
	}
	return cloneAssignment(assignment), nil
}

// RecordAtomicClaimStarted writes the ambiguity barrier immediately before
// calling the external tracker.
func (s *AssignmentStore) RecordAtomicClaimStarted(beadID, idempotencyKey string, startedAt time.Time) (*Assignment, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	a, err := s.atomicAssignmentLocked(beadID, idempotencyKey)
	if err != nil {
		return nil, err
	}
	if effectiveClaimState(a) == ClaimClaimed {
		return cloneAssignment(a), nil
	}
	a.Status = StatusClaiming
	a.ClaimState = ClaimClaiming
	a.ClaimAttempts++
	a.ClaimStartedAt = cloneTimePtr(&startedAt)
	a.ClaimError = ""
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return cloneAssignment(a), nil
}

// RecordAtomicClaim durably records tracker ownership before reservations or
// delivery.
func (s *AssignmentStore) RecordAtomicClaim(req AtomicRequest, claim ClaimReceipt) (*Assignment, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	existing := s.Assignments[req.BeadID]
	if existing == nil {
		return nil, fmt.Errorf("[ASSIGN] Atomic intent not found: %s", req.BeadID)
	}
	if existing.IdempotencyKey != req.IdempotencyKey {
		return nil, fmt.Errorf("%w: idempotency key does not own %s", ErrClaimConflict, req.BeadID)
	}
	if existing.ClaimActor != "" && existing.ClaimActor != claim.Actor {
		return nil, fmt.Errorf("%w: idempotency key actor changed from %s to %s", ErrClaimConflict, existing.ClaimActor, claim.Actor)
	}
	if !matchesAtomicIntent(existing, req) {
		return nil, fmt.Errorf("idempotency key %s was reused for a different assignment intent", req.IdempotencyKey)
	}
	existing.Status = StatusClaimed
	existing.ClaimActor = claim.Actor
	existing.ClaimState = ClaimClaimed
	existing.ClaimStatus = claim.Status
	existing.ClaimError = ""
	existing.ClaimedAt = cloneTimePtr(&claim.ClaimedAt)
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return cloneAssignment(existing), nil
}

// RecordAtomicClaimUncertain records a returned claim error without erasing
// the pre-call barrier. Conflict is a known failure; all other errors remain
// unknown until reconciled.
func (s *AssignmentStore) RecordAtomicClaimUncertain(beadID, idempotencyKey string, claimErr error) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	a, err := s.atomicAssignmentLocked(beadID, idempotencyKey)
	if err != nil {
		return err
	}
	a.ClaimState = ClaimUnknown
	if errors.Is(claimErr, ErrClaimConflict) {
		a.ClaimState = ClaimFailed
		a.Status = StatusFailed
		failedAt := time.Now().UTC()
		a.FailedAt = &failedAt
	}
	a.ClaimError = ""
	if claimErr != nil {
		a.ClaimError = claimErr.Error()
	}
	return s.saveLocked()
}

// RecordAtomicReservationStarted writes the ambiguity barrier immediately
// before calling the external reservation service.
func (s *AssignmentStore) RecordAtomicReservationStarted(beadID, idempotencyKey string, startedAt time.Time) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	a, err := s.atomicAssignmentLocked(beadID, idempotencyKey)
	if err != nil {
		return err
	}
	a.ReservationState = ReservationReserving
	a.ReservationAttempts++
	a.ReservationStartedAt = cloneTimePtr(&startedAt)
	a.ReservationError = ""
	return s.saveLocked()
}

// RecordAtomicReservation persists Agent Mail handoff metadata even when the
// reservation call partially succeeds and returns an error.
func (s *AssignmentStore) RecordAtomicReservation(beadID, idempotencyKey string, state ReservationState, lease LeaseReceipt, reservationErr error) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	a, err := s.atomicAssignmentLocked(beadID, idempotencyKey)
	if err != nil {
		return err
	}
	if state == ReservationFailed && leaseHasReservationHandles(lease) {
		state = ReservationReserved
		reservationErr = RequireReservationRelease(reservationErr)
	}
	if state == ReservationReserved && reservationErr != nil {
		if leaseHasReservationHandles(lease) || IsReservationReleaseRequired(reservationErr) {
			reservationErr = RequireReservationRelease(reservationErr)
		} else {
			state = ReservationFailed
			reservationErr = GuaranteeNoReservation(reservationErr)
		}
	}
	if state == ReservationUnknown && reservationErr == nil {
		reservationErr = ErrReservationOutcomeUnknown
	}
	a.ReservationAgent = lease.AgentName
	a.ReservationTarget = lease.Target
	a.ReservationRequested = append([]string(nil), lease.Requested...)
	a.ReservedPaths = append([]string(nil), lease.Granted...)
	a.ReservationIDs = append([]int(nil), lease.ReservationIDs...)
	a.ReservationExpiresAt = cloneTimePtr(lease.ExpiresAt)
	a.ReservationState = state
	a.ReservationCompleted = state == ReservationReserved && reservationErr == nil
	a.ReservationError = ""
	if reservationErr != nil {
		a.ReservationError = reservationErr.Error()
	}
	return s.saveLocked()
}

// RecordAtomicDispatchStarted writes the ambiguity barrier before transport.
func (s *AssignmentStore) RecordAtomicDispatchStarted(beadID, idempotencyKey string, startedAt time.Time) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	a, err := s.atomicAssignmentLocked(beadID, idempotencyKey)
	if err != nil {
		return err
	}
	if a.DispatchState == DispatchSent {
		return nil
	}
	if a.DispatchState == DispatchSending {
		return ErrDispatchOutcomeUnknown
	}
	a.DispatchState = DispatchSending
	a.DispatchAttempts++
	a.DispatchStartedAt = cloneTimePtr(&startedAt)
	a.LastDispatchError = ""
	return s.saveLocked()
}

// RecordAtomicDispatchFailed records a known transport failure as retryable.
func (s *AssignmentStore) RecordAtomicDispatchFailed(beadID, idempotencyKey string, dispatchErr error) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	a, err := s.atomicAssignmentLocked(beadID, idempotencyKey)
	if err != nil {
		return err
	}
	a.DispatchState = DispatchPending
	a.LastDispatchError = ""
	if dispatchErr != nil {
		a.LastDispatchError = dispatchErr.Error()
	}
	return s.saveLocked()
}

// RecordAtomicDispatchSent commits the final delivery receipt and exposes the
// assignment to existing assigned/working consumers.
func (s *AssignmentStore) RecordAtomicDispatchSent(beadID, idempotencyKey, prompt string, receipt DispatchReceipt, dispatchedAt time.Time) error {
	s.mutex.Lock()
	a, err := s.atomicAssignmentLocked(beadID, idempotencyKey)
	if err != nil {
		s.mutex.Unlock()
		return err
	}
	a.Status = StatusAssigned
	a.PromptSent = prompt
	a.PendingPrompt = ""
	a.DispatchState = DispatchSent
	a.DispatchedAt = cloneTimePtr(&dispatchedAt)
	a.DispatchReceiptID = receipt.DeliveryID
	a.DispatchDuration = receipt.Duration
	a.LastDispatchError = ""
	if err := s.saveLocked(); err != nil {
		s.mutex.Unlock()
		return err
	}
	cloned := cloneAssignment(a)
	s.mutex.Unlock()

	emitAtomicAssignmentEvent(s.SessionName, cloned)
	return nil
}

func (s *AssignmentStore) atomicAssignmentLocked(beadID, idempotencyKey string) (*Assignment, error) {
	a := s.Assignments[beadID]
	if a == nil {
		return nil, fmt.Errorf("[ASSIGN] Assignment not found: %s", beadID)
	}
	if a.IdempotencyKey != idempotencyKey {
		return nil, fmt.Errorf("%w: idempotency key does not own %s", ErrClaimConflict, beadID)
	}
	return a, nil
}

func emitAtomicAssignmentEvent(session string, a *Assignment) {
	if a == nil {
		return
	}
	events.DefaultEmitter().Emit(events.NewWebhookEvent(
		events.WebhookBeadAssigned,
		session,
		fmt.Sprintf("%d", a.Pane),
		a.AgentType,
		fmt.Sprintf("Bead assigned: %s", a.BeadID),
		map[string]string{
			"bead_id":         a.BeadID,
			"bead_title":      a.BeadTitle,
			"pane_index":      fmt.Sprintf("%d", a.Pane),
			"agent_type":      a.AgentType,
			"agent_name":      a.AgentName,
			"claim_actor":     a.ClaimActor,
			"idempotency_key": a.IdempotencyKey,
		},
	))
}
