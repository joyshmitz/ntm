package robot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/agentmail"
	"github.com/Dicklesworthstone/ntm/internal/assignment"
	"github.com/Dicklesworthstone/ntm/internal/bv"
	"github.com/Dicklesworthstone/ntm/internal/config"
	dispatchsvc "github.com/Dicklesworthstone/ntm/internal/dispatch"
	"github.com/Dicklesworthstone/ntm/internal/redaction"
	statuspkg "github.com/Dicklesworthstone/ntm/internal/status"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/internal/util"
)

const defaultBulkAssignTemplate = "Read AGENTS.md, register with Agent Mail. Work on: {bead_id} - {bead_title}.\nUse br show {bead_id} for details. Mark in_progress when starting. Use ultrathink."

// BulkAssignOptions configures --robot-bulk-assign behavior.
type BulkAssignOptions struct {
	Session            string
	FromBV             bool
	Strategy           string
	AllocationJSON     string
	DryRun             bool
	Parallel           bool
	Stagger            time.Duration
	RequireReservation bool
	ReservationPaths   []string
	// SkipPaneSelectors accepts canonical N, W.P, and %N pane selectors.
	SkipPaneSelectors  []string
	PromptTemplatePath string
	// DefaultTemplatePath is a project/user-level configured template file
	// (cfg.Assign.PromptTemplateFile). It is used when PromptTemplatePath is
	// empty, and overrides DefaultTemplate and the built-in const.
	DefaultTemplatePath string
	// DefaultTemplate is an inline project/user-level configured template
	// (cfg.Assign.PromptTemplate). It is used when neither PromptTemplatePath
	// nor DefaultTemplatePath resolves to content, and overrides the built-in const.
	DefaultTemplate string
	Deps            *BulkAssignDependencies
	projectDir      string
}

// BulkAssignDependencies allows tests to stub external interactions.
type BulkAssignDependencies struct {
	FetchTriage       func(context.Context, string) (*bv.TriageResponse, error)
	FetchInProgress   func(context.Context, string, int) ([]bv.BeadInProgress, error)
	ListPanes         func(context.Context, string) ([]tmux.Pane, error)
	ReadFile          func(path string) ([]byte, error)
	FetchBeadTitle    func(context.Context, string, string) (string, error)
	FetchBeadDetails  func(context.Context, string, string) (BeadDetails, error)
	Cwd               func() (string, error)
	PaneCurrentPath   func(context.Context, string) (string, error)
	ResolveProject    func(context.Context, string, []tmux.Pane) (string, error)
	LoadStore         func(session string) (*assignment.AssignmentStore, error)
	ClaimBead         func(context.Context, string, string, string) (bv.BeadClaimResult, error)
	GetBeadStatus     func(context.Context, string, string) (string, error)
	NewIdempotencyKey func() (string, error)
	ReservationPort   assignment.ReservationPort
	ResolveAgentName  func(context.Context, string, string, string, string) (string, error)
	ObserveSession    func(context.Context, string) (statuspkg.SessionObservation, error)
	DispatchDeliverer dispatchsvc.Deliverer
	DispatchPacer     dispatchsvc.Pacer
	LoadRedaction     func(dir string) (redaction.Config, error)
	Wait              func(context.Context, time.Duration) error
}

// BeadDetails captures metadata used for bulk prompt templating.
type BeadDetails struct {
	Title        string
	Type         string
	Dependencies []string
}

// BulkAssignOutput is the structured output for --robot-bulk-assign.
type BulkAssignOutput struct {
	RobotResponse
	Session          string                 `json:"session"`
	Strategy         string                 `json:"strategy"`
	Assignments      []BulkAssignAssignment `json:"assignments"`
	Summary          BulkAssignSummary      `json:"summary"`
	UnassignedBeads  []string               `json:"unassigned_beads,omitempty"`
	UnassignedPanes  []string               `json:"unassigned_panes,omitempty"`
	DryRun           bool                   `json:"dry_run,omitempty"`
	AllocationSource string                 `json:"allocation_source,omitempty"`
}

// BulkAssignAssignment is a single pane-to-bead allocation.
type BulkAssignAssignment struct {
	Pane              string `json:"pane"`
	PaneID            string `json:"pane_id"`
	Bead              string `json:"bead"`
	BeadTitle         string `json:"bead_title"`
	Reason            string `json:"reason"`
	AgentType         string `json:"agent_type"`
	Status            string `json:"status"`
	PromptSent        bool   `json:"prompt_sent"`
	Claimed           bool   `json:"claimed"`
	ClaimActor        string `json:"claim_actor,omitempty"`
	IdempotencyKey    string `json:"idempotency_key,omitempty"`
	DispatchReceiptID string `json:"dispatch_receipt_id,omitempty"`
	ReservationIDs    []int  `json:"reservation_ids,omitempty"`
	Error             string `json:"error,omitempty"`
	paneIndex         int
	paneTitle         string
	failureCause      error
	failureCode       string
}

// BulkAssignSummary aggregates assignment stats.
type BulkAssignSummary struct {
	TotalPanes int `json:"total_panes"`
	Assigned   int `json:"assigned"`
	Skipped    int `json:"skipped"`
	Failed     int `json:"failed"`
}

type bulkBeadSource string

const (
	bulkSourceImpact bulkBeadSource = "impact"
	bulkSourceReady  bulkBeadSource = "ready"
	bulkSourceStale  bulkBeadSource = "stale"
)

type bulkBead struct {
	ID            string
	Title         string
	Priority      int
	UnblocksCount int
	Status        string
	UpdatedAt     time.Time
	Source        bulkBeadSource
}

type bulkPane struct {
	Ref       tmux.PaneRef
	AgentType string
	Title     string
}

// GetBulkAssign generates the bulk assignment plan and returns the result.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetBulkAssign(ctx context.Context, opts BulkAssignOptions) (*BulkAssignOutput, error) {
	output := &BulkAssignOutput{
		RobotResponse:   NewRobotResponse(true),
		Session:         opts.Session,
		Assignments:     []BulkAssignAssignment{},
		UnassignedBeads: []string{},
		UnassignedPanes: []string{},
		DryRun:          opts.DryRun,
	}
	if ctx == nil {
		return nil, errors.New("robot bulk assignment context is required")
	}
	if err := ctx.Err(); err != nil {
		setBulkAssignCancellation(output, err)
		return output, nil
	}

	if opts.Session == "" {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("session name is required"),
			ErrCodeInvalidFlag,
			"Provide session name: ntm --robot-bulk-assign=myproject",
		)
		return output, nil
	}

	deps := bulkAssignDeps(opts.Deps)
	strategy, err := normalizeBulkAssignStrategy(opts.Strategy)
	if err != nil {
		output.RobotResponse = NewErrorResponse(err, ErrCodeInvalidFlag, "Use impact, ready, stale, or balanced")
		return output, nil
	}
	opts.Strategy = strategy
	output.Strategy = strategy

	panes, err := deps.ListPanes(ctx, opts.Session)
	if err != nil {
		errorCode, hint := bulkAssignPaneListError(err)
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("failed to get panes: %w", err),
			errorCode,
			hint,
		)
		return output, nil
	}
	if err := ctx.Err(); err != nil {
		setBulkAssignCancellation(output, err)
		return output, nil
	}

	paneList, err := filterBulkAssignPanes(panes, opts.SkipPaneSelectors)
	if err != nil {
		output.RobotResponse = NewErrorResponse(err, ErrCodeInvalidFlag, "Use N, W.P, or %N pane selectors; bare N must be unambiguous")
		return output, nil
	}
	projectDir, err := deps.ResolveProject(ctx, opts.Session, panes)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			setBulkAssignCancellation(output, err)
			return output, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			setBulkAssignCancellation(output, ctxErr)
			return output, nil
		}
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("failed to resolve session project: %w", err),
			ErrCodeInternalError,
			"Ensure the session has a saved or configured project directory",
		)
		return output, nil
	}
	opts.projectDir = projectDir

	if opts.AllocationJSON != "" {
		allocation, err := parseBulkAssignAllocation(opts.AllocationJSON)
		if err != nil {
			output.RobotResponse = NewErrorResponse(err, ErrCodeInvalidFlag, "Provide valid JSON mapping pane->bead")
			return output, nil
		}
		plan := planBulkAssignFromAllocation(ctx, opts, deps, paneList, allocation)
		output.AllocationSource = "explicit"
		applyBulkAssignPlan(ctx, opts, deps, output, plan)
		if err := ctx.Err(); err != nil {
			setBulkAssignCancellation(output, err)
		}
		return output, nil
	}

	if !opts.FromBV {
		output.RobotResponse = NewErrorResponse(
			errors.New("either --from-bv or --allocation is required"),
			ErrCodeInvalidFlag,
			"Use --from-bv or provide --allocation JSON",
		)
		return output, nil
	}

	triage, err := deps.FetchTriage(ctx, opts.projectDir)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			setBulkAssignCancellation(output, err)
			return output, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			setBulkAssignCancellation(output, ctxErr)
			return output, nil
		}
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("bv triage failed: %w", err),
			ErrCodeInternalError,
			"Ensure bv is installed and .beads exists",
		)
		return output, nil
	}
	if err := ctx.Err(); err != nil {
		setBulkAssignCancellation(output, err)
		return output, nil
	}

	inProgress, err := deps.FetchInProgress(ctx, opts.projectDir, 200)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			setBulkAssignCancellation(output, err)
			return output, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			setBulkAssignCancellation(output, ctxErr)
			return output, nil
		}
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("fetch in-progress failed: %w", err),
			ErrCodeInternalError,
			"Ensure br/bd is available for in-progress beads",
		)
		return output, nil
	}
	if err := ctx.Err(); err != nil {
		setBulkAssignCancellation(output, err)
		return output, nil
	}

	plan := planBulkAssignFromBV(opts, deps, paneList, triage, inProgress)
	output.AllocationSource = "bv"
	applyBulkAssignPlan(ctx, opts, deps, output, plan)
	if err := ctx.Err(); err != nil {
		setBulkAssignCancellation(output, err)
	}
	return output, nil
}

func setBulkAssignCancellation(output *BulkAssignOutput, err error) {
	if output == nil || err == nil {
		return
	}
	output.RobotResponse = NewErrorResponse(err, ErrCodeTimeout, "Retry the command after cancellation")
}

func bulkAssignPaneListError(err error) (string, string) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrCodeTimeout, "Retry after confirming the tmux server is responsive"
	}
	switch tmux.ClassifyCommandError(err).Kind {
	case tmux.CommandErrorSessionNotFound, tmux.CommandErrorPaneNotFound:
		// list-panes is session-scoped. tmux commonly reports a missing session as
		// "can't find window", but there is no pane selector at this boundary.
		return ErrCodeSessionNotFound, "Use 'ntm list' to see available sessions"
	case tmux.CommandErrorTimeout, tmux.CommandErrorCanceled:
		return ErrCodeTimeout, "Retry after confirming the tmux server is responsive"
	default:
		return ErrCodeInternalError, "Check tmux is running and session is accessible"
	}
}

// PrintBulkAssign handles the --robot-bulk-assign command.
// This is a thin wrapper around GetBulkAssign() for CLI output.
func PrintBulkAssign(ctx context.Context, opts BulkAssignOptions) error {
	output, err := GetBulkAssign(ctx, opts)
	if err != nil {
		return err
	}
	return encodeTerminalRobotOutput(output, output.RobotResponse, "robot bulk assignment failed")
}

func bulkAssignDeps(custom *BulkAssignDependencies) BulkAssignDependencies {
	observer := statuspkg.NewSessionObserver(statuspkg.NewDetector())
	deps := BulkAssignDependencies{
		FetchTriage:      bv.GetTriageContext,
		FetchInProgress:  bv.GetInProgressListContext,
		ListPanes:        tmux.GetPanesContext,
		ReadFile:         os.ReadFile,
		FetchBeadTitle:   fetchBeadTitle,
		FetchBeadDetails: fetchBeadDetails,
		Cwd:              os.Getwd,
		PaneCurrentPath: func(ctx context.Context, paneID string) (string, error) {
			return tmux.DefaultClient.RunContext(ctx, "display-message", "-p", "-t", paneID, "#{pane_current_path}")
		},
		LoadStore:         assignment.LoadStoreStrict,
		ClaimBead:         bv.ClaimBeadForAssignment,
		GetBeadStatus:     bv.GetBeadStatusContext,
		NewIdempotencyKey: assignment.NewAssignmentIdempotencyKey,
		ObserveSession:    observer.Observe,
		DispatchDeliverer: dispatchsvc.TMUXDeliverer{},
		LoadRedaction: func(dir string) (redaction.Config, error) {
			loaded, err := config.LoadMerged(dir, config.DefaultPath())
			if err != nil {
				return redaction.Config{}, err
			}
			return loaded.Redaction.ToRedactionLibConfig(), nil
		},
		Wait: func(ctx context.Context, delay time.Duration) error {
			if delay <= 0 {
				return nil
			}
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
	deps.ResolveProject = func(ctx context.Context, session string, panes []tmux.Pane) (string, error) {
		return resolveBulkAssignmentProject(ctx, session, panes, deps.PaneCurrentPath, deps.Cwd)
	}

	if custom == nil {
		return deps
	}
	if custom.FetchTriage != nil {
		deps.FetchTriage = custom.FetchTriage
	}
	if custom.FetchInProgress != nil {
		deps.FetchInProgress = custom.FetchInProgress
	}
	if custom.ListPanes != nil {
		deps.ListPanes = custom.ListPanes
	}
	if custom.ReadFile != nil {
		deps.ReadFile = custom.ReadFile
	}
	if custom.FetchBeadTitle != nil {
		deps.FetchBeadTitle = custom.FetchBeadTitle
	}
	if custom.FetchBeadDetails != nil {
		deps.FetchBeadDetails = custom.FetchBeadDetails
	}
	if custom.Cwd != nil {
		deps.Cwd = custom.Cwd
	}
	if custom.PaneCurrentPath != nil {
		deps.PaneCurrentPath = custom.PaneCurrentPath
	}
	if custom.ResolveProject != nil {
		deps.ResolveProject = custom.ResolveProject
	} else if custom.Cwd != nil && custom.PaneCurrentPath == nil {
		deps.ResolveProject = func(ctx context.Context, _ string, _ []tmux.Pane) (string, error) {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return custom.Cwd()
		}
	}
	if custom.LoadStore != nil {
		deps.LoadStore = custom.LoadStore
	}
	if custom.ClaimBead != nil {
		deps.ClaimBead = custom.ClaimBead
	}
	if custom.GetBeadStatus != nil {
		deps.GetBeadStatus = custom.GetBeadStatus
	}
	if custom.NewIdempotencyKey != nil {
		deps.NewIdempotencyKey = custom.NewIdempotencyKey
	}
	if custom.ReservationPort != nil {
		deps.ReservationPort = custom.ReservationPort
	}
	if custom.ResolveAgentName != nil {
		deps.ResolveAgentName = custom.ResolveAgentName
	}
	if custom.ObserveSession != nil {
		deps.ObserveSession = custom.ObserveSession
	}
	if custom.DispatchDeliverer != nil {
		deps.DispatchDeliverer = custom.DispatchDeliverer
	}
	if custom.DispatchPacer != nil {
		deps.DispatchPacer = custom.DispatchPacer
	}
	if custom.LoadRedaction != nil {
		deps.LoadRedaction = custom.LoadRedaction
	}
	if custom.Wait != nil {
		deps.Wait = custom.Wait
	}
	return deps
}

func resolveBulkAssignmentProject(
	ctx context.Context,
	session string,
	panes []tmux.Pane,
	paneCurrentPath func(context.Context, string) (string, error),
	cwd func() (string, error),
) (string, error) {
	if ctx == nil {
		return "", errors.New("bulk assignment project context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	session = strings.TrimSpace(session)
	if session == "" {
		return "", errors.New("session is required")
	}
	if err := tmux.ValidateSessionName(session); err != nil {
		return "", err
	}
	if len(panes) > 0 {
		projectDir, err := ResolveLiveSessionProjectContext(ctx, session, panes, paneCurrentPath)
		if err != nil {
			return "", err
		}
		if projectDir != "" {
			return projectDir, nil
		}
	}

	usable := func(candidate string) string {
		if util.ProjectDirScore(candidate) <= 0 {
			return ""
		}
		return util.ResolveProjectDir(candidate)
	}
	var lookupErrors []error
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if registry, err := agentmail.LoadBestSessionAgentRegistry(session); err != nil {
		lookupErrors = append(lookupErrors, fmt.Errorf("load session agent registry: %w", err))
	} else if registry != nil {
		if projectDir := usable(registry.ProjectKey); projectDir != "" {
			return projectDir, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if info, err := agentmail.LoadBestSessionAgent(session); err != nil {
		lookupErrors = append(lookupErrors, fmt.Errorf("load session agent identity: %w", err))
	} else if info != nil {
		if projectDir := usable(info.ProjectKey); projectDir != "" {
			return projectDir, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if cfg, err := config.Load(config.DefaultPath()); err != nil {
		lookupErrors = append(lookupErrors, fmt.Errorf("load configuration: %w", err))
	} else if cfg != nil {
		if projectDir := usable(cfg.GetProjectDir(session)); projectDir != "" {
			return projectDir, nil
		}
	}
	if cwd != nil {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if currentDir, err := cwd(); err != nil {
			lookupErrors = append(lookupErrors, fmt.Errorf("resolve caller working directory: %w", err))
		} else if projectDir := usable(currentDir); projectDir != "" {
			return projectDir, nil
		}
	}
	if err := errors.Join(lookupErrors...); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no usable project directory found for session %q", session)
}

// ResolveLiveSessionProject derives one authoritative project root from every
// live pane in a tmux session and fails closed when the panes disagree.
func ResolveLiveSessionProject(session string, panes []tmux.Pane, paneCurrentPath func(string) (string, error)) (string, error) {
	if paneCurrentPath == nil {
		return ResolveLiveSessionProjectContext(context.Background(), session, panes, nil)
	}
	return ResolveLiveSessionProjectContext(context.Background(), session, panes, func(_ context.Context, paneID string) (string, error) {
		return paneCurrentPath(paneID)
	})
}

// ResolveLiveSessionProjectContext derives one authoritative project root
// while honoring cancellation of every pane current-directory lookup.
func ResolveLiveSessionProjectContext(ctx context.Context, session string, panes []tmux.Pane, paneCurrentPath func(context.Context, string) (string, error)) (string, error) {
	if ctx == nil {
		return "", errors.New("live session project context is required")
	}
	if paneCurrentPath == nil {
		return "", fmt.Errorf("resolve live project for session %q: pane current-path lookup is not configured", session)
	}
	roots := make(map[string][]string)
	for _, pane := range panes {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		paneID := strings.TrimSpace(pane.ID)
		if paneID == "" {
			return "", fmt.Errorf("resolve live project for session %q: pane has no stable ID", session)
		}
		currentPath, err := paneCurrentPath(ctx, paneID)
		if err != nil {
			return "", fmt.Errorf("resolve live project for session %q pane %s: %w", session, paneID, err)
		}
		currentPath = strings.TrimSpace(currentPath)
		if !filepath.IsAbs(currentPath) {
			return "", fmt.Errorf("resolve live project for session %q pane %s: current path %q is not absolute", session, paneID, currentPath)
		}
		projectDir := util.ResolveProjectDir(currentPath)
		if util.ProjectDirScore(projectDir) <= 0 {
			return "", fmt.Errorf("resolve live project for session %q pane %s: current path %q is not usable", session, paneID, currentPath)
		}
		projectDir = filepath.Clean(projectDir)
		roots[projectDir] = append(roots[projectDir], paneID)
	}
	if len(roots) == 1 {
		for projectDir := range roots {
			return projectDir, nil
		}
	}

	rootNames := make([]string, 0, len(roots))
	for projectDir := range roots {
		rootNames = append(rootNames, projectDir)
	}
	sort.Strings(rootNames)
	parts := make([]string, 0, len(rootNames))
	for _, projectDir := range rootNames {
		paneIDs := append([]string(nil), roots[projectDir]...)
		sort.Strings(paneIDs)
		parts = append(parts, fmt.Sprintf("%s (%s)", projectDir, strings.Join(paneIDs, ",")))
	}
	return "", fmt.Errorf("resolve live project for session %q: panes span multiple project roots: %s", session, strings.Join(parts, "; "))
}

func normalizeBulkAssignStrategy(strategy string) (string, error) {
	strategy = strings.ToLower(strings.TrimSpace(strategy))
	if strategy == "" {
		return "impact", nil
	}
	switch strategy {
	case "impact", "ready", "stale", "balanced":
		return strategy, nil
	default:
		return "", fmt.Errorf("invalid bulk assignment strategy %q", strategy)
	}
}

func filterBulkAssignPanes(panes []tmux.Pane, skipSelectors []string) ([]bulkPane, error) {
	ordered := tmux.SortPanesByTopology(panes)
	skipSet := make(map[string]struct{}, len(skipSelectors))
	for _, selector := range skipSelectors {
		resolved, err := tmux.ResolvePaneSelectors(ordered, []string{selector}, true)
		if err != nil {
			return nil, fmt.Errorf("resolve skipped pane %q: %w", selector, err)
		}
		skipSet[resolved[0].Ref().StableKey()] = struct{}{}
	}

	filtered := make([]bulkPane, 0, len(ordered))
	for _, pane := range ordered {
		if _, skipped := skipSet[pane.Ref().StableKey()]; skipped {
			continue
		}
		agentType := paneAgentType(pane)
		if agentType == "unknown" || agentType == "user" {
			continue
		}
		filtered = append(filtered, bulkPane{
			Ref:       pane.Ref(),
			AgentType: agentType,
			Title:     pane.Title,
		})
	}
	return filtered, nil
}

func planBulkAssignFromBV(opts BulkAssignOptions, deps BulkAssignDependencies, panes []bulkPane, triage *bv.TriageResponse, inProgress []bv.BeadInProgress) bulkAssignPlan {
	candidates := buildBulkAssignCandidates(triage, inProgress)
	beads := selectBulkAssignBeads(opts.Strategy, candidates)
	return allocateBulkAssignBeads(panes, beads)
}

func planBulkAssignFromAllocation(ctx context.Context, opts BulkAssignOptions, deps BulkAssignDependencies, panes []bulkPane, allocation map[string]string) bulkAssignPlan {
	paneSet := make(map[string]bulkPane, len(panes))
	for _, pane := range panes {
		paneSet[pane.Ref.StableKey()] = pane
	}
	tmuxPanes := make([]tmux.Pane, 0, len(panes))
	for _, pane := range panes {
		tmuxPanes = append(tmuxPanes, tmux.Pane{
			ID: pane.Ref.ID, WindowIndex: pane.Ref.WindowIndex, Index: pane.Ref.PaneIndex,
			NTMIndex: pane.Ref.NTMIndex, Title: pane.Title, Type: bulkAssignTMUXAgentType(pane.AgentType),
		})
	}
	multiWindow := tmux.PanesSpanMultipleWindows(tmuxPanes)

	plan := bulkAssignPlan{}
	selectors := make([]string, 0, len(allocation))
	for selector := range allocation {
		selectors = append(selectors, selector)
	}
	sort.Strings(selectors)
	allocated := make(map[string]struct{}, len(selectors))
	assignmentByPane := make(map[string]int, len(selectors))
	selectorByPane := make(map[string]string, len(selectors))
	beadByPane := make(map[string]string, len(selectors))
	for _, selector := range selectors {
		beadID := allocation[selector]
		resolved, resolveErr := tmux.ResolvePaneSelectors(tmuxPanes, []string{selector}, true)
		var pane bulkPane
		stableKey := ""
		if resolveErr == nil {
			pane = paneSet[resolved[0].Ref().StableKey()]
			stableKey = pane.Ref.StableKey()
			allocated[stableKey] = struct{}{}
		}
		assignment := BulkAssignAssignment{
			Pane:      strings.TrimSpace(selector),
			Bead:      beadID,
			AgentType: "unknown",
			Status:    "planned",
		}
		if resolveErr != nil {
			failBulkAssignment(&assignment, resolveErr, ErrCodePaneNotFound)
			plan.Assignments = append(plan.Assignments, assignment)
			plan.failed++
			continue
		}
		assignment.Pane = pane.Ref.Canonical(multiWindow)
		assignment.PaneID = pane.Ref.ID
		assignment.paneIndex = pane.Ref.PaneIndex
		assignment.paneTitle = pane.Title
		assignment.AgentType = pane.AgentType
		assignment.Reason = "explicit"

		if existingIndex, duplicate := assignmentByPane[stableKey]; duplicate {
			if beadByPane[stableKey] == beadID {
				// W.P and %N may both name the same physical pane. Identical
				// allocations are one intent and must produce one claim/send.
				continue
			}
			conflict := fmt.Sprintf(
				"pane selectors %q and %q resolve to the same physical pane %s but assign different beads %q and %q",
				selectorByPane[stableKey], selector, pane.Ref.Physical(), beadByPane[stableKey], beadID,
			)
			if plan.Assignments[existingIndex].Status != "failed" {
				plan.failed++
			}
			conflictErr := errors.New(conflict)
			failBulkAssignment(&plan.Assignments[existingIndex], conflictErr, ErrCodeInvalidFlag)
			failBulkAssignment(&assignment, conflictErr, ErrCodeInvalidFlag)
			plan.Assignments = append(plan.Assignments, assignment)
			plan.failed++
			continue
		}
		assignmentByPane[stableKey] = len(plan.Assignments)
		selectorByPane[stableKey] = selector
		beadByPane[stableKey] = beadID

		if beadID == "" {
			failBulkAssignment(&assignment, errors.New("empty bead id"), ErrCodeInvalidFlag)
			plan.Assignments = append(plan.Assignments, assignment)
			plan.failed++
			continue
		}

		title, err := deps.FetchBeadTitle(ctx, opts.projectDir, beadID)
		if err != nil {
			failBulkAssignment(&assignment, err, "")
		} else {
			assignment.BeadTitle = title
		}
		plan.Assignments = append(plan.Assignments, assignment)
	}

	for _, pane := range panes {
		if _, ok := allocated[pane.Ref.StableKey()]; !ok {
			plan.UnassignedPanes = append(plan.UnassignedPanes, pane.Ref.Canonical(multiWindow))
		}
	}

	sort.Slice(plan.Assignments, func(i, j int) bool {
		return plan.Assignments[i].Pane < plan.Assignments[j].Pane
	})
	sort.Strings(plan.UnassignedPanes)

	return plan
}

func failBulkAssignment(output *BulkAssignAssignment, err error, code string) {
	if output == nil || err == nil {
		return
	}
	output.Status = "failed"
	output.PromptSent = false
	output.Error = err.Error()
	output.failureCause = err
	output.failureCode = code
}

type bulkAssignPlan struct {
	Assignments     []BulkAssignAssignment
	UnassignedBeads []string
	UnassignedPanes []string
	assigned        int
	failed          int
	skipped         int
}

func buildBulkAssignCandidates(triage *bv.TriageResponse, inProgress []bv.BeadInProgress) bulkAssignCandidates {
	candidates := bulkAssignCandidates{}
	if triage != nil {
		for _, blocker := range triage.Triage.BlockersToClear {
			candidates.impact = append(candidates.impact, bulkBead{
				ID:            blocker.ID,
				Title:         blocker.Title,
				UnblocksCount: blocker.UnblocksCount,
				Source:        bulkSourceImpact,
			})
		}

		for _, rec := range triage.Triage.Recommendations {
			priority := rec.Priority
			if priority < 0 {
				priority = 0
			}
			candidates.ready = append(candidates.ready, bulkBead{
				ID:            rec.ID,
				Title:         rec.Title,
				Priority:      priority,
				Status:        strings.ToLower(rec.Status),
				UnblocksCount: len(rec.UnblocksIDs),
				Source:        bulkSourceReady,
			})
		}
	}

	for _, item := range inProgress {
		candidates.stale = append(candidates.stale, bulkBead{
			ID:        item.ID,
			Title:     item.Title,
			UpdatedAt: item.UpdatedAt,
			Source:    bulkSourceStale,
		})
	}

	return candidates
}

type bulkAssignCandidates struct {
	impact []bulkBead
	ready  []bulkBead
	stale  []bulkBead
}

func selectBulkAssignBeads(strategy string, candidates bulkAssignCandidates) []bulkBead {
	switch strategy {
	case "ready":
		return selectReadyBeads(candidates.ready)
	case "stale":
		return selectStaleBeads(candidates.stale)
	case "balanced":
		return selectBalancedBeads(candidates)
	default:
		return selectImpactBeads(candidates)
	}
}

func selectImpactBeads(candidates bulkAssignCandidates) []bulkBead {
	impact := append([]bulkBead(nil), candidates.impact...)
	if len(impact) == 0 {
		return selectReadyBeads(candidates.ready)
	}
	sort.Slice(impact, func(i, j int) bool {
		if impact[i].UnblocksCount == impact[j].UnblocksCount {
			return impact[i].ID < impact[j].ID
		}
		return impact[i].UnblocksCount > impact[j].UnblocksCount
	})
	return impact
}

func selectReadyBeads(ready []bulkBead) []bulkBead {
	filtered := make([]bulkBead, 0, len(ready))
	for _, bead := range ready {
		switch bead.Status {
		case "", "ready", "open":
			filtered = append(filtered, bead)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Priority == filtered[j].Priority {
			return filtered[i].ID < filtered[j].ID
		}
		return filtered[i].Priority < filtered[j].Priority
	})
	return filtered
}

func selectStaleBeads(stale []bulkBead) []bulkBead {
	filtered := append([]bulkBead(nil), stale...)
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].UpdatedAt.Before(filtered[j].UpdatedAt)
	})
	return filtered
}

func selectBalancedBeads(candidates bulkAssignCandidates) []bulkBead {
	impact := selectImpactBeads(candidates)
	ready := selectReadyBeads(candidates.ready)
	stale := selectStaleBeads(candidates.stale)

	var result []bulkBead
	idx := 0
	for len(result) < len(impact)+len(ready)+len(stale) {
		added := false
		if idx < len(impact) {
			result = append(result, impact[idx])
			added = true
		}
		if idx < len(ready) {
			result = append(result, ready[idx])
			added = true
		}
		if idx < len(stale) {
			result = append(result, stale[idx])
			added = true
		}
		if !added {
			break
		}
		idx++
	}
	return result
}

func allocateBulkAssignBeads(panes []bulkPane, beads []bulkBead) bulkAssignPlan {
	plan := bulkAssignPlan{}
	multiWindow := bulkPanesSpanMultipleWindows(panes)

	if len(panes) == 0 {
		for _, bead := range beads {
			plan.UnassignedBeads = append(plan.UnassignedBeads, bead.ID)
		}
		return plan
	}

	limit := len(panes)
	if len(beads) < limit {
		limit = len(beads)
	}

	for i := 0; i < limit; i++ {
		pane := panes[i]
		bead := beads[i]
		assignment := BulkAssignAssignment{
			Pane:      pane.Ref.Canonical(multiWindow),
			PaneID:    pane.Ref.ID,
			Bead:      bead.ID,
			BeadTitle: bead.Title,
			AgentType: pane.AgentType,
			Reason:    bulkAssignReason(bead),
			Status:    "planned",
			paneIndex: pane.Ref.PaneIndex,
			paneTitle: pane.Title,
		}
		plan.Assignments = append(plan.Assignments, assignment)
		plan.assigned++
	}

	if len(beads) > limit {
		for i := limit; i < len(beads); i++ {
			plan.UnassignedBeads = append(plan.UnassignedBeads, beads[i].ID)
		}
	}

	if len(panes) > limit {
		for i := limit; i < len(panes); i++ {
			plan.UnassignedPanes = append(plan.UnassignedPanes, panes[i].Ref.Canonical(multiWindow))
		}
	}

	return plan
}

func bulkPanesSpanMultipleWindows(panes []bulkPane) bool {
	if len(panes) < 2 {
		return false
	}
	window := panes[0].Ref.WindowIndex
	for _, pane := range panes[1:] {
		if pane.Ref.WindowIndex != window {
			return true
		}
	}
	return false
}

func bulkAssignReason(bead bulkBead) string {
	switch bead.Source {
	case bulkSourceImpact:
		return fmt.Sprintf("highest_unblocks (%d items)", bead.UnblocksCount)
	case bulkSourceStale:
		if bead.UpdatedAt.IsZero() {
			return "stale_in_progress (unknown)"
		}
		return fmt.Sprintf("stale_in_progress (%s)", bead.UpdatedAt.UTC().Format(time.RFC3339))
	default:
		if bead.Priority > 0 {
			return fmt.Sprintf("ready_priority P%d", bead.Priority)
		}
		return "ready_priority"
	}
}

func applyBulkAssignPlan(ctx context.Context, opts BulkAssignOptions, deps BulkAssignDependencies, output *BulkAssignOutput, plan bulkAssignPlan) {
	if ctx == nil {
		for i := range plan.Assignments {
			failBulkAssignment(&plan.Assignments[i], errors.New("bulk assignment context is required"), ErrCodeInternalError)
		}
		finishBulkAssignOutput(output, plan)
		return
	}
	if err := ctx.Err(); err != nil {
		markPendingBulkAssignmentsCanceled(plan.Assignments, err)
		finishBulkAssignOutput(output, plan)
		return
	}
	redactionConfig, redactionErr := deps.LoadRedaction(opts.projectDir)
	if redactionErr != nil {
		for i := range plan.Assignments {
			plan.Assignments[i].BeadTitle = ""
			failBulkAssignment(&plan.Assignments[i], fmt.Errorf("load bulk assignment redaction policy: %w", redactionErr), "")
		}
		finishBulkAssignOutput(output, plan)
		return
	}
	durableRedactionConfig := redactionConfig.DeepCopy()
	durableRedactionConfig.Mode = redaction.ModeRedact
	for i := range plan.Assignments {
		planned := &plan.Assignments[i]
		titleResult := redaction.ScanAndRedact(planned.BeadTitle, redactionConfig)
		planned.BeadTitle = redaction.ScanAndRedact(planned.BeadTitle, durableRedactionConfig).Output
		if titleResult.Blocked {
			failBulkAssignment(planned, fmt.Errorf("assignment title blocked by redaction policy (%d findings)", len(titleResult.Findings)), "")
		}
	}
	for _, requestedPath := range opts.ReservationPaths {
		pathResult := redaction.ScanAndRedact(requestedPath, redactionConfig)
		if len(pathResult.Findings) == 0 {
			continue
		}
		for i := range plan.Assignments {
			if plan.Assignments[i].Status != "failed" {
				failBulkAssignment(&plan.Assignments[i], fmt.Errorf("assignment reservation path blocked by redaction policy (%d findings)", len(pathResult.Findings)), "")
			}
		}
		break
	}

	template, templateErr := loadBulkAssignTemplate(opts, deps)
	if templateErr != nil {
		for i := range plan.Assignments {
			failBulkAssignment(&plan.Assignments[i], templateErr, "")
			plan.failed++
		}
	}

	needsDetails := strings.Contains(template, "{bead_type}") || strings.Contains(template, "{bead_deps}")
	if opts.RequireReservation && len(opts.ReservationPaths) == 0 {
		for i := range plan.Assignments {
			if plan.Assignments[i].Status != "failed" {
				failBulkAssignment(&plan.Assignments[i], assignment.ErrReservationPathsRequired, ErrCodeInvalidFlag)
			}
		}
		finishBulkAssignOutput(output, plan)
		return
	}

	prompts := make([]string, len(plan.Assignments))
	for i := range plan.Assignments {
		assignment := &plan.Assignments[i]
		if assignment.Status == "failed" {
			continue
		}
		prompt, err := buildBulkAssignPrompt(ctx, template, deps, assignment, output.Session, opts.projectDir, needsDetails)
		if err != nil {
			failBulkAssignment(assignment, err, "")
			continue
		}
		prompts[i] = prompt
		if opts.DryRun {
			assignment.Status = "planned"
			assignment.PromptSent = false
		}
	}

	if opts.DryRun {
		finishBulkAssignOutput(output, plan)
		return
	}

	runtime, runtimeErr := newBulkAtomicRuntime(ctx, deps, output.Session, opts.projectDir)
	if runtimeErr != nil {
		for i := range plan.Assignments {
			if plan.Assignments[i].Status == "failed" {
				continue
			}
			failBulkAssignment(&plan.Assignments[i], runtimeErr, "")
		}
		finishBulkAssignOutput(output, plan)
		return
	}
	if err := ctx.Err(); err != nil {
		markPendingBulkAssignmentsCanceled(plan.Assignments, err)
		finishBulkAssignOutput(output, plan)
		return
	}

	workCtx, cancelWork := context.WithCancel(ctx)
	defer cancelWork()

	applyOne := func(index int) {
		assignmentResult := &plan.Assignments[index]
		if assignmentResult.Status == "failed" {
			return
		}
		if err := workCtx.Err(); err != nil {
			failBulkAssignment(assignmentResult, fmt.Errorf("bulk assignment canceled: %w", err), ErrCodeTimeout)
			return
		}
		runtime.execute(workCtx, output.Session, assignmentResult, prompts[index], opts.RequireReservation, opts.ReservationPaths)
	}

	if opts.Parallel {
		var wg sync.WaitGroup
		for i := range plan.Assignments {
			if plan.Assignments[i].Status == "failed" {
				continue
			}
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				applyOne(index)
			}(i)
		}
		wg.Wait()
	} else {
		attempted := false
		for i := range plan.Assignments {
			if plan.Assignments[i].Status == "failed" {
				continue
			}
			if attempted && opts.Stagger > 0 {
				if err := deps.Wait(workCtx, opts.Stagger); err != nil {
					markPendingBulkAssignmentsCanceled(plan.Assignments[i:], err)
					break
				}
			}
			if err := workCtx.Err(); err != nil {
				markPendingBulkAssignmentsCanceled(plan.Assignments[i:], err)
				break
			}
			attempted = true
			applyOne(i)
		}
	}

	finishBulkAssignOutput(output, plan)
}

func markPendingBulkAssignmentsCanceled(assignments []BulkAssignAssignment, err error) {
	for i := range assignments {
		if assignments[i].Status == "failed" || assignments[i].Status == "assigned" {
			continue
		}
		failBulkAssignment(&assignments[i], fmt.Errorf("bulk assignment canceled: %w", err), ErrCodeTimeout)
	}
}

func finishBulkAssignOutput(output *BulkAssignOutput, plan bulkAssignPlan) {
	output.Assignments = append(output.Assignments, plan.Assignments...)
	output.UnassignedBeads = append(output.UnassignedBeads, plan.UnassignedBeads...)
	output.UnassignedPanes = append(output.UnassignedPanes, plan.UnassignedPanes...)

	assigned := 0
	failed := 0
	for _, assignment := range output.Assignments {
		switch assignment.Status {
		case "assigned":
			assigned++
		case "failed":
			failed++
		}
	}

	output.Summary = BulkAssignSummary{
		TotalPanes: len(output.Assignments) + len(output.UnassignedPanes),
		Assigned:   assigned,
		Skipped:    0,
		Failed:     failed,
	}
	if failed > 0 {
		code, hint := bulkAssignFailureClass(output.Assignments)
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("%d of %d bulk assignments failed", failed, len(output.Assignments)),
			code,
			hint,
		)
	}
}

func bulkAssignFailureClass(assignments []BulkAssignAssignment) (string, string) {
	for _, item := range assignments {
		if errors.Is(item.failureCause, context.Canceled) || errors.Is(item.failureCause, context.DeadlineExceeded) || item.failureCode == ErrCodeTimeout {
			return ErrCodeTimeout, "Retry the command after cancellation"
		}
	}
	for _, item := range assignments {
		if item.failureCode == ErrCodeInvalidFlag {
			return ErrCodeInvalidFlag, "Correct the allocation or assignment options and retry"
		}
	}
	for _, item := range assignments {
		if item.failureCode == ErrCodePaneNotFound {
			return ErrCodePaneNotFound, "Inspect canonical pane addresses and retry"
		}
	}
	for _, item := range assignments {
		if errors.Is(item.failureCause, assignment.ErrDispatchOutcomeUnknown) || item.failureCode == ErrCodeDispatchUnknown {
			return ErrCodeDispatchUnknown, "Inspect the durable assignment receipt before retrying; delivery outcome is unknown"
		}
	}
	return "ASSIGNMENT_FAILED", "Inspect assignments[].error; no failed target was dispatched"
}

type robotAgentMailReservationClient interface {
	EnsureProject(context.Context, string) (*agentmail.Project, error)
	ListAgents(context.Context, string) ([]agentmail.Agent, error)
	ListReservations(context.Context, string, string, bool) ([]agentmail.FileReservation, error)
	ReservePaths(context.Context, agentmail.FileReservationOptions) (*agentmail.ReservationResult, error)
}

// robotAgentMailReservationRuntime binds reservation calls to the exact
// project and pane-owned Agent Mail identity discovered before a Beads claim.
type robotAgentMailReservationRuntime struct {
	client     robotAgentMailReservationClient
	projectKey string
	projectID  int
	registry   *agentmail.SessionAgentRegistry
	registered map[string]agentmail.Agent
}

func newRobotAgentMailReservationRuntime(
	ctx context.Context,
	projectKey, session string,
	client robotAgentMailReservationClient,
) (*robotAgentMailReservationRuntime, error) {
	projectKey = filepath.Clean(strings.TrimSpace(projectKey))
	if projectKey == "." || !filepath.IsAbs(projectKey) {
		return nil, fmt.Errorf("Agent Mail reservation project must be an absolute path: %q", projectKey)
	}
	var concrete *agentmail.Client
	if client == nil {
		concrete = agentmail.NewClient(agentmail.WithProjectKey(projectKey))
		client = concrete
	}
	project, err := client.EnsureProject(ctx, projectKey)
	if err != nil {
		return nil, fmt.Errorf("ensure Agent Mail project %s: %w", projectKey, err)
	}
	if project == nil || project.ID <= 0 || filepath.Clean(project.HumanKey) != projectKey {
		return nil, fmt.Errorf("Agent Mail returned an invalid project receipt for %s", projectKey)
	}
	registry, err := agentmail.LoadSessionAgentRegistry(session, projectKey)
	if err != nil {
		return nil, fmt.Errorf("load Agent Mail pane registry for %s: %w", session, err)
	}
	if registry != nil {
		if filepath.Clean(registry.ProjectKey) != projectKey {
			return nil, fmt.Errorf("Agent Mail pane registry project mismatch: got %s, want %s", registry.ProjectKey, projectKey)
		}
		if concrete != nil {
			registry.HydrateClientTokens(concrete)
		}
	}
	agents, err := client.ListAgents(ctx, projectKey)
	if err != nil {
		return nil, fmt.Errorf("list registered Agent Mail recipients for %s: %w", projectKey, err)
	}
	registered := make(map[string]agentmail.Agent, len(agents))
	for _, agent := range agents {
		name := strings.TrimSpace(agent.Name)
		if name == "" || agent.ProjectID != project.ID {
			continue
		}
		registered[name] = agent
	}
	return &robotAgentMailReservationRuntime{
		client: client, projectKey: projectKey, projectID: project.ID,
		registry: registry, registered: registered,
	}, nil
}

func (r *robotAgentMailReservationRuntime) ResolveRecipient(_ context.Context, projectKey, session, paneID, _ string) (string, error) {
	if r == nil {
		return "", errors.New("Agent Mail reservation runtime is nil")
	}
	if filepath.Clean(projectKey) != r.projectKey {
		return "", fmt.Errorf("Agent Mail recipient project mismatch: got %s, want %s", projectKey, r.projectKey)
	}
	if r.registry != nil && r.registry.SessionName != "" && r.registry.SessionName != session {
		return "", fmt.Errorf("Agent Mail pane registry session mismatch: got %s, want %s", r.registry.SessionName, session)
	}

	registryName := ""
	if r.registry != nil {
		registryName, _ = r.registry.GetAgentByID(paneID)
	}
	identityName, _ := agentmail.ResolveIdentity(r.projectKey, paneID)
	registryName = strings.TrimSpace(registryName)
	identityName = strings.TrimSpace(identityName)
	if registryName != "" && identityName != "" && registryName != identityName {
		return "", fmt.Errorf("conflicting Agent Mail identities for pane %s: registry=%s identity=%s", paneID, registryName, identityName)
	}
	name := registryName
	if name == "" {
		name = identityName
	}
	if name == "" {
		return "", fmt.Errorf("pane %s has no canonical Agent Mail identity", paneID)
	}
	registered, ok := r.registered[name]
	if !ok || registered.ProjectID != r.projectID {
		return "", fmt.Errorf("pane %s identity %s is not registered in Agent Mail project %s", paneID, name, r.projectKey)
	}
	return name, nil
}

func (r *robotAgentMailReservationRuntime) Reserve(ctx context.Context, req assignment.ReservationRequest) (assignment.LeaseReceipt, error) {
	lease := assignment.LeaseReceipt{AgentName: req.AgentName, Target: req.Target, Requested: append([]string(nil), req.RequestedPaths...)}
	registered, ok := r.registered[req.AgentName]
	if !ok || registered.ProjectID != r.projectID {
		return lease, assignment.GuaranteeNoReservation(fmt.Errorf("Agent Mail reservation recipient %s is not registered in project %s", req.AgentName, r.projectKey))
	}
	requested, err := validateRobotReservationPaths(req.RequestedPaths)
	if err != nil {
		return lease, assignment.GuaranteeNoReservation(err)
	}
	lease.Requested = append([]string(nil), requested...)
	ttlSeconds := int(req.TTL.Seconds())
	if ttlSeconds < 60 {
		ttlSeconds = 3600
	}
	result, reserveErr := r.client.ReservePaths(ctx, agentmail.FileReservationOptions{
		ProjectKey: r.projectKey, AgentName: req.AgentName, Paths: requested,
		TTLSeconds: ttlSeconds, Exclusive: true, Reason: fmt.Sprintf("bead assignment: %s", req.BeadID),
	})
	if result == nil {
		if reserveErr != nil {
			return lease, reserveErr
		}
		return lease, assignment.GuaranteeNoReservation(errors.New("Agent Mail returned no reservation result"))
	}
	requestedSet := make(map[string]struct{}, len(requested))
	for _, path := range requested {
		requestedSet[path] = struct{}{}
	}
	seenPaths := make(map[string]struct{}, len(result.Granted))
	seenIDs := make(map[int]struct{}, len(result.Granted))
	expectedReason := fmt.Sprintf("bead assignment: %s", req.BeadID)
	now := time.Now().UTC()
	var validationErrors []error
	for _, granted := range result.Granted {
		path := strings.TrimSpace(granted.PathPattern)
		validHandle := true
		if granted.ID <= 0 {
			validationErrors = append(validationErrors, fmt.Errorf("Agent Mail reservation for %s has invalid ID %d", path, granted.ID))
			validHandle = false
		} else if _, duplicate := seenIDs[granted.ID]; duplicate {
			validationErrors = append(validationErrors, fmt.Errorf("Agent Mail repeated reservation ID %d", granted.ID))
			validHandle = false
		} else {
			seenIDs[granted.ID] = struct{}{}
		}
		if granted.ProjectID != r.projectID || granted.AgentName != req.AgentName {
			validationErrors = append(validationErrors, fmt.Errorf("Agent Mail reservation %d receipt project or recipient mismatch", granted.ID))
		}
		if granted.Reason != expectedReason {
			validationErrors = append(validationErrors, fmt.Errorf("Agent Mail reservation %d receipt reason mismatch", granted.ID))
		}
		if granted.ReleasedTS != nil {
			validationErrors = append(validationErrors, fmt.Errorf("Agent Mail reservation %d is already released", granted.ID))
		}
		if !granted.Exclusive {
			validationErrors = append(validationErrors, fmt.Errorf("Agent Mail reservation %d is not exclusive", granted.ID))
		}
		if _, expected := requestedSet[path]; !expected {
			validationErrors = append(validationErrors, fmt.Errorf("Agent Mail granted unexpected path %q", path))
		} else if _, duplicate := seenPaths[path]; duplicate {
			validationErrors = append(validationErrors, fmt.Errorf("Agent Mail granted path %q more than once", path))
		} else {
			seenPaths[path] = struct{}{}
		}
		expiresAt := granted.ExpiresTS.Time
		if expiresAt.IsZero() || !expiresAt.After(now) {
			validationErrors = append(validationErrors, fmt.Errorf("Agent Mail reservation %d has no live future expiry", granted.ID))
		}
		if !expiresAt.IsZero() && (lease.ExpiresAt == nil || expiresAt.Before(*lease.ExpiresAt)) {
			lease.ExpiresAt = &expiresAt
		}
		if validHandle {
			lease.Granted = append(lease.Granted, path)
			lease.ReservationIDs = append(lease.ReservationIDs, granted.ID)
		}
	}
	sort.Strings(lease.Granted)
	sort.Ints(lease.ReservationIDs)
	if validationErr := errors.Join(validationErrors...); validationErr != nil {
		return lease, errors.Join(validationErr, reserveErr)
	}
	if reserveErr != nil {
		return lease, reserveErr
	}
	if len(result.Conflicts) != 0 {
		conflictErr := fmt.Errorf("Agent Mail reported %d reservation conflict(s)", len(result.Conflicts))
		if len(lease.ReservationIDs) == 0 {
			return lease, assignment.GuaranteeNoReservation(conflictErr)
		}
		return lease, conflictErr
	}
	if len(seenPaths) != len(requestedSet) {
		grantErr := fmt.Errorf("Agent Mail granted %d of %d requested paths", len(seenPaths), len(requestedSet))
		if len(lease.ReservationIDs) == 0 {
			return lease, assignment.GuaranteeNoReservation(grantErr)
		}
		return lease, grantErr
	}
	return lease, nil
}

func (r *robotAgentMailReservationRuntime) ReconcileReservation(ctx context.Context, req assignment.ReservationRequest, _ assignment.LeaseReceipt) (assignment.ReservationReconciliation, error) {
	if r == nil {
		return assignment.ReservationReconciliation{State: assignment.ReservationReconciliationUnknown}, errors.New("Agent Mail reservation runtime is nil")
	}
	requested, err := validateRobotReservationPaths(req.RequestedPaths)
	if err != nil {
		return assignment.ReservationReconciliation{State: assignment.ReservationReconciliationUnknown}, err
	}
	reservations, err := r.client.ListReservations(ctx, r.projectKey, req.AgentName, false)
	if err != nil {
		return assignment.ReservationReconciliation{State: assignment.ReservationReconciliationUnknown}, err
	}

	requestedSet := make(map[string]struct{}, len(requested))
	for _, path := range requested {
		requestedSet[path] = struct{}{}
	}
	lease := assignment.LeaseReceipt{
		AgentName: req.AgentName,
		Target:    req.Target,
		Requested: append([]string(nil), requested...),
	}
	seen := make(map[string]struct{}, len(requested))
	seenIDs := make(map[int]struct{}, len(requested))
	reason := fmt.Sprintf("bead assignment: %s", req.BeadID)
	now := time.Now().UTC()
	for _, reservation := range reservations {
		if reservation.ReleasedTS != nil || reservation.AgentName != req.AgentName || reservation.Reason != reason {
			continue
		}
		if _, wanted := requestedSet[reservation.PathPattern]; !wanted {
			continue
		}
		if reservation.ID <= 0 {
			return assignment.ReservationReconciliation{State: assignment.ReservationReconciliationUnknown, Lease: lease}, nil
		}
		if _, duplicate := seenIDs[reservation.ID]; duplicate {
			return assignment.ReservationReconciliation{State: assignment.ReservationReconciliationUnknown, Lease: lease}, nil
		}
		if _, duplicate := seen[reservation.PathPattern]; duplicate {
			return assignment.ReservationReconciliation{State: assignment.ReservationReconciliationUnknown, Lease: lease}, nil
		}
		seenIDs[reservation.ID] = struct{}{}
		seen[reservation.PathPattern] = struct{}{}
		lease.Granted = append(lease.Granted, reservation.PathPattern)
		lease.ReservationIDs = append(lease.ReservationIDs, reservation.ID)
		expiresAt := reservation.ExpiresTS.Time
		if reservation.ProjectID != r.projectID || !reservation.Exclusive ||
			expiresAt.IsZero() || !expiresAt.After(now) {
			return assignment.ReservationReconciliation{State: assignment.ReservationReconciliationUnknown, Lease: lease}, nil
		}
		if lease.ExpiresAt == nil || expiresAt.Before(*lease.ExpiresAt) {
			lease.ExpiresAt = &expiresAt
		}
	}
	if len(seen) == 0 {
		return assignment.ReservationReconciliation{State: assignment.ReservationReconciliationAbsent}, nil
	}
	if len(seen) != len(requestedSet) {
		return assignment.ReservationReconciliation{State: assignment.ReservationReconciliationUnknown, Lease: lease}, nil
	}
	sort.Strings(lease.Granted)
	sort.Ints(lease.ReservationIDs)
	return assignment.ReservationReconciliation{State: assignment.ReservationReconciliationReserved, Lease: lease}, nil
}

func validateRobotReservationPaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, assignment.ErrReservationPathsRequired
	}
	result := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			return nil, assignment.ErrReservationPathsRequired
		}
		if _, duplicate := seen[path]; duplicate {
			return nil, fmt.Errorf("duplicate reservation path %q", path)
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}
	return result, nil
}

type bulkAtomicRuntime struct {
	store        *assignment.AssignmentStore
	claimPort    assignment.ClaimPort
	dispatchPort *robotAtomicPaneDispatchPort
	deps         BulkAssignDependencies
	workDir      string
	newKey       func() (string, error)
	observeMu    sync.Mutex
	observation  statuspkg.SessionObservation
	observeErr   error
}

func (r *bulkAtomicRuntime) observeSession(ctx context.Context, session string) (statuspkg.SessionObservation, error) {
	r.observeMu.Lock()
	defer r.observeMu.Unlock()
	if r.observeErr == nil && statuspkg.DispatchObservationIsCurrent(r.observation.ObservedAt, time.Now()) {
		return r.observation, nil
	}
	r.observation, r.observeErr = r.deps.ObserveSession(ctx, session)
	return r.observation, r.observeErr
}

func newBulkAtomicRuntime(ctx context.Context, deps BulkAssignDependencies, session, workDir string) (*bulkAtomicRuntime, error) {
	if strings.TrimSpace(workDir) == "" {
		var err error
		workDir, err = deps.ResolveProject(ctx, session, nil)
		if err != nil {
			return nil, fmt.Errorf("resolve bulk assignment project: %w", err)
		}
	}
	store, err := deps.LoadStore(session)
	if err != nil {
		return nil, fmt.Errorf("load bulk assignment ledger: %w", err)
	}
	redactionConfig, err := deps.LoadRedaction(workDir)
	if err != nil {
		return nil, fmt.Errorf("load bulk assignment redaction policy: %w", err)
	}
	claimPort := newRobotAtomicClaimPort(workDir, deps.ClaimBead)
	dispatchPort := newRobotAtomicPaneDispatchPort(
		session,
		deps.ListPanes,
		deps.ObserveSession,
		redactionConfig,
		deps.DispatchDeliverer,
		deps.DispatchPacer,
	)
	return &bulkAtomicRuntime{
		store: store, claimPort: claimPort, dispatchPort: dispatchPort,
		deps: deps, workDir: workDir, newKey: deps.NewIdempotencyKey,
	}, nil
}

func (r *bulkAtomicRuntime) execute(ctx context.Context, session string, output *BulkAssignAssignment, prompt string, requireReservation bool, reservationPaths []string) {
	if ctx == nil {
		failBulkAssignment(output, errors.New("bulk assignment context is required"), ErrCodeInternalError)
		return
	}
	if err := ctx.Err(); err != nil {
		failBulkAssignment(output, fmt.Errorf("bulk assignment canceled: %w", err), ErrCodeTimeout)
		return
	}
	target := bulkAssignPaneTarget(session, output)
	reservationPort := r.deps.ReservationPort
	resolveAgentName := r.deps.ResolveAgentName
	agentName := ""
	idempotencyKey := ""
	if replay := robotAtomicReplayIntent(r.store, output.Bead, target, output.paneIndex, output.AgentType, prompt, requireReservation, reservationPaths); replay != nil {
		agentName = replay.AgentName
		idempotencyKey = replay.IdempotencyKey
	} else {
		observation, observeErr := r.observeSession(ctx, session)
		if observeErr != nil {
			failBulkAssignment(output, fmt.Errorf("observe pane %s before assignment: %w", output.Pane, observeErr), "")
			return
		}
		if err := ctx.Err(); err != nil {
			failBulkAssignment(output, fmt.Errorf("bulk assignment canceled after observation: %w", err), ErrCodeTimeout)
			return
		}
		if !statuspkg.DispatchObservationIsCurrent(observation.ObservedAt, time.Now()) {
			failBulkAssignment(output, fmt.Errorf("pane %s (%s) observation is stale", output.Pane, target), "")
			return
		}
		if !observation.SafeToDispatch(target) {
			failBulkAssignment(output, fmt.Errorf("pane %s (%s) is not safe to dispatch", output.Pane, target), "")
			return
		}

		if requireReservation && reservationPort == nil {
			mailRuntime, runtimeErr := newRobotAgentMailReservationRuntime(ctx, r.workDir, session, nil)
			if runtimeErr != nil {
				failBulkAssignment(output, runtimeErr, "")
				return
			}
			reservationPort = mailRuntime
			resolveAgentName = mailRuntime.ResolveRecipient
		}
		if err := ctx.Err(); err != nil {
			failBulkAssignment(output, fmt.Errorf("bulk assignment canceled before reservation identity resolution: %w", err), ErrCodeTimeout)
			return
		}

		agentName = fmt.Sprintf("ntm:%s:%s", session, target)
		if requireReservation {
			if resolveAgentName == nil {
				failBulkAssignment(output, errors.New("required reservation has no exact Agent Mail pane-identity resolver"), "")
				return
			}
			var resolveErr error
			agentName, resolveErr = resolveAgentName(ctx, r.workDir, session, target, output.paneTitle)
			if resolveErr != nil {
				failBulkAssignment(output, resolveErr, "")
				return
			}
			if err := ctx.Err(); err != nil {
				failBulkAssignment(output, fmt.Errorf("bulk assignment canceled after reservation identity resolution: %w", err), ErrCodeTimeout)
				return
			}
		}

		var keyErr error
		idempotencyKey, keyErr = robotAtomicIdempotencyKey(r.store, output.Bead, target, output.paneIndex, output.AgentType, agentName, prompt, requireReservation, reservationPaths, r.newKey)
		if keyErr != nil {
			failBulkAssignment(output, keyErr, "")
			return
		}
	}
	output.IdempotencyKey = idempotencyKey
	if err := ctx.Err(); err != nil {
		failBulkAssignment(output, fmt.Errorf("bulk assignment canceled before atomic claim: %w", err), ErrCodeTimeout)
		return
	}

	coordinator := assignment.NewAtomicCoordinator(r.store, r.claimPort, reservationPort, r.dispatchPort, r.dispatchPort).
		WithWorkItemStatusPort(assignment.WorkItemStatusFunc(func(statusCtx context.Context, beadID string) (string, error) {
			return r.deps.GetBeadStatus(statusCtx, r.workDir, beadID)
		}))
	result, executeErr := coordinator.Execute(ctx, assignment.AtomicRequest{
		BeadID:             output.Bead,
		BeadTitle:          output.BeadTitle,
		Target:             target,
		OccupancyKey:       target,
		Pane:               output.paneIndex,
		AgentType:          output.AgentType,
		AgentName:          agentName,
		Actor:              agentName,
		Prompt:             prompt,
		IdempotencyKey:     idempotencyKey,
		RequireReservation: requireReservation,
		RequestedPaths:     append([]string(nil), reservationPaths...),
		ReservationTTL:     time.Hour,
	})
	applyBulkAtomicExecutionResult(ctx, output, idempotencyKey, result, executeErr)
}

func applyBulkAtomicExecutionResult(ctx context.Context, output *BulkAssignAssignment, idempotencyKey string, result assignment.AtomicResult, executeErr error) {
	if output == nil {
		return
	}
	matchingAssignment := result.Assignment != nil && result.Assignment.BeadID == output.Bead && result.Assignment.IdempotencyKey == idempotencyKey
	if matchingAssignment {
		output.BeadTitle = result.Assignment.BeadTitle
		output.Claimed = result.Assignment.ClaimState == assignment.ClaimClaimed
		output.ClaimActor = result.Assignment.ClaimActor
		output.ReservationIDs = append([]int(nil), result.Assignment.ReservationIDs...)
		output.DispatchReceiptID = result.Assignment.DispatchReceiptID
	}
	if executeErr != nil {
		failBulkAssignment(output, executeErr, "")
		return
	}
	durableSent := result.Sent && matchingAssignment && result.Assignment.DispatchState == assignment.DispatchSent && strings.TrimSpace(result.Assignment.DispatchReceiptID) != ""
	if durableSent {
		output.Status = "assigned"
		output.PromptSent = true
		return
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			failBulkAssignment(output, fmt.Errorf("bulk assignment canceled: %w", err), ErrCodeTimeout)
			return
		}
	}
	if result.Sent || strings.TrimSpace(output.DispatchReceiptID) != "" {
		failBulkAssignment(output, assignment.ErrDispatchOutcomeUnknown, ErrCodeDispatchUnknown)
		return
	}
	failBulkAssignment(output, errors.New("atomic assignment completed without a durable dispatch receipt"), ErrCodeInternalError)
}

func newRobotAtomicClaimPort(workDir string, claim func(context.Context, string, string, string) (bv.BeadClaimResult, error)) assignment.ClaimPort {
	return assignment.ClaimFunc(func(ctx context.Context, beadID, actor string) (assignment.ClaimReceipt, error) {
		claimed, err := claim(ctx, workDir, beadID, actor)
		if err != nil {
			switch {
			case errors.Is(err, bv.ErrBeadAssignmentIneligible):
				return assignment.ClaimReceipt{}, fmt.Errorf("%w: %v", assignment.ErrClaimIneligible, err)
			case errors.Is(err, bv.ErrBeadAlreadyClaimed):
				return assignment.ClaimReceipt{}, fmt.Errorf("%w: %v", assignment.ErrClaimConflict, err)
			}
			return assignment.ClaimReceipt{}, err
		}
		return assignment.ClaimReceipt{
			BeadID: claimed.ID, Actor: claimed.Actor, Status: claimed.Status, ClaimedAt: claimed.ClaimedAt,
		}, nil
	})
}

type robotAtomicPaneDispatchPort struct {
	session         string
	listPanes       func(context.Context, string) ([]tmux.Pane, error)
	observeSession  func(context.Context, string) (statuspkg.SessionObservation, error)
	redactionConfig redaction.Config
	deliverer       dispatchsvc.Deliverer
	pacer           dispatchsvc.Pacer
}

func newRobotAtomicPaneDispatchPort(
	session string,
	listPanes func(context.Context, string) ([]tmux.Pane, error),
	observeSession func(context.Context, string) (statuspkg.SessionObservation, error),
	redactionConfig redaction.Config,
	deliverer dispatchsvc.Deliverer,
	pacer dispatchsvc.Pacer,
) *robotAtomicPaneDispatchPort {
	return &robotAtomicPaneDispatchPort{
		session: session, listPanes: listPanes, observeSession: observeSession, redactionConfig: redactionConfig,
		deliverer: deliverer, pacer: pacer,
	}
}

func (p *robotAtomicPaneDispatchPort) prepare(ctx context.Context, req assignment.DispatchRequest) (*dispatchsvc.Service, *dispatchsvc.Prepared, error) {
	panes, err := p.listPanes(ctx, p.session)
	if err != nil {
		return nil, nil, fmt.Errorf("load dispatch topology: %w", err)
	}
	service, _, err := newRobotDispatchService(p.redactionConfig, p.deliverer, p.pacer)
	if err != nil {
		return nil, nil, err
	}
	selector := strings.TrimSpace(req.Target)
	if prefix := p.session + ":"; strings.HasPrefix(selector, prefix) {
		selector = strings.TrimPrefix(selector, prefix)
	}
	prepared, err := service.Prepare(ctx, dispatchsvc.Request{
		Session: p.session, Panes: panes, Selectors: []string{selector}, RequireSingleSelector: true,
		IncludeUser: true, Message: req.Prompt, Submit: true, StopOnFailure: true,
	})
	if err != nil {
		return nil, prepared, err
	}
	return service, prepared, nil
}

func (p *robotAtomicPaneDispatchPort) Preflight(ctx context.Context, req assignment.DispatchRequest) (assignment.PromptPreflightResult, error) {
	titleResult := redaction.ScanAndRedact(req.BeadTitle, p.redactionConfig)
	if titleResult.Blocked {
		return assignment.PromptPreflightResult{}, &dispatchsvc.Error{
			Code: dispatchsvc.ErrRedactionBlocked,
			Err:  fmt.Errorf("assignment title blocked by redaction policy (%d findings)", len(titleResult.Findings)),
		}
	}
	for _, requestedPath := range req.RequestedPaths {
		pathResult := redaction.ScanAndRedact(requestedPath, p.redactionConfig)
		if len(pathResult.Findings) > 0 {
			return assignment.PromptPreflightResult{}, &dispatchsvc.Error{
				Code: dispatchsvc.ErrRedactionBlocked,
				Err:  fmt.Errorf("assignment reservation path blocked by redaction policy (%d findings)", len(pathResult.Findings)),
			}
		}
	}
	_, prepared, err := p.prepare(ctx, req)
	if err != nil {
		return assignment.PromptPreflightResult{}, err
	}
	dispatchPrompt, err := prepared.FinalMessageForSingleTarget()
	if err != nil {
		return assignment.PromptPreflightResult{}, err
	}
	durableConfig := p.redactionConfig.DeepCopy()
	durableConfig.Mode = redaction.ModeRedact
	durablePrompt := redaction.ScanAndRedact(req.Prompt, durableConfig).Output
	durableTitle := redaction.ScanAndRedact(req.BeadTitle, durableConfig).Output
	return assignment.PromptPreflightResult{DispatchPrompt: dispatchPrompt, DurablePrompt: durablePrompt, DurableTitle: durableTitle}, nil
}

func (p *robotAtomicPaneDispatchPort) Dispatch(ctx context.Context, req assignment.DispatchRequest) (assignment.DispatchReceipt, error) {
	started := time.Now()
	service, prepared, prepareErr := p.prepare(ctx, req)
	if prepareErr != nil {
		return assignment.DispatchReceipt{Duration: time.Since(started)}, assignment.GuaranteeNoActuation(prepareErr)
	}
	if p.observeSession == nil {
		return assignment.DispatchReceipt{Duration: time.Since(started)}, assignment.GuaranteeNoActuation(errors.New("dispatch-time pane observation is unavailable"))
	}
	observation, observeErr := p.observeSession(ctx, p.session)
	if observeErr != nil {
		return assignment.DispatchReceipt{Duration: time.Since(started)}, assignment.GuaranteeNoActuation(fmt.Errorf("re-observe pane %s before dispatch: %w", req.Target, observeErr))
	}
	if !statuspkg.DispatchObservationIsCurrent(observation.ObservedAt, time.Now()) {
		return assignment.DispatchReceipt{Duration: time.Since(started)}, assignment.GuaranteeNoActuation(fmt.Errorf("pane %s dispatch observation is stale", req.Target))
	}
	if !observation.SafeToDispatch(req.Target) {
		return assignment.DispatchReceipt{Duration: time.Since(started)}, assignment.GuaranteeNoActuation(fmt.Errorf("pane %s is no longer safe to dispatch", req.Target))
	}
	result, dispatchErr := service.Dispatch(ctx, prepared)
	receipt := assignment.DispatchReceipt{Duration: time.Since(started)}
	if len(result.Receipts) == 1 {
		delivery := result.Receipts[0]
		receipt.DeliveryID = assignment.DispatchDeliveryID(delivery.Target.Ref.StableKey(), string(delivery.Protocol), req.IdempotencyKey)
	}
	if dispatchErr != nil {
		return receipt, dispatchErr
	}
	if result.Delivered != 1 || len(result.Receipts) != 1 || result.Receipts[0].Status != dispatchsvc.ReceiptDelivered {
		return receipt, fmt.Errorf("dispatch delivered %d panes, want 1", result.Delivered)
	}
	return receipt, nil
}

func robotAtomicIdempotencyKey(
	store *assignment.AssignmentStore,
	beadID, target string,
	_ int,
	agentType, agentName, prompt string,
	requireReservation bool,
	requestedPaths []string,
	newKey func() (string, error),
) (string, error) {
	promptMatches := func(existing *assignment.Assignment) bool {
		if existing.IntentSHA256 != "" {
			return existing.IntentSHA256 == assignment.PromptSHA256(prompt)
		}
		return existing.PendingPrompt == prompt || existing.PromptSent == prompt
	}
	if existing := store.Get(beadID); existing != nil && !robotAtomicAssignmentTerminal(existing.Status) && existing.IdempotencyKey != "" &&
		existing.DispatchTarget == target &&
		existing.AgentType == agentType && existing.AgentName == agentName &&
		existing.ReservationRequired == requireReservation &&
		stringSlicesEqualRobot(existing.ReservationInputPaths, requestedPaths) &&
		promptMatches(existing) {
		return existing.IdempotencyKey, nil
	}
	return newKey()
}

func robotAtomicReplayIntent(
	store *assignment.AssignmentStore,
	beadID, target string,
	_ int,
	agentType, prompt string,
	requireReservation bool,
	requestedPaths []string,
) *assignment.Assignment {
	if store == nil {
		return nil
	}
	existing := store.Get(beadID)
	if existing == nil || robotAtomicAssignmentTerminal(existing.Status) || existing.IdempotencyKey == "" || existing.DispatchState != assignment.DispatchSent {
		return nil
	}
	occupancyKey := strings.TrimSpace(existing.OccupancyKey)
	if occupancyKey == "" {
		occupancyKey = strings.TrimSpace(existing.DispatchTarget)
	}
	intentMatches := existing.IntentSHA256 != "" && existing.IntentSHA256 == assignment.PromptSHA256(prompt)
	if existing.IntentSHA256 == "" {
		intentMatches = existing.PendingPrompt == prompt || existing.PromptSent == prompt
	}
	if existing.DispatchTarget != target || occupancyKey != target ||
		existing.AgentType != agentType || !intentMatches ||
		existing.ReservationRequired != requireReservation ||
		!stringSlicesEqualRobot(existing.ReservationInputPaths, requestedPaths) {
		return nil
	}
	return existing
}

func robotAtomicAssignmentTerminal(status assignment.AssignmentStatus) bool {
	switch status {
	case assignment.StatusCompleted, assignment.StatusFailed, assignment.StatusReassigned:
		return true
	default:
		return false
	}
}

func stringSlicesEqualRobot(a, b []string) bool {
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

func bulkAssignTMUXAgentType(agentType string) tmux.AgentType {
	switch normalizeAgentType(agentType) {
	case "claude":
		return tmux.AgentClaude
	case "codex":
		return tmux.AgentCodex
	case "gemini":
		return tmux.AgentGemini
	case "antigravity":
		return tmux.AgentAntigravity
	case "cursor":
		return tmux.AgentCursor
	case "windsurf":
		return tmux.AgentWindsurf
	case "aider":
		return tmux.AgentAider
	case "oc":
		return tmux.AgentOpencode
	case "ollama":
		return tmux.AgentOllama
	case "user":
		return tmux.AgentUser
	default:
		return tmux.AgentUnknown
	}
}

func bulkAssignPaneTarget(session string, assignment *BulkAssignAssignment) string {
	if assignment == nil {
		return session
	}
	if assignment.PaneID != "" {
		return assignment.PaneID
	}
	return assignment.Pane
}

func buildBulkAssignPrompt(ctx context.Context, template string, deps BulkAssignDependencies, assignment *BulkAssignAssignment, session, projectDir string, needsDetails bool) (string, error) {
	beadType := ""
	var beadDeps []string
	if needsDetails {
		if deps.FetchBeadDetails == nil {
			return "", fmt.Errorf("bead details fetcher not configured")
		}
		details, err := deps.FetchBeadDetails(ctx, projectDir, assignment.Bead)
		if err != nil {
			return "", err
		}
		if assignment.BeadTitle == "" {
			assignment.BeadTitle = details.Title
		}
		beadType = details.Type
		beadDeps = details.Dependencies
	}

	return expandBulkAssignTemplate(
		template,
		assignment.Bead,
		assignment.BeadTitle,
		beadType,
		beadDeps,
		session,
		assignment.Pane,
	), nil
}

// loadBulkAssignTemplate resolves the dispatch prompt template using the
// following precedence (first match wins):
//  1. Per-invocation --bulk-assign-template file (opts.PromptTemplatePath).
//  2. Project/user-level configured template file (opts.DefaultTemplatePath).
//  3. Project/user-level configured inline template (opts.DefaultTemplate).
//  4. The built-in defaultBulkAssignTemplate const.
//
// This lets a project pin its dispatch contract (e.g. "Read SKILL.md" or
// "Set gc.outcome when done") via .ntm/config.toml without wrapping every
// `ntm --robot-bulk-assign` call in --bulk-assign-template (#153).
func loadBulkAssignTemplate(opts BulkAssignOptions, deps BulkAssignDependencies) (string, error) {
	// 1. Explicit per-invocation override.
	if opts.PromptTemplatePath != "" {
		data, err := deps.ReadFile(opts.PromptTemplatePath)
		if err != nil {
			return "", fmt.Errorf("failed to read prompt template: %w", err)
		}
		return string(data), nil
	}

	// 2. Configured default template file.
	if opts.DefaultTemplatePath != "" {
		data, err := deps.ReadFile(opts.DefaultTemplatePath)
		if err != nil {
			return "", fmt.Errorf("failed to read configured prompt template %q: %w", opts.DefaultTemplatePath, err)
		}
		if strings.TrimSpace(string(data)) != "" {
			return string(data), nil
		}
	}

	// 3. Configured inline default template.
	if strings.TrimSpace(opts.DefaultTemplate) != "" {
		return opts.DefaultTemplate, nil
	}

	// 4. Built-in fallback.
	return defaultBulkAssignTemplate, nil
}

func expandBulkAssignTemplate(template, beadID, beadTitle, beadType string, beadDeps []string, session, pane string) string {
	if beadType == "" {
		beadType = "unknown"
	}
	depsValue := formatBulkAssignDeps(beadDeps)
	replacer := strings.NewReplacer(
		"{bead_id}", beadID,
		"{bead_title}", beadTitle,
		"{bead_type}", beadType,
		"{bead_deps}", depsValue,
		"{session}", session,
		"{pane}", pane,
	)
	return replacer.Replace(template)
}

func formatBulkAssignDeps(deps []string) string {
	if len(deps) == 0 {
		return "none"
	}
	return strings.Join(deps, ", ")
}

func parseBulkAssignAllocation(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("allocation JSON is empty")
	}

	var decoded map[string]string
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("allocation JSON parse failed: %w", err)
	}
	if len(decoded) == 0 {
		return nil, errors.New("allocation JSON must contain at least one pane-to-bead mapping")
	}

	result := make(map[string]string)
	for k, v := range decoded {
		selector := strings.TrimSpace(k)
		if _, err := tmux.ParsePaneSelector(selector); err != nil {
			return nil, fmt.Errorf("invalid pane selector %q: %w", k, err)
		}
		beadID := strings.TrimSpace(v)
		if beadID == "" {
			return nil, fmt.Errorf("empty bead id for pane %s", selector)
		}
		result[selector] = beadID
	}

	return result, nil
}

// decodeBulkAssignTriage parses bv --robot-triage JSON payloads.
func decodeBulkAssignTriage(raw []byte) (*bv.TriageResponse, error) {
	var resp bv.TriageResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func fetchBeadTitle(ctx context.Context, dir, beadID string) (string, error) {
	details, err := fetchBeadDetails(ctx, dir, beadID)
	if err != nil {
		return "", err
	}
	return details.Title, nil
}

func fetchBeadDetails(ctx context.Context, dir, beadID string) (BeadDetails, error) {
	if ctx == nil {
		return BeadDetails{}, errors.New("bead details context is required")
	}
	cmd := exec.CommandContext(ctx, "br", "show", beadID, "--json")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		return BeadDetails{}, fmt.Errorf("br show %s failed: %w", beadID, err)
	}

	var issues []struct {
		Title        string `json:"title"`
		IssueType    string `json:"issue_type"`
		Dependencies []struct {
			ID      string `json:"id"`
			DepType string `json:"dep_type"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(output, &issues); err != nil {
		return BeadDetails{}, fmt.Errorf("parse br show output: %w", err)
	}
	if len(issues) == 0 || issues[0].Title == "" {
		return BeadDetails{}, fmt.Errorf("bead %s not found", beadID)
	}

	depSet := make(map[string]struct{})
	for _, dep := range issues[0].Dependencies {
		if dep.DepType != "blocks" {
			continue
		}
		if dep.ID != "" {
			depSet[dep.ID] = struct{}{}
		}
	}
	deps := make([]string, 0, len(depSet))
	for id := range depSet {
		deps = append(deps, id)
	}
	sort.Strings(deps)

	return BeadDetails{
		Title:        issues[0].Title,
		Type:         issues[0].IssueType,
		Dependencies: deps,
	}, nil
}
