// Core Audio process-tap shim (M25). See docs/plans/active/
// m25-macos-system-audio.md for the design; tap_darwin.h for the ABI.
//
// Threads:
//   - the IOProc (Core Audio realtime thread) only copies samples into
//     a lock-free SPSC ring: no allocation, no locks, no Go;
//   - catchm_tap_read (a Go goroutine) drains the ring through an
//     AudioConverter to 16 kHz mono s16;
//   - `q`, a serial dispatch queue, owns (re)building the aggregate
//     device: initial start, property-listener rebuilds (default output
//     device or tap format changed) and stop. `mu` keeps a rebuild that
//     swaps the converter away from an in-flight read.
// Every 14.2+ API sits behind @available, so the binary still launches
// on older macOS (the symbols are weak-linked) and simply reports
// "unsupported" there.

#import <Foundation/Foundation.h>
#import <AudioToolbox/AudioToolbox.h>
#import <CoreAudio/AudioHardware.h>
#import <CoreAudio/AudioHardwareTapping.h>
#import <CoreAudio/CATapDescription.h>
#include <pthread.h>
#include <stdatomic.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

#include "tap_darwin.h"

#define TAP_API __attribute__((availability(macos, introduced = 14.2)))

// Mono float frames. 2^19 ≈ 2.7 s even at 192 kHz, ~11 s at 48 kHz:
// the reader drains every 20 ms, so this only fills if Go stalls.
#define RING_FRAMES (1u << 19)
#define RING_MASK (RING_FRAMES - 1)
#define SCRATCH_FRAMES 4096
#define OUT_RATE 16000.0

// Returned by the converter's input callback when the ring is empty:
// FillComplexBuffer stops early, hands back what it produced, and keeps
// its resampler state for the next call.
static const OSStatus kRingEmpty = 'cmty';

struct catchm_tap {
	void *q; // dispatch_queue_t, retained via CFBridgingRetain
	pthread_mutex_t mu;
	bool stopped; // only touched on q

	AudioObjectID tap;
	AudioObjectID agg;
	AudioDeviceIOProcID proc;

	void *outputListener; // AudioObjectPropertyListenerBlock, retained
	void *formatListener;

	// Guarded by mu.
	AudioConverterRef conv;
	Float64 srcRate;

	float *ring;
	_Atomic uint64_t w; // producer: IOProc
	_Atomic uint64_t r; // consumer: catchm_tap_read (under mu)
	_Atomic uint64_t overruns;

	float scratch[SCRATCH_FRAMES]; // converter input, valid until the next callback
};

static void seterr(char *buf, int len, const char *what, OSStatus st) {
	if (!buf || len <= 0) return;
	if (st == noErr) {
		snprintf(buf, (size_t)len, "%s", what);
		return;
	}
	char cc[5] = {0};
	UInt32 be = CFSwapInt32HostToBig((UInt32)st);
	memcpy(cc, &be, 4);
	bool printable = true;
	for (int i = 0; i < 4; i++)
		if (cc[i] < 32 || cc[i] > 126) printable = false;
	if (printable)
		snprintf(buf, (size_t)len, "%s (OSStatus '%s')", what, cc);
	else
		snprintf(buf, (size_t)len, "%s (OSStatus %d)", what, (int)st);
}

static dispatch_queue_t tapQueue(catchm_tap *t) { return (__bridge dispatch_queue_t)t->q; }

// ---- realtime side ------------------------------------------------------

// Averages every channel of every buffer into the mono ring. The tap is
// mono already (initMonoGlobalTap…), but nothing here assumes it.
static OSStatus ioproc(AudioObjectID dev, const AudioTimeStamp *now, const AudioBufferList *in,
                       const AudioTimeStamp *inTime, AudioBufferList *out, const AudioTimeStamp *outTime,
                       void *ctx) {
	(void)dev; (void)now; (void)inTime; (void)out; (void)outTime;
	catchm_tap *t = (catchm_tap *)ctx;
	if (!in || in->mNumberBuffers == 0) return noErr;

	UInt32 nbuf = in->mNumberBuffers;
	UInt32 totalCh = 0, frames = UINT32_MAX;
	for (UInt32 b = 0; b < nbuf; b++) {
		const AudioBuffer *ab = &in->mBuffers[b];
		UInt32 ch = ab->mNumberChannels ? ab->mNumberChannels : 1;
		if (!ab->mData) return noErr;
		UInt32 f = ab->mDataByteSize / (UInt32)(sizeof(float) * ch);
		if (f < frames) frames = f;
		totalCh += ch;
	}
	if (frames == 0 || frames == UINT32_MAX) return noErr;

	uint64_t w = atomic_load_explicit(&t->w, memory_order_relaxed);
	uint64_t r = atomic_load_explicit(&t->r, memory_order_acquire);
	if (RING_FRAMES - (w - r) < frames) {
		atomic_fetch_add_explicit(&t->overruns, 1, memory_order_relaxed);
		return noErr; // drop the whole buffer; counted, never silent
	}
	float scale = 1.0f / (float)totalCh;
	for (UInt32 i = 0; i < frames; i++) {
		float sum = 0;
		for (UInt32 b = 0; b < nbuf; b++) {
			const AudioBuffer *ab = &in->mBuffers[b];
			UInt32 ch = ab->mNumberChannels ? ab->mNumberChannels : 1;
			const float *src = (const float *)ab->mData + (size_t)i * ch;
			for (UInt32 c = 0; c < ch; c++) sum += src[c];
		}
		t->ring[(w + i) & RING_MASK] = sum * scale;
	}
	atomic_store_explicit(&t->w, w + frames, memory_order_release);
	return noErr;
}

// ---- reader side ----------------------------------------------------------

static OSStatus converterInput(AudioConverterRef conv, UInt32 *ioPackets, AudioBufferList *io,
                               AudioStreamPacketDescription **desc, void *ctx) {
	(void)conv;
	if (desc) *desc = NULL;
	catchm_tap *t = (catchm_tap *)ctx;
	uint64_t r = atomic_load_explicit(&t->r, memory_order_relaxed);
	uint64_t w = atomic_load_explicit(&t->w, memory_order_acquire);
	uint64_t avail = w - r;
	if (avail == 0) {
		*ioPackets = 0;
		return kRingEmpty;
	}
	UInt32 n = *ioPackets;
	if (n > avail) n = (UInt32)avail;
	if (n > SCRATCH_FRAMES) n = SCRATCH_FRAMES;
	for (UInt32 i = 0; i < n; i++) t->scratch[i] = t->ring[(r + i) & RING_MASK];
	atomic_store_explicit(&t->r, r + n, memory_order_release);

	io->mNumberBuffers = 1;
	io->mBuffers[0].mNumberChannels = 1;
	io->mBuffers[0].mData = t->scratch;
	io->mBuffers[0].mDataByteSize = n * (UInt32)sizeof(float);
	*ioPackets = n;
	return noErr;
}

int catchm_tap_read(catchm_tap *t, int16_t *out, int max_frames) {
	if (!t || max_frames <= 0) return 0;
	pthread_mutex_lock(&t->mu);
	if (!t->conv) {
		pthread_mutex_unlock(&t->mu);
		return 0;
	}
	UInt32 n = (UInt32)max_frames;
	AudioBufferList abl;
	abl.mNumberBuffers = 1;
	abl.mBuffers[0].mNumberChannels = 1;
	abl.mBuffers[0].mData = out;
	abl.mBuffers[0].mDataByteSize = n * (UInt32)sizeof(int16_t);
	OSStatus st = AudioConverterFillComplexBuffer(t->conv, converterInput, t, &n, &abl, NULL);
	pthread_mutex_unlock(&t->mu);
	if (st != noErr && st != kRingEmpty) return -1;
	return (int)n;
}

uint64_t catchm_tap_overruns(catchm_tap *t) {
	return t ? atomic_load_explicit(&t->overruns, memory_order_relaxed) : 0;
}

// ---- device management (always on q) ---------------------------------------

static OSStatus getTapFormat(catchm_tap *t, AudioStreamBasicDescription *f) TAP_API {
	AudioObjectPropertyAddress a = {kAudioTapPropertyFormat, kAudioObjectPropertyScopeGlobal,
	                                kAudioObjectPropertyElementMain};
	UInt32 sz = sizeof(*f);
	return AudioObjectGetPropertyData(t->tap, &a, 0, NULL, &sz, f);
}

// (Re)creates the converter when the tap's rate changed. The IOProc is
// stopped while this runs, so resetting the ring is race-free.
static OSStatus syncConverter(catchm_tap *t, Float64 rate) {
	pthread_mutex_lock(&t->mu);
	OSStatus st = noErr;
	if (!t->conv || t->srcRate != rate) {
		if (t->conv) {
			AudioConverterDispose(t->conv);
			t->conv = NULL;
		}
		AudioStreamBasicDescription src = {0}, dst = {0};
		src.mSampleRate = rate;
		src.mFormatID = kAudioFormatLinearPCM;
		src.mFormatFlags = kAudioFormatFlagIsFloat | kAudioFormatFlagIsPacked;
		src.mChannelsPerFrame = 1;
		src.mBitsPerChannel = 32;
		src.mFramesPerPacket = 1;
		src.mBytesPerFrame = src.mBytesPerPacket = 4;
		dst.mSampleRate = OUT_RATE;
		dst.mFormatID = kAudioFormatLinearPCM;
		dst.mFormatFlags = kAudioFormatFlagIsSignedInteger | kAudioFormatFlagIsPacked;
		dst.mChannelsPerFrame = 1;
		dst.mBitsPerChannel = 16;
		dst.mFramesPerPacket = 1;
		dst.mBytesPerFrame = dst.mBytesPerPacket = 2;
		st = AudioConverterNew(&src, &dst, &t->conv);
		if (st == noErr) {
			UInt32 q = kAudioConverterQuality_High;
			AudioConverterSetProperty(t->conv, kAudioConverterSampleRateConverterQuality, sizeof(q), &q);
			t->srcRate = rate;
			// Samples at the old rate would play back at the wrong speed.
			atomic_store_explicit(&t->r, atomic_load_explicit(&t->w, memory_order_acquire),
			                      memory_order_release);
		}
	}
	pthread_mutex_unlock(&t->mu);
	return st;
}

static void teardownDevice(catchm_tap *t) {
	if (t->agg) {
		if (t->proc) {
			AudioDeviceStop(t->agg, t->proc); // synchronous: the IOProc is done after this
			AudioDeviceDestroyIOProcID(t->agg, t->proc);
			t->proc = NULL;
		}
		AudioHardwareDestroyAggregateDevice(t->agg);
		t->agg = 0;
	}
}

// Private aggregate device whose only input is the tap. No sub-device:
// including the default output would also pull in that device's own
// input streams (AirPods / USB headsets have mics) and could switch a
// Bluetooth headset into its low-quality call profile.
static OSStatus buildDevice(catchm_tap *t, char *err, int errLen) TAP_API {
	AudioObjectPropertyAddress ua = {kAudioTapPropertyUID, kAudioObjectPropertyScopeGlobal,
	                                 kAudioObjectPropertyElementMain};
	CFStringRef tapUID = NULL;
	UInt32 sz = sizeof(tapUID);
	OSStatus st = AudioObjectGetPropertyData(t->tap, &ua, 0, NULL, &sz, &tapUID);
	if (st != noErr || !tapUID) {
		seterr(err, errLen, "read tap UID", st);
		return st ? st : kAudioHardwareUnspecifiedError;
	}

	NSDictionary *desc = @{
		@kAudioAggregateDeviceNameKey : @"CatchM System Audio",
		@kAudioAggregateDeviceUIDKey : [[NSUUID UUID] UUIDString],
		@kAudioAggregateDeviceIsPrivateKey : @YES,
		@kAudioAggregateDeviceIsStackedKey : @NO,
		@kAudioAggregateDeviceTapAutoStartKey : @YES,
		@kAudioAggregateDeviceSubDeviceListKey : @[],
		@kAudioAggregateDeviceTapListKey : @[ @{
			@kAudioSubTapUIDKey : (__bridge_transfer NSString *)tapUID,
			@kAudioSubTapDriftCompensationKey : @YES,
		} ],
	};
	st = AudioHardwareCreateAggregateDevice((__bridge CFDictionaryRef)desc, &t->agg);
	if (st != noErr) {
		t->agg = 0;
		seterr(err, errLen, "create aggregate device", st);
		return st;
	}

	AudioStreamBasicDescription f;
	st = getTapFormat(t, &f);
	if (st != noErr) {
		seterr(err, errLen, "read tap format", st);
		teardownDevice(t);
		return st;
	}
	if (f.mFormatID != kAudioFormatLinearPCM || !(f.mFormatFlags & kAudioFormatFlagIsFloat) ||
	    f.mBitsPerChannel != 32 || f.mSampleRate <= 0) {
		seterr(err, errLen, "unexpected tap format (want float32 PCM)", noErr);
		teardownDevice(t);
		return kAudioHardwareUnsupportedOperationError;
	}
	st = syncConverter(t, f.mSampleRate);
	if (st != noErr) {
		seterr(err, errLen, "create sample-rate converter", st);
		teardownDevice(t);
		return st;
	}

	st = AudioDeviceCreateIOProcID(t->agg, ioproc, t, &t->proc);
	if (st != noErr) {
		t->proc = NULL;
		seterr(err, errLen, "create IOProc", st);
		teardownDevice(t);
		return st;
	}
	// The macOS "System Audio Recording" prompt appears here on first
	// use. A denial doesn't fail: the tap then delivers silence, which
	// the Go side reports as NoSignal.
	st = AudioDeviceStart(t->agg, t->proc);
	if (st != noErr) {
		seterr(err, errLen, "start aggregate device", st);
		teardownDevice(t);
		return st;
	}
	return noErr;
}

// Default output changed (headphones in/out) or the tap's format
// changed: rebuild on the new route. The ring and converter survive
// unless the rate changed; the Mixer zero-pads the short gap.
static void rebuild(catchm_tap *t) TAP_API {
	if (t->stopped) return;
	teardownDevice(t);
	char err[256];
	if (buildDevice(t, err, sizeof(err)) != noErr) NSLog(@"catchm: system audio rebuild failed: %s", err);
}

static void addListeners(catchm_tap *t) TAP_API {
	AudioObjectPropertyListenerBlock onChange = ^(UInt32 n, const AudioObjectPropertyAddress *addrs) {
		(void)n; (void)addrs;
		if (@available(macOS 14.2, *)) rebuild(t);
	};
	AudioObjectPropertyAddress out = {kAudioHardwarePropertyDefaultOutputDevice, kAudioObjectPropertyScopeGlobal,
	                                  kAudioObjectPropertyElementMain};
	if (AudioObjectAddPropertyListenerBlock(kAudioObjectSystemObject, &out, tapQueue(t), onChange) == noErr)
		t->outputListener = (__bridge_retained void *)onChange;
	AudioObjectPropertyAddress fmt = {kAudioTapPropertyFormat, kAudioObjectPropertyScopeGlobal,
	                                  kAudioObjectPropertyElementMain};
	if (AudioObjectAddPropertyListenerBlock(t->tap, &fmt, tapQueue(t), onChange) == noErr)
		t->formatListener = (__bridge_retained void *)onChange;
}

static void removeListeners(catchm_tap *t) TAP_API {
	if (t->outputListener) {
		AudioObjectPropertyAddress a = {kAudioHardwarePropertyDefaultOutputDevice, kAudioObjectPropertyScopeGlobal,
		                                kAudioObjectPropertyElementMain};
		AudioObjectRemovePropertyListenerBlock(kAudioObjectSystemObject, &a, tapQueue(t),
		                                       (__bridge_transfer AudioObjectPropertyListenerBlock)t->outputListener);
		t->outputListener = NULL;
	}
	if (t->formatListener) {
		AudioObjectPropertyAddress a = {kAudioTapPropertyFormat, kAudioObjectPropertyScopeGlobal,
		                                kAudioObjectPropertyElementMain};
		AudioObjectRemovePropertyListenerBlock(t->tap, &a, tapQueue(t),
		                                       (__bridge_transfer AudioObjectPropertyListenerBlock)t->formatListener);
		t->formatListener = NULL;
	}
}

// Everything except freeing memory; safe on any partially built state.
static void teardownAll(catchm_tap *t) TAP_API {
	t->stopped = true;
	removeListeners(t);
	teardownDevice(t);
	if (t->tap) {
		AudioHardwareDestroyProcessTap(t->tap);
		t->tap = 0;
	}
}

static void freeTap(catchm_tap *t) {
	if (t->conv) AudioConverterDispose(t->conv);
	free(t->ring);
	pthread_mutex_destroy(&t->mu);
	if (t->q) CFRelease(t->q);
	free(t);
}

static OSStatus startOnQueue(catchm_tap *t, char *err, int errLen) TAP_API {
	// Exclude our own output (e.g. a future TTS readout) from the tap.
	// A process that has never touched audio has no process object yet
	// (kAudioObjectUnknown): then there's nothing of ours to exclude.
	AudioObjectPropertyAddress pa = {kAudioHardwarePropertyTranslatePIDToProcessObject,
	                                 kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
	pid_t pid = getpid();
	AudioObjectID self = kAudioObjectUnknown;
	UInt32 sz = sizeof(self);
	AudioObjectGetPropertyData(kAudioObjectSystemObject, &pa, sizeof(pid), &pid, &sz, &self);
	NSArray<NSNumber *> *exclude = self != kAudioObjectUnknown ? @[ @(self) ] : @[];

	CATapDescription *d = [[CATapDescription alloc] initMonoGlobalTapButExcludeProcesses:exclude];
	d.name = @"CatchM System Audio";
	d.privateTap = YES;
	d.muteBehavior = CATapUnmuted;

	OSStatus st = AudioHardwareCreateProcessTap(d, &t->tap);
	if (st != noErr) {
		t->tap = 0;
		seterr(err, errLen, "create process tap", st);
		return st;
	}
	st = buildDevice(t, err, errLen);
	if (st != noErr) return st;
	addListeners(t);
	return noErr;
}

int catchm_tap_supported(char *reason, int reason_len) {
	if (@available(macOS 14.2, *)) return 1;
	seterr(reason, reason_len, "system audio on macOS needs macOS 14.2 or later", noErr);
	return 0;
}

catchm_tap *catchm_tap_start(char *err, int err_len) {
	if (@available(macOS 14.2, *)) {
		catchm_tap *t = calloc(1, sizeof(*t));
		if (!t) {
			seterr(err, err_len, "out of memory", noErr);
			return NULL;
		}
		t->ring = calloc(RING_FRAMES, sizeof(float));
		if (!t->ring) {
			free(t);
			seterr(err, err_len, "out of memory", noErr);
			return NULL;
		}
		pthread_mutex_init(&t->mu, NULL);
		t->q = (__bridge_retained void *)dispatch_queue_create("com.spacingmind.catchm.tap", DISPATCH_QUEUE_SERIAL);

		__block OSStatus st = noErr;
		dispatch_sync(tapQueue(t), ^{
			@autoreleasepool {
				st = startOnQueue(t, err, err_len);
				if (st != noErr) teardownAll(t);
			}
		});
		if (st != noErr) {
			freeTap(t);
			return NULL;
		}
		return t;
	}
	catchm_tap_supported(err, err_len);
	return NULL;
}

static int countObjects(AudioObjectPropertySelector sel) {
	AudioObjectPropertyAddress a = {sel, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
	UInt32 sz = 0;
	if (AudioObjectGetPropertyDataSize(kAudioObjectSystemObject, &a, 0, NULL, &sz) != noErr) return 0;
	return (int)(sz / sizeof(AudioObjectID));
}

int catchm_tap_leftovers(void) {
	int n = 0;
	if (@available(macOS 14.2, *)) n += countObjects(kAudioHardwarePropertyTapList);
	AudioObjectPropertyAddress a = {kAudioHardwarePropertyDevices, kAudioObjectPropertyScopeGlobal,
	                                kAudioObjectPropertyElementMain};
	UInt32 sz = 0;
	if (AudioObjectGetPropertyDataSize(kAudioObjectSystemObject, &a, 0, NULL, &sz) != noErr) return n;
	AudioObjectID *ids = malloc(sz);
	if (!ids) return n;
	if (AudioObjectGetPropertyData(kAudioObjectSystemObject, &a, 0, NULL, &sz, ids) == noErr) {
		for (UInt32 i = 0; i < sz / sizeof(AudioObjectID); i++) {
			AudioObjectPropertyAddress na = {kAudioObjectPropertyName, kAudioObjectPropertyScopeGlobal,
			                                 kAudioObjectPropertyElementMain};
			CFStringRef name = NULL;
			UInt32 nsz = sizeof(name);
			if (AudioObjectGetPropertyData(ids[i], &na, 0, NULL, &nsz, &name) == noErr && name) {
				if (CFStringCompare(name, CFSTR("CatchM System Audio"), 0) == kCFCompareEqualTo) n++;
				CFRelease(name);
			}
		}
	}
	free(ids);
	return n;
}

void catchm_tap_rebuild(catchm_tap *t) {
	if (!t) return;
	if (@available(macOS 14.2, *)) {
		dispatch_sync(tapQueue(t), ^{
			@autoreleasepool {
				rebuild(t);
			}
		});
	}
}

void catchm_tap_stop(catchm_tap *t) {
	if (!t) return;
	if (@available(macOS 14.2, *)) {
		dispatch_sync(tapQueue(t), ^{
			@autoreleasepool {
				teardownAll(t);
			}
		});
		// Drain listener blocks queued before removal: they run now, see
		// `stopped` and return, so none can touch `t` after the free.
		dispatch_sync(tapQueue(t), ^{
		});
	}
	freeTap(t);
}
