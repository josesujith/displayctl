// menubar.h is the C face of the status item. Go calls mb_run once, and calls
// mb_add_item and mb_add_login while the menu is being rebuilt.
#ifndef DISPLAYCTL_MENUBAR_H
#define DISPLAYCTL_MENUBAR_H

#include <stdbool.h>
#include <stdint.h>

void mb_run(void); // shows the status item and runs the event loop; never returns
void mb_add_item(const char *title, uint32_t id, bool on, bool enabled);
void mb_add_login(const char *title, bool on, bool enabled);

#endif
