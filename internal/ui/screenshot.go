package ui

/*
#cgo pkg-config: gtk4
#include <stdint.h>
#include <stdlib.h>
#include <gtk/gtk.h>

// Renders a realized widget (with the dialogs inside it) into a PNG file.
// gotk4 cannot pass GskRenderNode around, so this stays in C.
static int klient_save_widget_png(uintptr_t widget, const char *path, double scale) {
	GtkWidget *w = (GtkWidget *)widget;
	int width = gtk_widget_get_width(w), height = gtk_widget_get_height(w);
	if (width <= 0 || height <= 0)
		return 0;
	GdkPaintable *p = gtk_widget_paintable_new(w);
	GtkSnapshot *s = gtk_snapshot_new();
	gtk_snapshot_scale(s, scale, scale);
	gdk_paintable_snapshot(p, s, width, height);
	GskRenderNode *node = gtk_snapshot_free_to_node(s);
	g_object_unref(p);
	if (node == NULL)
		return 0;
	GskRenderer *r = gtk_native_get_renderer(gtk_widget_get_native(w));
	graphene_rect_t rect = GRAPHENE_RECT_INIT(0, 0, width * scale, height * scale);
	GdkTexture *t = gsk_renderer_render_texture(r, node, &rect);
	gsk_render_node_unref(node);
	if (t == NULL)
		return 0;
	G_GNUC_BEGIN_IGNORE_DEPRECATIONS
	gboolean ok = gdk_texture_save_to_png(t, path);
	G_GNUC_END_IGNORE_DEPRECATIONS
	g_object_unref(t);
	return ok;
}
*/
import "C"

import (
	"errors"
	"unsafe"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// saveWidgetPNG renders a realized widget into a PNG at the given scale.
func saveWidgetPNG(w gtk.Widgetter, path string, scale float64) error {
	ptr := coreglib.InternObject(w).Native()
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	if C.klient_save_widget_png(C.uintptr_t(ptr), cpath, C.double(scale)) == 0 {
		return errors.New("rendering the widget failed")
	}
	return nil
}
