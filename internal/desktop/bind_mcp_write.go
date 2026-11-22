package desktop

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/peltonapp/Pelton/internal/mcpserver"
	"github.com/peltonapp/Pelton/internal/storage"
)

// agentActionLimit caps the log view. It is a record for answering "what did it
// do", not an archive.
const agentActionLimit = 200

// mcpPermissionKey is the settings key for one write tool. Per tool rather than
// per group: the groups exist to make the settings page readable, and enforcing
// on them as well would give two places for the answer to differ.
func mcpPermissionKey(tool string) string {
	return "mcp_allow_" + tool
}

// mcpPermissions reads the current permission set. Absent means off, so a tool
// added in a later version arrives switched off rather than inheriting a yes.
func (a *App) mcpPermissions() mcpserver.Permissions {
	perms := mcpserver.Permissions{}
	for _, tool := range mcpserver.WriteTools {
		perms[tool] = a.boolSetting(mcpPermissionKey(tool), false)
	}
	return perms
}

// MCPPermissionDTO is one write tool as the settings ui shows it.
type MCPPermissionDTO struct {
	Tool    string `json:"tool"`
	Group   string `json:"group"`
	Allowed bool   `json:"allowed"`
}

// MCPPermissions returns every write tool and whether it is permitted.
func (a *App) MCPPermissions() []MCPPermissionDTO {
	perms := a.mcpPermissions()
	out := make([]MCPPermissionDTO, 0, len(mcpserver.WriteTools))
	for _, tool := range mcpserver.WriteTools {
		out = append(out, MCPPermissionDTO{
			Tool:    tool,
			Group:   mcpserver.ToolGroup[tool],
			Allowed: perms[tool],
		})
	}
	return out
}

// SetMCPPermission turns one write tool on or off.
//
// The running server is updated in place rather than restarted. A restart drops
// whatever agent is connected, and if anything else holds the port, a second
// copy of Pelton most often, it fails and leaves the server down, so flipping a
// switch would take the whole feature with it.
func (a *App) SetMCPPermission(tool string, allowed bool) error {
	if _, known := mcpserver.ToolGroup[tool]; !known {
		return fmt.Errorf("desktop: %q is not a write tool", tool)
	}
	if err := a.store.Set(a.ctx, mcpPermissionKey(tool), strconv.FormatBool(allowed)); err != nil {
		return err
	}
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	if a.mcp != nil {
		a.mcp.SetPermissions(a.mcpPermissions())
	}
	return nil
}

// AgentActionDTO is one recorded write, for the log view.
type AgentActionDTO struct {
	ID      int64  `json:"id"`
	Tool    string `json:"tool"`
	Summary string `json:"summary"`
	Error   string `json:"error"`
	When    string `json:"when"`
}

// AgentActions returns the most recent writes an agent made.
func (a *App) AgentActions() ([]AgentActionDTO, error) {
	rows, err := a.store.ListAgentActions(a.ctx, agentActionLimit)
	if err != nil {
		return nil, err
	}
	out := make([]AgentActionDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, AgentActionDTO{
			ID:      r.ID,
			Tool:    r.Tool,
			Summary: r.Summary,
			Error:   r.Error,
			When:    formatDate(r.CreatedAt),
		})
	}
	return out, nil
}

// ClearAgentActions empties the log.
func (a *App) ClearAgentActions() error {
	return a.store.ClearAgentActions(a.ctx)
}

// AgentProposalDTO is a message an agent wants sent, waiting on the user.
type AgentProposalDTO struct {
	ID        int64  `json:"id"`
	AccountID int64  `json:"accountId"`
	To        string `json:"to"`
	Cc        string `json:"cc"`
	Bcc       string `json:"bcc"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	When      string `json:"when"`
}

// AgentProposals returns every proposed message awaiting approval.
func (a *App) AgentProposals() ([]AgentProposalDTO, error) {
	rows, err := a.store.ListAgentProposals(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AgentProposalDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, AgentProposalDTO{
			ID:        r.ID,
			AccountID: r.AccountID,
			To:        r.To,
			Cc:        r.Cc,
			Bcc:       r.Bcc,
			Subject:   r.Subject,
			Body:      r.Body,
			When:      formatDate(r.CreatedAt),
		})
	}
	return out, nil
}

// ApproveAgentProposal sends a proposed message and removes it from the queue.
// It goes through the ordinary send path, so the undo delay and everything else
// applies exactly as if the user had written it, which they have now agreed to.
func (a *App) ApproveAgentProposal(id int64) error {
	p, err := a.store.GetAgentProposal(a.ctx, id)
	if err != nil {
		return err
	}
	if _, err := a.SendMessage(ComposeRequest{
		AccountID: p.AccountID,
		To:        proposalAddresses(p.To),
		Cc:        proposalAddresses(p.Cc),
		Bcc:       proposalAddresses(p.Bcc),
		Subject:   p.Subject,
		Text:      p.Body,
	}); err != nil {
		return err
	}
	a.recordAgentAction(mcpserver.ToolSendMessage, 0,
		fmt.Sprintf("you approved and sent a proposed message to %s", p.To), nil)
	if err := a.store.DeleteAgentProposal(a.ctx, id); err != nil {
		return err
	}
	a.emit(EventAgentProposals, nil)
	return nil
}

// DiscardAgentProposal throws a proposed message away unsent.
func (a *App) DiscardAgentProposal(id int64) error {
	p, err := a.store.GetAgentProposal(a.ctx, id)
	if err != nil {
		return err
	}
	if err := a.store.DeleteAgentProposal(a.ctx, id); err != nil {
		return err
	}
	a.recordAgentAction(mcpserver.ToolSendMessage, 0,
		fmt.Sprintf("you discarded a proposed message to %s", p.To), nil)
	a.emit(EventAgentProposals, nil)
	return nil
}

// recordAgentAction appends to the log. A failure to record is logged and
// nothing more: losing the record must not undo the action or fail the tool.
func (a *App) recordAgentAction(tool string, messageID int64, summary string, cause error) {
	entry := storage.AgentAction{Tool: tool, MessageID: messageID, Summary: summary}
	if cause != nil {
		entry.Error = cause.Error()
	}
	if err := a.store.RecordAgentAction(a.ctx, entry); err != nil {
		a.log.Error("record agent action", "tool", tool, "err", err)
	}
}

// proposalAddresses turns a stored comma-separated address list back into the
// compose form. An agent supplies bare addresses, so there is no name to carry.
func proposalAddresses(list string) []AddressDTO {
	var out []AddressDTO
	for part := range strings.SplitSeq(list, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, AddressDTO{Email: part})
		}
	}
	return out
}
