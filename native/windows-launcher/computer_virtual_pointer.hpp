#pragma once

#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <windows.h>
#include <algorithm>
#include <atomic>
#include <chrono>
#include <condition_variable>
#include <mutex>
#include <string>
#include <thread>
#include <utility>

namespace aide_desktop {

// A display-only feedback window. It is layered, click-through, non-activating,
// DPI-aware, and owned by a small message thread so bridge workers never own UI.
class ComputerVirtualPointer {
public:
    struct Status {
        bool available = false;
        bool visible = false;
        std::wstring controllerId;
        std::wstring title;
    };

private:
    static constexpr UINT showMessage = WM_APP + 0x361;
    static constexpr UINT hideTimer = 0x361;
    static constexpr wchar_t className[] = L"AideComputerVirtualPointer";
    static constexpr COLORREF transparentColor = RGB(255, 0, 255);

    mutable std::mutex stateMutex_;
    std::mutex readyMutex_;
    std::condition_variable readyCondition_;
    std::thread uiThread_;
    HWND window_ = nullptr;
    DWORD threadId_ = 0;
    bool ready_ = false;
    Status status_;
    POINT point_{};
    COLORREF tint_ = RGB(20, 150, 155);
    UINT dpi_ = 96;

    static std::wstring clean(std::wstring value, size_t limit) {
        value.erase(std::remove_if(value.begin(), value.end(), [](wchar_t c) {
            return c < 0x20 || c == 0x7f;
        }), value.end());
        if (value.size() > limit) value.resize(limit);
        return value;
    }

    static UINT scale(int value, UINT dpi) { return UINT(MulDiv(value, int(dpi), 96)); }

    static UINT monitorDpi(HMONITOR monitor) {
        using QueryDpi = HRESULT(WINAPI*)(HMONITOR, int, UINT*, UINT*);
        HMODULE shcore = LoadLibraryW(L"shcore.dll");
        if (shcore) {
            auto query = reinterpret_cast<QueryDpi>(GetProcAddress(shcore, "GetDpiForMonitor"));
            UINT x = 96, y = 96;
            HRESULT result = query ? query(monitor, 0, &x, &y) : E_FAIL;
            FreeLibrary(shcore);
            if (SUCCEEDED(result) && x >= 72 && x <= 768) return x;
        }
        return 96;
    }

    static bool highContrast() {
        HIGHCONTRASTW value{};
        value.cbSize = sizeof(value);
        return SystemParametersInfoW(SPI_GETHIGHCONTRAST, sizeof(value), &value, 0) &&
            (value.dwFlags & HCF_HIGHCONTRASTON) != 0;
    }

    static bool darkApps() {
        DWORD light = 1, bytes = sizeof(light);
        return RegGetValueW(HKEY_CURRENT_USER,
            L"Software\\Microsoft\\Windows\\CurrentVersion\\Themes\\Personalize",
            L"AppsUseLightTheme", RRF_RT_REG_DWORD, nullptr, &light, &bytes) == ERROR_SUCCESS && light == 0;
    }

    static COLORREF colorFor(const std::wstring& id) {
        static const COLORREF colors[] = {
            RGB(20, 150, 155), RGB(130, 80, 210), RGB(225, 120, 25),
            RGB(40, 110, 210), RGB(210, 70, 135), RGB(35, 145, 85)
        };
        size_t hash = 0;
        for (wchar_t c : id) hash = (hash * 31 + size_t(c)) % 65521;
        return colors[hash % (sizeof(colors) / sizeof(colors[0]))];
    }

    static LRESULT CALLBACK windowProc(HWND window, UINT message, WPARAM w, LPARAM l) {
        auto self = reinterpret_cast<ComputerVirtualPointer*>(GetWindowLongPtrW(window, GWLP_USERDATA));
        if (message == WM_NCCREATE) {
            auto create = reinterpret_cast<CREATESTRUCTW*>(l);
            self = static_cast<ComputerVirtualPointer*>(create->lpCreateParams);
            SetWindowLongPtrW(window, GWLP_USERDATA, reinterpret_cast<LONG_PTR>(self));
        }
        if (!self) return DefWindowProcW(window, message, w, l);
        switch (message) {
        case WM_NCHITTEST: return HTTRANSPARENT;
        case WM_MOUSEACTIVATE: return MA_NOACTIVATE;
        case WM_ERASEBKGND: return 1;
        case WM_PAINT: self->paint(window); return 0;
        case showMessage: self->present(window); return 0;
        case WM_TIMER:
            KillTimer(window, hideTimer);
            self->hide(window);
            return 0;
        case WM_DPICHANGED:
            self->dpi_ = HIWORD(w);
            self->present(window);
            return 0;
        case WM_DESTROY:
            {
                std::lock_guard<std::mutex> lock(self->stateMutex_);
                self->window_ = nullptr;
                self->status_.visible = false;
            }
            return 0;
        }
        return DefWindowProcW(window, message, w, l);
    }

    void signalReady(bool available) {
        {
            std::lock_guard<std::mutex> lock(stateMutex_);
            status_.available = available;
        }
        {
            std::lock_guard<std::mutex> lock(readyMutex_);
            ready_ = true;
        }
        readyCondition_.notify_all();
    }

    void messageLoop() {
        PeekMessageW(nullptr, nullptr, WM_USER, WM_USER, PM_NOREMOVE);
        {
            std::lock_guard<std::mutex> lock(stateMutex_);
            threadId_ = GetCurrentThreadId();
        }
        SetThreadDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);
        WNDCLASSEXW klass{};
        klass.cbSize = sizeof(klass);
        klass.hInstance = GetModuleHandleW(nullptr);
        klass.lpfnWndProc = windowProc;
        klass.lpszClassName = className;
        klass.hCursor = LoadCursorW(nullptr, IDC_ARROW);
        if (!RegisterClassExW(&klass) && GetLastError() != ERROR_CLASS_ALREADY_EXISTS) {
            signalReady(false);
            return;
        }
        HWND window = CreateWindowExW(
            WS_EX_LAYERED | WS_EX_TRANSPARENT | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE,
            className, L"Aide virtual pointer", WS_POPUP,
            0, 0, 1, 1, nullptr, nullptr, klass.hInstance, this);
        if (!window || !SetLayeredWindowAttributes(window, transparentColor, 242, LWA_COLORKEY | LWA_ALPHA)) {
            if (window) DestroyWindow(window);
            signalReady(false);
            return;
        }
        {
            std::lock_guard<std::mutex> lock(stateMutex_);
            window_ = window;
            status_.available = true;
        }
        signalReady(true);

        MSG message{};
        while (GetMessageW(&message, nullptr, 0, 0) > 0) {
            TranslateMessage(&message);
            DispatchMessageW(&message);
        }
        if (IsWindow(window)) DestroyWindow(window);
        UnregisterClassW(className, klass.hInstance);
    }

    void present(HWND window) {
        Status current;
        POINT point;
        COLORREF tint;
        {
            std::lock_guard<std::mutex> lock(stateMutex_);
            current = status_;
            point = point_;
            tint = tint_;
        }
        if (!current.visible) return;

        HMONITOR monitor = MonitorFromPoint(point, MONITOR_DEFAULTTONEAREST);
        MONITORINFO info{};
        info.cbSize = sizeof(info);
        if (!GetMonitorInfoW(monitor, &info)) return;
        dpi_ = monitorDpi(monitor);
        const int width = int(scale(278, dpi_)), height = int(scale(46, dpi_));
        int x = point.x - int(scale(5, dpi_));
        int y = point.y - int(scale(39, dpi_));
        const RECT work = info.rcWork;
        x = std::max(int(work.left), std::min(x, int(work.right) - width));
        y = std::max(int(work.top), std::min(y, int(work.bottom) - height));
        SetWindowTextW(window, current.title.c_str());
        SetWindowPos(window, HWND_TOPMOST, x, y, width, height,
            SWP_NOACTIVATE | SWP_SHOWWINDOW);
        ShowWindow(window, SW_SHOWNOACTIVATE);
        SetTimer(window, hideTimer, 4000, nullptr);
        InvalidateRect(window, nullptr, FALSE);
    }

    void hide(HWND window) {
        {
            std::lock_guard<std::mutex> lock(stateMutex_);
            status_.visible = false;
            status_.controllerId.clear();
            status_.title.clear();
        }
        ShowWindow(window, SW_HIDE);
    }

    void paint(HWND window) {
        PAINTSTRUCT ps{};
        HDC dc = BeginPaint(window, &ps);
        RECT client{};
        GetClientRect(window, &client);
        Status current;
        COLORREF tint;
        {
            std::lock_guard<std::mutex> lock(stateMutex_);
            current = status_;
            tint = tint_;
        }
        const bool contrast = highContrast();
        const bool dark = !contrast && darkApps();
        const COLORREF background = contrast ? GetSysColor(COLOR_WINDOW) : dark ? RGB(36, 39, 46) : RGB(250, 250, 252);
        const COLORREF foreground = contrast ? GetSysColor(COLOR_WINDOWTEXT) : dark ? RGB(244, 246, 250) : RGB(28, 31, 38);
        const COLORREF accent = contrast ? GetSysColor(COLOR_HIGHLIGHT) : tint;
        const int width = client.right, height = client.bottom;
        HBRUSH transparent = CreateSolidBrush(transparentColor);
        FillRect(dc, &client, transparent);
        DeleteObject(transparent);

        const int left = int(scale(34, dpi_)), top = int(scale(6, dpi_));
        const int right = width - int(scale(4, dpi_)), bottom = height - int(scale(6, dpi_));
        HRGN panel = CreateRoundRectRgn(left, top, right, bottom, int(scale(9, dpi_)), int(scale(9, dpi_)));
        HBRUSH panelBrush = CreateSolidBrush(background);
        HBRUSH borderBrush = CreateSolidBrush(contrast ? GetSysColor(COLOR_WINDOWTEXT) : dark ? RGB(82, 88, 100) : RGB(205, 210, 220));
        FillRgn(dc, panel, panelBrush);
        FrameRgn(dc, panel, borderBrush, int(scale(1, dpi_)), int(scale(1, dpi_)));
        DeleteObject(panel);
        DeleteObject(panelBrush);
        DeleteObject(borderBrush);

        POINT arrow[] = {
            {int(scale(5, dpi_)), int(scale(40, dpi_))}, {int(scale(5, dpi_)), int(scale(13, dpi_))},
            {int(scale(12, dpi_)), int(scale(20, dpi_))}, {int(scale(18, dpi_)), int(scale(8, dpi_))},
            {int(scale(23, dpi_)), int(scale(11, dpi_))}, {int(scale(17, dpi_)), int(scale(23, dpi_))},
            {int(scale(28, dpi_)), int(scale(23, dpi_))}, {int(scale(23, dpi_)), int(scale(28, dpi_))},
            {int(scale(18, dpi_)), int(scale(28, dpi_))}, {int(scale(14, dpi_)), int(scale(36, dpi_))}
        };
        HPEN arrowPen = CreatePen(PS_SOLID, std::max(1, int(scale(1, dpi_))), contrast ? GetSysColor(COLOR_HIGHLIGHTTEXT) : RGB(255, 255, 255));
        HBRUSH arrowBrush = CreateSolidBrush(accent);
        auto oldPen = SelectObject(dc, arrowPen);
        auto oldBrush = SelectObject(dc, arrowBrush);
        Polygon(dc, arrow, int(sizeof(arrow) / sizeof(arrow[0])));
        SelectObject(dc, oldPen);
        SelectObject(dc, oldBrush);
        DeleteObject(arrowPen);
        DeleteObject(arrowBrush);

        SetBkMode(dc, TRANSPARENT);
        SetTextColor(dc, foreground);
        HFONT font = CreateFontW(int(scale(11, dpi_)), 0, 0, 0, FW_MEDIUM, FALSE, FALSE, FALSE,
            DEFAULT_CHARSET, OUT_DEFAULT_PRECIS, CLIP_DEFAULT_PRECIS, CLEARTYPE_QUALITY,
            DEFAULT_PITCH | FF_DONTCARE, L"Segoe UI");
        auto oldFont = SelectObject(dc, font);
        RECT text{int(scale(44, dpi_)), int(scale(6, dpi_)), width - int(scale(12, dpi_)), height - int(scale(6, dpi_))};
        DrawTextW(dc, current.title.c_str(), -1, &text, DT_SINGLELINE | DT_VCENTER | DT_END_ELLIPSIS | DT_NOPREFIX);
        SelectObject(dc, oldFont);
        DeleteObject(font);
        EndPaint(window, &ps);
    }

public:
    ~ComputerVirtualPointer() { stop(); }

    bool start() {
        if (uiThread_.joinable()) return status().available;
        {
            std::lock_guard<std::mutex> lock(readyMutex_);
            ready_ = false;
        }
        uiThread_ = std::thread([this] { messageLoop(); });
        std::unique_lock<std::mutex> lock(readyMutex_);
        if (!readyCondition_.wait_for(lock, std::chrono::seconds(3), [this] { return ready_; })) {
            DWORD id = 0;
            { std::lock_guard<std::mutex> stateLock(stateMutex_); id = threadId_; }
            if (id) PostThreadMessageW(id, WM_QUIT, 0, 0);
            lock.unlock();
            if (uiThread_.joinable()) uiThread_.join();
            return false;
        }
        lock.unlock();
        return status().available;
    }

    bool show(std::wstring controllerId, std::wstring label, std::wstring app, POINT point) {
        controllerId = clean(std::move(controllerId), 80);
        label = clean(std::move(label), 40);
        app = clean(std::move(app), 80);
        if (controllerId.empty()) controllerId = L"legacy";
        if (label.empty()) label = L"Aide";
        std::wstring title = label + L" · " + app;
        if (title.size() > 38) title.resize(38);
        HWND window = nullptr;
        {
            std::lock_guard<std::mutex> lock(stateMutex_);
            if (!status_.available || !window_) return false;
            status_.visible = true;
            status_.controllerId = controllerId;
            status_.title = title;
            point_ = point;
            tint_ = colorFor(controllerId);
            window = window_;
        }
        if (PostMessageW(window, showMessage, 0, 0)) return true;
        std::lock_guard<std::mutex> lock(stateMutex_);
        status_.visible = false;
        return false;
    }

    Status status() const {
        std::lock_guard<std::mutex> lock(stateMutex_);
        return status_;
    }

    void stop() {
        DWORD id = 0;
        { std::lock_guard<std::mutex> lock(stateMutex_); id = threadId_; }
        if (id) PostThreadMessageW(id, WM_QUIT, 0, 0);
        if (uiThread_.joinable()) uiThread_.join();
        std::lock_guard<std::mutex> lock(stateMutex_);
        status_.visible = false;
        status_.available = false;
        status_.controllerId.clear();
        status_.title.clear();
        threadId_ = 0;
    }
};

} // namespace aide_desktop
