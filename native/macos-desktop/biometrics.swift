import CryptoKit
import Foundation
import LocalAuthentication
import Security
import Darwin

enum BiometricFailure: String, Error {
    case unavailable, invalidOptions, noCredential, alreadyRegistered, cancelled, busy, failed, capacity, storage
    var message: String {
        switch self {
        case .unavailable: return "触控 ID 暂不可用，请使用账户密码"
        case .invalidOptions: return "指纹请求无效，请重新开始"
        case .noCredential: return "请先用密码解锁，在设置 → 账户中为 Aide 桌面绑定触控 ID"
        case .alreadyRegistered: return "本机已绑定 Touch ID；如需重新绑定，请先在账户设置中删除旧的 Aide 桌面凭据"
        case .cancelled: return "已取消触控 ID 验证"
        case .busy: return "触控 ID 正在验证，请稍候"
        case .capacity: return "本机指纹凭据已达上限，请使用账户密码"
        case .failed: return "触控 ID 验证失败，请重试或使用账户密码"
        case .storage: return "无法安全保存本机 Touch ID 凭据，请检查应用数据目录权限并使用账户密码"
        }
    }
}

// CryptoKit's representation is sealed by this Mac's Secure Enclave, not a
// plaintext/exported private key. Permanent Keychain enclave keys require
// provisioned entitlements that an ad-hoc source build does not have.
final class SealedCredentialStore {
    let directory: URL
    init(directory: URL) { self.directory = directory }
    private func withDirectory<T>(_ body: (Int32) throws -> T) throws -> T {
        if mkdir(directory.path, 0o700) != 0 && errno != EEXIST { throw BiometricFailure.storage }
        let fd = open(directory.path, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
        guard fd >= 0 else { throw BiometricFailure.storage }
        defer { close(fd) }
        var info = stat()
        guard fstat(fd, &info) == 0, info.st_uid == getuid(), info.st_mode & 0o777 == 0o700 else {
            throw BiometricFailure.storage
        }
        return try body(fd)
    }
    private func filename(_ id: String) throws -> String {
        guard NativeWebAuthn.decode(id)?.count == 32 else { throw BiometricFailure.invalidOptions }
        return id + ".sealed"
    }
    func read(_ id: String) throws -> Data {
        let name = try filename(id)
        return try withDirectory { dir in
            let fd = openat(dir, name, O_RDONLY | O_NOFOLLOW | O_NONBLOCK | O_CLOEXEC)
            guard fd >= 0 else { throw BiometricFailure.storage }
            defer { close(fd) }
            var info = stat()
            guard fstat(fd, &info) == 0, info.st_mode & S_IFMT == S_IFREG,
                  info.st_uid == getuid(), info.st_nlink == 1, info.st_mode & 0o777 == 0o600,
                  (1...16384).contains(info.st_size) else { throw BiometricFailure.storage }
            var data = Data(count: Int(info.st_size))
            let count = data.withUnsafeMutableBytes { Darwin.read(fd, $0.baseAddress!, $0.count) }
            guard count == data.count else { throw BiometricFailure.storage }
            return data
        }
    }
    func ids() throws -> [String] {
        try withDirectory { _ in
            try FileManager.default.contentsOfDirectory(atPath: directory.path).sorted().compactMap {
                guard $0.hasSuffix(".sealed") else { return nil }
                let id = String($0.dropLast(7))
                guard NativeWebAuthn.decode(id)?.count == 32, (try? read(id)) != nil else { return nil }
                return id
            }
        }
    }
    func write(_ data: Data, id: String) throws {
        let name = try filename(id)
        guard (1...16384).contains(data.count) else { throw BiometricFailure.storage }
        try withDirectory { dir in
            let fd = openat(dir, name, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW | O_CLOEXEC, 0o600)
            guard fd >= 0 else { throw BiometricFailure.storage }
            var completed = false
            defer { close(fd); if !completed { unlinkat(dir, name, 0) } }
            guard data.withUnsafeBytes({ Darwin.write(fd, $0.baseAddress!, $0.count) }) == data.count,
                  fsync(fd) == 0 else { throw BiometricFailure.storage }
            completed = true
        }
    }
    func remove(_ id: String) throws {
        let name = try filename(id)
        try withDirectory { dir in
            guard unlinkat(dir, name, 0) == 0 || errno == ENOENT else { throw BiometricFailure.storage }
        }
    }
}

// Wire formatting only. The production caller supplies a Secure Enclave key;
// no private key bytes, passwords, or access tokens cross the WebKit boundary.
enum NativeWebAuthn {
    static func b64(_ data: Data) -> String {
        data.base64EncodedString().replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "=", with: "")
    }
    static func decode(_ text: String) -> Data? {
        guard text.count <= 4096, text.range(of: "^[A-Za-z0-9_-]+$", options: .regularExpression) != nil else { return nil }
        let padded = text.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
            + String(repeating: "=", count: (4 - text.count % 4) % 4)
        guard let value = Data(base64Encoded: padded), b64(value) == text else { return nil }
        return value
    }
    static func challenge(_ options: [String: Any], create: Bool) throws -> String {
        let rp = create ? (options["rp"] as? [String: Any])?["id"] as? String : options["rpId"] as? String
        guard rp == "localhost", let text = options["challenge"] as? String,
              let bytes = decode(text), (16...64).contains(bytes.count) else { throw BiometricFailure.invalidOptions }
        if create {
            guard let algorithms = options["pubKeyCredParams"] as? [[String: Any]],
                  algorithms.contains(where: { $0["type"] as? String == "public-key" && $0["alg"] as? Int == -7 }),
                  let user = options["user"] as? [String: Any], let id = user["id"] as? String,
                  let handle = decode(id), (1...64).contains(handle.count),
                  (options["attestation"] as? String ?? "none") == "none",
                  (options["authenticatorSelection"] as? [String: Any])?["residentKey"] as? String != "required"
            else { throw BiometricFailure.invalidOptions }
        }
        return text
    }
    static func allowed(_ options: [String: Any], key: String) throws -> [String] {
        guard let descriptors = options[key] as? [[String: Any]], descriptors.count <= 128 else {
            if options[key] == nil && key == "excludeCredentials" { return [] }
            throw BiometricFailure.invalidOptions
        }
        return try descriptors.map {
            guard $0["type"] as? String == "public-key", let id = $0["id"] as? String,
                  let bytes = decode(id), (1...1024).contains(bytes.count) else { throw BiometricFailure.invalidOptions }
            return id
        }
    }
    static func clientData(_ challenge: String, origin: String, create: Bool) throws -> Data {
        try JSONSerialization.data(withJSONObject: ["type": create ? "webauthn.create" : "webauthn.get",
            "challenge": challenge, "origin": origin, "crossOrigin": false], options: [.sortedKeys])
    }
    static func authData(create: Bool) -> Data {
        Data(SHA256.hash(data: Data("localhost".utf8))) + Data([create ? 0x45 : 0x05, 0, 0, 0, 0])
    }
    // Minimal definite-length CBOR encoder: WebAuthn none attestation + COSE ES256.
    static func header(_ major: UInt8, _ count: Int) -> Data {
        if count < 24 { return Data([major << 5 | UInt8(count)]) }
        if count < 256 { return Data([major << 5 | 24, UInt8(count)]) }
        return Data([major << 5 | 25, UInt8(count >> 8), UInt8(count & 255)])
    }
    static func bytes(_ data: Data) -> Data { header(2, data.count) + data }
    static func text(_ value: String) -> Data { let data = Data(value.utf8); return header(3, data.count) + data }
    static func creation(id: Data, publicKey: Data, challenge: String, origin: String) throws -> [String: Any] {
        guard id.count == 32, publicKey.count == 65, publicKey.first == 4 else { throw BiometricFailure.failed }
        let cose = Data([0xa5, 0x01, 0x02, 0x03, 0x26, 0x20, 0x01, 0x21])
            + bytes(publicKey.subdata(in: 1..<33)) + Data([0x22]) + bytes(publicKey.subdata(in: 33..<65))
        let auth = authData(create: true) + Data(repeating: 0, count: 16) + Data([0, 32]) + id + cose
        let object = Data([0xa3]) + text("fmt") + text("none") + text("authData") + bytes(auth)
            + text("attStmt") + Data([0xa0])
        return ["id": b64(id), "rawId": b64(id), "type": "public-key",
                "response": ["attestationObject": b64(object),
                             "clientDataJSON": b64(try clientData(challenge, origin: origin, create: true)),
                             "transports": ["internal"]]]
    }
    static func assertion(id: Data, challenge: String, origin: String,
                          sign: (Data) throws -> Data) throws -> [String: Any] {
        let client = try clientData(challenge, origin: origin, create: false), auth = authData(create: false)
        let signature = try sign(auth + Data(SHA256.hash(data: client)))
        return ["id": b64(id), "rawId": b64(id), "type": "public-key",
                "response": ["authenticatorData": b64(auth), "clientDataJSON": b64(client),
                             "signature": b64(signature), "userHandle": NSNull()]]
    }
}

final class DesktopBiometrics {
    let namespace: String
    private var context: LAContext?
    private var cancelled = false
    private let queue = DispatchQueue(label: "local.aide.biometric-key")
    private lazy var store = SealedCredentialStore(directory: FileManager.default.urls(for: .applicationSupportDirectory,
        in: .userDomainMask)[0].appendingPathComponent(namespace, isDirectory: true))
    init(namespace: String) { self.namespace = namespace + ".touch-id" }
    private func tag(_ id: String) -> Data { Data((namespace + "." + id).utf8) }

    func status() -> [String: Any] {
        let probe = LAContext()
        let available = SecureEnclave.isAvailable && probe.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: nil)
            && probe.biometryType == .touchID
        do { return ["available": available, "credentialIds": try credentialIDs()] }
        catch { return ["available": false, "credentialIds": [], "error": BiometricFailure.storage.message] }
    }
    private func credentialIDs() throws -> [String] {
        Array(Set(try store.ids() + legacyCredentialIDs())).sorted()
    }
    private func legacyCredentialIDs() -> [String] {
        let metadata = LAContext()
        metadata.interactionNotAllowed = true
        let query: [CFString: Any] = [kSecClass: kSecClassKey, kSecAttrLabel: namespace,
            kSecAttrKeyClass: kSecAttrKeyClassPrivate, kSecAttrKeyType: kSecAttrKeyTypeECSECPrimeRandom,
            kSecReturnAttributes: true, kSecMatchLimit: kSecMatchLimitAll, kSecUseAuthenticationContext: metadata]
        var result: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess,
              let attributes = result as? [[CFString: Any]] else { return [] }
        return attributes.prefix(32).compactMap {
            guard let data = $0[kSecAttrApplicationTag] as? Data,
                  let stored = String(data: data, encoding: .utf8), stored.hasPrefix(namespace + ".") else { return nil }
            let id = String(stored.dropFirst(namespace.count + 1))
            return NativeWebAuthn.decode(id)?.count == 32 ? id : nil
        }
    }
    func cancel() { cancelled = true; context?.invalidate() }

    // Called on the main queue after the native host validates the frame/origin.
    func perform(operation: String, options: [String: Any], origin: String,
                 completion: @escaping (Result<[String: Any], BiometricFailure>) -> Void) {
        guard context == nil else { completion(.failure(.busy)); return }
        do {
            let challenge = try NativeWebAuthn.challenge(options, create: operation == "create")
            let existing = try credentialIDs()
            var selected: String?
            if operation == "create" {
                guard existing.count < 32 else { throw BiometricFailure.capacity }
                let excluded = try NativeWebAuthn.allowed(options, key: "excludeCredentials")
                // Separate app enrollment is intentional; existing Safari keys are untouched.
                if existing.contains(where: { excluded.contains($0) }) { throw BiometricFailure.alreadyRegistered }
            } else if operation == "get" {
                let allowed = try NativeWebAuthn.allowed(options, key: "allowCredentials")
                selected = existing.first(where: { allowed.contains($0) })
                guard selected != nil else { throw BiometricFailure.noCredential }
            } else { throw BiometricFailure.invalidOptions }
            guard status()["available"] as? Bool == true else { throw BiometricFailure.unavailable }
            let authentication = LAContext()
            authentication.localizedFallbackTitle = ""
            authentication.touchIDAuthenticationAllowableReuseDuration = 0
            context = authentication
            cancelled = false
            let deadline = DispatchWorkItem { authentication.invalidate() }
            DispatchQueue.main.asyncAfter(deadline: .now() + 120, execute: deadline)
            authentication.evaluatePolicy(.deviceOwnerAuthenticationWithBiometrics,
                localizedReason: operation == "create" ? "为 Aide 桌面绑定触控 ID" : "验证你的身份以解锁 Aide") { success, _ in
                self.queue.async {
                    let result: Result<[String: Any], BiometricFailure>
                    do {
                        guard success, DispatchQueue.main.sync(execute: { !self.cancelled }) else { throw BiometricFailure.cancelled }
                        if operation == "create" {
                            result = .success(try self.createKey(challenge: challenge, origin: origin, authentication: authentication))
                        } else {
                            result = .success(try self.sign(id: selected!, challenge: challenge, origin: origin, authentication: authentication))
                        }
                    } catch let error as BiometricFailure { result = .failure(error) }
                    catch { result = .failure(.failed) }
                    DispatchQueue.main.async {
                        deadline.cancel(); authentication.invalidate(); self.context = nil
                        if self.cancelled {
                            if operation == "create", case .success(let value) = result, let id = value["id"] as? String {
                                try? self.store.remove(id)
                                SecItemDelete([kSecClass: kSecClassKey, kSecAttrApplicationTag: self.tag(id),
                                               kSecAttrKeyClass: kSecAttrKeyClassPrivate] as CFDictionary)
                            }
                            completion(.failure(.cancelled))
                        } else { completion(result) }
                    }
                }
            }
        } catch let error as BiometricFailure { completion(.failure(error)) }
        catch { completion(.failure(.failed)) }
    }
    private func createKey(challenge: String, origin: String, authentication: LAContext) throws -> [String: Any] {
        var random = Data(count: 32)
        guard random.withUnsafeMutableBytes({ SecRandomCopyBytes(kSecRandomDefault, 32, $0.baseAddress!) }) == errSecSuccess,
              let access = SecAccessControlCreateWithFlags(nil, kSecAttrAccessibleWhenUnlockedThisDeviceOnly,
                    [.privateKeyUsage, .biometryCurrentSet], nil) else { throw BiometricFailure.failed }
        let id = NativeWebAuthn.b64(random)
        let key = try SecureEnclave.P256.Signing.PrivateKey(accessControl: access, authenticationContext: authentication)
        let result = try NativeWebAuthn.creation(id: random, publicKey: key.publicKey.x963Representation,
                                               challenge: challenge, origin: origin)
        try store.write(key.dataRepresentation, id: id)
        return result
    }
    private func sign(id: String, challenge: String, origin: String, authentication: LAContext) throws -> [String: Any] {
        if try store.ids().contains(id) {
            let key = try SecureEnclave.P256.Signing.PrivateKey(dataRepresentation: store.read(id),
                                                             authenticationContext: authentication)
            guard let raw = NativeWebAuthn.decode(id) else { throw BiometricFailure.invalidOptions }
            return try NativeWebAuthn.assertion(id: raw, challenge: challenge, origin: origin) {
                try key.signature(for: $0).derRepresentation
            }
        }
        let query: [CFString: Any] = [kSecClass: kSecClassKey, kSecAttrApplicationTag: tag(id),
            kSecAttrKeyClass: kSecAttrKeyClassPrivate, kSecAttrKeyType: kSecAttrKeyTypeECSECPrimeRandom,
            kSecReturnRef: true, kSecUseAuthenticationContext: authentication]
        var result: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess,
              let result = result, let raw = NativeWebAuthn.decode(id) else { throw BiometricFailure.noCredential }
        let key = result as! SecKey
        return try NativeWebAuthn.assertion(id: raw, challenge: challenge, origin: origin) { message in
            var error: Unmanaged<CFError>?
            guard let signed = SecKeyCreateSignature(key, .ecdsaSignatureMessageX962SHA256, message as CFData, &error) as Data?
            else { throw BiometricFailure.failed }
            return signed
        }
    }
}
