package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// dropTargetAttribute marks the elements of the page files may be dropped on.
// The wails runtime only reports drops that land on one of them.
const dropTargetAttribute = "data-file-drop-target"

// onFilesDropped hands files dropped from the system onto a drop target of the
// page to the frontend, naming the target so it knows what they are for.
func (a *App) onFilesDropped(e *application.WindowEvent) {
	ctx := e.Context()
	if ctx == nil {
		return
	}
	if event, ok := filesDropped(ctx.DroppedFiles(), ctx.DropTargetDetails()); ok {
		a.emit(EventFilesDropped, event)
	}
}

// filesDropped builds the event for a drop, or reports false when there is
// nothing to hand on.
func filesDropped(paths []string, target *application.DropTargetDetails) (FilesDroppedEvent, bool) {
	if len(paths) == 0 {
		return FilesDroppedEvent{}, false
	}
	event := FilesDroppedEvent{Paths: paths}
	if target != nil {
		event.Target = target.Attributes[dropTargetAttribute]
	}
	return event, true
}
