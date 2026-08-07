package ports

import (
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// ErrSessionNotFound reports an observation for an unknown session id.
var ErrSessionNotFound = errors.New("session not found")

// SpawnConfig is the request to start a new session: which project/issue, which
// agent harness, and the branch/prompt the agent launches with.
type SpawnConfig struct {
	ProjectID domain.ProjectID
	IssueID   domain.IssueID
	// IssueContext is optional pre-fetched tracker context for the task prompt.
	// Standing rules stay in SystemPrompt; issue facts belong to the user task.
	IssueContext string
	Kind         domain.SessionKind
	Harness      domain.AgentHarness
	// Model pins the model the session launches with. Empty falls back to the
	// role/project agent config. Spawn trims it and records the resolved value
	// on the session row, so the worker-mix census can group live sessions on
	// (harness, model).
	Model  string
	Effort domain.Effort
	Branch string
	Prompt string

	// AgentConfig overrides the resolved project/role agent config for this
	// single spawn. Empty fields keep the project defaults.
	AgentConfig AgentConfig

	// RequestedMode is the caller's explicit session mode, or empty to let the
	// daemon resolve its default. It is validated and persisted before any
	// controller launches. A later explicit interface transition may replace that
	// controller while preserving the AO session. An unsupported explicit request
	// fails the spawn rather than falling back to the other mode.
	RequestedMode domain.SessionMode

	// IssueTitle is the work item's title, resolved once by the session service
	// on whichever spawn path the request arrived through. It feeds the trailing
	// slug of the daemon-computed display name; an empty title degrades the name
	// rather than failing the spawn.
	IssueTitle string

	// DisplayName is the user-facing sidebar label. EMPTY IS THE SIGNAL that asks
	// the daemon to compute the name from the session's role and work item — it
	// does not mean "unnamed". A non-empty value is a deliberate operator
	// override and is honored as-is.
	DisplayName string

	// Force overrides the pause guard: a forced worker spawn proceeds even while
	// the project or fleet is paused. It is the manual escape hatch for an
	// operator who needs work started during a pause.
	Force bool
	// Attachments are files pasted or dropped into the task brief. They are
	// written into the session worktree and referenced by path in the prompt so
	// the agent can read them (CLI agents receive the prompt as text and cannot
	// consume inline binary data). Any file type is accepted except for
	// explicitly blocked types (e.g., SVG for security reasons).
	Attachments []SpawnAttachment
}

// SpawnAttachment is a single file attached to a spawn request. Data holds the
// already-decoded bytes; the manager derives the on-disk filename from the
// MIME type.
type SpawnAttachment struct {
	// Ext is the file extension (including the leading dot, e.g. ".png")
	// inferred from the attachment's declared MIME type, or ".bin" for unknown types.
	Ext  string
	Data []byte
}
