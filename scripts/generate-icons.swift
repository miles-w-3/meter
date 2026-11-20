import AppKit
import CoreGraphics

let dir = "go/internal/gui/assets/"
func load(_ name: String) -> CGImage {
    let source = CGImageSourceCreateWithURL(URL(fileURLWithPath: dir + name) as CFURL, nil)!
    return CGImageSourceCreateImageAtIndex(source, 0, nil)!
}
func save(_ image: CGImage, _ name: String) {
    let dest = CGImageDestinationCreateWithURL(URL(fileURLWithPath: dir + name) as CFURL, "public.png" as CFString, 1, nil)!
    CGImageDestinationAddImage(dest, image, nil)
    precondition(CGImageDestinationFinalize(dest))
}
func context(_ width: Int, _ height: Int) -> CGContext {
    CGContext(data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: width * 4, space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
}

let app = context(1024, 1024)
// macOS app icons occupy roughly 80% of the canvas, leaving transparent
// margins for alignment with the standard Dock icon silhouette.
let appBounds = CGRect(x: 102, y: 102, width: 820, height: 820)
app.addPath(CGPath(roundedRect: appBounds, cornerWidth: 184, cornerHeight: 184, transform: nil))
app.clip()
app.interpolationQuality = .high
app.draw(load("meter-source.png"), in: appBounds)
save(app.makeImage()!, "meter.png")

// The supplied artwork is white on black. Convert its luminance to alpha so
// AppKit can tint the fir and its glow for both light and dark menu bars.
// Crop the excess black around the centered artwork so the tree fills the
// menu bar's 18-point image height instead of appearing undersized.
let menu = context(600, 600)
let source = load("fir-menu-exact-source.png")
menu.draw(source, in: CGRect(x: -327, y: -327, width: 1254, height: 1254))
let pixels = menu.data!.assumingMemoryBound(to: UInt8.self)
for i in 0..<(600 * 600) {
    let p = i * 4
    let alpha = pixels[p]
    pixels[p] = 0; pixels[p + 1] = 0; pixels[p + 2] = 0; pixels[p + 3] = alpha
}
let small = context(32, 32)
small.interpolationQuality = .high
small.draw(menu.makeImage()!, in: CGRect(x: 0, y: 0, width: 32, height: 32))
save(small.makeImage()!, "fir-menu.png")

// Crop and tint the exact fir artwork for the title bar, preserving its glow.
let fir = context(700, 700)
fir.draw(load("fir-menu-exact-source.png"), in: CGRect(x: -277, y: -277, width: 1254, height: 1254))
let firPixels = fir.data!.assumingMemoryBound(to: UInt8.self)
for i in 0..<(700 * 700) {
    let p = i * 4
    let alpha = Int(firPixels[p])
    firPixels[p] = UInt8(alpha * 143 / 255)
    firPixels[p + 1] = UInt8(alpha * 174 / 255)
    firPixels[p + 2] = UInt8(alpha * 122 / 255)
    firPixels[p + 3] = UInt8(alpha)
}
let titleIcon = context(128, 128)
titleIcon.interpolationQuality = .high
titleIcon.draw(fir.makeImage()!, in: CGRect(x: 0, y: 0, width: 128, height: 128))
save(titleIcon.makeImage()!, "fir-title.png")
