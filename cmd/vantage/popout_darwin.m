//go:build darwin

#import "popout_darwin.h"

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <objc/runtime.h>

static char kAttachedKey;
static NSMutableArray *gOwners;
static BOOL gHooked;
static WKWebView *(*gOriginalCreate)(id, SEL, WKWebView *, WKWebViewConfiguration *, WKNavigationAction *, WKWindowFeatures *);
static void (*gOriginalDidClose)(id, SEL, WKWebView *);

@interface VantagePopoutOwner : NSObject <NSWindowDelegate>
@property(strong) NSWindow *window;
@property(strong) WKWebView *webView;
- (void)detach;
@end

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

static VantagePopoutOwner *owner_for_view(WKWebView *view) {
	for (VantagePopoutOwner *owner in gOwners) {
		if (owner.webView == view) {
			return owner;
		}
	}
	return nil;
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
	window.collectionBehavior = NSWindowCollectionBehaviorMoveToActiveSpace | NSWindowCollectionBehaviorFullScreenAuxiliary;
	if (@available(macOS 10.14, *)) {
		NSAppearance *appearance = parent.window.appearance;
		window.appearance = appearance ?: [NSAppearance appearanceNamed:NSAppearanceNameDarkAqua];
	}

	configuration.preferences.javaScriptCanOpenWindowsAutomatically = YES;
	WKWebView *child = [[WKWebView alloc] initWithFrame:NSZeroRect configuration:configuration];
	child.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
	child.UIDelegate = parent.UIDelegate;
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
	return child;
}

static WKWebView *VantageCreateWebView(id self, SEL cmd, WKWebView *webView, WKWebViewConfiguration *configuration,
    WKNavigationAction *action, WKWindowFeatures *features) {
	(void)self;
	(void)cmd;
	(void)action;
	WKWebView *created = vantage_make_popout(webView, configuration, features);
	if (created) {
		return created;
	}
	if (gOriginalCreate) {
		return gOriginalCreate(self, cmd, webView, configuration, action, features);
	}
	return nil;
}

static void VantageWebViewDidClose(id self, SEL cmd, WKWebView *webView) {
	VantagePopoutOwner *owner = owner_for_view(webView);
	if (owner.window) {
		[owner.window close];
	}
	if (gOriginalDidClose) {
		gOriginalDidClose(self, cmd, webView);
	}
}

static void hook_selector(Class cls, SEL sel, IMP imp, const char *types, IMP *original) {
	Method method = class_getInstanceMethod(cls, sel);
	if (method) {
		*original = method_setImplementation(method, imp);
		return;
	}
	class_addMethod(cls, sel, imp, types);
}

static void vantage_hook_delegate(id delegate) {
	if (!delegate || gHooked) {
		return;
	}
	Class cls = [delegate class];
	hook_selector(cls, @selector(webView:createWebViewWithConfiguration:forNavigationAction:windowFeatures:),
	    (IMP)VantageCreateWebView, "@@:@@@@", (IMP *)&gOriginalCreate);
	hook_selector(cls, @selector(webViewDidClose:), (IMP)VantageWebViewDidClose, "v@:@", (IMP *)&gOriginalDidClose);
	gHooked = YES;
}

static void vantage_attach_view(WKWebView *view) {
	if (!objc_getAssociatedObject(view, &kAttachedKey)) {
		objc_setAssociatedObject(view, &kAttachedKey, @YES, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
		view.configuration.preferences.javaScriptCanOpenWindowsAutomatically = YES;
	}
	vantage_hook_delegate(view.UIDelegate);
}

static WKWebView *find_webview(NSView *view) {
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
	for (NSWindow *window in NSApp.windows) {
		WKWebView *view = find_webview(window.contentView);
		if (!view) {
			continue;
		}
		found = YES;
		vantage_attach_view(view);
	}
	return found && gHooked;
}

@interface VantagePopoutAttacher : NSObject
@property int tries;
- (void)tick;
@end

static char kAttacherKey;

@implementation VantagePopoutAttacher
- (void)tick {
	if (vantage_attach_all() || ++self.tries > 100) {
		objc_setAssociatedObject(NSApp, &kAttacherKey, nil, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
		return;
	}
	[self performSelector:@selector(tick) withObject:nil afterDelay:0.05];
}
@end

void vantage_enable_webkit_popouts(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		VantagePopoutAttacher *attacher = [VantagePopoutAttacher new];
		objc_setAssociatedObject(NSApp, &kAttacherKey, attacher, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
		[attacher tick];
	});
}
