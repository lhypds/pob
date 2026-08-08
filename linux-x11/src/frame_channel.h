// The binary connection captured frames go down, instead of being base64'd
// into a JSON-RPC response on the core's stdin. Mirrors
// macos/Sources/Services/FrameChannel.swift and win/src/Services/FrameChannel.cs.
//
// A frame does not belong on that line. Base64 is a third again as many bytes
// and JSON has to be parsed as one enormous string before any of it can be
// used — but the real cost is that the line is shared. Every mouse move and
// keystroke answers on it too, and behind a megabyte of picture they wait. At
// one frame a second that is invisible; at thirty it is the whole difference
// between a view you can work in and one you can only watch.
//
// The core listens on loopback and sends the port and a token in a
// frames.channel notification; this connects back and pushes. If the
// connection is not up — not yet, or not any more — frame_channel_send says so
// and the caller answers the old way, so a lost channel costs frame rate and
// nothing else.
#ifndef POB_FRAME_CHANNEL_H
#define POB_FRAME_CHANNEL_H

#include <glib.h>

// Connects to the core's frame channel. Called when frames.channel arrives,
// and again if the core ever re-offers one.
void frame_channel_connect(int port, const char *token);

void frame_channel_stop(void);

// Pushes one frame: the request id it answers, a JSON object of metadata, and
// the encoded picture. Answers FALSE if the channel is not up, which is the
// caller's cue to reply on the JSON-RPC line instead.
gboolean frame_channel_send(const char *id, const char *meta_json,
                            const guchar *payload, gsize payload_len);

#endif
