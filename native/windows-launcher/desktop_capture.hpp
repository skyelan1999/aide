#pragma once

#include <windows.h>
#include <atomic>
#include <chrono>
#include <condition_variable>
#include <mutex>
#include <string>
#include <thread>
#include "capture_gesture.hpp"

namespace aide_desktop {

class DesktopCapture {
    HWND owner_ = nullptr;
    HHOOK keyboard_ = nullptr;
    HHOOK mouse_ = nullptr;
    std::thread listener_;
    DWORD listenerId_ = 0;
    std::wstring settingsFile_;
    mutable std::mutex mutex_;
    std::condition_variable ready_;
    bool enabled_ = false;
    bool readyState_ = false;
    bool listening_ = false;
    bool targetDown_ = false;
    std::wstring modifier_ = L"shift";
    uint64_t events_ = 0;
    uint64_t triggers_ = 0;
    std::string lastResult_;
    CaptureGesture gesture_;
    static DesktopCapture* active_;

    bool targetKey(DWORD key) const {
        if (modifier_ == L"control") return key == VK_LCONTROL || key == VK_RCONTROL || key == VK_CONTROL;
        return key == VK_LSHIFT || key == VK_RSHIFT || key == VK_SHIFT;
    }
    static LRESULT CALLBACK keyboardHook(int code, WPARAM message, LPARAM data) {
        if (code == HC_ACTION && active_) active_->keyboard(message, *reinterpret_cast<KBDLLHOOKSTRUCT*>(data));
        return CallNextHookEx(nullptr, code, message, data);
    }
    static LRESULT CALLBACK mouseHook(int code, WPARAM message, LPARAM data) {
        if (code == HC_ACTION && active_ && (message == WM_LBUTTONDOWN || message == WM_RBUTTONDOWN ||
            message == WM_MBUTTONDOWN || message == WM_XBUTTONDOWN || message == WM_MOUSEWHEEL || message == WM_MOUSEHWHEEL)) {
            std::lock_guard<std::mutex> lock(active_->mutex_);
            active_->gesture_.interrupt(GetTickCount64());
        }
        return CallNextHookEx(nullptr, code, message, data);
    }
    void keyboard(WPARAM message, const KBDLLHOOKSTRUCT& key) {
        std::lock_guard<std::mutex> lock(mutex_);
        if (!enabled_) return;
        ++events_;
        bool down = message == WM_KEYDOWN || message == WM_SYSKEYDOWN;
        bool up = message == WM_KEYUP || message == WM_SYSKEYUP;
        bool target = targetKey(key.vkCode);
        if (!target) { if (down) gesture_.interrupt(GetTickCount64()); return; }
        if (down) {
            bool repeated = targetDown_ || (key.flags & LLKHF_INJECTED) != 0;
            targetDown_ = true;
            gesture_.keyDown(true, repeated, false, GetTickCount64());
        } else if (up) {
            targetDown_ = false;
            if (gesture_.keyUp(true, false, GetTickCount64())) {
                ++triggers_;
                lastResult_ = "Capturing";
                auto targetWindow = GetForegroundWindow();
                if (owner_ && targetWindow && targetWindow != owner_) PostMessageW(owner_, WM_APP + 4, 0, reinterpret_cast<LPARAM>(targetWindow));
            }
        }
    }
    void listen() {
        active_ = this;
        { std::lock_guard<std::mutex> lock(mutex_); listenerId_ = GetCurrentThreadId(); }
        MSG message{}; PeekMessageW(&message, nullptr, WM_USER, WM_USER, PM_NOREMOVE);
        auto module = GetModuleHandleW(nullptr);
        keyboard_ = SetWindowsHookExW(WH_KEYBOARD_LL, keyboardHook, module, 0);
        mouse_ = SetWindowsHookExW(WH_MOUSE_LL, mouseHook, module, 0);
        { std::lock_guard<std::mutex> lock(mutex_); listening_ = keyboard_ && mouse_; readyState_ = true;
          if (!listening_) lastResult_ = "Windows could not start global input monitoring."; }
        ready_.notify_all();
        if (keyboard_ && mouse_) while (GetMessageW(&message, nullptr, 0, 0) > 0) { TranslateMessage(&message); DispatchMessageW(&message); }
        if (keyboard_) UnhookWindowsHookEx(keyboard_); if (mouse_) UnhookWindowsHookEx(mouse_);
        keyboard_ = mouse_ = nullptr; active_ = nullptr;
        { std::lock_guard<std::mutex> lock(mutex_); listening_ = false; listenerId_ = 0; }
    }
    void stopListener() {
        DWORD id = 0; { std::lock_guard<std::mutex> lock(mutex_); id = listenerId_; }
        if (id) PostThreadMessageW(id, WM_QUIT, 0, 0);
        if (listener_.joinable()) listener_.join();
        std::lock_guard<std::mutex> lock(mutex_); gesture_.cancel(); targetDown_ = false; readyState_ = false;
    }

public:
    DesktopCapture() = default;
    DesktopCapture(const DesktopCapture&) = delete;
    DesktopCapture& operator=(const DesktopCapture&) = delete;
    ~DesktopCapture() { { std::lock_guard<std::mutex> lock(mutex_); enabled_ = false; } stopListener(); }
    void initialize(HWND owner, const std::wstring& profile) {
        owner_ = owner; settingsFile_ = profile + L"\\capture.ini";
        wchar_t modifier[32]{}; GetPrivateProfileStringW(L"capture", L"modifier", L"shift", modifier, 32, settingsFile_.c_str());
        modifier_ = _wcsicmp(modifier, L"control") == 0 ? L"control" : L"shift";
        enabled_ = GetPrivateProfileIntW(L"capture", L"enabled", 0, settingsFile_.c_str()) != 0;
        if (enabled_) startListener();
    }
    void startListener() {
        if (listener_.joinable()) {
            bool active=false;{std::lock_guard<std::mutex> lock(mutex_);active=listenerId_!=0;}
            if(active)return;
            listener_.join();
        }
        { std::lock_guard<std::mutex> lock(mutex_); readyState_ = false; }
        listener_ = std::thread([this] { listen(); });
        std::unique_lock<std::mutex> lock(mutex_); ready_.wait_for(lock, std::chrono::seconds(3), [this] { return readyState_; });
    }
    json configure(bool enabled, const std::string& modifier) {
        if (modifier != "shift" && modifier != "control") return {{"error", "不支持的全局快捷键"}};
        { std::lock_guard<std::mutex> lock(mutex_); enabled_ = enabled; modifier_ = modifier == "control" ? L"control" : L"shift";
          gesture_.cancel(); targetDown_ = false; lastResult_.clear(); }
        WritePrivateProfileStringW(L"capture", L"enabled", enabled ? L"1" : L"0", settingsFile_.c_str());
        WritePrivateProfileStringW(L"capture", L"modifier", modifier_.c_str(), settingsFile_.c_str());
        if (enabled) startListener(); else stopListener();
        return status();
    }
    json status() const {
        std::lock_guard<std::mutex> lock(mutex_);
        return {{"platform", "windows"}, {"supported", true}, {"enabled", enabled_},
            {"modifier", modifier_ == L"control" ? "control" : "shift"}, {"listening", listening_},
            {"accessibility", true}, {"screenRecording", true}, {"modifierEvents", events_},
            {"triggerCount", triggers_}, {"lastResult", lastResult_}};
    }
    void result(const std::string& value) { std::lock_guard<std::mutex> lock(mutex_); lastResult_ = value; }
};

inline DesktopCapture* DesktopCapture::active_ = nullptr;

}
