//go:build darwin

#import <Foundation/Foundation.h>

extern void peltonScreenLocked(void);

// peltonWatchScreenLock reports the screen locking, which macOS announces to
// every app as a distributed notification. The observer is added on the main
// queue so the notification arrives on the main run loop.
void peltonWatchScreenLock(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [[NSDistributedNotificationCenter defaultCenter]
            addObserverForName:@"com.apple.screenIsLocked"
                        object:nil
                         queue:nil
                    usingBlock:^(NSNotification *note) {
            peltonScreenLocked();
        }];
    });
}
