package desktop

import (
	"reflect"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestFilesDroppedNamesTheTarget(t *testing.T) {
	target := &application.DropTargetDetails{Attributes: map[string]string{dropTargetAttribute: "mail-import"}}
	got, ok := filesDropped([]string{"/tmp/a.mbox", "/tmp/b.eml"}, target)
	if !ok {
		t.Fatal("a drop with files was not handed on")
	}
	want := FilesDroppedEvent{Target: "mail-import", Paths: []string{"/tmp/a.mbox", "/tmp/b.eml"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestFilesDroppedWithoutFilesOrTarget(t *testing.T) {
	if _, ok := filesDropped(nil, nil); ok {
		t.Fatal("a drop without files was handed on")
	}
	got, ok := filesDropped([]string{"/tmp/a.mbox"}, nil)
	if !ok || got.Target != "" {
		t.Fatalf("got %+v, %v; want an untargeted drop", got, ok)
	}
}
