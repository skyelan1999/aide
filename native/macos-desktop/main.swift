import AppKit
import AVFoundation
import CryptoKit
import Foundation
import Security
import WebKit
import ApplicationServices
import Network
import ScreenCaptureKit

struct DesktopSession: Decodable {
    let origin: String
    let token: String
    let certificateSHA256: String
    let instanceId: String

    func validate() throws -> URL {
        guard let url = URL(string: origin), url.scheme == "https", url.host == "localhost",
              let port = url.port, (1...65535).contains(port), url.user == nil, url.password == nil,
              url.query == nil, url.fragment == nil, ["", "/"].contains(url.path),
              token.range(of: "^[A-Za-z0-9_.-]{16,512}$", options: .regularExpression) != nil,
              certificateSHA256.range(of: "^[a-f0-9]{64}$", options: .regularExpression) != nil,
              !instanceId.isEmpty else { throw desktopError("本地会话校验未通过。") }
        return url
    }
}

func desktopError(_ message: String) -> NSError {
    NSError(domain: "AideDesktop", code: 1, userInfo: [NSLocalizedDescriptionKey: message])
}

enum DesktopPolicy {
    static func local(_ url: URL, origin: URL) -> Bool {
        if url.scheme == "blob", let embedded = URL(string: String(url.absoluteString.dropFirst(5))) {
            return embedded.scheme != "blob" && local(embedded, origin: origin)
        }
        return url.scheme == origin.scheme && url.host?.lowercased() == origin.host?.lowercased()
            && (url.port ?? 443) == (origin.port ?? 443) && url.user == nil && url.password == nil
    }

    static func external(_ url: URL) -> Bool {
        if url.scheme == "mailto" { return true }
        return ["http", "https"].contains(url.scheme ?? "") && url.host != nil
            && url.user == nil && url.password == nil
    }

    static func pluginURL(_ url: URL, origin: URL) -> Bool {
        local(url, origin: origin) && url.path.range(of: "^/plugin-workbench/[a-f0-9]{64}$", options: .regularExpression) != nil
    }

    static func uiFrame(_ frame: WKFrameInfo, expected: URL) -> Bool {
        media(frame.securityOrigin, expected: expected)
            || frame.request.url.map { pluginURL($0, origin: expected) } == true
    }

    static func media(_ securityOrigin: WKSecurityOrigin, expected: URL) -> Bool {
        securityOrigin.protocol == expected.scheme && securityOrigin.host.lowercased() == expected.host?.lowercased()
            && securityOrigin.port == (expected.port ?? 443)
    }

    static func biometric(_ url: URL, origin: URL) -> Bool {
        guard local(url, origin: origin), url.scheme == "https", url.path == "/", url.query == nil else { return false }
        let parameters = URLComponents(string: "?" + (url.fragment ?? ""))?.queryItems ?? []
        return !parameters.contains(where: { $0.name == "pluginWorkbench" })
    }
}

final class DesktopApp: NSObject, NSApplicationDelegate, NSWindowDelegate,
    WKNavigationDelegate, WKUIDelegate, WKDownloadDelegate, WKScriptMessageHandler, WKScriptMessageHandlerWithReply {
    let root: URL
    let smokeOutput: URL?
    var mainWindow: NSWindow!
    var views: [NSWindow: WKWebView] = [:]
    var session: DesktopSession?
    var origin: URL?
    var startup: Process?
    var computerControlHost: AideComputerControlHost?
    var statusLabel: NSTextField?
    var retryButton: NSButton?
    var pageErrors = 0
    var smokeAttempts = 0
    var smokeTabsChecked = false
    var smokePluginTabsChecked = false
    var smokeTabResults: [String: Any] = [:]
    weak var smokeComponentWeb: WKWebView?
    var smokeComponents: [String: Bool] = [:]
    var smokePluginFrames: [ObjectIdentifier: WKFrameInfo] = [:]
    var downloads: [ObjectIdentifier: WKDownload] = [:]
    var configuredControllers = Set<ObjectIdentifier>()
    var titleObservers: [ObjectIdentifier: NSKeyValueObservation] = [:]
    var approvedCloses = Set<ObjectIdentifier>()
    lazy var biometrics = DesktopBiometrics(namespace: Bundle.main.bundleIdentifier ?? "local.aide.desktop.invalid")

    init(root: URL, smokeOutput: URL?) {
        self.root = root.resolvingSymlinksInPath()
        self.smokeOutput = smokeOutput
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.regular)
        NSWindow.allowsAutomaticWindowTabbing = true
        makeMenus()
        mainWindow = makeWindow()
        showLoading("正在连接本地工作台…")
        mainWindow.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        connect()
    }

    func startComputerControlHost() {
        // connect() also runs after a service restart; retain the existing listener.
        guard computerControlHost == nil else { return }
        let envFile = root.appendingPathComponent(".env")
        guard let contents = try? String(contentsOf: envFile, encoding: .utf8),
              let line = contents.split(whereSeparator: \.isNewline).first(where: { $0.hasPrefix("AIDE_COMPUTER_BRIDGE_TOKEN=") }) else {
            fputs("Aide computer control is unavailable: local token is not configured.\n", stderr)
            return
        }
        let token = String(line.dropFirst("AIDE_COMPUTER_BRIDGE_TOKEN=".count)).trimmingCharacters(in: CharacterSet(charactersIn: " \"'"))
        guard token.count >= 32 else { fputs("Aide computer control is unavailable: invalid local token.\n", stderr); return }
        let host = AideComputerControlHost(token: token)
        do { try host.start(); computerControlHost = host }
        catch { fputs("Aide computer control failed to start: \(error.localizedDescription)\n", stderr) }
    }

    func makeWindow() -> NSWindow {
        let screen = NSScreen.main?.visibleFrame ?? NSRect(x: 0, y: 0, width: 1440, height: 1000)
        let size = NSSize(width: min(1440, screen.width - 60), height: min(960, screen.height - 70))
        let window = NSWindow(contentRect: NSRect(origin: .zero, size: size),
                              styleMask: [.titled, .closable, .miniaturizable, .resizable],
                              backing: .buffered, defer: false)
        window.title = "Aide"
        window.minSize = NSSize(width: 720, height: 540)
        window.center()
        window.isReleasedWhenClosed = false
        window.delegate = self
        window.tabbingMode = .preferred
        window.tabbingIdentifier = Bundle.main.bundleIdentifier ?? "local.aide.desktop"
        return window
    }

    func makeMenus() {
        let menu = NSMenu()
        func section(_ title: String) -> NSMenu {
            let item = NSMenuItem()
            let submenu = NSMenu(title: title)
            item.submenu = submenu
            menu.addItem(item)
            return submenu
        }
        func item(_ menu: NSMenu, _ title: String, _ action: Selector?, _ key: String = "") {
            let entry = NSMenuItem(title: title, action: action, keyEquivalent: key)
            entry.target = self
            menu.addItem(entry)
        }
        let app = section("Aide")
        item(app, "关于 Aide", #selector(about))
        item(app, "设置…", #selector(settings), ",")
        app.addItem(.separator())
        item(app, "隐藏 Aide", #selector(hideApp), "h")
        app.addItem(.separator())
        item(app, "停止本地服务并退出…", #selector(stopAndQuit))
        item(app, "退出 Aide", #selector(quit), "q")
        let file = section("文件")
        item(file, "新建工作台标签页", #selector(newTab), "t")
        item(file, "关闭窗口", #selector(closeWindow), "w")
        item(file, "打印…", #selector(printPage), "p")
        let edit = section("编辑")
        for (title, action, key) in [("撤销", "undo:", "z"), ("剪切", "cut:", "x"),
                                     ("拷贝", "copy:", "c"), ("粘贴", "paste:", "v"),
                                     ("全选", "selectAll:", "a")] {
            edit.addItem(withTitle: title, action: NSSelectorFromString(action), keyEquivalent: key)
        }
        let view = section("显示")
        item(view, "重新加载", #selector(reload), "r")
        item(view, "放大", #selector(zoomIn), "+")
        item(view, "缩小", #selector(zoomOut), "-")
        item(view, "实际大小", #selector(actualSize), "0")
        item(view, "在浏览器打开工作台", #selector(openBrowser))
        let windows = section("窗口")
        item(windows, "显示工作台", #selector(reopen))
        windows.addItem(withTitle: "最小化", action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        NSApp.windowsMenu = windows
        NSApp.mainMenu = menu
    }

    func showLoading(_ message: String) {
        let container = NSView()
        let label = NSTextField(labelWithString: message)
        label.font = .systemFont(ofSize: 17, weight: .medium)
        let note = NSTextField(labelWithString: "Aide")
        note.font = .systemFont(ofSize: 36, weight: .bold)
        let retry = NSButton(title: "重试连接", target: self, action: #selector(retryConnect))
        retry.isHidden = true
        let stack = NSStackView(views: [note, label, retry])
        stack.orientation = .vertical
        stack.spacing = 20
        stack.translatesAutoresizingMaskIntoConstraints = false
        container.addSubview(stack)
        NSLayoutConstraint.activate([stack.centerXAnchor.constraint(equalTo: container.centerXAnchor),
                                     stack.centerYAnchor.constraint(equalTo: container.centerYAnchor)])
        statusLabel = label
        retryButton = retry
        mainWindow.contentView = container
    }

    @objc func retryConnect() {
        guard startup == nil else { return }
        showLoading("正在连接本地工作台…")
        connect()
    }

    func connect() {
        guard let helper = Bundle.main.url(forResource: "desktop-session", withExtension: "py") else {
            connectionFailed("桌面组件不完整，请重新构建 Aide.app。")
            return
        }
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        process.arguments = ["python3", helper.path, "--root", root.path, "--start", "--private-stdio"]
        var environment = ProcessInfo.processInfo.environment
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        environment["PATH"] = [home + "/.docker/bin", "/opt/homebrew/bin", "/usr/local/bin",
                               "/usr/bin", "/bin", "/usr/sbin", "/sbin"].joined(separator: ":")
        environment["AIDE_OPEN_BROWSER"] = "0"
        environment["PYTHONIOENCODING"] = "utf-8"
        process.environment = environment
        process.currentDirectoryURL = root
        let output = Pipe(), errors = Pipe()
        process.standardOutput = output
        process.standardError = errors
        process.standardInput = FileHandle.nullDevice
        startup = process
        DispatchQueue.global(qos: .userInitiated).async {
            do {
                try process.run()
                let data = output.fileHandleForReading.readDataToEndOfFile()
                let diagnostic = errors.fileHandleForReading.readDataToEndOfFile()
                process.waitUntilExit()
                guard process.terminationStatus == 0 else {
                    // Python's error boundary emits a short redacted message, not subprocess output.
                    let message = String(data: diagnostic.prefix(1024), encoding: .utf8) ?? "本地服务连接失败。"
                    throw desktopError(message)
                }
                let session = try JSONDecoder().decode(DesktopSession.self, from: data)
                let origin = try session.validate()
                DispatchQueue.main.async {
                    self.startup = nil
                    self.session = session
                    self.origin = origin
                    self.startComputerControlHost()
                    let web = self.makeWebView(configuration: WKWebViewConfiguration(), window: self.mainWindow)
                    var url = URLComponents(url: origin, resolvingAgainstBaseURL: false)!
                    url.fragment = "token=" + session.token
                    web.load(URLRequest(url: url.url!))
                }
            } catch {
                DispatchQueue.main.async {
                    self.startup = nil
                    self.connectionFailed(error.localizedDescription)
                }
            }
        }
    }

    func connectionFailed(_ message: String) {
        statusLabel?.stringValue = String(message.prefix(500))
        retryButton?.isHidden = false
        if smokeOutput != nil { writeSmoke(["ok": false, "stage": "connect", "message": "Local startup failed"]) }
    }

    func makeWebView(configuration: WKWebViewConfiguration, window: NSWindow) -> WKWebView {
        configuration.preferences.javaScriptCanOpenWindowsAutomatically = true
        configuration.mediaTypesRequiringUserActionForPlayback = []
        // No generic native-command bridge is exposed to website JavaScript.
        let controller = configuration.userContentController
        let script = """
        if (window === top) {
          window.addEventListener('error', () => window.webkit.messageHandlers.aideDesktopDiagnostics.postMessage('runtime-error'));
          window.addEventListener('unhandledrejection', () => window.webkit.messageHandlers.aideDesktopDiagnostics.postMessage('promise-error'));
        } else { window.webkit.messageHandlers.aideDesktopDiagnostics.postMessage('plugin-frame-ready'); }
        """
        if configuredControllers.insert(ObjectIdentifier(controller)).inserted {
            controller.add(self, name: "aideDesktopDiagnostics")
            controller.addScriptMessageHandler(self, contentWorld: .page, name: "aideBiometrics")
            controller.addUserScript(WKUserScript(source: """
            if (window === top) {
              const request = (operation, options) => window.webkit.messageHandlers.aideBiometrics.postMessage({operation, options:options || {}}).then(result => {
                if (result.error) throw new DOMException(result.error.message, result.error.name);
                return result;
              });
              Object.defineProperty(window, 'AideNativeBiometrics', {value:Object.freeze({
                status:()=>request('status'), create:options=>request('create',options), get:options=>request('get',options)
              }), writable:false, configurable:false});
            }
            """, injectionTime: .atDocumentStart, forMainFrameOnly: true))
            controller.addUserScript(WKUserScript(source: script, injectionTime: .atDocumentStart,
                                                  forMainFrameOnly: smokeOutput == nil))
        }
        let web = WKWebView(frame: .zero, configuration: configuration)
        web.navigationDelegate = self
        web.uiDelegate = self
        web.allowsBackForwardNavigationGestures = true
        web.isInspectable = smokeOutput != nil
        window.contentView = web
        views[window] = web
        titleObservers[ObjectIdentifier(web)] = web.observe(\.title, options: [.new]) { [weak window] view, _ in
            window?.title = String((view.title?.isEmpty == false ? view.title! : "Aide").prefix(120))
        }
        window.makeFirstResponder(web)
        return web
    }

    var activeWeb: WKWebView? { views[NSApp.keyWindow ?? mainWindow] ?? views[mainWindow] }

    @objc func newTab() {
        guard let origin = origin else { return }
        let child = makeWindow()
        let web = makeWebView(configuration: WKWebViewConfiguration(), window: child)
        mainWindow.addTabbedWindow(child, ordered: .above)
        child.makeKeyAndOrderFront(nil)
        web.load(URLRequest(url: origin))
    }

    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                 for navigationAction: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        guard let origin = origin, let url = navigationAction.request.url else { return nil }
        if DesktopPolicy.local(url, origin: origin) || url.absoluteString == "about:blank" {
            let child = makeWindow()
            let web = makeWebView(configuration: configuration, window: child)
            (webView.window ?? mainWindow).addTabbedWindow(child, ordered: .above)
            child.makeKeyAndOrderFront(nil)
            return web
        }
        if navigationAction.navigationType == .linkActivated && DesktopPolicy.external(url) {
            NSWorkspace.shared.open(url)
        }
        return nil
    }

    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard let origin = origin, let url = navigationAction.request.url else {
            decisionHandler(.cancel); return
        }
        if navigationAction.shouldPerformDownload && url.absoluteString.hasPrefix("blob:null/")
            && DesktopPolicy.uiFrame(navigationAction.sourceFrame, expected: origin) {
            decisionHandler(.download)
        } else if DesktopPolicy.local(url, origin: origin) || url.absoluteString == "about:blank" {
            decisionHandler(navigationAction.shouldPerformDownload ? .download : .allow)
        } else {
            // Embedded remote subframes are ordinary web content with no local media grant.
            if navigationAction.targetFrame?.isMainFrame == false
                && (["http", "https"].contains(url.scheme ?? "")
                    || url.scheme == "data" && DesktopPolicy.media(navigationAction.sourceFrame.securityOrigin, expected: origin)) {
                decisionHandler(.allow)
            } else {
                if navigationAction.navigationType == .linkActivated && DesktopPolicy.external(url) {
                    NSWorkspace.shared.open(url)
                }
                decisionHandler(.cancel)
            }
        }
    }

    func webView(_ webView: WKWebView, decidePolicyFor navigationResponse: WKNavigationResponse,
                 decisionHandler: @escaping (WKNavigationResponsePolicy) -> Void) {
        decisionHandler(navigationResponse.canShowMIMEType ? .allow : .download)
    }

    func webView(_ webView: WKWebView, didReceive challenge: URLAuthenticationChallenge,
                 completionHandler: @escaping (URLSession.AuthChallengeDisposition, URLCredential?) -> Void) {
        guard let origin = origin, let session = session,
              challenge.protectionSpace.authenticationMethod == NSURLAuthenticationMethodServerTrust,
              challenge.protectionSpace.host == origin.host,
              challenge.protectionSpace.port == origin.port else {
            completionHandler(.performDefaultHandling, nil); return
        }
        guard let trust = challenge.protectionSpace.serverTrust,
              let chain = SecTrustCopyCertificateChain(trust) as? [SecCertificate],
              let certificate = chain.first else { completionHandler(.cancelAuthenticationChallenge, nil); return }
        let digest = SHA256.hash(data: SecCertificateCopyData(certificate) as Data).map { String(format: "%02x", $0) }.joined()
        guard digest == session.certificateSHA256 else { completionHandler(.cancelAuthenticationChallenge, nil); return }
        // Exact installation certificate pin, hostname and date validation; no global trust change.
        SecTrustSetAnchorCertificates(trust, [certificate] as CFArray)
        SecTrustSetAnchorCertificatesOnly(trust, true)
        SecTrustSetPolicies(trust, SecPolicyCreateSSL(true, "localhost" as CFString))
        if SecTrustEvaluateWithError(trust, nil) {
            completionHandler(.useCredential, URLCredential(trust: trust))
        } else { completionHandler(.cancelAuthenticationChallenge, nil) }
    }

    func webView(_ webView: WKWebView, requestMediaCapturePermissionFor securityOrigin: WKSecurityOrigin,
                 initiatedByFrame frame: WKFrameInfo, type: WKMediaCaptureType,
                 decisionHandler: @escaping (WKPermissionDecision) -> Void) {
        guard let origin = origin, DesktopPolicy.media(securityOrigin, expected: origin),
              DesktopPolicy.media(frame.securityOrigin, expected: origin) else {
            decisionHandler(.deny); return
        }
        decisionHandler(.prompt)
    }

    func webView(_ webView: WKWebView, runOpenPanelWith parameters: WKOpenPanelParameters,
                 initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping ([URL]?) -> Void) {
        guard let origin = origin, DesktopPolicy.uiFrame(frame, expected: origin) else {
            completionHandler(nil); return
        }
        let panel = NSOpenPanel()
        panel.canChooseDirectories = parameters.allowsDirectories
        panel.canChooseFiles = true
        panel.allowsMultipleSelection = parameters.allowsMultipleSelection
        panel.beginSheetModal(for: webView.window ?? mainWindow) { response in
            completionHandler(response == .OK ? panel.urls : nil)
        }
        if webView === smokeComponentWeb {
            smokeComponents["upload"] = true
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.15) { panel.cancel(nil) }
        }
    }

    func webView(_ webView: WKWebView, runJavaScriptAlertPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping () -> Void) {
        guard let origin = origin, DesktopPolicy.uiFrame(frame, expected: origin) else {
            completionHandler(); return
        }
        let alert = NSAlert()
        alert.messageText = "Aide"
        alert.informativeText = String(message.prefix(8000))
        alert.addButton(withTitle: "好")
        alert.beginSheetModal(for: webView.window ?? mainWindow) { _ in completionHandler() }
        if webView === smokeComponentWeb {
            smokeComponents["alert"] = true
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.15) { alert.buttons[0].performClick(nil) }
        }
    }

    func webView(_ webView: WKWebView, runJavaScriptConfirmPanelWithMessage message: String,
                 initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping (Bool) -> Void) {
        guard let origin = origin, DesktopPolicy.uiFrame(frame, expected: origin) else {
            completionHandler(false); return
        }
        let alert = NSAlert()
        alert.messageText = "Aide"
        alert.informativeText = String(message.prefix(8000))
        alert.addButton(withTitle: "确认"); alert.addButton(withTitle: "取消")
        alert.beginSheetModal(for: webView.window ?? mainWindow) { response in completionHandler(response == .alertFirstButtonReturn) }
        if webView === smokeComponentWeb {
            smokeComponents["confirm"] = true
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.15) { alert.buttons[1].performClick(nil) }
        }
    }

    func webView(_ webView: WKWebView, runJavaScriptTextInputPanelWithPrompt prompt: String,
                 defaultText: String?, initiatedByFrame frame: WKFrameInfo, completionHandler: @escaping (String?) -> Void) {
        guard let origin = origin, DesktopPolicy.uiFrame(frame, expected: origin) else {
            completionHandler(nil); return
        }
        let alert = NSAlert()
        alert.messageText = String(prompt.prefix(2000))
        let input = NSTextField(string: defaultText ?? "")
        input.frame = NSRect(x: 0, y: 0, width: 320, height: 24)
        alert.accessoryView = input
        alert.addButton(withTitle: "确认"); alert.addButton(withTitle: "取消")
        alert.beginSheetModal(for: webView.window ?? mainWindow) { response in
            completionHandler(response == .alertFirstButtonReturn ? input.stringValue : nil)
        }
        if webView === smokeComponentWeb {
            smokeComponents["prompt"] = true
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.15) { alert.buttons[1].performClick(nil) }
        }
    }

    func webView(_ webView: WKWebView, navigationAction: WKNavigationAction, didBecome download: WKDownload) {
        track(download)
    }
    func webView(_ webView: WKWebView, navigationResponse: WKNavigationResponse, didBecome download: WKDownload) {
        track(download)
    }
    func track(_ download: WKDownload) {
        downloads[ObjectIdentifier(download)] = download
        download.delegate = self
    }
    func download(_ download: WKDownload, decideDestinationUsing response: URLResponse,
                  suggestedFilename: String, completionHandler: @escaping (URL?) -> Void) {
        let panel = NSSavePanel()
        panel.nameFieldStringValue = URL(fileURLWithPath: suggestedFilename).lastPathComponent
        panel.directoryURL = FileManager.default.urls(for: .downloadsDirectory, in: .userDomainMask).first
        panel.beginSheetModal(for: download.webView?.window ?? mainWindow) { result in
            completionHandler(result == .OK ? panel.url : nil)
        }
        if download.webView === smokeComponentWeb {
            smokeComponents["download"] = true
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.15) { panel.cancel(nil) }
        }
    }
    func downloadDidFinish(_ download: WKDownload) { downloads.removeValue(forKey: ObjectIdentifier(download)) }
    func download(_ download: WKDownload, didFailWithError error: Error, resumeData: Data?) {
        downloads.removeValue(forKey: ObjectIdentifier(download))
    }

    func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
        if smokeOutput != nil, let origin = origin, let web = message.webView,
           message.body as? String == "plugin-frame-ready", let url = message.frameInfo.request.url,
           DesktopPolicy.pluginURL(url, origin: origin) {
            smokePluginFrames[ObjectIdentifier(web)] = message.frameInfo
            return
        }
        guard let origin = origin, message.frameInfo.isMainFrame,
              DesktopPolicy.media(message.frameInfo.securityOrigin, expected: origin) else { return }
        if let kind = message.body as? String, ["runtime-error", "promise-error"].contains(kind) { pageErrors += 1 }
    }

    func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage,
                               replyHandler: @escaping (Any?, String?) -> Void) {
        func reject(_ error: BiometricFailure, name: String = "NotAllowedError") {
            replyHandler(["error": ["name": name, "message": error.message]], nil)
        }
        guard message.name == "aideBiometrics", let origin = origin, message.frameInfo.isMainFrame,
              DesktopPolicy.media(message.frameInfo.securityOrigin, expected: origin),
              let web = message.webView, let source = web.url, DesktopPolicy.biometric(source, origin: origin),
              let body = message.body as? [String: Any], body.count <= 2,
              let encoded = try? JSONSerialization.data(withJSONObject: body), encoded.count <= 65536,
              let operation = body["operation"] as? String, ["status", "create", "get"].contains(operation),
              let options = body["options"] as? [String: Any], options.count <= 16 else {
            reject(.invalidOptions, name: "SecurityError"); return
        }
        if operation == "status" { replyHandler(biometrics.status(), nil); return }
        guard web.window?.isVisible == true, web === activeWeb else { reject(.cancelled); return }
        let port = origin.port ?? 443
        let ceremonyOrigin = "https://localhost" + (port == 443 ? "" : ":\(port)")
        biometrics.perform(operation: operation, options: options, origin: ceremonyOrigin) { result in
            guard web.url == source, web.window?.isVisible == true, web === self.activeWeb else { reject(.cancelled); return }
            switch result {
            case .success(let value): replyHandler(value, nil)
            case .failure(let error): reject(error, name: error == .cancelled ? "NotAllowedError" : "OperationError")
            }
        }
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        if smokeOutput != nil && webView == views[mainWindow] { smokeProbe(webView) }
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        if (error as NSError).code == NSURLErrorCancelled { return }
        if webView == views[mainWindow] && session != nil {
            showLoading("工作台连接失败，请确认本地服务仍在运行。")
            retryButton?.isHidden = false
        }
    }

    func smokeProbe(_ web: WKWebView) {
        smokeAttempts += 1
        if !smokeTabsChecked {
            smokeTabsChecked = true
            checkNativeTabs(web)
            return
        }
        let script = """
        const ready = typeof state !== 'undefined' && Boolean(state.config) && !document.getElementById('login-dialog')?.open;
        if (!ready) return {ready:false};
        const locked = !document.getElementById('lock-screen')?.hidden || window.LockCluster?.effectiveLocked?.() === true;
        if (locked) return {ready:false, needsUnlock:true};
        if (!document.getElementById('settings-sheet')?.classList.contains('open')) document.getElementById('brand-mini')?.click();
        await new Promise(r=>setTimeout(r,700));
        return {
          ready:true, title:document.title, motion:document.documentElement.dataset.motionLayer,
          settings:document.getElementById('settings-sheet')?.classList.contains('open'),
          width:innerWidth, scrollWidth:document.documentElement.scrollWidth,
          media:typeof navigator.mediaDevices?.getUserMedia === 'function',
          webSocket:typeof WebSocket === 'function', audio:typeof AudioContext === 'function',
          speechRecognition:typeof (window.SpeechRecognition||window.webkitSpeechRecognition) === 'function',
          webAuthn:typeof PublicKeyCredential === 'function',
          platformAuthenticator:await (window.PublicKeyCredential?.isUserVerifyingPlatformAuthenticatorAvailable?.().catch(()=>false)??false)
        };
        """
        web.callAsyncJavaScript(script, arguments: [:], in: nil, in: .page) { result in
            if case .success(let raw) = result, var record = raw as? [String: Any], record["ready"] as? Bool == true {
                if !self.smokePluginTabsChecked {
                    self.smokePluginTabsChecked = true
                    self.checkPluginPages(web) { self.smokeProbe(web) }
                    return
                }
                record.merge(self.smokeTabResults) { _, new in new }
                record["pageErrors"] = self.pageErrors
                record["microphoneAuthorization"] = AVCaptureDevice.authorizationStatus(for: .audio).rawValue
                record["ok"] = self.pageErrors == 0 && record["settings"] as? Bool == true && record["nativeTabs"] as? Bool == true && record["closeReopen"] as? Bool == true && record["nativePanels"] as? Bool == true && record["pluginPages"] as? Bool == true && record["sandboxPanels"] as? Bool == true
                record["nativeApp"] = Bundle.main.bundleIdentifier ?? ""
                self.writeSmoke(record)
                if let destination = self.smokeOutput?.deletingLastPathComponent().appendingPathComponent("native-workbench.png") {
                    web.takeSnapshot(with: nil) { image, _ in
                        if let data = image?.tiffRepresentation, let bitmap = NSBitmapImageRep(data: data),
                           let png = bitmap.representation(using: .png, properties: [:]) {
                            try? png.write(to: destination, options: .atomic)
                        }
                    }
                }
            } else if self.smokeAttempts < 300 {
                var record = (try? result.get()) as? [String: Any] ?? [:]
                record["ok"] = false
                record["stage"] = record["needsUnlock"] as? Bool == true ? "awaiting-user-unlock" : "page-ready"
                record.merge(self.smokeTabResults) { _, new in new }
                self.writeSmoke(record)
                DispatchQueue.main.asyncAfter(deadline: .now() + 2) { self.smokeProbe(web) }
            } else { self.writeSmoke(["ok": false, "stage": "page-ready", "pageErrors": self.pageErrors]) }
        }
    }

    // Exercise actual AppKit tabs and close/reopen without inspecting private page data.
    func checkNativeTabs(_ web: WKWebView) {
        web.evaluateJavaScript("window.open(location.origin + '/', '_blank');") { _, _ in
            DispatchQueue.main.asyncAfter(deadline: .now() + 3) {
                let children = self.views.keys.filter { $0 != self.mainWindow }
                self.smokeTabResults["nativeTabs"] = children.count == 1 && self.mainWindow.tabbedWindows?.count == 2
                if let child = children.first, let childWeb = self.views[child] {
                    self.smokeTabResults["sharedWebStore"] = childWeb.configuration.websiteDataStore === web.configuration.websiteDataStore
                    self.checkNativePanels(childWeb) {
                        child.performClose(nil)
                        self.finishTabCheck(web)
                    }
                } else { self.finishTabCheck(web) }
            }
        }
    }

    func checkPluginPages(_ web: WKWebView, completion: @escaping () -> Void) {
        let script = """
        if (!window.AidePluginWorkbenchTabs) return false;
        await loadPluginsPanel();
        const plugins=['compare','professional-project'].map(id=>state.plugins.find(p=>p.id===id&&p.enabled&&p.workbench?.protocol===1));
        if (plugins.some(p=>!p)) return false;
        for (const plugin of plugins) if (!await window.AidePluginWorkbench.open(plugin,pluginWorkbenchServices())) return false;
        return true;
        """
        web.callAsyncJavaScript(script, arguments: [:], in: nil, in: .page) { result in
            guard (try? result.get()) as? Bool == true else {
                self.smokeTabResults["pluginPages"] = false; completion(); return
            }
            self.waitForPluginPages(attempt: 0, completion: completion)
        }
    }

    func waitForPluginPages(attempt: Int, completion: @escaping () -> Void) {
        let children = views.keys.filter { $0 != mainWindow }
        let group = DispatchGroup()
        var ready = 0
        for child in children {
            guard let web = views[child] else { continue }
            group.enter()
            web.evaluateJavaScript("Boolean(document.querySelector('.plugin-workbench-page iframe')?.getAttribute('src')?.startsWith('/plugin-workbench/'))") { value, _ in
                if value as? Bool == true {
                    ready += 1
                    if let output = self.smokeOutput {
                        let plugin = URLComponents(url: web.url ?? self.origin!, resolvingAgainstBaseURL: false)?.fragment
                            .flatMap { URLComponents(string: "?" + $0)?.queryItems?.first(where: { $0.name == "pluginWorkbench" })?.value }
                        if ["compare", "professional-project"].contains(plugin ?? "") {
                            web.takeSnapshot(with: nil) { image, _ in
                                if let data = image?.tiffRepresentation, let bitmap = NSBitmapImageRep(data: data),
                                   let png = bitmap.representation(using: .png, properties: [:]) {
                                    try? png.write(to: output.deletingLastPathComponent().appendingPathComponent("native-plugin-" + plugin! + ".png"), options: .atomic)
                                }
                            }
                        }
                    }
                }
                group.leave()
            }
        }
        group.notify(queue: .main) {
            if ready == 2 || attempt >= 20 {
                self.smokeTabResults["pluginPages"] = ready == 2 && self.mainWindow.tabbedWindows?.count == 3
                let finish = {
                    for child in children { child.performClose(nil) }
                    self.reopen()
                    completion()
                }
                if let child = children.first, let childWeb = self.views[child],
                   let frame = self.smokePluginFrames[ObjectIdentifier(childWeb)] {
                    self.checkNativePanels(childWeb, frame: frame) {
                        self.smokeTabResults["sandboxPanels"] = self.smokeTabResults["nativePanels"]
                        finish()
                    }
                } else { self.smokeTabResults["sandboxPanels"] = false; finish() }
            } else {
                DispatchQueue.main.asyncAfter(deadline: .now() + 1) { self.waitForPluginPages(attempt: attempt + 1, completion: completion) }
            }
        }
    }

    func finishTabCheck(_ web: WKWebView) {
        mainWindow.performClose(nil)
        let hidden = !mainWindow.isVisible
        reopen()
        smokeTabResults["closeReopen"] = hidden && mainWindow.isVisible && views[mainWindow] === web
        smokeProbe(web)
    }

    func checkNativePanels(_ web: WKWebView, frame: WKFrameInfo? = nil, completion: @escaping () -> Void) {
        smokeComponentWeb = web
        smokeComponents = [:]
        // Controlled local UI fixtures only; no files selected, uploaded or saved.
        let script = """
        alert('Aide native dialog check');
        const cancelled = !confirm('Aide native cancel check') && prompt('Aide native prompt check') === null;
        const input = document.createElement('input'); input.type = 'file'; document.body.append(input); input.click();
        await new Promise(r=>setTimeout(r,900)); input.remove();
        const url = URL.createObjectURL(new Blob(['Aide native download check'], {type:'text/plain'}));
        const link = document.createElement('a'); link.href=url; link.download='aide-desktop-check.txt';
        document.body.append(link); link.click();
        await new Promise(r=>setTimeout(r,1500)); link.remove(); URL.revokeObjectURL(url);
        return cancelled;
        """
        web.callAsyncJavaScript(script, arguments: [:], in: frame, in: .page) { result in
            self.smokeTabResults["nativePanels"] = (try? result.get()) as? Bool == true
                && ["alert", "confirm", "prompt", "upload", "download"].allSatisfy { self.smokeComponents[$0] == true }
            self.smokeComponentWeb = nil
            completion()
        }
    }

    func writeSmoke(_ record: [String: Any]) {
        guard let output = smokeOutput else { return }
        var record = record
        let biometricStatus = biometrics.status()
        record["nativeTouchIdAvailable"] = biometricStatus["available"]
        record["nativeTouchIdCredentialCount"] = (biometricStatus["credentialIds"] as? [String])?.count ?? 0
        guard let data = try? JSONSerialization.data(withJSONObject: record, options: [.prettyPrinted]) else { return }
        try? FileManager.default.createDirectory(at: output.deletingLastPathComponent(), withIntermediateDirectories: true)
        try? data.write(to: output, options: .atomic)
    }

    func stopMedia(_ web: WKWebView) {
        biometrics.cancel()
        web.evaluateJavaScript("window.aideAssistantCall?.end?.();window.aideAssistantLive?.stop?.();if(typeof voiceClose==='function')voiceClose();if(typeof ttsCancel==='function')ttsCancel();", completionHandler: nil)
    }
    func webViewDidClose(_ webView: WKWebView) { webView.window?.performClose(nil) }

    func windowShouldClose(_ sender: NSWindow) -> Bool {
        if sender == mainWindow {
            if let web = views[sender] { stopMedia(web) }
            sender.orderOut(nil)
            return false
        }
        let identity = ObjectIdentifier(sender)
        if approvedCloses.remove(identity) != nil {
            if let web = views[sender] {
                stopMedia(web)
                titleObservers.removeValue(forKey: ObjectIdentifier(web))
            }
            views.removeValue(forKey: sender)
            return true
        }
        guard let web = views[sender] else { return true }
        web.evaluateJavaScript("window.AidePluginWorkbench?.isDirty?.() === true") { value, error in
            let finish = {
                self.approvedCloses.insert(identity)
                sender.performClose(nil)
            }
            if value as? Bool == true || error != nil {
                let alert = NSAlert()
                alert.messageText = "关闭这个标签页？"
                alert.informativeText = "插件可能有未保存的修改。"
                alert.addButton(withTitle: "取消"); alert.addButton(withTitle: "关闭标签页")
                alert.beginSheetModal(for: sender) { response in
                    if response == .alertSecondButtonReturn { finish() }
                }
            } else { finish() }
        }
        return false
    }

    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        let group = DispatchGroup()
        var dirty = false
        for web in views.values {
            group.enter()
            web.evaluateJavaScript("window.AidePluginWorkbench?.isDirty?.() === true") { value, _ in
                if value as? Bool == true { dirty = true }
                group.leave()
            }
        }
        group.notify(queue: .main) {
            guard dirty else { sender.reply(toApplicationShouldTerminate: true); return }
            let alert = NSAlert()
            alert.messageText = "退出 Aide？"
            alert.informativeText = "插件中有未保存的修改，退出后无法恢复。"
            alert.addButton(withTitle: "取消"); alert.addButton(withTitle: "退出")
            alert.beginSheetModal(for: self.mainWindow) { response in
                sender.reply(toApplicationShouldTerminate: response == .alertSecondButtonReturn)
            }
        }
        return .terminateLater
    }
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if startup == nil && (session == nil || views[mainWindow]?.url?.scheme == "about") {
            showLoading("正在重新连接本地工作台…")
            connect()
        }
        reopen(); return true
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }
    func applicationWillTerminate(_ notification: Notification) {
        computerControlHost?.stop()
        for web in views.values { stopMedia(web) }
        startup?.terminate()
    }

    @objc func about() { NSApp.orderFrontStandardAboutPanel(nil) }
    @objc func settings() {
        activeWeb?.evaluateJavaScript("""
        if (document.getElementById('lock-screen')?.hidden && !document.getElementById('login-dialog')?.open
            && window.LockCluster?.effectiveLocked?.() !== true) document.getElementById('brand-mini')?.click();
        else document.getElementById('lock-password')?.focus();
        """, completionHandler: nil)
    }
    @objc func hideApp() { for web in views.values { stopMedia(web) }; NSApp.hide(nil) }
    @objc func reopen() { mainWindow.makeKeyAndOrderFront(nil); NSApp.activate(ignoringOtherApps: true) }
    @objc func closeWindow() { (NSApp.keyWindow ?? mainWindow).performClose(nil) }
    @objc func quit() { NSApp.terminate(nil) }
    @objc func reload() { activeWeb?.reload() }
    @objc func zoomIn() { if let web = activeWeb { web.pageZoom = min(2, web.pageZoom + 0.1) } }
    @objc func zoomOut() { if let web = activeWeb { web.pageZoom = max(0.5, web.pageZoom - 0.1) } }
    @objc func actualSize() { activeWeb?.pageZoom = 1 }
    @objc func openBrowser() { if let origin = origin { NSWorkspace.shared.open(origin) } }
    @objc func printPage() { activeWeb?.printOperation(with: .shared).run() }

    @objc func stopAndQuit() {
        let alert = NSAlert()
        alert.messageText = "停止 Aide 本地服务？"
        alert.informativeText = "正在执行的任务和通话将中断，数据会保留。"
        alert.addButton(withTitle: "取消"); alert.addButton(withTitle: "停止并退出")
        alert.beginSheetModal(for: mainWindow) { result in
            guard result == .alertSecondButtonReturn else { return }
            let process = Process()
            process.executableURL = URL(fileURLWithPath: "/bin/bash")
            process.arguments = [self.root.appendingPathComponent("scripts/aide-maintenance.sh").path, "stop"]
            process.currentDirectoryURL = self.root
            process.environment = ProcessInfo.processInfo.environment.merging(
                ["PATH": FileManager.default.homeDirectoryForCurrentUser.path + "/.docker/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"]) { _, new in new }
            process.standardOutput = FileHandle.nullDevice
            process.standardError = FileHandle.nullDevice
            process.terminationHandler = { finished in
                DispatchQueue.main.async {
                    if finished.terminationStatus == 0 { NSApp.terminate(nil) }
                    else {
                        let warning = NSAlert()
                        warning.messageText = "部分本地组件未确认停止"
                        warning.informativeText = "请检查本地服务日志；数据已保留。"
                        warning.runModal()
                    }
                }
            }
            do { try process.run() } catch { NSSound.beep() }
        }
    }
}

func policyTests() {
    let origin = URL(string: "https://localhost:9999")!
    for value in ["https://localhost:9999/", "https://localhost:9999/api/files", "blob:https://localhost:9999/owned"] {
        precondition(DesktopPolicy.local(URL(string: value)!, origin: origin), value)
    }
    for value in ["https://localhost:9998/", "http://localhost:9999/", "https://localhost.evil.test:9999/",
                  "https://user:pass@localhost:9999/", "file:///etc/passwd", "javascript:alert(1)",
                  "blob:https://elsewhere.test/owned"] {
        precondition(!DesktopPolicy.local(URL(string: value)!, origin: origin), value)
    }
    precondition(DesktopPolicy.external(URL(string: "https://example.com")!))
    precondition(!DesktopPolicy.external(URL(string: "file:///etc/passwd")!))
    precondition(!DesktopPolicy.external(URL(string: "ssh://host")!))
    precondition(DesktopPolicy.pluginURL(URL(string: "https://localhost:9999/plugin-workbench/" + String(repeating: "a", count: 64))!, origin: origin))
    for path in ["/api/config", "/plugin-workbench/short", "/plugin-workbench/" + String(repeating: "a", count: 64) + "/extra"] {
        precondition(!DesktopPolicy.pluginURL(URL(string: "https://localhost:9999" + path)!, origin: origin))
    }
    precondition(DesktopPolicy.biometric(URL(string: "https://localhost:9999/#token=fixture")!, origin: origin))
    for value in ["https://localhost:9999/#pluginWorkbench=compare&workspace=fixture",
                  "https://localhost:9999/plugin-workbench/" + String(repeating: "a", count: 64),
                  "https://localhost:9999/?external=1", "https://localhost:9998/", "blob:https://localhost:9999/owned"] {
        precondition(!DesktopPolicy.biometric(URL(string: value)!, origin: origin), value)
    }
    print("PASS desktop origin, blob, external-scheme policy")
}

let arguments = CommandLine.arguments
if arguments.contains("--policy-test") { policyTests(); exit(0) }
if arguments.contains("--prepare-update") {
    guard let identifier = Bundle.main.bundleIdentifier else { exit(2) }
    let others = NSRunningApplication.runningApplications(withBundleIdentifier: identifier).filter { $0.processIdentifier != getpid() }
    for app in others { _ = app.terminate() }
    let deadline = Date().addingTimeInterval(20)
    while others.contains(where: { !$0.isTerminated }) && Date() < deadline {
        RunLoop.current.run(until: Date().addingTimeInterval(0.1))
    }
    if others.contains(where: { !$0.isTerminated }) {
        fputs("Finish or cancel unsaved plugin edits, quit Aide, then retry the desktop update.\n", stderr)
        exit(1)
    }
    exit(0)
}

func argument(_ name: String) -> String? {
    guard let index = arguments.firstIndex(of: name), index + 1 < arguments.count else { return nil }
    return arguments[index + 1]
}
let boundRoot = Bundle.main.object(forInfoDictionaryKey: "AideProjectRoot") as? String
let rootPath = argument("--root") ?? boundRoot
if let rootPath = rootPath, let boundRoot = boundRoot,
   URL(fileURLWithPath: rootPath).resolvingSymlinksInPath() != URL(fileURLWithPath: boundRoot).resolvingSymlinksInPath() {
    fputs("This Aide.app belongs to another installation; rebuild desktop.command in the intended directory.\n", stderr); exit(2)
}
guard let rootPath = rootPath, rootPath.hasPrefix("/") else {
    fputs("Aide installation directory is missing; rebuild with desktop.command.\n", stderr); exit(2)
}
let output = argument("--smoke-output").map { URL(fileURLWithPath: $0) }
let application = NSApplication.shared
let delegate = DesktopApp(root: URL(fileURLWithPath: rootPath), smokeOutput: output)
application.delegate = delegate
application.run()
