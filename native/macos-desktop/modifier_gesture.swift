import Foundation

// Two clean, short modifier taps; typing, clicks and held chords cancel the pair.
struct DoubleModifierGesture {
    var pressedAt: TimeInterval?
    var lastTap: TimeInterval?
    mutating func cancel() { pressedAt = nil; lastTap = nil }
    mutating func flagsChanged(targetKey: Bool, onlyTargetDown: Bool, allReleased: Bool, time: TimeInterval) -> Bool {
        guard targetKey else { cancel(); return false }
        if onlyTargetDown {
            if pressedAt == nil { pressedAt = time }
            return false
        }
        guard allReleased, let start = pressedAt else { cancel(); return false }
        pressedAt = nil
        guard time >= start, time - start <= 0.4 else { lastTap = nil; return false }
        if let first = lastTap, time > first, time - first <= 0.8 { lastTap = nil; return true }
        lastTap = time
        return false
    }
}
