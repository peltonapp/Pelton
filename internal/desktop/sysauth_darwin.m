//go:build darwin

#import <Foundation/Foundation.h>
#import <LocalAuthentication/LocalAuthentication.h>

// peltonCanAuthenticate reports whether the device owner prompt (Touch ID or
// the account password) can be shown. It shows nothing itself.
int peltonCanAuthenticate(void) {
    @autoreleasepool {
        LAContext *context = [[LAContext alloc] init];
        BOOL ok = [context canEvaluatePolicy:LAPolicyDeviceOwnerAuthentication error:nil];
        [context release];
        return ok ? 1 : 0;
    }
}

// peltonAuthenticate shows the device owner prompt and waits for its answer.
// It is a yes or no check: no keychain item is read, written or created. It
// must not run on the main thread, which the prompt's reply needs free.
int peltonAuthenticate(const char *reason, long *code) {
    @autoreleasepool {
        LAContext *context = [[LAContext alloc] init];
        dispatch_semaphore_t done = dispatch_semaphore_create(0);
        __block BOOL passed = NO;
        __block long failure = 0;
        [context evaluatePolicy:LAPolicyDeviceOwnerAuthentication
                localizedReason:[NSString stringWithUTF8String:reason]
                          reply:^(BOOL success, NSError *error) {
            passed = success;
            if (error != nil) {
                failure = (long)error.code;
            }
            dispatch_semaphore_signal(done);
        }];
        dispatch_semaphore_wait(done, DISPATCH_TIME_FOREVER);
        dispatch_release(done);
        [context release];
        *code = failure;
        return passed ? 1 : 0;
    }
}
