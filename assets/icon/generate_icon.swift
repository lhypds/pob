#!/usr/bin/env swift
// Draws the Pob app icon — "Pob" in Fira Code, black on white — and writes the
// three files the platforms cut their own icons from:
//
//   pob_icon_1024.png   master; macOS build.sh sips/iconutil it into AppIcon.icns
//   pob.ico             Windows; Pob.csproj embeds it in Pob.exe
//   pob.png             256×256; Linux ships it beside the binary
//
// Those files are committed, so no build runs this — one drawing reaches all
// three platforms, and they cannot drift apart. Re-run it (on macOS, with Fira
// Code installed) only to change the icon itself:
//
//   swift assets/icon/generate_icon.swift [output-dir]
import Cocoa
import CoreText
import ImageIO
import UniformTypeIdentifiers

let masterSize = 1024
let text = "Pob"

// The sizes Windows picks between, from the Start menu tile down to the title
// bar. 256 is the largest an .ico may hold.
let icoSizes = [16, 24, 32, 48, 64, 128, 256]
let linuxSize = 256

let outputDir = CommandLine.arguments.count > 1
    ? CommandLine.arguments[1]
    : URL(fileURLWithPath: CommandLine.arguments[0]).deletingLastPathComponent().path

func fail(_ message: String) -> Never {
    FileHandle.standardError.write("❌ \(message)\n".data(using: .utf8)!)
    exit(1)
}

// ── draw the master ─────────────────────────────────────────────────────────

// Fira Code, and nothing else: falling back to the system font would quietly
// produce a different icon than the one this script exists to draw.
let fontName = "FiraCode-Regular"
guard let font = NSFont(name: fontName, size: 360) else {
    fail("\(fontName) not found — install Fira Code (brew install --cask font-fira-code).")
}

let colorSpace = CGColorSpaceCreateDeviceRGB()
let bitmapInfo = CGBitmapInfo(rawValue: CGImageAlphaInfo.premultipliedFirst.rawValue)
guard let ctx = CGContext(
    data: nil, width: masterSize, height: masterSize,
    bitsPerComponent: 8, bytesPerRow: 0,
    space: colorSpace, bitmapInfo: bitmapInfo.rawValue
) else { fail("could not create the drawing context") }

/// Rounded-rect background
ctx.setFillColor(CGColor(red: 1.0, green: 1.0, blue: 1.0, alpha: 1.0))
let radius: CGFloat = 200
ctx.addPath(CGPath(
    roundedRect: CGRect(x: 0, y: 0, width: masterSize, height: masterSize),
    cornerWidth: radius, cornerHeight: radius, transform: nil
))
ctx.fillPath()

/// Draw text centered
let str = NSAttributedString(string: text, attributes: [
    .font: font,
    .foregroundColor: NSColor.black,
])
// Centre on the ink, not on the line box: the line box carries the font's full
// ascender and descender, and "Pob" reaches into neither — centring on it would
// sit the word visibly high. CTLineDraw is what takes a baseline origin, which
// is what the ink rectangle is measured against; NSAttributedString.draw(at:)
// would take the corner of the line box instead.
let line = CTLineCreateWithAttributedString(str)
let ink = CTLineGetBoundsWithOptions(line, .useGlyphPathBounds)
ctx.textPosition = CGPoint(
    x: (CGFloat(masterSize) - ink.width) / 2 - ink.minX,
    y: (CGFloat(masterSize) - ink.height) / 2 - ink.minY
)
CTLineDraw(line, ctx)

guard let master = ctx.makeImage() else { fail("could not render the icon") }

// ── write it out ────────────────────────────────────────────────────────────

func scaled(_ image: CGImage, to side: Int) -> CGImage {
    if side == image.width { return image }
    guard let small = CGContext(
        data: nil, width: side, height: side,
        bitsPerComponent: 8, bytesPerRow: 0,
        space: colorSpace, bitmapInfo: bitmapInfo.rawValue
    ) else { fail("could not create the \(side)×\(side) context") }
    small.interpolationQuality = .high
    small.draw(image, in: CGRect(x: 0, y: 0, width: side, height: side))
    guard let out = small.makeImage() else { fail("could not scale to \(side)×\(side)") }
    return out
}

func pngData(_ image: CGImage) -> Data {
    let data = NSMutableData()
    guard let dest = CGImageDestinationCreateWithData(
        data as CFMutableData, UTType.png.identifier as CFString, 1, nil
    ) else { fail("could not encode a PNG") }
    CGImageDestinationAddImage(dest, image, nil)
    guard CGImageDestinationFinalize(dest) else { fail("could not finalize a PNG") }
    return data as Data
}

func writePNG(_ image: CGImage, to name: String) {
    let url = URL(fileURLWithPath: outputDir).appendingPathComponent(name)
    do { try pngData(image).write(to: url) } catch { fail("could not write \(name): \(error)") }
    print("Wrote \(url.path)")
}

// ── the Windows .ico ────────────────────────────────────────────────────────
//
// Assembled by hand. ImageIO writes .ico files too, but its DIB entries are
// wrong in two ways that Windows does not survive: the pixels keep
// CoreGraphics' ARGB byte order where a DIB is read as BGRA — so the blue
// channel is taken for alpha, and every black pixel in "Pob" comes out fully
// transparent — and the 1-bit AND mask is written at twice the stride. The
// result is a blank white tile, which is what the app icon looked like on
// Windows while macOS and Linux (both PNG, which ImageIO does encode
// correctly) were fine.

/// The image as a Windows DIB: BITMAPINFOHEADER, bottom-up BGRA rows, then the
/// 1-bit AND mask. The mask is all zero — every pixel of this icon is opaque,
/// and the 32-bit alpha channel is what modern Windows composites with anyway.
func dib(_ image: CGImage, side: Int) -> Data {
    let stride = side * 4
    var pixels = [UInt8](repeating: 0, count: stride * side)
    pixels.withUnsafeMutableBytes { raw in
        // byteOrder32Little + premultipliedFirst lays the channels out as
        // B, G, R, A — the order a DIB wants, so the rows copy straight out.
        guard let c = CGContext(
            data: raw.baseAddress, width: side, height: side,
            bitsPerComponent: 8, bytesPerRow: stride, space: colorSpace,
            bitmapInfo: CGImageAlphaInfo.premultipliedFirst.rawValue
                | CGBitmapInfo.byteOrder32Little.rawValue
        ) else { fail("could not create the \(side)×\(side) DIB context") }
        c.interpolationQuality = .high
        c.draw(image, in: CGRect(x: 0, y: 0, width: side, height: side))
    }

    var out = Data()
    // BITMAPINFOHEADER. The height covers the colour rows and the mask rows
    // together, which is why it is doubled.
    func u16(_ v: UInt16) { withUnsafeBytes(of: v.littleEndian) { out.append(contentsOf: $0) } }
    func u32(_ v: UInt32) { withUnsafeBytes(of: v.littleEndian) { out.append(contentsOf: $0) } }
    func i32(_ v: Int32) { withUnsafeBytes(of: v.littleEndian) { out.append(contentsOf: $0) } }
    u32(40)                       // biSize
    i32(Int32(side))              // biWidth
    i32(Int32(side * 2))          // biHeight — colour rows + mask rows
    u16(1)                        // biPlanes
    u16(32)                       // biBitCount
    u32(0)                        // biCompression = BI_RGB
    u32(UInt32(stride * side))    // biSizeImage
    i32(0); i32(0); u32(0); u32(0)

    // A bitmap context holds row 0 at the top; a DIB stores it at the bottom.
    for row in (0..<side).reversed() {
        out.append(contentsOf: pixels[row * stride ..< (row + 1) * stride])
    }
    // AND mask: one bit per pixel, each row padded to 4 bytes.
    out.append(Data(count: ((side + 31) / 32) * 4 * side))
    return out
}

func writeICO(_ sides: [Int], to name: String) {
    // 256 goes in PNG-compressed — the convention every icon editor follows,
    // and a raw 256×256 DIB would be a quarter of a megabyte on its own.
    let images: [(side: Int, payload: Data)] = sides.sorted(by: >).map { side in
        let image = scaled(master, to: side)
        return (side, side >= 256 ? pngData(image) : dib(image, side: side))
    }

    var out = Data()
    func u16(_ v: UInt16) { withUnsafeBytes(of: v.littleEndian) { out.append(contentsOf: $0) } }
    func u32(_ v: UInt32) { withUnsafeBytes(of: v.littleEndian) { out.append(contentsOf: $0) } }

    u16(0); u16(1); u16(UInt16(images.count))          // ICONDIR
    var offset = 6 + 16 * images.count
    for (side, payload) in images {                     // ICONDIRENTRY each
        out.append(UInt8(side >= 256 ? 0 : side))       // 0 means 256
        out.append(UInt8(side >= 256 ? 0 : side))
        out.append(0)                                   // bColorCount
        out.append(0)                                   // bReserved
        u16(1)                                          // wPlanes
        u16(32)                                         // wBitCount
        u32(UInt32(payload.count))
        u32(UInt32(offset))
        offset += payload.count
    }
    for (_, payload) in images { out.append(payload) }

    let url = URL(fileURLWithPath: outputDir).appendingPathComponent(name)
    do { try out.write(to: url) } catch { fail("could not write \(name): \(error)") }
    print("Wrote \(url.path)")
}

writePNG(master, to: "pob_icon_1024.png")
writeICO(icoSizes, to: "pob.ico")
writePNG(scaled(master, to: linuxSize), to: "pob.png")
