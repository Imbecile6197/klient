#import <Foundation/Foundation.h>

/// Runs `block` and returns any raised `NSException` instead of aborting.
/// Swift `do/catch` does not catch Objective-C exceptions.
NSException *_Nullable KlientCatchException(void (NS_NOESCAPE ^block)(void)) {
    @try {
        block();
        return nil;
    } @catch (NSException *ex) {
        return ex;
    }
}
