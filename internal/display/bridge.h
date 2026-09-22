// bridge.h is the C side of package display.
//
// Everything that needs Apple's C APIs sits behind these few functions. They
// take and return plain structs, so the Go side never touches CoreFoundation.
#ifndef DISPLAYCTL_BRIDGE_H
#define DISPLAYCTL_BRIDGE_H

#include <stdbool.h>
#include <stdint.h>

#define DC_MAX_DISPLAYS 16
#define DC_ERR_NO_MODE (-1)

typedef struct {
    uint32_t width, height;             // logical size in points
    uint32_t pixel_width, pixel_height; // framebuffer size in pixels
    double refresh;                     // Hz
    int32_t io_mode_id;
    uint32_t io_flags;
    bool usable;                        // usable for the desktop GUI
    bool current;
} dc_mode;

typedef struct {
    uint32_t id;
    char name[128];
    char uuid[40];                      // stable across reboots, unlike the id
    uint32_t vendor, model, serial;
    bool builtin, main;
    bool active;                        // part of the desktop right now
    uint32_t mirror_of;                 // 0 unless mirroring another display
    double x, y, rotation;
    dc_mode mode;
} dc_display;

// Displays and modes (public CoreGraphics API).
int dc_list_displays(dc_display *out, int max);

// Connect / disconnect (private SkyLight API, looked up at runtime).
bool dc_can_set_enabled(void);
int dc_set_enabled(uint32_t id, bool enabled, bool permanent); // CGError
int dc_list_modes(uint32_t id, dc_mode *out, int max); // returns total count
int dc_set_mode(uint32_t id, dc_mode want);             // CGError or DC_ERR_NO_MODE

// Raw I²C to an external monitor (private IOAVService API, Apple Silicon).
typedef struct dc_ddc dc_ddc;
dc_ddc *dc_ddc_open(uint32_t id); // NULL if the display has no DDC channel
void dc_ddc_close(dc_ddc *d);
int dc_ddc_write(dc_ddc *d, uint8_t offset, uint8_t *buf, uint32_t len);
int dc_ddc_read(dc_ddc *d, uint8_t offset, uint8_t *buf, uint32_t len);

#endif
