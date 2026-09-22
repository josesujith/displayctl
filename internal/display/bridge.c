// bridge.c talks to CoreGraphics, IOKit and a few private Apple APIs.
//
// The private symbols below have no public headers. They are the same ones
// BetterDisplay, MonitorControl and m1ddc rely on, and Apple can change them
// in any macOS release.

#include "bridge.h"

#include <ColorSync/ColorSync.h>
#include <CoreFoundation/CoreFoundation.h>
#include <CoreGraphics/CoreGraphics.h>
#include <IOKit/IOKitLib.h>
#include <dlfcn.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef CFTypeRef IOAVServiceRef;
extern IOAVServiceRef IOAVServiceCreateWithService(CFAllocatorRef, io_service_t);
extern IOReturn IOAVServiceReadI2C(IOAVServiceRef, uint32_t chip, uint32_t offset, void *buf, uint32_t len);
extern IOReturn IOAVServiceWriteI2C(IOAVServiceRef, uint32_t chip, uint32_t offset, void *buf, uint32_t len);
extern CFDictionaryRef CoreDisplay_DisplayCreateInfoDictionary(CGDirectDisplayID);

static bool string_equals(CFTypeRef v, CFStringRef want) {
    return v && CFGetTypeID(v) == CFStringGetTypeID() &&
           CFStringCompare(v, want, 0) == kCFCompareEqualTo;
}

// ---- Connect / disconnect --------------------------------------------------
//
// There is no public API for this. SkyLight, the framework behind WindowServer,
// exports the function that adds and removes a display from the desktop, and
// CoreGraphics re-exports it under its older CGS name. Neither is in any SDK,
// so both are looked up at runtime and the tool reports it plainly if a future
// macOS moves them.

typedef CGError (*configure_enabled_fn)(CGDisplayConfigRef, CGDirectDisplayID, bool);
typedef CGError (*get_display_list_fn)(uint32_t, CGDirectDisplayID *, uint32_t *);

static void *skylight(void) {
    static void *handle;
    static bool tried;
    if (!tried) {
        tried = true;
        handle = dlopen("/System/Library/PrivateFrameworks/SkyLight.framework/SkyLight", RTLD_LAZY | RTLD_LOCAL);
    }
    return handle;
}

static configure_enabled_fn configure_enabled(void) {
    static configure_enabled_fn fn;
    static bool tried;
    if (!tried) {
        tried = true;
        void *sl = skylight();
        if (sl) fn = (configure_enabled_fn)dlsym(sl, "SLSConfigureDisplayEnabled");
        if (!fn) {
            void *cg = dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", RTLD_LAZY | RTLD_LOCAL);
            if (cg) fn = (configure_enabled_fn)dlsym(cg, "CGSConfigureDisplayEnabled");
        }
    }
    return fn;
}

bool dc_can_set_enabled(void) { return configure_enabled() != NULL; }

int dc_set_enabled(uint32_t id, bool enabled, bool permanent) {
    configure_enabled_fn configure = configure_enabled();
    if (!configure) return kCGErrorNotImplemented;

    CGDisplayConfigRef cfg;
    CGError err = CGBeginDisplayConfiguration(&cfg);
    if (err != kCGErrorSuccess) return err;
    if ((err = configure(cfg, id, enabled)) != kCGErrorSuccess) {
        CGCancelDisplayConfiguration(cfg);
        return err;
    }
    // For the session by default: a reboot then undoes any mistake.
    return CGCompleteDisplayConfiguration(cfg, permanent ? kCGConfigurePermanently : kCGConfigureForSession);
}

// ---- Displays and modes ----------------------------------------------------

static void fill_mode(dc_mode *out, CGDisplayModeRef m) {
    memset(out, 0, sizeof *out);
    out->width = (uint32_t)CGDisplayModeGetWidth(m);
    out->height = (uint32_t)CGDisplayModeGetHeight(m);
    out->pixel_width = (uint32_t)CGDisplayModeGetPixelWidth(m);
    out->pixel_height = (uint32_t)CGDisplayModeGetPixelHeight(m);
    out->refresh = CGDisplayModeGetRefreshRate(m);
    out->io_mode_id = CGDisplayModeGetIODisplayModeID(m);
    out->io_flags = CGDisplayModeGetIOFlags(m);
    out->usable = CGDisplayModeIsUsableForDesktopGUI(m);
}

static bool same_mode(const dc_mode *a, const dc_mode *b) {
    return a->width == b->width && a->height == b->height &&
           a->pixel_width == b->pixel_width && a->pixel_height == b->pixel_height &&
           a->io_mode_id == b->io_mode_id && fabs(a->refresh - b->refresh) < 0.01;
}

// copy_modes returns every mode, including the HiDPI ("looks like") variants
// that macOS hides unless asked for them.
static CFArrayRef copy_modes(CGDirectDisplayID id) {
    const void *keys[] = {kCGDisplayShowDuplicateLowResolutionModes};
    const void *vals[] = {kCFBooleanTrue};
    CFDictionaryRef opts = CFDictionaryCreate(NULL, keys, vals, 1,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFArrayRef modes = CGDisplayCopyAllDisplayModes(id, opts);
    CFRelease(opts);
    return modes;
}

static void copy_name(CGDirectDisplayID id, char *buf, size_t len) {
    buf[0] = '\0';
    CFDictionaryRef info = CoreDisplay_DisplayCreateInfoDictionary(id);
    if (!info) return;
    CFTypeRef names = CFDictionaryGetValue(info, CFSTR("DisplayProductName"));
    if (names && CFGetTypeID(names) == CFDictionaryGetTypeID()) {
        CFTypeRef name = CFDictionaryGetValue(names, CFSTR("en_US"));
        CFIndex n = CFDictionaryGetCount(names);
        if (!name && n > 0) { // any localization will do
            const void **vals = malloc(n * sizeof *vals);
            CFDictionaryGetKeysAndValues(names, NULL, vals);
            name = vals[0];
            free(vals);
        }
        if (name && CFGetTypeID(name) == CFStringGetTypeID())
            CFStringGetCString(name, buf, (CFIndex)len, kCFStringEncodingUTF8);
    }
    CFRelease(info);
}

// name_from_registry finds a display's name in the EDID data the display
// controller keeps. Unlike CoreDisplay, it still answers for a display that is
// plugged in but switched off, or one that has been disconnected.
static bool name_from_registry(uint32_t vendor, uint32_t model, char *buf, size_t len) {
    io_registry_entry_t root = IORegistryGetRootEntry(kIOMainPortDefault);
    io_iterator_t it;
    kern_return_t kr = IORegistryEntryCreateIterator(root, kIOServicePlane, kIORegistryIterateRecursively, &it);
    IOObjectRelease(root);
    if (kr != KERN_SUCCESS) return false;

    bool found = false;
    io_service_t s;
    while (!found && (s = IOIteratorNext(it))) {
        CFTypeRef hints = IORegistryEntryCreateCFProperty(s, CFSTR("DisplayHints"), NULL, 0);
        if (hints && CFGetTypeID(hints) == CFDictionaryGetTypeID()) {
            CFTypeRef edid = CFDictionaryGetValue(hints, CFSTR("EDID UUID"));
            CFTypeRef name = CFDictionaryGetValue(hints, CFSTR("ProductName"));
            char uuid[64];
            unsigned v, lo, hi;
            // "10AC6643-..." is EDID bytes 8-11: a big-endian vendor ID
            // followed by a little-endian product code.
            if (edid && name && CFGetTypeID(edid) == CFStringGetTypeID() &&
                CFGetTypeID(name) == CFStringGetTypeID() &&
                CFStringGetCString(edid, uuid, sizeof uuid, kCFStringEncodingUTF8) &&
                sscanf(uuid, "%4x%2x%2x", &v, &lo, &hi) == 3 &&
                v == vendor && ((hi << 8) | lo) == model) {
                found = CFStringGetCString(name, buf, (CFIndex)len, kCFStringEncodingUTF8);
            }
        }
        if (hints) CFRelease(hints);
        IOObjectRelease(s);
    }
    IOObjectRelease(it);
    return found;
}

static void copy_uuid(CGDirectDisplayID id, char *buf, size_t len) {
    buf[0] = '\0';
    CFUUIDRef uuid = CGDisplayCreateUUIDFromDisplayID(id);
    if (!uuid) return;
    CFStringRef s = CFUUIDCreateString(NULL, uuid);
    if (s) {
        CFStringGetCString(s, buf, (CFIndex)len, kCFStringEncodingUTF8);
        CFRelease(s);
    }
    CFRelease(uuid);
}

// gather_ids collects every display macOS will admit to. SkyLight's list keeps
// reporting displays that have been switched off, which CoreGraphics drops from
// its "online" list, so the two are merged.
static uint32_t gather_ids(CGDirectDisplayID *ids, uint32_t max) {
    uint32_t n = 0;
    void *sl = skylight();
    get_display_list_fn sls = sl ? (get_display_list_fn)dlsym(sl, "SLSGetDisplayList") : NULL;
    if (sls && sls(max, ids, &n) != kCGErrorSuccess) n = 0;

    CGDirectDisplayID online[DC_MAX_DISPLAYS];
    uint32_t cgn = 0;
    if (CGGetOnlineDisplayList(DC_MAX_DISPLAYS, online, &cgn) != kCGErrorSuccess) return n;
    for (uint32_t i = 0; i < cgn && n < max; i++) {
        bool seen = false;
        for (uint32_t j = 0; j < n; j++) seen = seen || ids[j] == online[i];
        if (!seen) ids[n++] = online[i];
    }
    return n;
}

int dc_list_displays(dc_display *out, int max) {
    CGDirectDisplayID ids[DC_MAX_DISPLAYS];
    uint32_t n = gather_ids(ids, DC_MAX_DISPLAYS);

    int count = 0;
    for (uint32_t i = 0; i < n && count < max; i++) {
        CGDirectDisplayID id = ids[i];
        dc_display *d = &out[count++];
        memset(d, 0, sizeof *d);
        d->id = id;
        d->active = CGDisplayIsActive(id);
        copy_uuid(id, d->uuid, sizeof d->uuid);
        copy_name(id, d->name, sizeof d->name);
        d->vendor = CGDisplayVendorNumber(id);
        d->model = CGDisplayModelNumber(id);
        if (d->name[0] == '\0')
            name_from_registry(d->vendor, d->model, d->name, sizeof d->name);
        d->serial = CGDisplaySerialNumber(id);
        d->builtin = CGDisplayIsBuiltin(id);
        d->main = CGDisplayIsMain(id);
        d->mirror_of = CGDisplayMirrorsDisplay(id);
        CGRect b = CGDisplayBounds(id);
        d->x = b.origin.x;
        d->y = b.origin.y;
        d->rotation = CGDisplayRotation(id);
        CGDisplayModeRef m = CGDisplayCopyDisplayMode(id);
        if (m) {
            fill_mode(&d->mode, m);
            d->mode.current = true;
            CGDisplayModeRelease(m);
        }
    }
    return count;
}

int dc_list_modes(uint32_t id, dc_mode *out, int max) {
    CFArrayRef modes = copy_modes(id);
    if (!modes) return -1;

    dc_mode cur = {0};
    CGDisplayModeRef cm = CGDisplayCopyDisplayMode(id);
    if (cm) {
        fill_mode(&cur, cm);
        CGDisplayModeRelease(cm);
    }

    CFIndex n = CFArrayGetCount(modes);
    for (CFIndex i = 0; i < n && i < max; i++) {
        fill_mode(&out[i], (CGDisplayModeRef)CFArrayGetValueAtIndex(modes, i));
        out[i].current = cm && same_mode(&out[i], &cur);
    }
    CFRelease(modes);
    return (int)n;
}

int dc_set_mode(uint32_t id, dc_mode want) {
    CFArrayRef modes = copy_modes(id);
    if (!modes) return kCGErrorFailure;

    int err = DC_ERR_NO_MODE;
    for (CFIndex i = 0; i < CFArrayGetCount(modes); i++) {
        CGDisplayModeRef m = (CGDisplayModeRef)CFArrayGetValueAtIndex(modes, i);
        dc_mode got;
        fill_mode(&got, m);
        if (!same_mode(&got, &want)) continue;

        CGDisplayConfigRef cfg;
        if ((err = CGBeginDisplayConfiguration(&cfg)) != kCGErrorSuccess) break;
        if ((err = CGConfigureDisplayWithDisplayMode(cfg, id, m, NULL)) != kCGErrorSuccess) {
            CGCancelDisplayConfiguration(cfg);
            break;
        }
        // Permanently = survives logout and reboot, like System Settings.
        err = CGCompleteDisplayConfiguration(cfg, kCGConfigurePermanently);
        break;
    }
    CFRelease(modes);
    return err;
}

// ---- DDC/CI over I²C (Apple Silicon) ---------------------------------------

struct dc_ddc {
    IOAVServiceRef service;
    uint32_t chip; // I²C address of the monitor's DDC/CI endpoint
};

// On M1/M2 Mac minis the HDMI port goes through an MCDP29xx DP-to-HDMI chip,
// which answers DDC on a different I²C address.
static bool is_mcdp29xx(io_service_t proxy) {
    io_registry_entry_t parent;
    if (IORegistryEntryGetParentEntry(proxy, kIOServicePlane, &parent) != KERN_SUCCESS)
        return false;
    CFTypeRef cls = IORegistryEntryCreateCFProperty(parent, CFSTR("EPICProviderClass"), NULL, 0);
    bool yes = string_equals(cls, CFSTR("AppleDCPMCDP29XX"));
    if (cls) CFRelease(cls);
    IOObjectRelease(parent);
    return yes;
}

static bool is_external_av_proxy(io_service_t s) {
    io_name_t name;
    if (IORegistryEntryGetName(s, name) != KERN_SUCCESS || strcmp(name, "DCPAVServiceProxy") != 0)
        return false;
    CFTypeRef loc = IORegistryEntryCreateCFProperty(s, CFSTR("Location"), NULL, 0);
    bool yes = string_equals(loc, CFSTR("External"));
    if (loc) CFRelease(loc);
    return yes;
}

// framebuffer_id finds the IORegistry entry ID of the display's framebuffer,
// using the registry path CoreDisplay reports for it.
static uint64_t framebuffer_id(CGDirectDisplayID id) {
    CFDictionaryRef info = CoreDisplay_DisplayCreateInfoDictionary(id);
    if (!info) return 0;
    uint64_t fbID = 0;
    CFTypeRef path = CFDictionaryGetValue(info, CFSTR("IODisplayLocation"));
    if (path && CFGetTypeID(path) == CFStringGetTypeID()) {
        io_registry_entry_t fb = IORegistryEntryCopyFromPath(kIOMainPortDefault, path);
        if (fb) {
            IORegistryEntryGetRegistryEntryID(fb, &fbID);
            IOObjectRelease(fb);
        }
    }
    CFRelease(info);
    return fbID;
}

dc_ddc *dc_ddc_open(uint32_t id) {
    uint64_t fbID = framebuffer_id(id);
    if (!fbID) return NULL;

    // Walk the whole service plane in order. The DCPAVServiceProxy that comes
    // after a framebuffer belongs to that framebuffer's display.
    io_registry_entry_t root = IORegistryGetRootEntry(kIOMainPortDefault);
    io_iterator_t it;
    kern_return_t kr = IORegistryEntryCreateIterator(root, kIOServicePlane, kIORegistryIterateRecursively, &it);
    IOObjectRelease(root);
    if (kr != KERN_SUCCESS) return NULL;

    dc_ddc *out = NULL;
    bool inDisplay = false;
    io_service_t s;
    while (!out && (s = IOIteratorNext(it))) {
        if (IOObjectConformsTo(s, "IOMobileFramebuffer")) {
            uint64_t sid = 0;
            inDisplay = IORegistryEntryGetRegistryEntryID(s, &sid) == KERN_SUCCESS && sid == fbID;
        } else if (inDisplay && is_external_av_proxy(s)) {
            IOAVServiceRef av = IOAVServiceCreateWithService(kCFAllocatorDefault, s);
            if (av) {
                out = calloc(1, sizeof *out);
                out->service = av;
                out->chip = is_mcdp29xx(s) ? 0xB7 : 0x37;
            }
        }
        IOObjectRelease(s);
    }
    IOObjectRelease(it);
    return out;
}

void dc_ddc_close(dc_ddc *d) {
    if (!d) return;
    CFRelease(d->service);
    free(d);
}

int dc_ddc_write(dc_ddc *d, uint8_t offset, uint8_t *buf, uint32_t len) {
    return IOAVServiceWriteI2C(d->service, d->chip, offset, buf, len);
}

int dc_ddc_read(dc_ddc *d, uint8_t offset, uint8_t *buf, uint32_t len) {
    return IOAVServiceReadI2C(d->service, d->chip, offset, buf, len);
}
