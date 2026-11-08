import AppKit

private final class VirtualPointerPanel: NSPanel {
    override var canBecomeKey: Bool { false }
    override var canBecomeMain: Bool { false }
}

private final class VirtualPointerView: NSView {
    var tint = NSColor.systemTeal
    var title = ""
    override func draw(_ dirtyRect: NSRect) {
        let arrow = NSBezierPath()
        arrow.move(to: NSPoint(x: 5, y: 39)); arrow.line(to: NSPoint(x: 5, y: 13))
        arrow.line(to: NSPoint(x: 12, y: 20)); arrow.line(to: NSPoint(x: 18, y: 8))
        arrow.line(to: NSPoint(x: 23, y: 11)); arrow.line(to: NSPoint(x: 17, y: 23))
        arrow.line(to: NSPoint(x: 28, y: 23)); arrow.close()
        tint.setFill(); arrow.fill(); NSColor.white.withAlphaComponent(0.85).setStroke(); arrow.lineWidth = 1; arrow.stroke()
        NSColor.windowBackgroundColor.withAlphaComponent(0.95).setFill()
        NSBezierPath(roundedRect: NSRect(x: 34, y: 6, width: bounds.width - 36, height: 30), xRadius: 9, yRadius: 9).fill()
        (title as NSString).draw(in: NSRect(x: 44, y: 13, width: bounds.width - 54, height: 18), withAttributes: [
            .font: NSFont.systemFont(ofSize: 11, weight: .medium), .foregroundColor: NSColor.labelColor
        ])
    }
}

/// Display only: never posts mouse events, activates apps, or intercepts input.
/// Background apps use a labelled rail, rather than drawing over another app
/// at the same desktop coordinate.
final class DesktopVirtualPointers {
    private struct Entry { let panel: VirtualPointerPanel; var expiry: DispatchWorkItem?; var updated: Date; let slot: Int; let point: CGPoint? }
    private var entries: [String: Entry] = [:]

    func show(id: String, label: String, app: String, point: CGPoint?, background: Bool) {
        if entries[id] == nil && entries.count >= 16, let oldest = entries.min(by: { $0.value.updated < $1.value.updated })?.key {
            entries.removeValue(forKey: oldest)?.panel.orderOut(nil)
        }
        let existing = entries[id]
        let effectivePoint = point ?? existing?.point
        let usedSlots = Set(entries.values.map { $0.slot })
        let slot = existing?.slot ?? (0..<16).first { !usedSlots.contains($0) } ?? 0
        let panel = existing?.panel ?? VirtualPointerPanel(contentRect: NSRect(x: 0, y: 0, width: 278, height: 46), styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: false)
        panel.isOpaque = false; panel.backgroundColor = .clear; panel.hasShadow = true
        panel.ignoresMouseEvents = true; panel.hidesOnDeactivate = false; panel.level = .statusBar
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .ignoresCycle]
        let view = (panel.contentView as? VirtualPointerView) ?? VirtualPointerView(frame: NSRect(x: 0, y: 0, width: 278, height: 46))
        let palette: [NSColor] = [.systemTeal, .systemPurple, .systemOrange, .systemBlue, .systemPink, .systemGreen]
        let hash = (id.split(separator: "|").first.map(String.init) ?? id).utf8.reduce(0) { ($0 * 31 + Int($1)) % 65521 }
        view.tint = palette[hash % palette.count]
        view.title = String((label + " · " + app + (background ? " · 后台" : "")).prefix(38))
        panel.title = "Aide 虚拟光标 · " + view.title
        view.setAccessibilityElement(true); view.setAccessibilityRole(.image); view.setAccessibilityLabel(panel.title)
        panel.contentView = view; view.needsDisplay = true
        let desktopTop = NSScreen.screens.first?.frame.maxY ?? 0
        let screenPoint = effectivePoint.map { NSPoint(x: $0.x, y: desktopTop - $0.y) }
        let screen = screenPoint.flatMap { p in NSScreen.screens.first { $0.frame.contains(p) } } ?? NSScreen.main ?? NSScreen.screens.first
        guard let screen else { return }
        var origin: NSPoint
        if !background, let point = effectivePoint { origin = NSPoint(x: point.x - 5, y: desktopTop - point.y - 39) }
        else {
            origin = NSPoint(x: screen.visibleFrame.maxX - 292 - CGFloat(slot / 8) * 284, y: screen.visibleFrame.maxY - 56 - CGFloat(slot % 8) * 48)
        }
        origin.x = max(screen.visibleFrame.minX, min(origin.x, screen.visibleFrame.maxX - 278))
        origin.y = max(screen.visibleFrame.minY, min(origin.y, screen.visibleFrame.maxY - 46))
        existing?.expiry?.cancel()
        if panel.isVisible && !NSWorkspace.shared.accessibilityDisplayShouldReduceMotion {
            NSAnimationContext.runAnimationGroup { context in context.duration = 0.16; panel.animator().setFrameOrigin(origin) }
        } else { panel.setFrameOrigin(origin) }
        panel.alphaValue = 1; panel.orderFrontRegardless()
        let expiry = DispatchWorkItem { [weak panel] in panel?.orderOut(nil) }
        entries[id] = Entry(panel: panel, expiry: expiry, updated: Date(), slot: slot, point: effectivePoint)
        DispatchQueue.main.asyncAfter(deadline: .now() + 4, execute: expiry)
    }

    func status() -> [[String: Any]] {
        entries.map { id, entry in ["controllerId": id, "title": entry.panel.title, "visible": entry.panel.isVisible, "windowId": entry.panel.windowNumber] }
    }

    func stop() { for entry in entries.values { entry.expiry?.cancel(); entry.panel.orderOut(nil) }; entries.removeAll() }
}
