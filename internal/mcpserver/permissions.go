package mcpserver

// Write tool names. They are the permission keys as well as the tool names an
// agent sees, so there is one vocabulary rather than two that can drift.
const (
	ToolMarkRead      = "mark_read"
	ToolMoveMessage   = "move_message"
	ToolArchive       = "archive_message"
	ToolFlagMessage   = "flag_message"
	ToolSetFlagColor  = "set_flag_color"
	ToolDeleteMessage = "delete_message"
	ToolSendMessage   = "send_message"
)

// WriteTools is every write tool, in the order the settings ui lists them.
var WriteTools = []string{
	ToolMarkRead, ToolMoveMessage, ToolArchive, ToolFlagMessage, ToolSetFlagColor,
	ToolDeleteMessage, ToolSendMessage,
}

// Group names. A group is a convenience in the ui, not a second layer of
// enforcement: only the per-tool permission is ever checked.
const (
	GroupOrganise = "organise"
	GroupDelete   = "delete"
	GroupSend     = "send"
)

// ToolGroup says which group a write tool belongs to. Organise actions are all
// reversible; delete and send are not, which is why they are apart.
var ToolGroup = map[string]string{
	ToolMarkRead:      GroupOrganise,
	ToolMoveMessage:   GroupOrganise,
	ToolArchive:       GroupOrganise,
	ToolFlagMessage:   GroupOrganise,
	ToolSetFlagColor:  GroupOrganise,
	ToolDeleteMessage: GroupDelete,
	ToolSendMessage:   GroupSend,
}

// Permissions says which write tools an agent may use. The zero value allows
// nothing, so a caller that forgets to fill it in gets a read-only server
// rather than an open one.
type Permissions map[string]bool

// Allows reports whether a tool may run.
func (p Permissions) Allows(tool string) bool {
	return p[tool]
}
