#include "frame_channel.h"
#include "app_logger.h"

#include <arpa/inet.h>
#include <errno.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>

// Magic at the head of every frame, so a desynchronised stream is spotted
// rather than read as garbage lengths.
static const char FRAME_MAGIC[4] = {'P', 'O', 'B', 'F'};

static GMutex channel_mutex;
static int channel_fd = -1;

static void close_locked(void) {
    if (channel_fd >= 0) {
        close(channel_fd);
        channel_fd = -1;
    }
}

// write_all loops until everything is out or the socket gives up: a short
// write on a socket is ordinary, and stopping at one would leave half a frame
// on the wire with no way to resynchronise the reader.
static gboolean write_all(int fd, const guchar *data, gsize len) {
    gsize sent = 0;
    while (sent < len) {
        ssize_t n = send(fd, data + sent, len - sent, MSG_NOSIGNAL);
        if (n < 0) {
            if (errno == EINTR) continue;
            return FALSE;
        }
        sent += (gsize)n;
    }
    return TRUE;
}

void frame_channel_connect(int port, const char *token) {
    frame_channel_stop();
    if (port <= 0 || port > 65535 || !token || !*token) return;

    int fd = socket(AF_INET, SOCK_STREAM, 0);
    if (fd < 0) {
        app_logger_log("FrameChannel: cannot open a socket (%s)", g_strerror(errno));
        return;
    }

    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_port = htons((uint16_t)port);
    addr.sin_addr.s_addr = htonl(INADDR_LOOPBACK);

    if (connect(fd, (struct sockaddr *)&addr, sizeof(addr)) != 0) {
        app_logger_log("FrameChannel: cannot connect on port %d (%s); frames stay on "
                "the JSON-RPC line", port, g_strerror(errno));
        close(fd);
        return;
    }

    // Frames are pushed one after another and nothing comes back, so waiting
    // for an ack that will never arrive would only add latency to every one.
    int one = 1;
    (void)setsockopt(fd, IPPROTO_TCP, TCP_NODELAY, &one, sizeof(one));

    // The token goes first, before any frame. It is not the security boundary
    // — the core listens on loopback only — but it keeps anything else on this
    // machine that happens to find the port from feeding it frames.
    gchar *hello = g_strdup_printf("%s\n", token);
    gboolean ok = write_all(fd, (const guchar *)hello, strlen(hello));
    g_free(hello);
    if (!ok) {
        close(fd);
        return;
    }

    g_mutex_lock(&channel_mutex);
    channel_fd = fd;
    g_mutex_unlock(&channel_mutex);
    app_logger_log("FrameChannel: connected on port %d", port);
}

void frame_channel_stop(void) {
    g_mutex_lock(&channel_mutex);
    close_locked();
    g_mutex_unlock(&channel_mutex);
}

// Big-endian throughout, which is what every language's "write an integer to a
// socket" reaches for first.
static void put_u16(GByteArray *out, guint16 value) {
    guint8 bytes[2] = {(guint8)(value >> 8), (guint8)(value & 0xFF)};
    g_byte_array_append(out, bytes, 2);
}

static void put_u32(GByteArray *out, guint32 value) {
    guint8 bytes[4] = {
        (guint8)((value >> 24) & 0xFF), (guint8)((value >> 16) & 0xFF),
        (guint8)((value >> 8) & 0xFF), (guint8)(value & 0xFF)};
    g_byte_array_append(out, bytes, 4);
}

gboolean frame_channel_send(const char *id, const char *meta_json,
                            const guchar *payload, gsize payload_len) {
    if (!id || !payload) return FALSE;
    gsize id_len = strlen(id);
    if (id_len > G_MAXUINT16) return FALSE;
    gsize meta_len = meta_json ? strlen(meta_json) : 0;

    // Built whole and written under one lock: TCP keeps the order, so the
    // reader only has to read lengths, but a frame interleaved with the next
    // one cannot be resynchronised — there is nothing in a length-prefixed
    // stream to resynchronise against.
    GByteArray *frame = g_byte_array_sized_new(
        (guint)(sizeof(FRAME_MAGIC) + 2 + id_len + 4 + meta_len + 4 + payload_len));
    g_byte_array_append(frame, (const guint8 *)FRAME_MAGIC, sizeof(FRAME_MAGIC));
    put_u16(frame, (guint16)id_len);
    g_byte_array_append(frame, (const guint8 *)id, (guint)id_len);
    put_u32(frame, (guint32)meta_len);
    if (meta_len) g_byte_array_append(frame, (const guint8 *)meta_json, (guint)meta_len);
    put_u32(frame, (guint32)payload_len);
    g_byte_array_append(frame, payload, (guint)payload_len);

    g_mutex_lock(&channel_mutex);
    gboolean ok = FALSE;
    if (channel_fd >= 0) {
        ok = write_all(channel_fd, frame->data, frame->len);
        // Half-written, so this connection can no longer be read from in step.
        // Drop it and answer the old way from here on.
        if (!ok) close_locked();
    }
    g_mutex_unlock(&channel_mutex);

    g_byte_array_unref(frame);
    return ok;
}
