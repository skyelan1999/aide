import CryptoKit
import Foundation
import LocalAuthentication
import Security
import Darwin

@main
enum StoreTests {
    static func check(_ condition: Bool) { precondition(condition) }
    static func rejected(_ body: () throws -> Void) {
        do { try body(); fatalError("unsafe operation accepted") } catch {}
    }
    static func main() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: false)
        defer { try? FileManager.default.removeItem(at: root) }
        let directory = root.appendingPathComponent("credentials")
        let store = SealedCredentialStore(directory: directory)
        let id = NativeWebAuthn.b64(Data(repeating: 7, count: 32))
        let file = directory.appendingPathComponent(id + ".sealed")
        let fixture = Data("opaque fixture".utf8)
        check(try store.ids().isEmpty)
        try store.write(fixture, id: id)
        check(try store.read(id) == fixture)
        check(try store.ids() == [id])
        rejected { try store.write(Data([1]), id: id) }
        check(try store.read(id) == fixture)
        rejected { try store.write(fixture, id: "../outside") }
        chmod(file.path, 0o644)
        rejected { _ = try store.read(id) }
        chmod(file.path, 0o600)
        let link = root.appendingPathComponent("hardlink")
        precondition(Darwin.link(file.path, link.path) == 0)
        rejected { _ = try store.read(id) }
        try FileManager.default.removeItem(at: link)
        try store.remove(id)
        let outside = root.appendingPathComponent("preserve")
        try fixture.write(to: outside)
        try FileManager.default.createSymbolicLink(at: file, withDestinationURL: outside)
        rejected { _ = try store.read(id) }
        rejected { try store.write(fixture, id: id) }
        try store.remove(id)
        check(try Data(contentsOf: outside) == fixture)
        precondition(mkfifo(file.path, 0o600) == 0)
        rejected { _ = try store.read(id) }
        try store.remove(id)
        chmod(directory.path, 0o755)
        rejected { _ = try store.ids() }
        chmod(directory.path, 0o700)
        let symlinkStore = SealedCredentialStore(directory: root.appendingPathComponent("linked"))
        try FileManager.default.createSymbolicLink(at: symlinkStore.directory, withDestinationURL: directory)
        rejected { try symlinkStore.write(fixture, id: id) }
        print("PASS owner-only store, round trip, duplicate/traversal/symlink/hardlink/FIFO/permission rejection")

        guard SecureEnclave.isAvailable else { print("NOT_RUN Secure Enclave hardware unavailable"); return }
        let access = SecAccessControlCreateWithFlags(nil, kSecAttrAccessibleWhenUnlockedThisDeviceOnly,
            [.privateKeyUsage, .biometryCurrentSet], nil)!
        let key = try SecureEnclave.P256.Signing.PrivateKey(accessControl: access)
        try store.write(key.dataRepresentation, id: id)
        let auth = LAContext(); auth.interactionNotAllowed = true
        let restored = try SecureEnclave.P256.Signing.PrivateKey(dataRepresentation: store.read(id), authenticationContext: auth)
        precondition(key.publicKey.x963Representation == restored.publicKey.x963Representation)
        rejected { _ = try restored.signature(for: Data("no biometric must fail".utf8)) }
        print("PASS real enclave creation, sealed persistence/reopen and biometric-required signature rejection")
        // Real user-approved signing is deliberately opt-in; no production data.
        if CommandLine.arguments.contains("--interactive") {
            let authentication = LAContext()
            authentication.localizedFallbackTitle = ""
            authentication.touchIDAuthenticationAllowableReuseDuration = 0
            let semaphore = DispatchSemaphore(value: 0)
            var authenticated = false
            authentication.evaluatePolicy(.deviceOwnerAuthenticationWithBiometrics,
                localizedReason: "验证 Aide 修复后的 Touch ID 签名（隔离测试）") { success, _ in
                authenticated = success; semaphore.signal()
            }
            precondition(semaphore.wait(timeout: .now() + 120) == .success && authenticated)
            let signer = try SecureEnclave.P256.Signing.PrivateKey(dataRepresentation: store.read(id), authenticationContext: authentication)
            let message = Data("isolated signature fixture".utf8)
            let signature = try signer.signature(for: message)
            precondition(key.publicKey.isValidSignature(signature, for: message))
            authentication.invalidate()
            print("PASS actual Touch ID and restored hardware signature verification")
        } else { print("NOT_RUN user-approved Touch ID signature (requires --interactive)") }
    }
}
