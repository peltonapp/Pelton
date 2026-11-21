package cliproto

// Capabilities are the switches of command line access. Each is off until
// the user turns it on, and turning one on turns on no other.
const (
	CapRead          = "read"
	CapSearch        = "search"
	CapChange        = "change"
	CapSend          = "send"
	CapSendUnasked   = "send_unasked"
	CapDeleteUnasked = "delete_unasked"
	CapScripts       = "scripts"
	CapExport        = "export"
	CapPIMRead       = "pim_read"
	CapPIMChange     = "pim_change"
)

// Capabilities is every switch, in the order settings list them.
var Capabilities = []string{
	CapRead, CapSearch, CapChange, CapSend, CapSendUnasked, CapDeleteUnasked,
	CapScripts, CapExport, CapPIMRead, CapPIMChange,
}

// CapabilityLabels are the switch names the settings show, which an error
// about a switch names too.
var CapabilityLabels = map[string]string{
	CapRead:          "Read mail",
	CapSearch:        "Search mail",
	CapChange:        "Change mail",
	CapSend:          "Send mail",
	CapSendUnasked:   "Send without asking",
	CapDeleteUnasked: "Delete permanently without asking",
	CapScripts:       "Run automations",
	CapExport:        "Export mail",
	CapPIMRead:       "Read calendar, tasks and contacts",
	CapPIMChange:     "Add tasks",
}

// The wire commands. Several pelton commands share one: flag, unflag, read,
// unread, archive, trash and move are all CmdAct.
const (
	CmdStatus      = "status"
	CmdAccounts    = "accounts"
	CmdFolders     = "folders"
	CmdList        = "list"
	CmdSearch      = "search"
	CmdShow        = "show"
	CmdAttachments = "attachments"
	CmdAttachment  = "attachment"
	CmdAct         = "act"
	CmdSend        = "send"
	CmdDraft       = "draft"
	CmdCompose     = "compose"
	CmdRun         = "run"
	CmdExport      = "export"
	CmdCalendar    = "calendar"
	CmdTasks       = "tasks"
	CmdTaskAdd     = "task-add"
	CmdContacts    = "contacts"
	CmdOpen        = "open"
)

// Need says what a wire command needs besides command line access being on.
// Cap is the switch, "" for none. Unlocked is false only for status, the one
// command that answers while the app is locked.
type Need struct {
	Cap      string
	Unlocked bool
}

// Needs maps every wire command to what it needs. A command missing here is
// unknown and refused.
var Needs = map[string]Need{
	CmdStatus:      {},
	CmdAccounts:    {Cap: CapRead, Unlocked: true},
	CmdFolders:     {Cap: CapRead, Unlocked: true},
	CmdList:        {Cap: CapRead, Unlocked: true},
	CmdSearch:      {Cap: CapSearch, Unlocked: true},
	CmdShow:        {Cap: CapRead, Unlocked: true},
	CmdAttachments: {Cap: CapRead, Unlocked: true},
	CmdAttachment:  {Cap: CapRead, Unlocked: true},
	CmdAct:         {Cap: CapChange, Unlocked: true},
	CmdSend:        {Cap: CapSend, Unlocked: true},
	CmdDraft:       {Cap: CapSend, Unlocked: true},
	CmdCompose:     {Unlocked: true},
	CmdRun:         {Cap: CapScripts, Unlocked: true},
	CmdExport:      {Cap: CapExport, Unlocked: true},
	CmdCalendar:    {Cap: CapPIMRead, Unlocked: true},
	CmdTasks:       {Cap: CapPIMRead, Unlocked: true},
	CmdTaskAdd:     {Cap: CapPIMChange, Unlocked: true},
	CmdContacts:    {Cap: CapPIMRead, Unlocked: true},
	CmdOpen:        {Unlocked: true},
}

// Actions of CmdAct.
const (
	ActFlag    = "flag"
	ActUnflag  = "unflag"
	ActRead    = "read"
	ActUnread  = "unread"
	ActArchive = "archive"
	ActTrash   = "trash"
	ActMove    = "move"
)
