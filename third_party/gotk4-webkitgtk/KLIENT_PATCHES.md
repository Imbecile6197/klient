# Local patches (Klient)

Subset of github.com/diamondburned/gotk4-webkitgtk/pkg @ v0.0.0-20240108031600-dee1973cf440
(MPL-2.0, see LICENSE): only webkit/v6, javascriptcore/v6 and soup/v3.
Used through a `replace` directive in the root go.mod.

Change: removed `webkit/v6/WebKitPointerLockPermissionRequest*.go` — WebKitGTK 2.54
no longer ships `WebKitPointerLockPermissionRequest`, so the generated cgo code
failed to compile. Klient does not use pointer lock.
