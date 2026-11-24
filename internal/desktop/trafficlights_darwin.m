//go:build darwin

#import <Cocoa/Cocoa.h>

// peltonTrafficLights measures where a window's traffic lights sit: how far
// below the top edge of the window their centre line is, and how far from the
// leading edge the last of the three ends, both in points. Both are 0 when the
// window has no such buttons. macOS has moved the buttons between releases and
// a toolbar moves them again, so the page asks instead of assuming.
void peltonTrafficLights(void *nsWindow, double *center, double *end) {
    *center = 0;
    *end = 0;
    NSWindow *window = (__bridge NSWindow *)nsWindow;
    NSButton *close = [window standardWindowButton:NSWindowCloseButton];
    NSButton *zoom = [window standardWindowButton:NSWindowZoomButton];
    if (close == nil || zoom == nil || close.superview == nil) {
        return;
    }
    NSRect closeRect = [close convertRect:close.bounds toView:nil];
    NSRect zoomRect = [zoom convertRect:zoom.bounds toView:nil];
    *center = window.frame.size.height - NSMidY(closeRect);
    *end = MAX(NSMaxX(closeRect), NSMaxX(zoomRect));
}
