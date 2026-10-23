package main

/*
#include <stdlib.h>

typedef void (*KlientEventCB)(const char *json);

static void KlientEventInvoke(KlientEventCB cb, const char *json) {
	if (cb) {
		cb(json);
	}
}
*/
import "C"

import (
	"sync"
	"unsafe"

	"github.com/Imbecile6197/klient/internal/macbridge"
)

var (
	eventMu sync.Mutex
	eventCB C.KlientEventCB
)

//export KlientInit
func KlientInit(cb C.KlientEventCB) {
	eventMu.Lock()
	eventCB = cb
	eventMu.Unlock()
	macbridge.Init(func(json string) {
		eventMu.Lock()
		c := eventCB
		eventMu.Unlock()
		if c == nil {
			return
		}
		cs := C.CString(json)
		defer C.free(unsafe.Pointer(cs))
		C.KlientEventInvoke(c, cs)
	})
}

//export KlientVersion
func KlientVersion() *C.char {
	return C.CString(macbridge.Version)
}

//export KlientCall
func KlientCall(req *C.char) *C.char {
	return C.CString(macbridge.Call(C.GoString(req)))
}

//export KlientFree
func KlientFree(p *C.char) {
	C.free(unsafe.Pointer(p))
}
