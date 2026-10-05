// menubar.m is the AppKit side of the status item: an NSStatusItem whose menu
// is rebuilt from Go every time it opens, so displays that come and go are
// always shown correctly.

#import <Cocoa/Cocoa.h>

#include "menubar.h"
#include "_cgo_export.h"

@interface MBController : NSObject <NSMenuDelegate, NSApplicationDelegate>
@end

static NSStatusItem *statusItem;
static NSMenu *displayMenu;
static MBController *controller;

@implementation MBController

// The status item is created here rather than before [NSApp run]: AppKit can
// quietly drop one created before the app has finished launching.
- (void)applicationDidFinishLaunching:(NSNotification *)note {
    statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
    NSImage *icon = [NSImage imageWithSystemSymbolName:@"display" accessibilityDescription:@"Displays"];
    if (icon) {
        icon.template = YES;
        statusItem.button.image = icon;
    } else {
        statusItem.button.title = @"Displays";
    }
    statusItem.visible = YES;

    displayMenu = [[NSMenu alloc] init];
    // Keep AppKit from second-guessing the enabled state set in Go.
    displayMenu.autoenablesItems = NO;
    displayMenu.delegate = self;
    statusItem.menu = displayMenu;

    // A menu bar app has no terminal to complain to, so say what happened.
    fprintf(stderr, "menubar: statusItem=%d button=%d icon=%d visible=%d screens=%lu\n",
        statusItem != nil, statusItem.button != nil, icon != nil, statusItem.visible,
        (unsigned long)NSScreen.screens.count);
}

- (void)menuNeedsUpdate:(NSMenu *)menu {
    [menu removeAllItems];
    menubarRebuild(); // Go calls mb_add_item for each display

    [menu addItem:[NSMenuItem separatorItem]];
    menubarLogin(); // Go calls mb_add_login
    NSMenuItem *quit = [[NSMenuItem alloc] initWithTitle:@"Quit"
                                                  action:@selector(quit:)
                                           keyEquivalent:@"q"];
    quit.target = self;
    [menu addItem:quit];
}

- (void)toggle:(NSMenuItem *)sender {
    menubarToggle((uint32_t)sender.tag);
}

- (void)toggleLogin:(NSMenuItem *)sender {
    menubarLoginToggle();
}

- (void)quit:(id)sender {
    [NSApp terminate:nil];
}

@end

void mb_run(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        // Accessory: a menu bar app, with no icon in the Dock.
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];

        controller = [MBController new];
        NSApp.delegate = controller;
        [NSApp run];
    }
}

void mb_add_item(const char *title, uint32_t id, bool on, bool enabled) {
    NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:@(title)
                                                  action:@selector(toggle:)
                                           keyEquivalent:@""];
    item.target = controller;
    item.tag = (NSInteger)id;
    item.state = on ? NSControlStateValueOn : NSControlStateValueOff;
    item.enabled = enabled;
    [displayMenu addItem:item];
}

void mb_add_login(const char *title, bool on, bool enabled) {
    NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:@(title)
                                                  action:@selector(toggleLogin:)
                                           keyEquivalent:@""];
    item.target = controller;
    item.state = on ? NSControlStateValueOn : NSControlStateValueOff;
    item.enabled = enabled;
    [displayMenu addItem:item];
}
