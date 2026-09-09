//go:build darwin

#if !__has_feature(objc_arc)
#error popout_darwin.m must be compiled with ARC
#endif

#import "popout_darwin.h"

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <objc/runtime.h>

static char kAttachedKey;
static char kProxyKey;
static char kAttacherKey;
static NSMutableArray *gOwners;

@interface VantagePopoutOwner : NSObject <NSWindowDelegate>
@property(strong) NSWindow *window;
@property(strong) WKWebView *webView;
- (void)detach;
@end

@interface VantageUIDelegate : NSObject <WKUIDelegate>
@property(strong) id original;
@end

static WKWebView *vantage_make_popout(WKWebView *parent, WKWebViewConfiguration *configuration, WKWindowFeatures *features);

@implementation VantagePopoutOwner

- (void)observeValueForKeyPath:(NSString *)keyPath
                      ofObject:(id)object
                        change:(NSDictionary<NSKeyValueChangeKey, id> *)change
                       context:(void *)context {
	if ([keyPath isEqualToString:@"title"]) {
		NSString *title = self.webView.title;
		if (title.length > 0) {
			self.window.title = title;
		}
		return;
	}
	[super observeValueForKeyPath:keyPath ofObject:object change:change context:context];
}

- (void)windowWillClose:(NSNotification *)notification {
	(void)notification;
	[self detach];
	[gOwners removeObject:self];
}

- (void)detach {
	if (self.webView) {
		@try {
			[self.webView removeObserver:self forKeyPath:@"title"];
		} @catch (NSException *ex) {
			(void)ex;
		}
		self.webView.UIDelegate = nil;
		self.webView = nil;
	}
	self.window.delegate = nil;
	self.window = nil;
}

@end

@implementation VantageUIDelegate

- (BOOL)respondsToSelector:(SEL)aSelector {
	if (aSelector == @selector(webView:createWebViewWithConfiguration:forNavigationAction:windowFeatures:) ||
	    aSelector == @selector(webViewDidClose:)) {
		return YES;
	}
	if ([self.original respondsToSelector:aSelector]) {
		return YES;
	}
	return [super respondsToSelector:aSelector];
}

- (id)forwardingTargetForSelector:(SEL)aSelector {
	if ([self.original respondsToSelector:aSelector]) {
		return self.original;
	}
	return [super forwardingTargetForSelector:aSelector];
}

- (WKWebView *)webView:(WKWebView *)webView
    createWebViewWithConfiguration:(WKWebViewConfiguration *)configuration
             forNavigationAction:(WKNavigationAction *)navigationAction
                  windowFeatures:(WKWindowFeatures *)windowFeatures {
	(void)navigationAction;
	NSLog(@"vantage: createWebView for %@", navigationAction.request.URL);
	return vantage_make_popout(webView, configuration, windowFeatures);
}

- (void)webViewDidClose:(WKWebView *)webView {
	for (VantagePopoutOwner *owner in [gOwners copy]) {
		if (owner.webView == webView && owner.window) {
			[owner.window close];
			break;
		}
	}
	if ([self.original respondsToSelector:@selector(webViewDidClose:)]) {
		[self.original webViewDidClose:webView];
	}
}

@end

static void vantage_allow_js_windows(WKWebView *view) {
	@try {
		view.configuration.preferences.javaScriptCanOpenWindowsAutomatically = YES;
		if (@available(macOS 12.3, *)) {
			view.configuration.preferences.elementFullscreenEnabled = YES;
		}
	} @catch (NSException *ex) {
		(void)ex;
	}
	Ivar ivar = class_getInstanceVariable(object_getClass(view), "_configuration");
	if (!ivar) {
		return;
	}
	WKWebViewConfiguration *cfg = object_getIvar(view, ivar);
	if ([cfg isKindOfClass:[WKWebViewConfiguration class]]) {
		cfg.preferences.javaScriptCanOpenWindowsAutomatically = YES;
		if (@available(macOS 12.3, *)) {
			cfg.preferences.elementFullscreenEnabled = YES;
		}
	}
}

static void vantage_enable_native_fullscreen(NSWindow *window) {
	if (!window) {
		return;
	}
	window.collectionBehavior |= NSWindowCollectionBehaviorFullScreenPrimary;
}

static void vantage_install_proxy(WKWebView *view) {
	vantage_enable_native_fullscreen(view.window);
	id current = view.UIDelegate;
	if ([current isKindOfClass:[VantageUIDelegate class]]) {
		vantage_allow_js_windows(view);
		return;
	}
	VantageUIDelegate *proxy = [VantageUIDelegate new];
	proxy.original = current;
	objc_setAssociatedObject(view, &kProxyKey, proxy, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
	view.UIDelegate = proxy;
	vantage_allow_js_windows(view);
	NSLog(@"vantage: pop-out UIDelegate installed on %@", NSStringFromClass(object_getClass(view)));
}

static WKWebView *vantage_make_popout(WKWebView *parent, WKWebViewConfiguration *configuration, WKWindowFeatures *features) {
	CGFloat width = 1280;
	CGFloat height = 800;
	if (features.width) {
		width = features.width.doubleValue;
	}
	if (features.height) {
		height = features.height.doubleValue;
	}
	if (width < 400) {
		width = 400;
	}
	if (height < 300) {
		height = 300;
	}

	NSRect screen = NSScreen.mainScreen.visibleFrame;
	NSRect frame = NSMakeRect(NSMidX(screen) - width / 2.0, NSMidY(screen) - height / 2.0, width, height);
	NSWindow *window = [[NSWindow alloc]
	    initWithContentRect:frame
	              styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable |
	                        NSWindowStyleMaskResizable
	                backing:NSBackingStoreBuffered
	                  defer:NO];
	window.releasedWhenClosed = NO;
	window.title = @"Vantage";
	window.backgroundColor = [NSColor colorWithRed:13.0 / 255.0 green:17.0 / 255.0 blue:23.0 / 255.0 alpha:1.0];
	window.collectionBehavior = NSWindowCollectionBehaviorFullScreenPrimary | NSWindowCollectionBehaviorMoveToActiveSpace;
	if (@available(macOS 10.14, *)) {
		NSAppearance *appearance = parent.window.appearance;
		window.appearance = appearance ?: [NSAppearance appearanceNamed:NSAppearanceNameDarkAqua];
	}

	configuration.preferences.javaScriptCanOpenWindowsAutomatically = YES;
	if (@available(macOS 12.3, *)) {
		configuration.preferences.elementFullscreenEnabled = YES;
	}
	WKWebView *child = [[WKWebView alloc] initWithFrame:NSMakeRect(0, 0, width, height) configuration:configuration];
	child.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
	id parentDelegate = parent.UIDelegate;
	if (parentDelegate) {
		objc_setAssociatedObject(child, &kProxyKey, objc_getAssociatedObject(parent, &kProxyKey), OBJC_ASSOCIATION_RETAIN_NONATOMIC);
		child.UIDelegate = parentDelegate;
	}
	window.contentView = child;

	VantagePopoutOwner *owner = [VantagePopoutOwner new];
	owner.window = window;
	owner.webView = child;
	window.delegate = owner;
	[child addObserver:owner forKeyPath:@"title" options:NSKeyValueObservingOptionNew context:NULL];
	if (!gOwners) {
		gOwners = [NSMutableArray new];
	}
	[gOwners addObject:owner];

	[NSApp activateIgnoringOtherApps:YES];
	[window makeKeyAndOrderFront:nil];
	[window displayIfNeeded];
	return child;
}

static WKWebView *find_webview(NSView *view) {
	if (!view) {
		return nil;
	}
	if ([view isKindOfClass:[WKWebView class]]) {
		return (WKWebView *)view;
	}
	for (NSView *sub in view.subviews) {
		WKWebView *found = find_webview(sub);
		if (found) {
			return found;
		}
	}
	return nil;
}

static BOOL vantage_attach_all(void) {
	BOOL found = NO;
	NSMutableArray *windows = [NSMutableArray array];
	if (NSApp.windows.count) {
		[windows addObjectsFromArray:NSApp.windows];
	}
	if (NSApp.mainWindow && ![windows containsObject:NSApp.mainWindow]) {
		[windows addObject:NSApp.mainWindow];
	}
	if (NSApp.keyWindow && ![windows containsObject:NSApp.keyWindow]) {
		[windows addObject:NSApp.keyWindow];
	}
	for (NSWindow *window in windows) {
		NSView *root = window.contentView.superview ?: window.contentView;
		WKWebView *view = find_webview(root);
		if (!view) {
			continue;
		}
		found = YES;
		if (!objc_getAssociatedObject(view, &kAttachedKey)) {
			objc_setAssociatedObject(view, &kAttachedKey, @YES, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
		}
		vantage_install_proxy(view);
	}
	return found;
}

@interface VantagePopoutAttacher : NSObject
@property int tries;
- (void)tick;
@end

@implementation VantagePopoutAttacher
- (void)tick {
	if (vantage_attach_all() || ++self.tries > 200) {
		if (self.tries > 200) {
			NSLog(@"vantage: pop-out hook did not find a WKWebView");
		}
		objc_setAssociatedObject(NSApp, &kAttacherKey, nil, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
		return;
	}
	[self performSelector:@selector(tick) withObject:nil afterDelay:0.05];
}
@end

void vantage_enable_webkit_popouts(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		VantagePopoutAttacher *existing = objc_getAssociatedObject(NSApp, &kAttacherKey);
		if (existing) {
			if (vantage_attach_all()) {
				return;
			}
			return;
		}
		VantagePopoutAttacher *attacher = [VantagePopoutAttacher new];
		objc_setAssociatedObject(NSApp, &kAttacherKey, attacher, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
		[attacher tick];
	});
}
