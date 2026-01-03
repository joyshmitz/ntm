# NTM Improvement Plan

This document outlines strategic improvements to elevate NTM from a capable power-user tool to a compelling, accessible platform for AI-assisted development.

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Integration with Existing Projects](#integration-with-existing-projects)
3. [Web Dashboard](#web-dashboard)
4. [Zero-Config Quick Start](#zero-config-quick-start)
5. [Notifications System](#notifications-system)
6. [Session Templates](#session-templates)
7. [Intelligent Error Recovery](#intelligent-error-recovery)
8. [IDE Integration](#ide-integration)
9. [Agent Orchestration Patterns](#agent-orchestration-patterns)
10. [Interactive Tutorial & Onboarding](#interactive-tutorial--onboarding)
11. [Shareable Sessions](#shareable-sessions)
12. [UX Polish](#ux-polish)
13. [Implementation Roadmap](#implementation-roadmap)

---

## Executive Summary

NTM has solid foundations for multi-agent tmux session management, but several gaps limit broader adoption:

| Gap | Impact | Current State |
|-----|--------|---------------|
| No web interface | Users must watch terminal | TUI dashboard only |
| Complex setup | High barrier to entry | Config files required |
| Silent operation | Users miss important events | No notifications |
| No templates | Repetitive configuration | Manual setup each time |
| Cryptic errors | Frustration, support burden | Technical error messages |

This plan addresses each gap with concrete implementation strategies.

---

## Integration with Existing Projects

Three existing projects should be integrated to provide critical functionality without reinventing the wheel:

### 1. coding_agent_account_manager
**Repository:** https://github.com/Dicklesworthstone/coding_agent_account_manager

**Purpose:** Account management for AI coding agents

**Integration Points:**
- Session authentication and authorization
- Multi-user support for team environments
- API key management and rotation
- Usage analytics and reporting

**Investigation Required:**
- [ ] Analyze API surface and integration patterns
- [ ] Determine authentication flow for NTM sessions
- [ ] Identify shared data models
- [ ] Plan migration path for existing users

### 2. cass_memory_system
**Repository:** https://github.com/Dicklesworthstone/cass_memory_system

**Purpose:** Persistent memory system for AI agents

**Integration Points:**
- Agent memory persistence across sessions
- Project-specific context retention
- User preference learning
- Cross-session knowledge sharing between agents

**Investigation Required:**
- [ ] Map CASS memory concepts to NTM agent lifecycle
- [ ] Design memory injection points in agent prompts
- [ ] Plan memory synchronization strategy
- [ ] Determine storage and privacy implications

### 3. slb (Smart Load Balancer)
**Repository:** https://github.com/Dicklesworthstone/slb

**Purpose:** Automated approval system with agents

**Integration Points:**
- Automated review and approval of agent actions
- Risk assessment for file modifications
- Configurable approval policies
- Audit trail for compliance

**Investigation Required:**
- [ ] Analyze SLB approval workflow API
- [ ] Design integration with NTM's pipeline system
- [ ] Plan approval UI in both TUI and web dashboard
- [ ] Determine policy configuration format

---

## Web Dashboard

### Why It Matters

The web dashboard is the single highest-impact improvement. It transforms NTM from a power-user CLI tool into an accessible platform.

**Benefits:**
- Works on any device without terminal expertise
- Enables remote monitoring (coffee shop, meetings, commute)
- Shareable with teammates for collaboration
- Rich visualizations impossible in terminal (charts, syntax-highlighted diffs)
- Foundation for all other UX improvements

### Proposed Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Web Dashboard                           │
├─────────────────────────────────────────────────────────────────┤
│  Frontend (React/Svelte)                                        │
│  ├── Real-time agent output streaming (WebSocket)               │
│  ├── Session management UI                                      │
│  ├── File diff viewer with syntax highlighting                  │
│  ├── Agent configuration panels                                 │
│  └── Notification center                                        │
├─────────────────────────────────────────────────────────────────┤
│  Backend API (Go, part of ntm binary)                           │
│  ├── REST API for CRUD operations                               │
│  ├── WebSocket server for real-time updates                     │
│  ├── Authentication via coding_agent_account_manager            │
│  └── Session state synchronization                              │
├─────────────────────────────────────────────────────────────────┤
│  Existing NTM Core                                              │
│  ├── tmux session management                                    │
│  ├── Agent lifecycle                                            │
│  └── Robot mode (JSON API already exists)                       │
└─────────────────────────────────────────────────────────────────┘
```

### Key Features

#### 1. Real-time Agent Output Streaming
```
┌─────────────────────────────────────────────────────────────────┐
│  Agent: claude-planner                              [Interrupt] │
├─────────────────────────────────────────────────────────────────┤
│  > Analyzing codebase structure...                              │
│  > Found 47 Go files in internal/                               │
│  > Identifying authentication-related code...                   │
│  >                                                              │
│  > I've identified the following files that handle auth:        │
│  > - internal/auth/jwt.go (JWT token generation)                │
│  > - internal/auth/session.go (session management)              │
│  > - internal/middleware/auth.go (HTTP middleware)              │
│  >                                                              │
│  > Shall I proceed with the refactoring plan?                   │
│  ▌                                                              │
└─────────────────────────────────────────────────────────────────┘
```

#### 2. Multi-Agent Overview
```
┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│ claude-planner  │  │ claude-coder    │  │ codex-tester    │
│ ████████░░ 80%  │  │ ██████████ Done │  │ ████░░░░░░ 40%  │
│ Planning auth   │  │ Waiting for     │  │ Writing tests   │
│ refactor...     │  │ approval        │  │ for jwt.go      │
│                 │  │                 │  │                 │
│ [View] [Stop]   │  │ [View] [Resume] │  │ [View] [Stop]   │
└─────────────────┘  └─────────────────┘  └─────────────────┘
```

#### 3. File Change Viewer
```
┌─────────────────────────────────────────────────────────────────┐
│  Pending Changes (3 files)                         [Approve All]│
├─────────────────────────────────────────────────────────────────┤
│  📄 internal/auth/jwt.go                          +45 -12       │
│  ├── Line 23: Added token refresh logic                         │
│  └── Line 67: Updated expiration handling                       │
│                                                                 │
│  📄 internal/auth/session.go                      +8 -3         │
│  └── Line 12: Import new jwt package                            │
│                                                                 │
│  📄 internal/middleware/auth.go                   +22 -5        │
│  └── Line 45: Added refresh token endpoint                      │
├─────────────────────────────────────────────────────────────────┤
│  [View Diff] [Approve] [Reject] [Request Changes]               │
└─────────────────────────────────────────────────────────────────┘
```

### Implementation Strategy

**Phase 1: Embedded Server**
- Add HTTP/WebSocket server to ntm binary
- Serve static files for frontend
- Expose robot-mode API via REST
- `ntm dashboard --web` starts on localhost:8080

**Phase 2: Frontend Development**
- Choose framework (recommend Svelte for bundle size)
- Implement core views: sessions, agents, output
- Add WebSocket integration for real-time updates

**Phase 3: Polish & Features**
- Syntax highlighting for code output
- Diff viewer for file changes
- Session recording and playback
- Dark/light theme support

---

## Zero-Config Quick Start

### Why It Matters

Current NTM requires understanding of:
- tmux concepts (sessions, panes, windows)
- Configuration file syntax (TOML)
- Agent CLI installations and configurations
- Various flags and options

This creates a significant barrier for new users.

### Proposed Solution

#### 1. Intelligent Defaults

Auto-detect everything possible:

```go
// Pseudo-code for smart defaults
func detectEnvironment() Config {
    config := Config{}

    // Detect installed AI CLIs
    if commandExists("claude") {
        config.Agents = append(config.Agents, AgentConfig{
            Type: "claude",
            Name: "claude-1",
        })
    }
    if commandExists("codex") {
        config.Agents = append(config.Agents, AgentConfig{
            Type: "codex",
            Name: "codex-1",
        })
    }

    // Detect project type
    if fileExists("go.mod") {
        config.ProjectType = "go"
    } else if fileExists("package.json") {
        config.ProjectType = "node"
    } else if fileExists("requirements.txt") {
        config.ProjectType = "python"
    }

    // Use sensible resource defaults based on available memory
    config.MaxAgents = min(runtime.NumCPU(), 4)

    return config
}
```

#### 2. One-Liner Commands

```bash
# Current (complex)
ntm spawn --session=myproject --agents=claude:2,codex:1 \
    --workdir=/path/to/project --config=~/.config/ntm/config.toml

# Proposed (simple)
ntm go "refactor the auth module to use JWT"

# Or even simpler - just start working
ntm
# Auto-detects project, spawns appropriate agents, opens dashboard
```

#### 3. Interactive Prompts When Needed

```
$ ntm go "implement user authentication"

Detected: Go project (go.mod found)
Available agents: Claude, Codex

? How many agents should work on this? (1-4) [2]
? Enable auto-restart if agents crash? [Y/n]

Starting session "auth-impl-2024-01-03"...
├── Agent 1 (claude-opus): Planning...
└── Agent 2 (claude-sonnet): Ready

Dashboard: http://localhost:8080
Press Ctrl+C to stop, or use `ntm attach` from another terminal.
```

### Implementation Strategy

1. **Create `ntm go` command** - single entry point for quick tasks
2. **Build environment detection** - project type, available agents
3. **Add interactive prompts** - only ask what can't be auto-detected
4. **Store learned preferences** - remember choices for next time

---

## Notifications System

### Why It Matters

Users shouldn't need to watch the terminal constantly. They should be able to work on other things and be notified when:
- An agent needs input or approval
- A task completes successfully
- An error occurs that needs attention
- Rate limiting or other issues arise

### Proposed Implementation

#### 1. Desktop Notifications

Using native OS notification APIs:

```go
// internal/notify/desktop.go
func SendDesktopNotification(title, body string, urgency Urgency) error {
    switch runtime.GOOS {
    case "darwin":
        return sendMacOSNotification(title, body, urgency)
    case "linux":
        return sendLinuxNotification(title, body, urgency)
    case "windows":
        return sendWindowsNotification(title, body, urgency)
    }
    return nil
}
```

**Notification Types:**
| Event | Title | Body | Urgency |
|-------|-------|------|---------|
| Agent needs input | "Agent waiting" | "claude-1 is waiting for your response" | High |
| Task complete | "Task complete" | "Auth refactoring finished successfully" | Normal |
| Error occurred | "Agent error" | "claude-2 crashed: context overflow" | Critical |
| Approval needed | "Approval required" | "3 file changes pending review" | High |

#### 2. Slack/Discord Webhooks

```toml
# ~/.config/ntm/config.toml
[notifications.slack]
enabled = true
webhook_url = "https://hooks.slack.com/services/..."
events = ["error", "complete", "approval_needed"]

[notifications.discord]
enabled = true
webhook_url = "https://discord.com/api/webhooks/..."
events = ["error", "complete"]
```

**Slack Message Format:**
```json
{
  "blocks": [
    {
      "type": "header",
      "text": {"type": "plain_text", "text": "🤖 NTM: Task Complete"}
    },
    {
      "type": "section",
      "fields": [
        {"type": "mrkdwn", "text": "*Session:*\nauth-refactor"},
        {"type": "mrkdwn", "text": "*Duration:*\n45m 23s"},
        {"type": "mrkdwn", "text": "*Files Changed:*\n12"},
        {"type": "mrkdwn", "text": "*Status:*\n✅ Success"}
      ]
    }
  ]
}
```

#### 3. Sound Cues

Optional audio feedback for accessibility and awareness:

```toml
[notifications.sound]
enabled = true
on_complete = "chime"      # Built-in sound
on_error = "alert"         # Built-in sound
on_approval = "~/sounds/approval.wav"  # Custom sound
volume = 0.7
```

### Implementation Strategy

1. **Create notification abstraction** - `internal/notify/notify.go`
2. **Implement desktop notifications** - platform-specific backends
3. **Add webhook support** - Slack, Discord, generic HTTP
4. **Add sound support** - cross-platform audio playback
5. **Configuration system** - per-event notification routing

---

## Session Templates

### Why It Matters

Users frequently need similar session configurations:
- Code review: 2 reviewers + 1 summarizer
- Feature development: planner + coder + tester
- Bug investigation: 1 agent with verbose logging
- Pair programming: 2 collaborating agents

Currently, each session requires manual configuration.

### Proposed Implementation

#### Built-in Templates

```yaml
# internal/templates/code-review.yaml
name: code-review
description: "Two reviewers analyze code, one summarizes findings"
agents:
  - name: reviewer-1
    type: claude
    model: opus
    system_prompt: |
      You are a senior code reviewer. Focus on:
      - Logic errors and edge cases
      - Security vulnerabilities
      - Performance issues

  - name: reviewer-2
    type: claude
    model: opus
    system_prompt: |
      You are a senior code reviewer. Focus on:
      - Code style and readability
      - Best practices adherence
      - Documentation quality

  - name: summarizer
    type: claude
    model: sonnet
    depends_on: [reviewer-1, reviewer-2]
    system_prompt: |
      Summarize the findings from the two reviewers.
      Prioritize issues by severity.
      Suggest a review order for addressing issues.
```

#### User Commands

```bash
# List available templates
$ ntm template list
Built-in templates:
  code-review     Two reviewers + summarizer
  feature-dev     Planner + coder + tester
  debug           Single agent with verbose logging
  pair            Two collaborating agents

Custom templates:
  my-workflow     Your saved workflow (created 2024-01-01)

# Use a template
$ ntm template use code-review
Starting session from template "code-review"...
├── reviewer-1 (claude-opus): Ready
├── reviewer-2 (claude-opus): Ready
└── summarizer (claude-sonnet): Waiting for reviewers

# Save current session as template
$ ntm template save my-refactor-workflow
Template saved: my-refactor-workflow

# Edit a template
$ ntm template edit my-refactor-workflow
# Opens in $EDITOR

# Share a template
$ ntm template export my-workflow > my-workflow.yaml
$ ntm template import colleague-workflow.yaml
```

### Implementation Strategy

1. **Define template schema** - YAML format with validation
2. **Bundle built-in templates** - embed in binary
3. **User template storage** - `~/.config/ntm/templates/`
4. **Template commands** - list, use, save, edit, export, import
5. **Template variables** - `{{ .ProjectName }}`, `{{ .GitBranch }}`

---

## Intelligent Error Recovery

### Why It Matters

Current errors are technical and unhelpful:
```
Error: agent crashed: exit status 1
```

Users need:
- Clear explanation of what went wrong
- Actionable suggestions to fix it
- One-command recovery options

### Proposed Implementation

#### Error Categories and Suggestions

```go
// internal/errors/recovery.go
type RecoverableError struct {
    Code        ErrorCode
    Message     string
    Details     string
    Suggestions []Suggestion
}

type Suggestion struct {
    Description string
    Command     string
    Risk        Risk // low, medium, high
}

var errorSuggestions = map[ErrorCode][]Suggestion{
    ErrContextOverflow: {
        {
            Description: "Save current state before recovery",
            Command:     "ntm checkpoint save",
            Risk:        RiskLow,
        },
        {
            Description: "Compact context to reduce size",
            Command:     "ntm context compact --aggressive",
            Risk:        RiskMedium,
        },
        {
            Description: "Restart agent with fresh context",
            Command:     "ntm restart %s --fresh",
            Risk:        RiskMedium,
        },
    },
    ErrRateLimit: {
        {
            Description: "Wait and retry automatically",
            Command:     "ntm retry --backoff=exponential",
            Risk:        RiskLow,
        },
        {
            Description: "Switch to a different agent/model",
            Command:     "ntm switch %s --model=sonnet",
            Risk:        RiskLow,
        },
    },
    // ... more error types
}
```

#### User-Facing Output

```
❌ Agent "planner" crashed: context window exceeded

   The agent's context grew to 210,000 tokens, exceeding the
   200,000 token limit for Claude Opus.

   Suggested fixes:
   ┌────────────────────────────────────────────────────────┐
   │ 1. Save checkpoint (recommended)                       │
   │    $ ntm checkpoint save                               │
   │    Risk: Low - Preserves current state                 │
   │                                                        │
   │ 2. Compact context                                     │
   │    $ ntm context compact --aggressive                  │
   │    Risk: Medium - May lose some conversation history   │
   │                                                        │
   │ 3. Restart with fresh context                          │
   │    $ ntm restart planner --fresh                       │
   │    Risk: Medium - Loses current agent state            │
   └────────────────────────────────────────────────────────┘

   Quick fix: Press [1], [2], or [3] to run a suggestion

   Run `ntm doctor` for full system diagnostics.
```

#### Auto-Recovery Options

```toml
[recovery]
# Automatically attempt recovery for certain errors
auto_recover = true

# What to do on context overflow
context_overflow = "compact"  # compact | checkpoint_and_restart | ask

# What to do on rate limit
rate_limit = "backoff"  # backoff | switch_model | ask

# What to do on crash
crash = "restart"  # restart | checkpoint | ask

# Maximum auto-recovery attempts
max_retries = 3
```

### Implementation Strategy

1. **Categorize all errors** - map exit codes and messages to categories
2. **Build suggestion database** - context-aware recovery suggestions
3. **Implement interactive recovery** - press key to run suggestion
4. **Add auto-recovery** - configurable automatic fixes
5. **Create `ntm doctor`** - comprehensive diagnostics command

---

## IDE Integration

### Why It Matters

Developers spend most of their time in IDEs. Forcing them to switch to a terminal for NTM reduces adoption and workflow efficiency.

### VSCode Extension

#### Features

1. **Sidebar Panel**
   - View active sessions and agents
   - One-click to view agent output
   - Status indicators (working, idle, error)

2. **Command Palette Integration**
   - `NTM: Start Session` - launch from VSCode
   - `NTM: Send to Agent` - send selected code to agent
   - `NTM: Ask Agent` - ask question about current file
   - `NTM: Approve Changes` - review and approve pending changes

3. **Inline Features**
   - CodeLens showing "Ask NTM about this function"
   - Hover information from agent analysis
   - Inline diff preview for pending changes

4. **Output Panel**
   - Dedicated NTM output channel
   - Syntax-highlighted agent responses
   - Clickable file references

#### Implementation

```typescript
// extension.ts
import * as vscode from 'vscode';
import { NtmClient } from './ntm-client';

export function activate(context: vscode.ExtensionContext) {
    const ntm = new NtmClient();

    // Register sidebar view
    const sessionProvider = new SessionTreeProvider(ntm);
    vscode.window.registerTreeDataProvider('ntm-sessions', sessionProvider);

    // Register commands
    context.subscriptions.push(
        vscode.commands.registerCommand('ntm.startSession', () => {
            ntm.startSession(vscode.workspace.rootPath);
        }),
        vscode.commands.registerCommand('ntm.sendToAgent', () => {
            const editor = vscode.window.activeTextEditor;
            const selection = editor?.document.getText(editor.selection);
            ntm.sendToAgent(selection);
        }),
        vscode.commands.registerCommand('ntm.approveChanges', () => {
            ntm.showPendingChanges();
        })
    );

    // Register CodeLens provider
    context.subscriptions.push(
        vscode.languages.registerCodeLensProvider('*', new NtmCodeLensProvider(ntm))
    );
}
```

### Neovim/Vim Plugin

#### Features

```vim
" Commands
:NTMStatus          " Show session status in floating window
:NTMSend            " Send visual selection to agent
:NTMAsk <question>  " Ask agent about current file
:NTMApprove         " Show and approve pending changes
:NTMOutput          " Open agent output in split

" Keybindings (suggested)
nnoremap <leader>ns :NTMStatus<CR>
vnoremap <leader>na :NTMSend<CR>
nnoremap <leader>nq :NTMAsk
nnoremap <leader>no :NTMOutput<CR>
```

#### Implementation

```lua
-- lua/ntm/init.lua
local M = {}

function M.setup(opts)
    opts = opts or {}

    -- Create commands
    vim.api.nvim_create_user_command('NTMStatus', function()
        M.show_status()
    end, {})

    vim.api.nvim_create_user_command('NTMSend', function(args)
        local lines = vim.api.nvim_buf_get_lines(0, args.line1-1, args.line2, false)
        M.send_to_agent(table.concat(lines, '\n'))
    end, { range = true })

    vim.api.nvim_create_user_command('NTMAsk', function(args)
        M.ask_agent(args.args)
    end, { nargs = '+' })
end

function M.show_status()
    local status = vim.fn.system('ntm status --robot')
    local parsed = vim.fn.json_decode(status)
    -- Display in floating window
    M.show_float(M.format_status(parsed))
end

return M
```

### Implementation Strategy

1. **Define NTM API for IDEs** - stable JSON protocol over stdio/socket
2. **Build VSCode extension first** - largest user base
3. **Create Neovim plugin** - second largest among target audience
4. **Document API** - enable community plugins (JetBrains, Emacs, etc.)

---

## Agent Orchestration Patterns

### Why It Matters

Current NTM treats agents as independent workers. Real-world tasks often need:
- Sequential dependencies (plan before implement)
- Parallel execution (multiple files simultaneously)
- Approval gates (review before commit)
- Conditional branching (test results affect next steps)

### Proposed Implementation

#### Pipeline Definition

```yaml
# pipelines/feature-implementation.yaml
name: feature-implementation
description: "Complete feature implementation with planning, coding, review, and testing"

variables:
  feature_description: ""  # Set at runtime

stages:
  - id: plan
    name: "Plan Implementation"
    agent: claude-opus
    prompt: |
      Create a detailed implementation plan for:
      {{ .feature_description }}

      Break it down into specific tasks with file changes.
    outputs:
      - tasks: "$.tasks[]"
    approval: required

  - id: implement
    name: "Implement Tasks"
    agent: claude-sonnet
    parallel: true
    for_each: "{{ .plan.tasks }}"
    prompt: |
      Implement this task:
      {{ .item.description }}

      Files to modify: {{ .item.files }}
    depends_on: [plan]

  - id: review
    name: "Code Review"
    agent: claude-opus
    prompt: |
      Review all the changes made in the implement stage.
      Check for:
      - Correctness
      - Security issues
      - Performance problems
      - Code style
    depends_on: [implement]
    approval: required

  - id: test
    name: "Write and Run Tests"
    agent: codex
    prompt: |
      Write tests for the new functionality.
      Run them and report results.
    depends_on: [review]
    on_failure:
      goto: implement
      max_retries: 2

  - id: complete
    name: "Completion Summary"
    agent: claude-sonnet
    prompt: |
      Summarize what was accomplished.
      List all files changed.
      Provide any follow-up recommendations.
    depends_on: [test]
```

#### Pipeline Commands

```bash
# Run a pipeline
$ ntm pipeline run feature-implementation \
    --var feature_description="Add user authentication with JWT"

# View pipeline status
$ ntm pipeline status
Pipeline: feature-implementation (running)
┌─────────┬────────────────────┬──────────┬──────────┐
│ Stage   │ Name               │ Status   │ Duration │
├─────────┼────────────────────┼──────────┼──────────┤
│ plan    │ Plan Implementation│ ✅ Done  │ 2m 34s   │
│ impl    │ Implement Tasks    │ 🔄 3/5   │ 8m 12s   │
│ review  │ Code Review        │ ⏳ Waiting│ -        │
│ test    │ Write and Run Tests│ ⏳ Waiting│ -        │
│ complete│ Completion Summary │ ⏳ Waiting│ -        │
└─────────┴────────────────────┴──────────┴──────────┘

# Cancel a pipeline
$ ntm pipeline cancel

# View pipeline history
$ ntm pipeline history
```

### Implementation Strategy

1. **Extend existing pipeline system** - NTM already has basic pipelines
2. **Add parallel execution** - fan-out/fan-in patterns
3. **Implement approval gates** - integrate with slb
4. **Add conditional logic** - on_failure, when clauses
5. **Create pipeline UI** - visual pipeline editor in web dashboard

---

## Interactive Tutorial & Onboarding

### Why It Matters

New users need guidance. A well-designed tutorial can:
- Reduce time to first success from hours to minutes
- Prevent common mistakes
- Build confidence
- Reduce support burden

### Proposed Implementation

#### First-Run Experience

```
$ ntm

╔══════════════════════════════════════════════════════════════════╗
║                                                                  ║
║   ███╗   ██╗████████╗███╗   ███╗                                 ║
║   ████╗  ██║╚══██╔══╝████╗ ████║                                 ║
║   ██╔██╗ ██║   ██║   ██╔████╔██║                                 ║
║   ██║╚██╗██║   ██║   ██║╚██╔╝██║                                 ║
║   ██║ ╚████║   ██║   ██║ ╚═╝ ██║                                 ║
║   ╚═╝  ╚═══╝   ╚═╝   ╚═╝     ╚═╝                                 ║
║                                                                  ║
║   Named Tmux Manager - AI Agent Orchestration                    ║
║                                                                  ║
╚══════════════════════════════════════════════════════════════════╝

👋 Welcome! Looks like this is your first time using NTM.

? Would you like to run the interactive tutorial? (recommended for new users)
  ❯ Yes, show me around (5 minutes)
    No, I know what I'm doing
    Just show me the quick start
```

#### Tutorial Sections

```
Tutorial Progress: [████░░░░░░] 40%

Section 3 of 5: Creating Your First Session

NTM manages "sessions" - workspaces where AI agents collaborate.
Let's create one now.

┌────────────────────────────────────────────────────────────────┐
│  Try this command:                                             │
│                                                                │
│  $ ntm spawn --agents=1                                        │
│                                                                │
└────────────────────────────────────────────────────────────────┘

Type the command above, or press [Enter] to run it automatically.
Press [S] to skip this section, [Q] to quit tutorial.
```

#### Contextual Help

```bash
# Help with examples
$ ntm spawn --help

ntm spawn - Create a new NTM session with AI agents

USAGE:
    ntm spawn [OPTIONS]

OPTIONS:
    --session, -s    Session name (default: auto-generated)
    --agents, -a     Number or specification of agents (default: 1)
    --workdir, -w    Working directory (default: current)
    --template, -t   Use a session template

EXAMPLES:
    # Simple: one agent in current directory
    ntm spawn

    # Multiple agents
    ntm spawn --agents=3

    # Named session with specific agents
    ntm spawn --session=my-project --agents=claude:2,codex:1

    # Using a template
    ntm spawn --template=code-review

TIPS:
    💡 Use `ntm template list` to see available templates
    💡 Use `ntm go "task"` for quick one-off tasks
```

### Implementation Strategy

1. **Create tutorial framework** - step-by-step progression
2. **Write tutorial content** - clear, concise, tested
3. **Add progress persistence** - resume where you left off
4. **Implement contextual help** - examples in all --help output
5. **Add `ntm docs`** - searchable documentation browser

---

## Shareable Sessions

### Why It Matters

Development is collaborative. Users need to:
- Show progress to teammates
- Get help from experts
- Document what happened
- Review async (not everyone online at same time)

### Proposed Implementation

#### Read-Only Sharing

```bash
# Generate shareable link (read-only, expires in 24h)
$ ntm share
Session shared! View at:
https://ntm.sh/s/abc123xyz

Link expires in 24 hours. Anyone with the link can:
- View real-time agent output
- See file changes
- NOT send commands or make changes

To stop sharing: ntm share --stop
```

#### Team Collaboration

```bash
# Invite teammate with write access
$ ntm invite teammate@company.com --role=collaborator
Invitation sent! They can join with:
  ntm join abc123xyz

# Or generate a join code
$ ntm invite --code
Join code: BLUE-FISH-GAMMA
Expires in 1 hour. Share this code with your teammate.
They can join with: ntm join BLUE-FISH-GAMMA
```

#### Session Export

```bash
# Export session as HTML report
$ ntm export --format=html > session-report.html

# Export as markdown (for GitHub issues, docs)
$ ntm export --format=markdown > session-report.md

# Export raw JSON (for tooling)
$ ntm export --format=json > session-data.json
```

### Implementation Strategy

1. **Build sharing infrastructure** - temporary URLs, access control
2. **Create read-only view** - web-based session viewer
3. **Add collaboration** - invite system, join codes
4. **Implement export** - HTML, Markdown, JSON formats
5. **Security review** - ensure proper access control

---

## UX Polish

### Small Improvements, Big Impact

#### 1. Progress & Feedback

Every operation should feel responsive:

```
$ ntm spawn --agents=3
Creating session "dev-session"...
├── Initializing tmux session... ✓
├── Starting agent 1/3 (claude-opus)... ✓
├── Starting agent 2/3 (claude-sonnet)... ✓
├── Starting agent 3/3 (codex)... ✓
├── Verifying agent health... ✓
└── Session ready in 2.3s

┌─────────────────────────────────────────────────────┐
│  Session "dev-session" is running                   │
│                                                     │
│  Attach:     ntm attach                             │
│  Dashboard:  ntm dashboard (or http://localhost:8080)│
│  Status:     ntm status                             │
│  Stop:       ntm stop                               │
└─────────────────────────────────────────────────────┘
```

#### 2. Fuzzy Matching & Typo Tolerance

```
$ ntm spwan
Unknown command: spwan

Did you mean?
  ntm spawn  - Create a new session with AI agents

Run `ntm help` for all commands.
```

```
$ ntm checkpoint lst
Unknown subcommand: lst

Did you mean?
  ntm checkpoint list  - List all checkpoints

Run `ntm checkpoint --help` for all subcommands.
```

#### 3. Contextual Hints

```
$ ntm status
Session: auth-refactor (running 45m)

Agents:
  claude-1: idle (last activity 12m ago)
  claude-2: working on src/auth/jwt.go (active)
  codex-1:  idle (last activity 3m ago)

Checkpoints: 2 saved

💡 Hints:
   - claude-1 has been idle for a while
     Send a task: ntm send claude-1 "review the jwt implementation"

   - Consider saving a checkpoint
     Run: ntm checkpoint save "before-testing"
```

#### 4. Confirmation for Destructive Actions

```
$ ntm stop --force
⚠️  Warning: This will immediately terminate all agents.
   Unsaved work may be lost.

   Session: auth-refactor
   Agents: 3 (2 active, 1 idle)
   Running time: 1h 23m
   Last checkpoint: 45m ago

? Are you sure you want to stop this session? (y/N)

💡 Tip: Save a checkpoint first with `ntm checkpoint save`
```

#### 5. Command Suggestions

```
$ ntm
No command specified. What would you like to do?

  ntm spawn         Create a new session with AI agents
  ntm go <task>     Quick one-off task
  ntm status        Show current session status
  ntm dashboard     Open the web dashboard
  ntm template list Show available session templates

Run `ntm help` for all commands.
```

### Implementation Strategy

1. **Audit all commands** - ensure consistent progress feedback
2. **Implement fuzzy matching** - Levenshtein distance for suggestions
3. **Add hint system** - context-aware suggestions
4. **Review destructive operations** - add confirmation prompts
5. **Create empty-command handler** - helpful guidance when no args

---

## Implementation Roadmap

### Phase 1: Foundation (Weeks 1-4)
**Goal:** Core infrastructure for future features

- [ ] HTTP/WebSocket server in ntm binary
- [ ] Define IDE/external tool API
- [ ] Notification system framework
- [ ] Error recovery framework
- [ ] Template system

### Phase 2: Web Dashboard MVP (Weeks 5-8)
**Goal:** Basic but functional web interface

- [ ] Frontend framework setup (Svelte)
- [ ] Session list and management
- [ ] Real-time agent output streaming
- [ ] Basic file change viewer
- [ ] Desktop notifications

### Phase 3: UX Polish (Weeks 9-10)
**Goal:** Smooth, intuitive experience

- [ ] Zero-config quick start (`ntm go`)
- [ ] Interactive tutorial
- [ ] Improved error messages
- [ ] Fuzzy matching and hints
- [ ] Progress feedback for all commands

### Phase 4: IDE Integration (Weeks 11-14)
**Goal:** Meet developers where they work

- [ ] VSCode extension
- [ ] Neovim plugin
- [ ] API documentation for community plugins

### Phase 5: Advanced Features (Weeks 15-20)
**Goal:** Power user capabilities

- [ ] Advanced pipeline orchestration
- [ ] Session sharing and collaboration
- [ ] Export functionality
- [ ] Integration with external projects (account manager, memory, slb)

### Phase 6: Integration & Polish (Weeks 21-24)
**Goal:** Production-ready

- [ ] coding_agent_account_manager integration
- [ ] cass_memory_system integration
- [ ] slb approval system integration
- [ ] Security audit
- [ ] Performance optimization
- [ ] Documentation overhaul

---

## Success Metrics

### Adoption Metrics
- Time to first successful session < 5 minutes
- Tutorial completion rate > 70%
- Weekly active users growth

### Engagement Metrics
- Average session length
- Commands per session
- Web dashboard vs CLI usage ratio

### Quality Metrics
- Error recovery success rate > 80%
- User-reported issues decrease
- Support ticket volume

### Satisfaction Metrics
- NPS score
- GitHub stars growth rate
- Community contributions

---

## Conclusion

This plan transforms NTM from a capable power-user tool into an accessible, compelling platform for AI-assisted development. The key insight is that **accessibility drives adoption**, and adoption drives community, which drives further development.

The highest-impact improvements are:
1. **Web Dashboard** - opens NTM to non-terminal users
2. **Zero-Config Quick Start** - reduces barrier to entry
3. **Notifications** - frees users from terminal watching
4. **IDE Integration** - meets developers where they work

With the integration of the three related projects (account manager, memory system, approval system), NTM becomes a comprehensive platform for managing AI-assisted development workflows.
