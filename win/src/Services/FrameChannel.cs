// The binary connection captured frames go down, instead of being base64'd
// into a JSON-RPC response on the core's stdin. Mirrors macos/FrameChannel.swift
// and linux-x11/frame_channel.c.
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
// connection is not up — not yet, or not any more — Send says so and the
// caller answers the old way, so a lost channel costs frame rate and nothing
// else.
using System.Net.Sockets;
using System.Text;
using System.Text.Json;

namespace Pob.Services;

public static class FrameChannel
{
    // Magic at the head of every frame, so a desynchronised stream is spotted
    // rather than read as garbage lengths.
    private static readonly byte[] Magic = "POBF"u8.ToArray();

    private static readonly object Lock = new();
    private static TcpClient? _client;
    private static NetworkStream? _stream;

    // Connects to the core's frame channel. Called when frames.channel
    // arrives, and again if the core ever re-offers one.
    //
    // Off the calling thread: that is the thread reading the core's messages,
    // and a connect that stalls there would stall every message behind it.
    // Until it lands, captures answer on the JSON-RPC line as they always did.
    public static void Connect(int port, string token)
    {
        Stop();
        Task.Run(() => Dial(port, token));
    }

    private static void Dial(int port, string token)
    {
        try
        {
            var client = new TcpClient();
            client.Connect("127.0.0.1", port);
            // Frames are pushed one after another and nothing comes back, so
            // waiting for an ack that will never arrive would only add
            // latency to every one of them.
            client.NoDelay = true;
            NetworkStream stream = client.GetStream();

            // The token goes first, before any frame. It is not the security
            // boundary — the core listens on loopback only — but it keeps
            // anything else on this machine that happens to find the port from
            // feeding it frames.
            byte[] hello = Encoding.ASCII.GetBytes(token + "\n");
            stream.Write(hello, 0, hello.Length);
            stream.Flush();

            lock (Lock)
            {
                _client = client;
                _stream = stream;
            }
            AppLogger.Log($"FrameChannel: connected on port {port}");
        }
        catch (Exception e)
        {
            AppLogger.Log($"FrameChannel: cannot connect on port {port} ({e.Message}); frames stay on the JSON-RPC line");
        }
    }

    public static void Stop()
    {
        lock (Lock)
        {
            try { _stream?.Dispose(); } catch (Exception) { /* already gone */ }
            try { _client?.Dispose(); } catch (Exception) { /* already gone */ }
            _stream = null;
            _client = null;
        }
    }

    // Pushes one frame. Answers false if the channel is not up, which is the
    // caller's cue to reply on the JSON-RPC line instead.
    //
    // The frame is built whole and written under one lock: TCP keeps the
    // order, so the reader on the other end only has to read lengths, but a
    // frame interleaved with the next one cannot be resynchronised — there is
    // nothing in a length-prefixed stream to resynchronise against.
    public static bool Send(string id, Dictionary<string, object?> meta, byte[] payload)
    {
        byte[] idBytes = Encoding.ASCII.GetBytes(id);
        if (idBytes.Length > ushort.MaxValue) return false;
        byte[] metaBytes = JsonSerializer.SerializeToUtf8Bytes(meta);

        var frame = new byte[Magic.Length + 2 + idBytes.Length + 4 + metaBytes.Length + 4 + payload.Length];
        int at = 0;
        Buffer.BlockCopy(Magic, 0, frame, at, Magic.Length); at += Magic.Length;
        WriteUInt16(frame, ref at, (ushort)idBytes.Length);
        Buffer.BlockCopy(idBytes, 0, frame, at, idBytes.Length); at += idBytes.Length;
        WriteUInt32(frame, ref at, (uint)metaBytes.Length);
        Buffer.BlockCopy(metaBytes, 0, frame, at, metaBytes.Length); at += metaBytes.Length;
        WriteUInt32(frame, ref at, (uint)payload.Length);
        Buffer.BlockCopy(payload, 0, frame, at, payload.Length);

        lock (Lock)
        {
            if (_stream == null) return false;
            try
            {
                _stream.Write(frame, 0, frame.Length);
                _stream.Flush();
                return true;
            }
            catch (Exception)
            {
                // Half-written, so this connection can no longer be read from
                // in step. Drop it and answer the old way from here on.
                try { _stream.Dispose(); } catch (Exception) { /* already gone */ }
                try { _client?.Dispose(); } catch (Exception) { /* already gone */ }
                _stream = null;
                _client = null;
                return false;
            }
        }
    }

    // Big-endian throughout, which is what every language's "write an integer
    // to a socket" reaches for first.
    private static void WriteUInt16(byte[] buffer, ref int at, ushort value)
    {
        buffer[at++] = (byte)(value >> 8);
        buffer[at++] = (byte)(value & 0xFF);
    }

    private static void WriteUInt32(byte[] buffer, ref int at, uint value)
    {
        buffer[at++] = (byte)((value >> 24) & 0xFF);
        buffer[at++] = (byte)((value >> 16) & 0xFF);
        buffer[at++] = (byte)((value >> 8) & 0xFF);
        buffer[at++] = (byte)(value & 0xFF);
    }
}
