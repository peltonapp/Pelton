package desktop

import (
	"bytes"
	"encoding/json"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The lock gate keeps the bound methods shut while the app is locked. Every
// call from a window reaches Go as a POST to the Wails runtime endpoint, and
// the asset server middleware sees it before Wails does, so one check here
// covers every method without each of them knowing about the lock. It fails
// closed: a call it cannot read, a method it does not know, and anything not
// on lockAllowed is refused while locked.

// runtimePath is where the Wails runtime posts its calls.
const runtimePath = "/wails/runtime"

// runtimeChunkHeader marks a call the runtime split into chunks. Those carry
// large payloads, which nothing on the lock screen sends.
const runtimeChunkHeader = "x-wails-chunk-id"

// maxGatedBody is the largest call body the gate reads while locked; anything
// bigger is refused unread.
const maxGatedBody = 64 << 10

// the runtime's object ids (pkg/application/messageprocessor.go).
const (
	runtimeObjectCall        = 0
	runtimeObjectApplication = 2
	runtimeObjectEvents      = 3
	runtimeObjectWindow      = 6
	runtimeObjectScreens     = 7
	runtimeObjectSystem      = 8
	runtimeObjectCancelCall  = 10
)

// lockOpenObjects are the runtime objects that stay reachable while locked:
// window and app chrome (move, minimise, hide, quit), events, screen and
// system queries, and cancelling a call. Clipboard, dialogs, context menus
// and the browser are refused with the calls.
var lockOpenObjects = map[int]bool{
	runtimeObjectApplication: true,
	runtimeObjectEvents:      true,
	runtimeObjectWindow:      true,
	runtimeObjectScreens:     true,
	runtimeObjectSystem:      true,
	runtimeObjectCancelCall:  true,
}

// lockAllowed are the App methods callable while locked: the lock screen's
// own, what main.ts asks before mounting anything, and the window chrome. None
// returns or changes mail, contacts, calendars or settings.
var lockAllowed = map[string]bool{
	"GetLockState":          true,
	"UnlockWithSystem":      true,
	"UnlockWithPassphrase":  true,
	"LockNow":               true,
	"NoteActivity":          true,
	"IsDemoMode":            true,
	"WindowMaterialActive":  true,
	"SystemColorScheme":     true,
	"SetWindowTheme":        true,
	"WindowFrameIntegrated": true,
	"TrafficLights":         true,
	"TitleBarDoubleClick":   true,
	"MinimiseWindow":        true,
	"ToggleMaximiseWindow":  true,
	"IsWindowMaximised":     true,
	"IsFullscreen":          true,
	"ToggleFullscreen":      true,
	"CloseWindow":           true,
	"QuitApp":               true,
	"HideApp":               true,
}

// appMethodPrefix is the fully qualified name the bindings give App's
// methods, and the start of the name a by-name call carries.
var appMethodPrefix = reflect.TypeFor[App]().PkgPath() + ".App."

// lockGate is the middleware in front of the runtime endpoint.
type lockGate struct {
	locked func() bool
	// methods maps a bound method id to its App method name. The id is what
	// the generated bindings call by: the FNV-1a hash of the qualified name.
	methods map[uint32]string
}

// newLockGate builds the gate for App's methods, consulting locked on every
// call.
func newLockGate(locked func() bool) *lockGate {
	t := reflect.TypeFor[*App]()
	methods := make(map[uint32]string, t.NumMethod())
	for i := range t.NumMethod() {
		name := t.Method(i).Name
		methods[methodID(appMethodPrefix+name)] = name
	}
	return &lockGate{locked: locked, methods: methods}
}

// methodID is the id Wails gives a bound method with the qualified name fqn.
func methodID(fqn string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(fqn))
	return h.Sum32()
}

// middleware wraps the asset server's handler chain.
func (g *lockGate) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path != runtimePath || !g.locked() {
			next.ServeHTTP(rw, req)
			return
		}
		if req.Header.Get(runtimeChunkHeader) != "" {
			refuseLocked(rw)
			return
		}
		var body []byte
		if req.Body != nil {
			read, err := io.ReadAll(io.LimitReader(req.Body, maxGatedBody+1))
			if err != nil || len(read) > maxGatedBody {
				refuseLocked(rw)
				return
			}
			body = read
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		if !g.permits(body, req.URL.Query()) {
			refuseLocked(rw)
			return
		}
		next.ServeHTTP(rw, req)
	})
}

// gatedCall is the part of a runtime call the gate reads.
type gatedCall struct {
	Object *int            `json:"object"`
	Method *int            `json:"method"`
	Args   json.RawMessage `json:"args"`
}

// gatedCallArgs names the bound method of a call.
type gatedCallArgs struct {
	MethodID   uint32 `json:"methodID"`
	MethodName string `json:"methodName"`
}

// permits reports whether a runtime call may go through while locked. An
// empty body is read from the query, which is how WebKitGTK can deliver it.
func (g *lockGate) permits(body []byte, query url.Values) bool {
	var call gatedCall
	if len(body) > 0 {
		if err := json.Unmarshal(body, &call); err != nil {
			return false
		}
	} else {
		object, errO := strconv.Atoi(query.Get("object"))
		method, errM := strconv.Atoi(query.Get("method"))
		if errO != nil || errM != nil {
			return false
		}
		call.Object, call.Method = &object, &method
		call.Args = json.RawMessage(query.Get("args"))
	}
	if call.Object == nil || call.Method == nil {
		return false
	}
	if *call.Object != runtimeObjectCall {
		return lockOpenObjects[*call.Object]
	}
	if *call.Method != application.CallBinding {
		return false
	}
	var args gatedCallArgs
	if err := json.Unmarshal(call.Args, &args); err != nil {
		return false
	}
	return lockAllowed[g.methodName(args)]
}

// methodName resolves the App method a call names, or "" for anything else.
func (g *lockGate) methodName(args gatedCallArgs) string {
	if args.MethodName != "" {
		name, ok := strings.CutPrefix(args.MethodName, appMethodPrefix)
		if !ok {
			return ""
		}
		return name
	}
	return g.methods[args.MethodID]
}

// refuseLocked answers a refused call the way Wails answers a bound method
// that returned an error, with the cause saying it was the lock.
func refuseLocked(rw http.ResponseWriter) {
	payload, _ := json.Marshal(&application.CallError{
		Message: LockedError{}.Error(),
		Kind:    application.RuntimeError,
		Cause:   map[string]string{"code": "locked"},
	})
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = rw.Write(payload)
}
