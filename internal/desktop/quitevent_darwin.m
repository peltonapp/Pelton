//go:build darwin

#import <Cocoa/Cocoa.h>

extern void peltonQuitEvent(void);
extern void peltonReopenEvent(void);

// PeltonQuitHandler receives the quit Apple event, which is what the Dock's
// Quit, logout and shutdown send. A window's close button never does, so this
// is the one place the two can be told apart.
@interface PeltonQuitHandler : NSObject
@end

@implementation PeltonQuitHandler
- (void)handleQuit:(NSAppleEventDescriptor *)event withReplyEvent:(NSAppleEventDescriptor *)reply {
    peltonQuitEvent();
}

// handleReopen receives a click on the Dock icon. Wails has no reopen handling,
// so a window that started hidden had no way back.
- (void)handleReopen:(NSAppleEventDescriptor *)event withReplyEvent:(NSAppleEventDescriptor *)reply {
    peltonReopenEvent();
}
@end

static PeltonQuitHandler *peltonQuitHandler;

// peltonWatchQuitEvent replaces AppKit's own quit handler. AppKit installs that
// while the app finishes launching, so this is queued on the main thread to run
// after it rather than being overwritten by it.
void peltonWatchQuitEvent(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (peltonQuitHandler == nil) {
            peltonQuitHandler = [PeltonQuitHandler new];
        }
        [[NSAppleEventManager sharedAppleEventManager]
            setEventHandler:peltonQuitHandler
                andSelector:@selector(handleQuit:withReplyEvent:)
              forEventClass:kCoreEventClass
                 andEventID:kAEQuitApplication];
        [[NSAppleEventManager sharedAppleEventManager]
            setEventHandler:peltonQuitHandler
                andSelector:@selector(handleReopen:withReplyEvent:)
              forEventClass:kCoreEventClass
                 andEventID:kAEReopenApplication];
    });
}
