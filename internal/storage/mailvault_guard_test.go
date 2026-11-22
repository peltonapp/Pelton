package storage

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// touchesMessages matches SQL that reads or writes the messages table itself,
// or message_trust, which holds a message's sender headers in clear.
var touchesMessages = regexp.MustCompile(`(?i)\b(from|join|update|into)\s+(messages\b(?:[^_]|$)|message_trust\b)`)

// vaultGuards are the names that keep vault mail out of a query: the SQL
// predicate for aggregates and the gates for the mail space.
var vaultGuards = []string{
	"notVaulted", "notVaultedM", "visibleFolderIDs", "visibleMessages", "visibleMessage",
	"FolderVisible", "sealedKey",
}

// vaultOptOuts are the functions allowed to touch messages without a guard,
// each with the reason it cannot leak sealed content or presence. Adding a
// query over messages anywhere else fails TestEveryMessageQueryIsVaultGuarded
// until it carries a guard or is listed here: vault mail is excluded by
// default.
var vaultOptOuts = map[string]string{
	// the vault's own machinery.
	"insertMessage":          "the insert path; sealed rows reach it emptied by insertSealed",
	"fillSealed":             "fills a vaulted stub with emptied columns and seals the content",
	"upsertListMeta":         "a vaulted folder's new stub goes through insertListSealed, a sealed row refreshes list state only",
	"replaceMessageBody":     "routes a vaulted folder's fill through fillSealed inside the write transaction",
	"InsertMessage":          "routes a vaulted folder through insertSealed",
	"sealedState":            "folder and vault ids only, read by the gates",
	"sealExisting":           "the sealing job",
	"unsealOne":              "the unsealing job, behind the gate",
	"UnsealPending":          "ids of one vault, behind the gate",
	"SealPending":            "ids of one vault",
	"CountVaultWork":         "counts of one vault for its progress",
	"DeleteMailVaultIfEmpty": "existence check of one vault",
	"rebuildThreadKeys":      "rewrites thread keys from the ids already in the rows, keyed hashes for sealed mail",
	"purgeDerived":           "deletes what other tables copied out of a message being sealed",

	// writes keyed by a row, folder or thread the caller already holds.
	"DeleteMessage":         "write by id",
	"MarkFlagsPending":      "write by id",
	"MarkCopiesFlagPending": "flag write by Message-ID; a sealed row's id is a keyed hash",
	"MarkDeletePending":     "write by id", "MarkFolderDeletePending": "write by folder",
	"ClearDeletePending": "write by id", "PurgeFolderMessages": "delete by folder",
	"SetSnooze": "write by id", "ClearSnooze": "write by id", "ReviveSnoozed": "write by id",
	"AdoptPin": "write by id", "MarkReplySent": "flag write through copiesWithout, which is guarded",
	"SetFlagColor": "write by id", "SetOffline": "write by id", "SetPinned": "write by id",
	"SetThreadPinned": "write by thread", "ClearPinPending": "write by folder",
	"markRefetch": "write by id", "ClearRefetchMark": "write by id",
	"MessagesNeedingRefetch": "ids and uids for the charset repair", "NextLocalUID": "the next uid of a local folder",
	"RepairMessageText": "write by id that refuses sealed rows (vault_id = 0)",
	"mergeThread":       "renumbers thread ids, no content",
	"rethread":          "thread ids of one row a fill holds; a sealed fill passes the row with keyed hashes",

	// sync state: ids, uids, flags and pins, never content.
	"ListMessageStates": "uids and flags for sync", "FolderFlagColors": "uids and colors for sync",
	"FolderPinStates": "uids and pins for sync", "DueSnoozes": "ids of due snoozes; readers go through GetMessage",
	"AdoptServerFlags": "flag write by remote id for sync", "ResolvePendingFlags": "flag write by id after a push",
	"DeleteCachedMessage": "delete by id", "FolderHasMessages": "existence of rows in one folder for sync",
	"MessageBodyStates": "body state by remote id for sync", "MessageIDByRemoteID": "id by remote id for sync",
	"MessageIDsByRemoteID": "ids by remote id for sync", "MessageNeedsBody": "body state of one row for sync",
	"MessageSizesByRemoteID": "sizes by remote id for sync", "MessageStatesByRemoteID": "uids and flags by remote id for sync",
	"RemoteIDsNeedingBody": "remote ids of stubs for the body pass", "remoteIDsNeedingBodyNewestQuery": "builder for RemoteIDsNeedingBody",
	"bodyFillTarget": "id and body state of the row a fill writes", "RecordSearchState": "search state write by id",
	"recoverStagingRoot":     "ids of rows whose attachment files were staged",
	"reindexBatch":           "read for ListMessagesForReindex, which drops every row of a vault folder and every sealed row",
	"ListMessagesForReindex": "drops every row of a vault folder and every sealed row: vault mail is never indexed",

	// threading of legacy rows: sealed rows are threaded on insert with keyed hashes and an empty subject.
	"BackfillThreads": "threads rows inserted before threading existed", "unthreadedRows": "rows with thread_id 0",
	"noteTargetFor": "account, thread and Message-ID of a row for a note key; a sealed row's id is a keyed hash",

	// pure SQL builders: their callers filter folder ids through visibleFolderIDs.
	"pageQuery": "builder for QueryMessages", "folderWhere": "builder for the list queries",
	"pinClause": "builder for the list queries",
}

func hasGuard(text string) bool {
	for _, g := range vaultGuards {
		if regexp.MustCompile(`\b` + g + `\b`).MatchString(text) {
			return true
		}
	}
	return false
}

func TestEveryMessageQueryIsVaultGuarded(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	type fn struct {
		name string
		body string
	}
	var funcs []fn
	// sqlConsts maps each package-level value whose SQL touches messages to
	// whether it carries a guard itself.
	sqlConsts := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Body == nil {
					continue
				}
				funcs = append(funcs, fn{d.Name.Name, string(src[fset.Position(d.Body.Pos()).Offset:fset.Position(d.Body.End()).Offset])})
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					text := string(src[fset.Position(vs.Pos()).Offset:fset.Position(vs.End()).Offset])
					if touchesMessages.MatchString(text) {
						for _, n := range vs.Names {
							sqlConsts[n.Name] = hasGuard(text)
						}
					}
				}
			}
		}
	}
	var unguarded []string
	known := map[string]bool{}
	for _, f := range funcs {
		known[f.name] = true
	}
	for name := range vaultOptOuts {
		if !known[name] {
			t.Errorf("vaultOptOuts names %s, which no longer exists", name)
		}
	}
	for _, f := range funcs {
		direct := touchesMessages.MatchString(f.body)
		viaUnguarded, viaAny := false, false
		for c, guarded := range sqlConsts {
			if regexp.MustCompile(`\b` + c + `\b`).MatchString(f.body) {
				viaAny = true
				viaUnguarded = viaUnguarded || !guarded
			}
		}
		if !direct && !viaAny {
			continue
		}
		if _, ok := vaultOptOuts[f.name]; ok || hasGuard(f.body) {
			continue
		}
		if direct || viaUnguarded {
			unguarded = append(unguarded, f.name)
		}
	}
	sort.Strings(unguarded)
	if len(unguarded) > 0 {
		t.Fatalf("these functions query messages without keeping vault mail out; add a guard or list them in vaultOptOuts with the reason:\n%s",
			strings.Join(unguarded, "\n"))
	}
}
