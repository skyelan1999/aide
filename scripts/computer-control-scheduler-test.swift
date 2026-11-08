import Foundation

actor ControlTestLog {
    var values: [Int] = []
    func add(_ value: Int) { values.append(value) }
    func snapshot() -> [Int] { values }
}

@main struct ControlSchedulerTest {
    static func waitFor(_ gate: DesktopControlScheduler, _ resource: String, _ count: Int) async {
        for _ in 0..<1000 {
            if await gate.pendingCount(resource) == count { return }
            try? await Task.sleep(nanoseconds: 1_000_000)
        }
        fatalError("queue did not reach expected count")
    }

    static func main() async throws {
        for raw in ["CMD+SHIFT+Z", "CTRL+ALT+LEFT", "ALT+SHIFT+RIGHT", "CMD+A", "F20", "PAGEUP"] { precondition(DesktopKeyChord.parse(raw) != nil) }
        for raw in ["CMD", "CMD+CMD+A", "CTRL++A", "CMD+UNKNOWN", "CTRL+A+B"] { precondition(DesktopKeyChord.parse(raw) == nil) }
        let chord=DesktopKeyChord.parse("CMD+ALT+SHIFT+Z")!
        precondition(chord.code==6 && chord.modifiers==Set(["CMD","ALT","SHIFT"]))
        let deadline=DesktopSingleFlightDeadline<Int>()
        let releaseWorker=DispatchSemaphore(value:0)
        let firstValue=await deadline.run(timeout:0.01){releaseWorker.wait();return 42}
        precondition(firstValue==nil)
        let busyValue=await deadline.run(timeout:0.01){fatalError("busy recognizer must not spawn another worker");return 0}
        precondition(busyValue==nil)
        releaseWorker.signal()
        for _ in 0..<1000 {if !deadline.isBusy {break};try await Task.sleep(nanoseconds:1_000_000)}
        precondition(!deadline.isBusy)
        let recoveredValue=await deadline.run(timeout:1){return 7}
        precondition(recoveredValue==7)
        let gate = DesktopControlScheduler(), log = ControlTestLog()
        try await gate.acquire("app:A")
        try await gate.acquire("app:B") // An unrelated app is not blocked.
        await gate.release("app:B")
        let first = Task {
            try await gate.acquire("app:A")
            await log.add(1); await gate.release("app:A")
        }
        await waitFor(gate,"app:A",1)
        let second = Task {
            try await gate.acquire("app:A")
            await log.add(2); await gate.release("app:A")
        }
        await waitFor(gate,"app:A",2)
        await gate.release("app:A")
        try await first.value; try await second.value
        let order = await log.snapshot(); precondition(order == [1,2])

        try await gate.acquire("system-input")
        let cancelled = Task { try await gate.acquire("system-input") }
        await waitFor(gate,"system-input",1)
        cancelled.cancel()
        do { try await cancelled.value; fatalError("cancelled waiter was granted input") } catch is CancellationError {}
        await waitFor(gate,"system-input",0)
        await gate.release("system-input")
        try await gate.acquire("system-input"); await gate.release("system-input")

        for _ in 0..<30 {
            try await gate.acquire("race")
            let waiter = Task {
                try await gate.acquire("race")
                defer { Task { await gate.release("race") } }
                try Task.checkCancellation()
            }
            await waitFor(gate,"race",1)
            waiter.cancel(); await gate.release("race")
            _ = await waiter.result
            try await gate.acquire("race"); await gate.release("race")
        }

        precondition(DesktopTextSelection.replacing("a😀b", range: NSRange(location:1,length:2), with:"X") == "aXb")
        precondition(DesktopTextSelection.replacing("a😀b", range: NSRange(location:2,length:1), with:"X") == nil)
        precondition(DesktopTextSelection.replacing("hello", range: NSRange(location:0,length:5), with:"new") == "new")
        precondition(DesktopTextSelection.replacing("hello", range: NSRange(location:Int.max,length:1), with:"X") == nil)
        for (w,h) in [(1280.0,800.0),(2670,1740),(4000,3000),(20000,1000),(600,2400),(800,540)] {
            guard let s = DesktopCaptureGeometry.size(width:w,height:h) else { fatalError("size missing") }
            precondition(s.width * s.height <= 4_000_000 && max(s.width,s.height) <= 2560)
            precondition(abs(Double(s.width)/Double(s.height)-w/h) < 0.003)
        }
        precondition(DesktopCaptureGeometry.size(width:0,height:100) == nil)
        precondition(DesktopCaptureGeometry.size(width:.infinity,height:100) == nil)
        print("PASS independent app lanes, FIFO system input, cancellation, Unicode selection and image budgets")
    }
}
