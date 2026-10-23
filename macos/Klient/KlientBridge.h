#ifndef KLIENT_BRIDGE_H
#define KLIENT_BRIDGE_H

#include <stdlib.h>

#ifdef __OBJC__
#import <Foundation/Foundation.h>
/// Runs `block` and returns any raised `NSException` (Swift cannot catch these).
NSException *_Nullable KlientCatchException(void (NS_NOESCAPE ^_Nonnull block)(void));
#endif

typedef void (*KlientEventCB)(const char *json);

void KlientInit(KlientEventCB cb);
char *KlientVersion(void);
char *KlientCall(const char *req);
void KlientFree(char *p);

#endif
