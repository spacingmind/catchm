// Core Audio process-tap shim for system-audio capture on macOS 14.2+
// (M25). Plain C ABI so the cgo preamble never sees Objective-C; the
// implementation is tap_darwin.m.

#ifndef CATCHM_TAP_DARWIN_H
#define CATCHM_TAP_DARWIN_H

#include <stdint.h>

typedef struct catchm_tap catchm_tap;

// 1 when process taps are available at runtime (macOS >= 14.2), else 0
// with a human-readable reason written to `reason`.
int catchm_tap_supported(char *reason, int reason_len);

// Starts a global, CatchM-excluding mono tap on a private aggregate
// device. NULL on failure, with the cause in `err`.
catchm_tap *catchm_tap_start(char *err, int err_len);

// Non-blocking: drains what the tap has captured so far, converted to
// 16 kHz mono s16. Returns frames written (0 = nothing yet), -1 on a
// converter error.
int catchm_tap_read(catchm_tap *t, int16_t *out, int max_frames);

// IOProc buffers dropped because the ring was full (reader too slow).
uint64_t catchm_tap_overruns(catchm_tap *t);

// Rebuilds the aggregate device and IOProc, exactly what the
// default-output / tap-format listeners do. Exported so tests can
// exercise the device-switch path on a Mac with a single output.
void catchm_tap_rebuild(catchm_tap *t);

// Process taps plus audio devices named "CatchM System Audio" that
// this process can currently see (private ones are visible only to
// us). Tests use it to prove catchm_tap_stop leaves nothing behind.
int catchm_tap_leftovers(void);

// Full teardown and free; safe on a partially started tap. `t` must not
// be used afterwards, and no catchm_tap_read may be in flight.
void catchm_tap_stop(catchm_tap *t);

#endif
