#!/usr/bin/env swift
import Cocoa

let size = 1024
let text = "Pob"

let colorSpace = CGColorSpaceCreateDeviceRGB()
let bitmapInfo = CGBitmapInfo(rawValue: CGImageAlphaInfo.premultipliedFirst.rawValue)
guard let ctx = CGContext(
    data: nil, width: size, height: size,
    bitsPerComponent: 8, bytesPerRow: 0,
    space: colorSpace, bitmapInfo: bitmapInfo.rawValue
) else { exit(1) }

/// Rounded-rect background
let bgColor = CGColor(red: 0.18, green: 0.25, blue: 0.75, alpha: 1.0)
ctx.setFillColor(bgColor)
let radius: CGFloat = 200
let path = CGPath(
    roundedRect: CGRect(x: 0, y: 0, width: size, height: size),
    cornerWidth: radius, cornerHeight: radius, transform: nil
)
ctx.addPath(path)
ctx.fillPath()

/// Draw text centered
let nsCtx = NSGraphicsContext(cgContext: ctx, flipped: false)
NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = nsCtx

let font = NSFont.boldSystemFont(ofSize: 440)
let attrs: [NSAttributedString.Key: Any] = [
    .font: font,
    .foregroundColor: NSColor.white,
]
let str = NSAttributedString(string: text, attributes: attrs)
let textSize = str.size()
let x = (CGFloat(size) - textSize.width) / 2
let y = (CGFloat(size) - textSize.height) / 2
str.draw(at: CGPoint(x: x, y: y))

NSGraphicsContext.restoreGraphicsState()

guard let image = ctx.makeImage() else { exit(1) }
let nsImage = NSImage(cgImage: image, size: NSSize(width: size, height: size))
guard let tiffData = nsImage.tiffRepresentation,
      let bitmap = NSBitmapImageRep(data: tiffData),
      let pngData = bitmap.representation(using: .png, properties: [:]) else { exit(1) }

let outputPath = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "pob_icon_1024.png"
try! pngData.write(to: URL(fileURLWithPath: outputPath))
print("Icon written to \(outputPath)")
