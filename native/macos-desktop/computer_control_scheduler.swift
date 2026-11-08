import Foundation

/// Independent apps can progress together. Work for the same app, and the
/// system input device, each have a cancellable FIFO lane.
actor DesktopControlScheduler {
    private struct Waiter { let id: UUID; let continuation: CheckedContinuation<Void, Error> }
    private var held = Set<String>()
    private var waiting: [String: [Waiter]] = [:]

    func acquire(_ resource: String) async throws {
        let id = UUID()
        try Task.checkCancellation()
        try await withTaskCancellationHandler(operation: {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
                if Task.isCancelled { continuation.resume(throwing: CancellationError()); return }
                if held.insert(resource).inserted { continuation.resume(); return }
                waiting[resource, default: []].append(Waiter(id: id, continuation: continuation))
            }
        }, onCancel: { Task { await self.cancel(resource, id) } })
        // The owner installs its release defer before checking cancellation.
        // A cancellation racing a grant must not leak the resource lease.
    }

    private func cancel(_ resource: String, _ id: UUID) {
        guard let index = waiting[resource]?.firstIndex(where: { $0.id == id }),
              let waiter = waiting[resource]?.remove(at: index) else { return }
        waiter.continuation.resume(throwing: CancellationError())
        if waiting[resource]?.isEmpty == true { waiting.removeValue(forKey: resource) }
    }

    func release(_ resource: String) {
        if var queue = waiting[resource], !queue.isEmpty {
            let next = queue.removeFirst()
            if queue.isEmpty { waiting.removeValue(forKey: resource) } else { waiting[resource] = queue }
            next.continuation.resume()
        } else { held.remove(resource) }
    }

    func pendingCount(_ resource: String) -> Int { waiting[resource]?.count ?? 0 }
}

enum DesktopTextSelection {
    static func replacing(_ value: String, range: NSRange, with text: String) -> String? {
        guard range.location >= 0, range.length >= 0,
              range.location <= value.utf16.count, range.length <= value.utf16.count - range.location,
              let swiftRange = Range(range, in: value) else { return nil }
        let units = Array(value.utf16)
        func boundary(_ index: Int) -> Bool { index == units.count || index == 0 || !(0xDC00...0xDFFF).contains(units[index]) }
        guard boundary(range.location), boundary(range.location + range.length) else { return nil }
        return value.replacingCharacters(in: swiftRange, with: text)
    }
}

enum DesktopCaptureGeometry {
    // Same per-image limits as plugin_images.go. Preserve the window aspect.
    static func size(width: Double, height: Double) -> (width: Int, height: Int)? {
        guard width.isFinite, height.isFinite, width > 0, height > 0 else { return nil }
        let scale = min(2, min(2560 / max(width, height), sqrt(4_000_000 / width / height)))
        return (max(1, Int(floor(width * scale))), max(1, Int(floor(height * scale))))
    }
}

/// Canonical physical-key chords, shared by validation and native regression.
struct DesktopKeyChord {
    let code: UInt16
    let modifiers: Set<String>
    static func parse(_ raw: String) -> DesktopKeyChord? {
        let parts = raw.uppercased().split(separator: "+", omittingEmptySubsequences: false).map(String.init)
        guard !parts.isEmpty, parts.count <= 5, let key = parts.last else { return nil }
        let mods = Array(parts.dropLast()), allowed: Set<String> = ["CMD", "CTRL", "ALT", "SHIFT"]
        guard Set(mods).count == mods.count, mods.allSatisfy({ allowed.contains($0) }) else { return nil }
        let codes: [String: UInt16] = ["A":0,"S":1,"D":2,"F":3,"H":4,"G":5,"Z":6,"X":7,"C":8,"V":9,"B":11,"Q":12,"W":13,"E":14,"R":15,"Y":16,"T":17,"1":18,"2":19,"3":20,"4":21,"6":22,"5":23,"=":24,"9":25,"7":26,"-":27,"8":28,"0":29,
            "]":30,"O":31,"U":32,"[":33,"I":34,"P":35,"ENTER":36,"L":37,"J":38,"'":39,"K":40,";":41,"\\":42,",":43,"/":44,"N":45,"M":46,".":47,"TAB":48,"SPACE":49,"`":50,"BACKSPACE":51,"ESCAPE":53,
            "F17":64,"F18":79,"F19":80,"F20":90,"F5":96,"F6":97,"F7":98,"F3":99,"F8":100,"F9":101,"F11":103,"F13":105,"F16":106,"F14":107,"F10":109,"F12":111,"F15":113,"HOME":115,"PAGEUP":116,"DELETE":117,"F4":118,"END":119,"F2":120,"PAGEDOWN":121,"F1":122,"LEFT":123,"RIGHT":124,"DOWN":125,"UP":126]
        guard let code = codes[key] else { return nil }
        return DesktopKeyChord(code: code, modifiers: Set(mods))
    }
}

/// A slow optional recognizer cannot hold the UI or accumulate background jobs.
/// The deadline ends the caller's wait; a late worker retains the single slot
/// until it really finishes, and its late result cannot resume a caller twice.
final class DesktopSingleFlightDeadline<Value>: @unchecked Sendable {
    private let lock=NSLock()
    private var busy=false
    var isBusy:Bool {lock.lock();defer{lock.unlock()};return busy}
    private final class Result: @unchecked Sendable {
        let lock=NSLock()
        var continuation: CheckedContinuation<Value?,Never>?
        init(_ continuation:CheckedContinuation<Value?,Never>){self.continuation=continuation}
        func finish(_ value:Value?){lock.lock();let c=continuation;continuation=nil;lock.unlock();c?.resume(returning:value)}
    }
    func run(timeout:Double,work:@escaping ()->Value) async -> Value? {
        await withCheckedContinuation { continuation in
            lock.lock()
            if busy {lock.unlock();continuation.resume(returning:nil);return}
            busy=true;lock.unlock()
            let result=Result(continuation)
            DispatchQueue.global(qos:.utility).asyncAfter(deadline:.now()+timeout){result.finish(nil)}
            DispatchQueue.global(qos:.utility).async {
                let value=work();self.lock.lock();self.busy=false;self.lock.unlock()
                result.finish(value)
            }
        }
    }
}
