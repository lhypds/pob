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
// bar. 256 is the largest an .ico may hold, and six is the most ImageIO's ICO
// encoder will write — asking for a seventh fails the whole file.
let icoSizes = [16, 32, 48, 64, 128, 256]
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

func write(_ images: [CGImage], as type: UTType, to name: String) {
    let url = URL(fileURLWithPath: outputDir).appendingPathComponent(name)
    guard let dest = CGImageDestinationCreateWithURL(
        url as CFURL, type.identifier as CFString, images.count, nil
    ) else { fail("could not write \(name)") }
    for image in images { CGImageDestinationAddImage(dest, image, nil) }
    guard CGImageDestinationFinalize(dest) else { fail("could not finalize \(name)") }
    print("Wrote \(url.path)")
}

write([master], as: .png, to: "pob_icon_1024.png")
write(icoSizes.map { scaled(master, to: $0) }, as: .ico, to: "pob.ico")
write([scaled(master, to: linuxSize)], as: .png, to: "pob.png")
