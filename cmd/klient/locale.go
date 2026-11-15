package main

/*
#include <locale.h>
#include <stddef.h>

// userLocaleWorks reports whether the C library has the locale that the
// environment asks for.
static int userLocaleWorks(void) { return setlocale(LC_ALL, "") != NULL; }
*/
import "C"

import "os"

// ensureLocale falls back to C.UTF-8 when the C library does not have the
// user's locale. This happens in the Flatpak sandbox when the host locale is
// not installed in the runtime: GTK then logs "Locale not supported by C
// library" and uses the plain "C" locale, WebKitGTK fails to start its
// helper process through flatpak-spawn ("Invalid byte sequence in conversion
// input") and GLib aborts the application.
//
// It has to run before GTK starts. The language of the interface is chosen in
// the init function of the i18n package, which runs before main, so it still
// follows the user's own LANG.
func ensureLocale() {
	if C.userLocaleWorks() != 0 {
		return
	}
	os.Setenv("LC_ALL", "C.UTF-8")
	C.userLocaleWorks()
}
