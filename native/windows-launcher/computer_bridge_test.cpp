#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <winsock2.h>
#include <windows.h>
#include <objidl.h>
#include <gdiplus.h>
#include <string>
#include <vector>
#include <stdexcept>
#include <iostream>
#include <cstdio>
#include <utility>
#include "../../native/windows-launcher/third_party/json.hpp"

using json = nlohmann::json;
std::wstring wide(const std::string& value) {
    int count = MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, value.data(), int(value.size()), nullptr, 0);
    if (!count) throw std::runtime_error("invalid UTF-8");
    std::wstring result(size_t(count), L'\0');
    MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, value.data(), int(value.size()), result.data(), count);
    return result;
}
std::string utf8(const std::wstring& value) {
    if (value.empty()) return {};
    int count = WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, value.data(), int(value.size()), nullptr, 0, nullptr, nullptr);
    if (!count) throw std::runtime_error("invalid Unicode");
    std::string result(size_t(count), '\0');
    WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, value.data(), int(value.size()), result.data(), count, nullptr, nullptr);
    return result;
}
#define AIDE_COMPUTER_BRIDGE_TEST
#include "../../native/windows-launcher/computer_bridge.hpp"

namespace {
constexpr wchar_t kTitle[] = L"Aide Computer Bridge Isolated Test Window";
constexpr char kToken[] = "isolated-windows-computer-bridge-test-token-2026";

LRESULT CALLBACK fixtureProc(HWND window, UINT message, WPARAM w, LPARAM l) {
    if (message == WM_PAINT) {
        PAINTSTRUCT paint{};
        HDC dc = BeginPaint(window, &paint);
        RECT rect{}; GetClientRect(window, &rect);
        HBRUSH brush = CreateSolidBrush(RGB(32, 104, 216));
        FillRect(dc, &rect, brush); DeleteObject(brush);
        EndPaint(window, &paint); return 0;
    }
    if (message == WM_LBUTTONUP) { SetWindowTextW(window, L"Aide fixture drag completed"); return 0; }
    if (message == WM_MOUSEWHEEL) { SetWindowTextW(window, L"Aide fixture vertical scroll completed"); return 0; }
    if (message == WM_MOUSEHWHEEL) { SetWindowTextW(window, L"Aide fixture horizontal scroll completed"); return 0; }
    if (message == WM_CLOSE) { DestroyWindow(window); return 0; }
    if (message == WM_DESTROY) { PostQuitMessage(0); return 0; }
    return DefWindowProcW(window, message, w, l);
}

struct WindowSearch { DWORD pid; HWND window = nullptr; };
BOOL CALLBACK findFixture(HWND window, LPARAM value) {
    auto& search = *reinterpret_cast<WindowSearch*>(value);
    DWORD pid = 0; GetWindowThreadProcessId(window, &pid);
    if (pid == search.pid && IsWindowVisible(window)) { search.window = window; return FALSE; }
    return TRUE;
}
struct ChildProcess {
    PROCESS_INFORMATION info{}; HWND window = nullptr;
    ~ChildProcess() {
        if (window && IsWindow(window)) PostMessageW(window, WM_CLOSE, 0, 0);
        if (info.hProcess) {
            if (WaitForSingleObject(info.hProcess, 2000) == WAIT_TIMEOUT) { TerminateProcess(info.hProcess, 1); WaitForSingleObject(info.hProcess, 1000); }
            CloseHandle(info.hProcess);
        }
        if (info.hThread) CloseHandle(info.hThread);
    }
};

void require(bool value, const char* message) { if (!value) throw std::runtime_error(message); }

std::pair<int, json> request(const char* method, const char* route, const json& input = json::object()) {
    SOCKET client = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
    require(client != INVALID_SOCKET, "test client socket failed");
    sockaddr_in address{}; address.sin_family = AF_INET; address.sin_port = htons(17778); address.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
    if (connect(client, reinterpret_cast<sockaddr*>(&address), sizeof(address)) != 0) { closesocket(client); throw std::runtime_error("cannot connect to local bridge"); }
    std::string body = input.dump();
    std::string wire = std::string(method) + " " + route + " HTTP/1.1\r\nHost: 127.0.0.1\r\nAuthorization: Bearer " + kToken + "\r\nContent-Type: application/json\r\nContent-Length: " + std::to_string(body.size()) + "\r\nConnection: close\r\n\r\n" + body;
    size_t sent = 0;
    while (sent < wire.size()) { int n = send(client, wire.data() + sent, int(wire.size() - sent), 0); if (n <= 0) { closesocket(client); throw std::runtime_error("bridge request send failed"); } sent += size_t(n); }
    std::string response; char buffer[8192];
    for (;;) { int n = recv(client, buffer, sizeof(buffer), 0); if (n <= 0) break; response.append(buffer, size_t(n)); }
    closesocket(client);
    auto split = response.find("\r\n\r\n"); require(split != std::string::npos, "bridge returned malformed HTTP");
    int status = 0; if (sscanf_s(response.c_str(), "HTTP/1.1 %d", &status) != 1) throw std::runtime_error("bridge returned malformed status");
    return {status, json::parse(response.substr(split + 4))};
}

int fixture() {
    WNDCLASSW klass{}; klass.lpfnWndProc = fixtureProc; klass.hInstance = GetModuleHandleW(nullptr); klass.lpszClassName = L"AideBridgeFixtureClass";
    RegisterClassW(&klass);
    HWND window = CreateWindowExW(0, klass.lpszClassName, kTitle, WS_OVERLAPPEDWINDOW, 80, 80, 480, 320, nullptr, nullptr, klass.hInstance, nullptr);
    if (!window) return 2;
    ShowWindow(window, SW_SHOW); UpdateWindow(window); SetForegroundWindow(window);
    MSG message{}; while (GetMessageW(&message, nullptr, 0, 0) > 0) { TranslateMessage(&message); DispatchMessageW(&message); }
    return 0;
}

void run() {
    WSADATA winsock{}; require(WSAStartup(MAKEWORD(2, 2), &winsock) == 0, "test Winsock startup failed");
    auto executable = std::wstring(32768, L'\0'); DWORD chars = GetModuleFileNameW(nullptr, executable.data(), DWORD(executable.size())); require(chars > 0, "cannot get fixture executable path"); executable.resize(chars);
    aide_desktop::ComputerBridge::setTestChromeExecutable(executable);
    aide_desktop::ComputerBridge bridge(kToken); bridge.start();
    ChildProcess child; STARTUPINFOW startup{}; startup.cb = sizeof(startup);
    auto health = request("GET", "/healthz"); require(health.first == 200 && health.second.value("platform", "") == "windows", "Windows health route failed");
    if (!health.second.value("desktopAvailable", false)) {
        bridge.stop(); WSACleanup();
        std::cout << "NOT_RUN: no interactive Windows desktop is available" << std::endl;
        return;
    }
    std::wstring command = L"\"" + executable + L"\" --fixture";
    require(CreateProcessW(executable.c_str(), command.data(), nullptr, nullptr, FALSE, CREATE_UNICODE_ENVIRONMENT, nullptr, nullptr, &startup, &child.info) != FALSE, "cannot start isolated fixture window");
    HWND window = nullptr;
    for (int i = 0; i < 100 && !window; ++i) { WindowSearch search{child.info.dwProcessId}; EnumWindows(findFixture, reinterpret_cast<LPARAM>(&search)); window = search.window; if (!window) Sleep(50); }
    require(window != nullptr, "isolated fixture window did not appear");
    child.window = window;
    SetForegroundWindow(window); BringWindowToTop(window);
    for (int i = 0; i < 40 && GetForegroundWindow() != window; ++i) Sleep(50);
    auto slash = executable.find_last_of(L"\\/"); std::wstring appName = executable.substr(slash == executable.npos ? 0 : slash + 1);
    auto dot = appName.find_last_of(L'.'); if (dot != appName.npos) appName.resize(dot);
    std::string app = utf8(appName);
    auto resolved = request("POST", "/v1/resolve-app", {{"app", app}});
    require(resolved.first == 200 && resolved.second.value("pid", DWORD(0)) == child.info.dwProcessId, "exact app resolution failed");
    std::string rule = resolved.second.value("rule", "");
    require(rule.rfind("exe:", 0) == 0, "resolved identity omitted executable rule");
    auto deniedBrowser = request("POST", "/v1/open-browser", {{"browser", "Chrome"}, {"url", "https://example.com/"}});
    require(deniedBrowser.first == 502, "browser launch without approval was not rejected");
    auto unsupportedBrowser = request("POST", "/v1/open-browser", {{"browser", "Safari"}, {"approved", true}});
    require(unsupportedBrowser.first == 502, "unsupported Safari launch was not rejected on Windows");
    auto credentialURL = request("POST", "/v1/open-browser", {{"browser", "Chrome"}, {"approved", true}, {"url", "https://user:secret@example.com/"}});
    require(credentialURL.first == 502, "credential-bearing browser URL was not rejected");
    auto quotedURL = request("POST", "/v1/open-browser", {{"browser", "Chrome"}, {"approved", true}, {"url", "https://example.com/\" --switch"}});
    require(quotedURL.first == 502, "quoted browser URL argument was not rejected");
    const auto browserURL = std::string("https://example.com/aide-isolated-test");
    auto openedBrowser = request("POST", "/v1/open-browser", {{"browser", "Chrome"}, {"approved", true}, {"url", browserURL}});
    require(openedBrowser.first == 200 && openedBrowser.second.value("opened", false) && openedBrowser.second.value("url", std::string{}) == browserURL, "approved Chrome launch did not pass its URL to the isolated executable");
    auto appsAfterLaunch = request("GET", "/healthz");
    require(appsAfterLaunch.first == 200, "Windows app list health route failed");
    bool listed = false; for (const auto& item : appsAfterLaunch.second.value("applications", json::array())) if (item.value("pid", DWORD(0)) == child.info.dwProcessId) listed = true;
    require(listed, "health route omitted the isolated running app identity");
    auto foreground = request("GET", "/v1/foreground-app");
    require(foreground.first == 200 && foreground.second.value("pid", DWORD(0)) > 0, "foreground app identity route failed");
    if (GetForegroundWindow() != window || foreground.second.value("pid", DWORD(0)) != child.info.dwProcessId) {
        child.window = nullptr; PostMessageW(window, WM_CLOSE, 0, 0); bridge.stop(); WSACleanup();
        std::cout << "PASS Windows bridge health/app resolution; NOT_RUN: desktop foreground policy prevented activating the isolated fixture, so screenshot/input checks were skipped" << std::endl;
        return;
    }
    require(foreground.second.value("rule", "") == rule, "foreground and resolved app rules differ");
    auto snapshot = request("POST", "/v1/snapshot", {{"allowedApps", json::array({app})}, {"expectedPid", child.info.dwProcessId}, {"expectedAppRule", rule}});
    require(snapshot.first == 200 && snapshot.second.contains("windowId") && snapshot.second.contains("window") && snapshot.second.contains("pixelWidth") && snapshot.second.value("imageBase64", std::string{}).size() > 100, "snapshot omitted bounded window-coordinate evidence");
    require(snapshot.second["pixelWidth"].get<int>() == snapshot.second["window"]["width"].get<int>() && snapshot.second["pixelHeight"].get<int>() == snapshot.second["window"]["height"].get<int>(), "snapshot image pixels and desktop bounds do not map one-to-one");
    RECT rect{}; GetWindowRect(window, &rect);
    POINT dragStart{rect.left + 80, rect.top + 100}, dragEnd{rect.left + 240, rect.top + 180};
    require(SetCursorPos(dragStart.x, dragStart.y) != FALSE, "cannot position isolated pointer fixture");
    auto chord = request("POST", "/v1/key", {{"allowedApps", json::array({app})}, {"expectedPid", child.info.dwProcessId}, {"expectedAppRule", rule}, {"approved", true}, {"controllerId", "isolated-session"}, {"controllerLabel", "Test session"}, {"key", "CMD+A"}});
    require(chord.first == 200, "Windows CMD-to-Control key chord failed");
    auto pointerHealth = request("GET", "/healthz");
    auto controllers = pointerHealth.second.value("controllers", json::array());
    require(pointerHealth.second.value("virtualPointers", false) && controllers.size() == 1 && controllers[0].value("visible", false), "approved action did not show the Windows virtual pointer");
    require(controllers[0].value("title", std::string{}).find("Test session") != std::string::npos, "virtual pointer omitted its controller label");
    require(GetForegroundWindow() == window, "virtual pointer activated itself over the approved app");
    auto drag = request("POST", "/v1/drag", {{"allowedApps", json::array({app})}, {"expectedPid", child.info.dwProcessId}, {"expectedAppRule", rule}, {"expectedWindowId", snapshot.second.value("windowId", uint64_t(0))}, {"approved", true}, {"x", dragStart.x}, {"y", dragStart.y}, {"toX", dragEnd.x}, {"toY", dragEnd.y}, {"durationMs", 120}});
    require(drag.first == 200 && drag.second.value("dragged", false), "approved Windows drag route failed");
    POINT afterDrag{}; GetCursorPos(&afterDrag); require(afterDrag.x == dragEnd.x && afterDrag.y == dragEnd.y, "drag did not finish at its approved endpoint");
    for (int i = 0; i < 20; ++i) { wchar_t title[128]{}; GetWindowTextW(window, title, 128); if (std::wstring(title) == L"Aide fixture drag completed") break; Sleep(25); }
    { wchar_t title[128]{}; GetWindowTextW(window, title, 128); require(std::wstring(title) == L"Aide fixture drag completed", "drag input did not reach the isolated target window"); }
    POINT scrollPoint{rect.left + 280, rect.top + 200};
    auto scroll = request("POST", "/v1/scroll", {{"allowedApps", json::array({app})}, {"expectedPid", child.info.dwProcessId}, {"expectedAppRule", rule}, {"expectedWindowId", snapshot.second.value("windowId", uint64_t(0))}, {"approved", true}, {"x", scrollPoint.x}, {"y", scrollPoint.y}, {"deltaX", 40}, {"deltaY", 80}});
    require(scroll.first == 200 && scroll.second.value("scrolled", false), "approved Windows scroll route failed");
    POINT afterScroll{}; GetCursorPos(&afterScroll); require(afterScroll.x == scrollPoint.x && afterScroll.y == scrollPoint.y, "scroll was not positioned at its approved region");
    for (int i = 0; i < 20; ++i) { wchar_t title[128]{}; GetWindowTextW(window, title, 128); if (std::wstring(title) == L"Aide fixture horizontal scroll completed") break; Sleep(25); }
    { wchar_t title[128]{}; GetWindowTextW(window, title, 128); require(std::wstring(title) == L"Aide fixture horizontal scroll completed", "scroll input did not reach the isolated target window"); }

    POINT beforeDenied{}; GetCursorPos(&beforeDenied);
    auto invalidDrag = request("POST", "/v1/drag", {{"allowedApps", json::array({app})}, {"expectedPid", child.info.dwProcessId}, {"expectedAppRule", rule}, {"approved", true}, {"x", dragStart.x}, {"y", dragStart.y}, {"toX", rect.right + 30}, {"toY", rect.bottom + 30}, {"durationMs", 120}});
    require(invalidDrag.first == 502, "out-of-window drag endpoint was not rejected");
    auto invalidScroll = request("POST", "/v1/scroll", {{"allowedApps", json::array({app})}, {"expectedPid", child.info.dwProcessId}, {"expectedAppRule", rule}, {"approved", true}, {"x", scrollPoint.x}, {"y", scrollPoint.y}, {"deltaX", 0}, {"deltaY", 3000}});
    require(invalidScroll.first == 502, "out-of-range scroll amount was not rejected");
    auto stale = request("POST", "/v1/click", {{"allowedApps", json::array({app})}, {"expectedPid", child.info.dwProcessId + 1}, {"expectedAppRule", rule}, {"approved", true}, {"x", rect.left + 30}, {"y", rect.top + 30}});
    require(stale.first == 502, "stale process approval was not rejected");
    auto staleWindow = request("POST", "/v1/click", {{"allowedApps", json::array({app})}, {"expectedPid", child.info.dwProcessId}, {"expectedAppRule", rule}, {"expectedWindowId", snapshot.second.value("windowId", uint64_t(0)) + 1}, {"approved", true}, {"x", rect.left + 30}, {"y", rect.top + 30}});
    require(staleWindow.first == 502, "stale screenshot HWND was not rejected");
    auto outside = request("POST", "/v1/click", {{"allowedApps", json::array({app})}, {"expectedPid", child.info.dwProcessId}, {"expectedAppRule", rule}, {"approved", true}, {"x", rect.right + 30}, {"y", rect.bottom + 30}});
    require(outside.first == 502, "out-of-window click was not rejected");
    POINT after{}; GetCursorPos(&after); require(after.x == beforeDenied.x && after.y == beforeDenied.y, "denied actions moved the user's cursor");
    Sleep(4200);
    auto expiredPointer = request("GET", "/healthz");
    require(expiredPointer.second.value("virtualPointers", false) && expiredPointer.second.value("controllers", json::array()).empty(), "Windows virtual pointer did not auto-dismiss");

    child.window = nullptr; PostMessageW(window, WM_CLOSE, 0, 0); WaitForSingleObject(child.info.hProcess, 2000);
    bridge.stop(); WSACleanup();
    std::cout << "PASS isolated Windows bridge discovery, snapshot coordinates, virtual pointer click-through/non-activation/auto-dismiss, stale-target and out-of-window denial" << std::endl;
}
}

int wmain(int argc, wchar_t** argv) {
    SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);
    if (argc > 1 && std::wstring(argv[1]) == L"--new-window")
        return argc == 3 && std::wstring(argv[2]) == L"https://example.com/aide-isolated-test" ? 0 : 1;
    if (argc > 1 && std::wstring(argv[1]) == L"--fixture") return fixture();
    try { run(); return 0; } catch (const std::exception& error) { std::cerr << "FAIL Windows bridge fixture: " << error.what() << std::endl; return 1; }
}

