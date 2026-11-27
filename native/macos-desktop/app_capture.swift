import AppKit
import ApplicationServices
import ScreenCaptureKit

final class DesktopAppCapture {
    private var globalMonitor: Any?
    private var localMonitor: Any?
    private var gesture = DoubleModifierGesture()
    private let preferences = UserDefaults.standard
    var onTrigger: (() -> Void)?
    var lastResult = ""
    private var modifierEvents = 0
    private var triggerCount = 0
    var enabled: Bool { preferences.bool(forKey: "appCaptureEnabled") }
    var modifier: String { preferences.string(forKey: "appCaptureModifier") == "command" ? "command" : "shift" }

    func status() -> [String: Any] {
        ["supported": true, "enabled": enabled, "modifier": modifier,
         "accessibility": AXIsProcessTrusted(), "screenRecording": CGPreflightScreenCaptureAccess(),
         "lastResult": lastResult, "modifierEvents": modifierEvents, "triggerCount": triggerCount, "listening": globalMonitor != nil && localMonitor != nil]
    }
    func configure(enabled: Bool, modifier: String) {
        preferences.set(enabled, forKey: "appCaptureEnabled")
        preferences.set(modifier, forKey: "appCaptureModifier")
        start()
    }
    func start() {
        stop()
        guard enabled else { return }
        let mask: NSEvent.EventTypeMask = [.flagsChanged, .keyDown, .leftMouseDown, .rightMouseDown, .otherMouseDown]
        globalMonitor = NSEvent.addGlobalMonitorForEvents(matching: mask) { [weak self] event in self?.handle(event) }
        localMonitor = NSEvent.addLocalMonitorForEvents(matching: mask) { [weak self] event in self?.handle(event); return event }
    }
    func stop() {
        if let monitor = globalMonitor { NSEvent.removeMonitor(monitor) }
        if let monitor = localMonitor { NSEvent.removeMonitor(monitor) }
        globalMonitor = nil; localMonitor = nil; gesture.cancel()
    }
    private func handle(_ event: NSEvent) {
        guard enabled else { return }
        guard event.type == .flagsChanged else {
            if gesture.pressedAt != nil || gesture.lastTap.map({ event.timestamp - $0 <= 0.8 }) == true {
                lastResult = "两次修饰键之间出现了其他按键或鼠标操作，已取消。"
            }
            gesture.cancel(); return
        }
        modifierEvents += 1
        let flags = event.modifierFlags.intersection([.shift, .control, .option, .command])
        let command = modifier == "command", codes: [UInt16] = command ? [54,55] : [56,60]
        let onlyTarget = flags == (command ? .command : .shift)
        let targetTransition = codes.contains(event.keyCode) || onlyTarget || (flags.isEmpty && gesture.pressedAt != nil)
        if flags.isEmpty, let pressed = gesture.pressedAt, event.timestamp - pressed > 0.4 {
            lastResult = "单次按压过长。请短按后松开，再短按一次。"
        } else if flags.isEmpty, let first = gesture.lastTap, event.timestamp - first > 0.8 {
            lastResult = "双击间隔过长。请在 0.8 秒内短按两次。"
        }
        if gesture.flagsChanged(targetKey: targetTransition, onlyTargetDown: onlyTarget, allReleased: flags.isEmpty, time: event.timestamp) { triggerCount += 1; onTrigger?() }
    }
    func requestPermissions() {
        _ = AXIsProcessTrustedWithOptions([kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true] as CFDictionary)
        _ = CGRequestScreenCaptureAccess()
    }
    @MainActor func capture(_ target: NSRunningApplication) async throws -> [String: Any] {
        guard CGPreflightScreenCaptureAccess() else { throw desktopError("请先在系统隐私设置中允许 Aide 屏幕录制。") }
        guard target.activationPolicy == .regular, target.bundleIdentifier != "com.apple.loginwindow",
              NSWorkspace.shared.frontmostApplication?.processIdentifier == target.processIdentifier else { throw desktopError("前台应用已改变，请重新双击快捷键。") }
        let content = try await SCShareableContent.excludingDesktopWindows(true, onScreenWindowsOnly: true)
        let rows = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] ?? []
        let ids = rows.filter { ($0[kCGWindowOwnerPID as String] as? Int) == Int(target.processIdentifier) && ($0[kCGWindowLayer as String] as? Int) == 0 }.compactMap { $0[kCGWindowNumber as String] as? UInt32 }
        guard let window = ids.compactMap({ id in content.windows.first { $0.windowID == id && $0.frame.width > 1 && $0.frame.height > 1 } }).first else { throw desktopError("当前应用没有可截取的窗口。") }
        let filter = SCContentFilter(desktopIndependentWindow: window), config = SCStreamConfiguration()
        let scale = min(2, 2560 / max(window.frame.width, window.frame.height))
        config.width = max(1, Int(window.frame.width * scale)); config.height = max(1, Int(window.frame.height * scale)); config.showsCursor = false
        let image = try await SCScreenshotManager.captureImage(contentFilter: filter, configuration: config)
        guard NSWorkspace.shared.frontmostApplication?.processIdentifier == target.processIdentifier else { throw desktopError("前台应用已改变，截图已取消。") }
        guard let png = NSBitmapImageRep(cgImage: image).representation(using: .png, properties: [:]), png.count <= 16 * 1024 * 1024 else { throw desktopError("截图过大或编码失败，请缩小窗口后重试。") }
        return ["imageBase64": png.base64EncodedString(), "app": target.localizedName ?? "App", "bundleId": target.bundleIdentifier ?? "", "windowTitle": String((window.title ?? "").prefix(500)), "capturedAt": ISO8601DateFormatter().string(from: Date()), "pixelWidth": image.width, "pixelHeight": image.height]
    }
}
