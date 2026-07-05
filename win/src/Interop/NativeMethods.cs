// Win32 interop used by the Pob Windows shell: extended window styles
// (click-through / no-activate), SendInput mouse & keyboard synthesis,
// cursor save/restore, screen capture via BitBlt, and monitor geometry.
using System.Runtime.InteropServices;

namespace Pob.Interop;

internal static class NativeMethods
{
    // ── window styles ───────────────────────────────────────────────────────

    public const int GWL_EXSTYLE = -20;
    public const long WS_EX_TRANSPARENT = 0x00000020;
    public const long WS_EX_NOACTIVATE = 0x08000000;

    [DllImport("user32.dll", EntryPoint = "GetWindowLongPtrW")]
    public static extern IntPtr GetWindowLongPtr(IntPtr hWnd, int nIndex);

    [DllImport("user32.dll", EntryPoint = "SetWindowLongPtrW")]
    public static extern IntPtr SetWindowLongPtr(IntPtr hWnd, int nIndex, IntPtr dwNewLong);

    public static void SetExStyleFlag(IntPtr hwnd, long flag, bool on)
    {
        if (hwnd == IntPtr.Zero) return;
        long ex = GetWindowLongPtr(hwnd, GWL_EXSTYLE).ToInt64();
        long updated = on ? ex | flag : ex & ~flag;
        if (updated != ex) SetWindowLongPtr(hwnd, GWL_EXSTYLE, new IntPtr(updated));
    }

    // ── cursor ──────────────────────────────────────────────────────────────

    [StructLayout(LayoutKind.Sequential)]
    public struct POINT
    {
        public int X;
        public int Y;
    }

    [DllImport("user32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    public static extern bool GetCursorPos(out POINT lpPoint);

    [DllImport("user32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    public static extern bool SetCursorPos(int x, int y);

    // ── SendInput ───────────────────────────────────────────────────────────

    public const uint INPUT_MOUSE = 0;
    public const uint INPUT_KEYBOARD = 1;

    public const uint MOUSEEVENTF_LEFTDOWN = 0x0002;
    public const uint MOUSEEVENTF_LEFTUP = 0x0004;
    public const uint MOUSEEVENTF_RIGHTDOWN = 0x0008;
    public const uint MOUSEEVENTF_RIGHTUP = 0x0010;
    public const uint MOUSEEVENTF_WHEEL = 0x0800;
    public const uint MOUSEEVENTF_HWHEEL = 0x1000;

    public const uint KEYEVENTF_EXTENDEDKEY = 0x0001;
    public const uint KEYEVENTF_KEYUP = 0x0002;
    public const uint KEYEVENTF_UNICODE = 0x0004;

    [StructLayout(LayoutKind.Sequential)]
    public struct MOUSEINPUT
    {
        public int dx;
        public int dy;
        public uint mouseData;
        public uint dwFlags;
        public uint time;
        public IntPtr dwExtraInfo;
    }

    [StructLayout(LayoutKind.Sequential)]
    public struct KEYBDINPUT
    {
        public ushort wVk;
        public ushort wScan;
        public uint dwFlags;
        public uint time;
        public IntPtr dwExtraInfo;
    }

    [StructLayout(LayoutKind.Explicit)]
    public struct INPUTUNION
    {
        [FieldOffset(0)] public MOUSEINPUT mi;
        [FieldOffset(0)] public KEYBDINPUT ki;
    }

    [StructLayout(LayoutKind.Sequential)]
    public struct INPUT
    {
        public uint type;
        public INPUTUNION u;
    }

    [DllImport("user32.dll", SetLastError = true)]
    public static extern uint SendInput(uint nInputs, INPUT[] pInputs, int cbSize);

    public static void Send(params INPUT[] inputs)
    {
        SendInput((uint)inputs.Length, inputs, Marshal.SizeOf<INPUT>());
    }

    public static INPUT MouseInput(uint flags, uint data = 0)
    {
        return new INPUT
        {
            type = INPUT_MOUSE,
            u = new INPUTUNION { mi = new MOUSEINPUT { dwFlags = flags, mouseData = data } },
        };
    }

    public static INPUT KeyInput(ushort vk, ushort scan, uint flags)
    {
        return new INPUT
        {
            type = INPUT_KEYBOARD,
            u = new INPUTUNION { ki = new KEYBDINPUT { wVk = vk, wScan = scan, dwFlags = flags } },
        };
    }

    // ── screen metrics ──────────────────────────────────────────────────────

    public const int SM_XVIRTUALSCREEN = 76;
    public const int SM_YVIRTUALSCREEN = 77;
    public const int SM_CXVIRTUALSCREEN = 78;
    public const int SM_CYVIRTUALSCREEN = 79;

    [DllImport("user32.dll")]
    public static extern int GetSystemMetrics(int nIndex);

    [StructLayout(LayoutKind.Sequential)]
    public struct RECT
    {
        public int Left;
        public int Top;
        public int Right;
        public int Bottom;
    }

    [StructLayout(LayoutKind.Sequential)]
    public struct MONITORINFO
    {
        public int cbSize;
        public RECT rcMonitor;
        public RECT rcWork;
        public uint dwFlags;
    }

    public const uint MONITOR_DEFAULTTONEAREST = 2;

    [DllImport("user32.dll")]
    public static extern IntPtr MonitorFromWindow(IntPtr hwnd, uint dwFlags);

    [DllImport("user32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    public static extern bool GetMonitorInfo(IntPtr hMonitor, ref MONITORINFO lpmi);

    // ── screen capture (GDI) ────────────────────────────────────────────────

    public const uint SRCCOPY = 0x00CC0020;
    public const uint CAPTUREBLT = 0x40000000;

    [DllImport("user32.dll")]
    public static extern IntPtr GetDC(IntPtr hWnd);

    [DllImport("user32.dll")]
    public static extern int ReleaseDC(IntPtr hWnd, IntPtr hDC);

    [DllImport("gdi32.dll")]
    public static extern IntPtr CreateCompatibleDC(IntPtr hdc);

    [DllImport("gdi32.dll")]
    public static extern IntPtr CreateCompatibleBitmap(IntPtr hdc, int width, int height);

    [DllImport("gdi32.dll")]
    public static extern IntPtr SelectObject(IntPtr hdc, IntPtr hgdiobj);

    [DllImport("gdi32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    public static extern bool BitBlt(IntPtr hdcDest, int xDest, int yDest, int width, int height,
                                     IntPtr hdcSrc, int xSrc, int ySrc, uint rop);

    [DllImport("gdi32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    public static extern bool DeleteObject(IntPtr hObject);

    [DllImport("gdi32.dll")]
    [return: MarshalAs(UnmanagedType.Bool)]
    public static extern bool DeleteDC(IntPtr hdc);
}
