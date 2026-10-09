package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework UserNotifications
#include <stdbool.h>
#include <stdlib.h>
#import <Foundation/Foundation.h>
#import <UserNotifications/UserNotifications.h>

// peltonCanNotify reports whether this process is running from an app bundle.
// UNUserNotificationCenter requires one: currentNotificationCenter raises an
// exception in a process with no bundle identifier, which would take the app
// down rather than fail to notify. A `go run` or an unbundled dev build has
// none, so the caller falls back to beeep there.
static bool peltonCanNotify(void) {
	return [[NSBundle mainBundle] bundleIdentifier] != nil;
}

// peltonNotify posts one notification through UserNotifications.
//
// The point of using this over an osascript "display notification" is the
// icon: macOS shows the icon of whichever process posted, so going through
// osascript brands every new-mail alert as Script Editor. Posting from inside
// the bundle makes macOS use the bundle's own icon, with no asset to ship.
//
// Authorization is requested on every call. After the first time it resolves
// from the stored answer without prompting again, so this costs nothing and
// means the prompt appears the first time a notification would actually be
// shown rather than at launch.
static void peltonNotify(const char *title, const char *body) {
	NSString *t = [NSString stringWithUTF8String:title];
	NSString *b = [NSString stringWithUTF8String:body];
	UNUserNotificationCenter *center = [UNUserNotificationCenter currentNotificationCenter];

	[center requestAuthorizationWithOptions:UNAuthorizationOptionAlert
	                      completionHandler:^(BOOL granted, NSError *error) {
		if (!granted) {
			// the only signal there is: delivery is asynchronous and the Go
			// caller has already returned. Without this a user whose
			// notifications never arrive has nothing at all to look at.
			NSLog(@"pelton: notification authorization denied: %@", error);
			return;
		}
		UNMutableNotificationContent *content = [[UNMutableNotificationContent alloc] init];
		content.title = t;
		content.body = b;
		// no sound: this change is about the icon, and what Pelton should make
		// a noise about is its own question (#240).
		UNNotificationRequest *request =
		    [UNNotificationRequest requestWithIdentifier:[[NSUUID UUID] UUIDString]
		                                         content:content
		                                         trigger:nil];
		[center addNotificationRequest:request
		         withCompletionHandler:^(NSError *addError) {
			if (addError != nil) {
				NSLog(@"pelton: notification not posted: %@", addError);
			}
		}];
		[content release];
	}];
}
*/
import "C"

import (
	"unsafe"

	"github.com/gen2brain/beeep"
)

// deliverNotification raises one notification. On macOS this goes through
// UserNotifications rather than beeep, which falls back to osascript when
// terminal-notifier is absent and so shows the Script Editor automation icon
// instead of the Pelton logo (#143). Posting from the app bundle makes macOS
// use the bundle icon on its own.
//
// A process with no bundle (a dev run, a cli tool) cannot use the framework at
// all. It keeps the old path and so keeps the wrong icon. Delivery is
// asynchronous once handed over, so a rejected authorization or a failed post
// is not reported back here; a notification is not worth failing a sync over.
//
// No click callback is wired up, so the message id on the notification goes
// unused here; that would need a UNUserNotificationCenter delegate on the
// bundle, which is its own piece of work.
func (a *App) deliverNotification(n notification) error {
	if !bool(C.peltonCanNotify()) {
		beeep.AppName = notifyAppName
		return beeep.Notify(n.title, n.body, "")
	}

	cTitle := C.CString(n.title)
	defer C.free(unsafe.Pointer(cTitle))
	cBody := C.CString(n.body)
	defer C.free(unsafe.Pointer(cBody))

	C.peltonNotify(cTitle, cBody)
	return nil
}
