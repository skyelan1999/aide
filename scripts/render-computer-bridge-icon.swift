import AppKit
import CoreText
import Foundation

guard CommandLine.arguments.count == 2 else {
    fputs("usage: render-computer-bridge-icon.swift <iconset-directory>\n", stderr)
    exit(2)
}

let output = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
let fileManager = FileManager.default
try fileManager.createDirectory(at: output, withIntermediateDirectories: true)

// Match internal/server/web/favicon.svg: Aide blue, rounded square, white “a”.
let variants: [(String, Int)] = [
    ("icon_16x16.png", 16),
    ("icon_16x16@2x.png", 32),
    ("icon_32x32.png", 32),
    ("icon_32x32@2x.png", 64),
    ("icon_128x128.png", 128),
    ("icon_128x128@2x.png", 256),
    ("icon_256x256.png", 256),
    ("icon_256x256@2x.png", 512),
    ("icon_512x512.png", 512),
    ("icon_512x512@2x.png", 1024),
]

for (name, pixels) in variants {
    let side = CGFloat(pixels)
    guard let context = CGContext(data: nil,
                                  width: pixels,
                                  height: pixels,
                                  bitsPerComponent: 8,
                                  bytesPerRow: 0,
                                  space: CGColorSpaceCreateDeviceRGB(),
                                  bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else {
        throw NSError(domain: "AideBridgeIcon", code: 1,
                      userInfo: [NSLocalizedDescriptionKey: "Could not create bitmap context for \(name)"])
    }
    context.setFillColor(red: 0, green: 122.0 / 255.0, blue: 1, alpha: 1)
    let roundedSquare = CGPath(roundedRect: CGRect(x: 0, y: 0, width: side, height: side),
                               cornerWidth: side * 15.0 / 64.0,
                               cornerHeight: side * 15.0 / 64.0,
                               transform: nil)
    context.addPath(roundedSquare)
    context.fillPath()

    let fontSize = side * 53.0 / 64.0
    let font = CTFontCreateWithName("Helvetica-Bold" as CFString, fontSize, nil)
    let attributes: [NSAttributedString.Key: Any] = [
        kCTFontAttributeName as NSAttributedString.Key: font,
        kCTForegroundColorAttributeName as NSAttributedString.Key: CGColor(gray: 1, alpha: 1),
    ]
    let line = CTLineCreateWithAttributedString(NSAttributedString(string: "a", attributes: attributes))
    let markWidth = CGFloat(CTLineGetTypographicBounds(line, nil, nil, nil))
    context.textPosition = CGPoint(x: (side - markWidth) / 2, y: side * 15.0 / 64.0)
    CTLineDraw(line, context)

    guard let cgImage = context.makeImage(),
          let data = NSBitmapImageRep(cgImage: cgImage).representation(using: .png, properties: [:]) else {
        throw NSError(domain: "AideBridgeIcon", code: 1,
                      userInfo: [NSLocalizedDescriptionKey: "Could not encode \(name)"])
    }
    try data.write(to: output.appendingPathComponent(name), options: .atomic)
}
