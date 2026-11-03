// Test-only software key: never compiled into Aide.app, never persisted.
import Foundation
import Security

@main struct BiometricWireTests {
    static func main() throws {
        let origin = "https://localhost:9999"
        let register = NativeWebAuthn.b64(Data(repeating: 0x41, count: 32))
        let login = NativeWebAuthn.b64(Data(repeating: 0x42, count: 32))
        let id = Data(repeating: 0x43, count: 32)
        let options: [String: Any] = ["challenge":register, "rp":["id":"localhost"],
            "user":["id":NativeWebAuthn.b64(Data("aide-local-v1".utf8))],
            "pubKeyCredParams":[["type":"public-key","alg":-7]], "attestation":"none"]
        let validated = try NativeWebAuthn.challenge(options, create: true)
        precondition(validated == register)
        for change in [["challenge":"short"], ["rp":["id":"remote.test"]],
                       ["pubKeyCredParams":[["type":"public-key","alg":-257]]],
                       ["authenticatorSelection":["residentKey":"required"]], ["attestation":"direct"]] as [[String: Any]] {
            do {
                _ = try NativeWebAuthn.challenge(options.merging(change) { _, next in next }, create: true)
                fatalError("unsafe options accepted")
            } catch is BiometricFailure { }
        }
        precondition(NativeWebAuthn.decode("bad+/=") == nil)
        precondition(NativeWebAuthn.decode("a") == nil)
        let excluded = try NativeWebAuthn.allowed([:], key: "excludeCredentials")
        precondition(excluded.isEmpty)
        do { _ = try NativeWebAuthn.allowed([:], key: "allowCredentials"); fatalError("missing allow list") }
        catch is BiometricFailure { }
        precondition(NativeWebAuthn.authData(create:false).count == 37)
        let key = SecKeyCreateRandomKey([kSecAttrKeyType:kSecAttrKeyTypeECSECPrimeRandom,
                                         kSecAttrKeySizeInBits:256] as CFDictionary, nil)!
        let publicKey = SecKeyCopyExternalRepresentation(SecKeyCopyPublicKey(key)!, nil)! as Data
        let creation = try NativeWebAuthn.creation(id:id,publicKey:publicKey,challenge:register,origin:origin)
        let assertion = try NativeWebAuthn.assertion(id:id,challenge:login,origin:origin) { message in
            guard let signature = SecKeyCreateSignature(key,.ecdsaSignatureMessageX962SHA256,message as CFData,nil) else {
                throw BiometricFailure.failed
            }
            return signature as Data
        }
        if CommandLine.arguments.count == 3 && CommandLine.arguments[1] == "--fixture" {
            let fixture: [String:Any] = ["origin":origin,"registrationChallenge":register,"assertionChallenge":login,
                                       "registration":creation,"assertion":assertion]
            try JSONSerialization.data(withJSONObject:fixture,options:[.prettyPrinted,.sortedKeys])
                .write(to:URL(fileURLWithPath:CommandLine.arguments[2]))
        }
        print("PASS native WebAuthn formatting and rejected options; ephemeral software test key only")
    }
}
