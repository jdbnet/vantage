#include "popout_linux.h"

#include <gtk/gtk.h>
#include <webkit2/webkit2.h>

static const char *kAttached = "vantage-popout-attached";

static void on_title(WebKitWebView *view, GParamSpec *pspec, GtkWindow *window) {
	(void)pspec;
	const char *title = webkit_web_view_get_title(view);
	if (title && title[0] != '\0') {
		gtk_window_set_title(window, title);
	}
}

static gboolean on_close(WebKitWebView *view, GtkWidget *window) {
	(void)view;
	gtk_widget_destroy(window);
	return TRUE;
}

static void on_ready_to_show(WebKitWebView *view, GtkWidget *window) {
	WebKitWindowProperties *props = webkit_web_view_get_window_properties(view);
	GdkRectangle geometry;
	webkit_window_properties_get_geometry(props, &geometry);
	if (geometry.width > 0 && geometry.height > 0) {
		gtk_window_resize(GTK_WINDOW(window), geometry.width, geometry.height);
	}
	gtk_widget_show_all(window);
}

static WebKitWebView *on_create(WebKitWebView *parent, WebKitNavigationAction *navigation, gpointer user_data) {
	(void)navigation;
	(void)user_data;

	GtkWidget *window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
	gtk_window_set_default_size(GTK_WINDOW(window), 1280, 800);
	gtk_window_set_title(GTK_WINDOW(window), "Vantage");
	gtk_window_set_type_hint(GTK_WINDOW(window), GDK_WINDOW_TYPE_HINT_NORMAL);

	GtkWidget *toplevel = gtk_widget_get_toplevel(GTK_WIDGET(parent));
	if (GTK_IS_WINDOW(toplevel)) {
		GdkPixbuf *icon = gtk_window_get_icon(GTK_WINDOW(toplevel));
		if (icon) {
			gtk_window_set_icon(GTK_WINDOW(window), icon);
		}
	}

	GtkWidget *view = webkit_web_view_new_with_related_view(parent);
	gtk_container_add(GTK_CONTAINER(window), view);

	GdkRGBA bg = {13.0 / 255.0, 17.0 / 255.0, 23.0 / 255.0, 1.0};
	webkit_web_view_set_background_color(WEBKIT_WEB_VIEW(view), &bg);

	g_signal_connect(view, "ready-to-show", G_CALLBACK(on_ready_to_show), window);
	g_signal_connect(view, "close", G_CALLBACK(on_close), window);
	g_signal_connect(view, "notify::title", G_CALLBACK(on_title), window);

	gtk_widget_show_all(window);
	return WEBKIT_WEB_VIEW(view);
}

static gboolean on_decide_policy(WebKitWebView *view, WebKitPolicyDecision *decision, WebKitPolicyDecisionType type, gpointer user_data) {
	(void)view;
	(void)user_data;
	if (type == WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION) {
		webkit_policy_decision_use(decision);
		return TRUE;
	}
	return FALSE;
}

static void attach_view(WebKitWebView *view) {
	if (g_object_get_data(G_OBJECT(view), kAttached)) {
		return;
	}
	g_object_set_data(G_OBJECT(view), kAttached, GINT_TO_POINTER(1));

	WebKitSettings *settings = webkit_web_view_get_settings(view);
	webkit_settings_set_javascript_can_open_windows_automatically(settings, TRUE);

	g_signal_connect(view, "create", G_CALLBACK(on_create), NULL);
	g_signal_connect(view, "decide-policy", G_CALLBACK(on_decide_policy), NULL);
}

static void walk_widget(GtkWidget *widget, gpointer data) {
	int *found = data;
	if (WEBKIT_IS_WEB_VIEW(widget)) {
		attach_view(WEBKIT_WEB_VIEW(widget));
		*found = 1;
		return;
	}
	if (GTK_IS_CONTAINER(widget)) {
		gtk_container_forall(GTK_CONTAINER(widget), walk_widget, data);
	}
}

static gboolean attach_tick(gpointer data) {
	int *tries = data;
	int found = 0;
	GList *l;
	GList *toplevels = gtk_window_list_toplevels();
	for (l = toplevels; l != NULL; l = l->next) {
		walk_widget(GTK_WIDGET(l->data), &found);
	}
	g_list_free(toplevels);
	if (found || ++(*tries) > 100) {
		g_free(tries);
		return G_SOURCE_REMOVE;
	}
	return G_SOURCE_CONTINUE;
}

void vantage_enable_webkit_popouts(void) {
	int *tries = g_new0(int, 1);
	g_timeout_add(50, attach_tick, tries);
}
