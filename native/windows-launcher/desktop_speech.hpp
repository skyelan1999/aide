#pragma once

#include <mutex>
#include <thread>
#include <sapi.h>
#include <sphelper.h>

namespace aide_desktop {

// Owns the recognizer on a dedicated COM thread. The thread waits without
// opening the microphone; only an explicit start request activates dictation.
class LocalSpeech {
    HANDLE command_ = CreateEventW(nullptr, FALSE, FALSE, nullptr);
    std::thread worker_;
    mutable std::mutex mutex_;
    bool closing_ = false;
    bool requested_ = false;
    bool engineAvailable_ = false;
    std::string session_;
    std::string state_ = "stopped";
    std::string text_;
    std::string interim_;
    std::string error_;

    static std::string utf8Text(const wchar_t* value) {
        if (!value) return {};
        int size = WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, value, -1, nullptr, 0, nullptr, nullptr);
        if (size <= 1) return {};
        std::string out(size, '\0');
        WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, value, -1, out.data(), size, nullptr, nullptr);
        out.pop_back();
        return out;
    }

    void run() {
        HRESULT init = CoInitializeEx(nullptr, COINIT_MULTITHREADED);
        ISpRecoContext* context = nullptr;
        ISpRecoGrammar* grammar = nullptr;
        HRESULT hr = FAILED(init) ? init : CoCreateInstance(CLSID_SpSharedRecoContext, nullptr, CLSCTX_ALL,
            IID_ISpRecoContext, reinterpret_cast<void**>(&context));
        if (SUCCEEDED(hr)) hr = context->SetNotifyWin32Event();
        const ULONGLONG speechEvents = SPFEI(SPEI_RECOGNITION) | SPFEI(SPEI_HYPOTHESIS);
        if (SUCCEEDED(hr)) hr = context->SetInterest(speechEvents, speechEvents);
        if (SUCCEEDED(hr)) hr = context->CreateGrammar(1, &grammar);
        if (SUCCEEDED(hr)) hr = grammar->LoadDictation(nullptr, SPLO_STATIC);
        if (SUCCEEDED(hr)) {
            ISpRecognizer* recognizer = nullptr; ISpObjectToken* token = nullptr; LPWSTR language = nullptr;
            hr = context->GetRecognizer(&recognizer);
            if (SUCCEEDED(hr)) hr = recognizer->GetRecognizer(&token);
            if (SUCCEEDED(hr)) hr = token->GetStringValue(L"Language", &language);
            bool chinese = false;
            if (SUCCEEDED(hr) && language) {
                wchar_t* cursor = language;
                while (*cursor) {
                    wchar_t* end = nullptr; auto lcid = wcstoul(cursor, &end, 16);
                    if (end == cursor) break;
                    if (lcid == 0x0804) chinese = true;
                    cursor = *end ? end + 1 : end;
                }
            }
            if (language) CoTaskMemFree(language);
            if (token) token->Release(); if (recognizer) recognizer->Release();
            if (SUCCEEDED(hr) && !chinese) hr = HRESULT_FROM_WIN32(ERROR_NOT_SUPPORTED);
        }
        HANDLE events[2] = {command_, SUCCEEDED(hr) ? context->GetNotifyEventHandle() : nullptr};
        if (SUCCEEDED(hr) && (!events[1] || events[1] == INVALID_HANDLE_VALUE)) hr = E_HANDLE;
        { std::lock_guard<std::mutex> lock(mutex_); engineAvailable_ = SUCCEEDED(hr); }
        if (FAILED(hr)) {
            std::lock_guard<std::mutex> lock(mutex_);
            error_ = "Windows 本机中文语音识别不可用，请安装并启用简体中文语音组件后重试。";
            state_ = "error";
            requested_ = false;
        }

        bool active = false;
        while (true) {
            { std::lock_guard<std::mutex> lock(mutex_); if (closing_) break; }
            DWORD wait = SUCCEEDED(hr) ? WaitForMultipleObjects(2, events, FALSE, INFINITE) : WaitForSingleObject(command_, INFINITE);
            if (wait == WAIT_OBJECT_0) {
                bool want = false;
                { std::lock_guard<std::mutex> lock(mutex_); if (closing_) break; want = requested_; }
                if (want && !active && SUCCEEDED(hr)) {
                    HRESULT start = grammar->SetDictationState(SPRS_ACTIVE);
                    std::lock_guard<std::mutex> lock(mutex_);
                    if (SUCCEEDED(start) && requested_) { active = true; state_ = "recording"; error_.clear(); }
                    else if (FAILED(start)) { state_ = "error"; requested_ = false; error_ = "无法打开麦克风或启动 Windows 本机语音识别，请检查麦克风权限和中文语音组件。"; }
                    if (SUCCEEDED(start) && !requested_) { grammar->SetDictationState(SPRS_INACTIVE); active = false; }
                } else if (!want && active) {
                    grammar->SetDictationState(SPRS_INACTIVE); active = false;
                }
                continue;
            }
            if (wait != WAIT_OBJECT_0 + 1 || !context) continue;
            SPEVENT event{}; ULONG count = 0;
            while (SUCCEEDED(context->GetEvents(1, &event, &count)) && count) {
                if (event.eEventId == SPEI_RECOGNITION || event.eEventId == SPEI_HYPOTHESIS) {
                    auto result = reinterpret_cast<ISpRecoResult*>(event.lParam);
                    wchar_t* phrase = nullptr; BYTE chars = 0;
                    if (result && SUCCEEDED(result->GetText(SP_GETWHOLEPHRASE, SP_GETWHOLEPHRASE, TRUE, &phrase, &chars))) {
                        auto converted = utf8Text(phrase); CoTaskMemFree(phrase);
                        std::lock_guard<std::mutex> lock(mutex_);
                        if (requested_ && state_ == "recording") {
                            if (event.eEventId == SPEI_RECOGNITION) { text_ += converted; interim_.clear(); }
                            else interim_ = std::move(converted);
                        }
                    }
                }
                SpClearEvent(&event);
            }
        }
        if (active && grammar) grammar->SetDictationState(SPRS_INACTIVE);
        if (grammar) grammar->Release();
        if (context) context->Release();
        if (SUCCEEDED(init)) CoUninitialize();
    }

public:
    LocalSpeech() {
        if (!command_) throw std::runtime_error("Speech worker event unavailable");
        worker_ = std::thread([this] { run(); });
    }
    LocalSpeech(const LocalSpeech&) = delete;
    LocalSpeech& operator=(const LocalSpeech&) = delete;
    ~LocalSpeech() {
        { std::lock_guard<std::mutex> lock(mutex_); closing_ = true; requested_ = false; }
        SetEvent(command_);
        if (worker_.joinable()) worker_.join();
        CloseHandle(command_);
    }

    json start(const std::string& session) {
        if (session.size() != 36) return {{"error", "语音会话无效，请重新开始。"}};
        { std::lock_guard<std::mutex> lock(mutex_);
          if (!engineAvailable_) return {{"error", error_.empty() ? "Windows 本机中文语音识别不可用，请检查系统语音组件。" : error_}};
          if (requested_ || state_ == "starting" || state_ == "recording") return {{"error", "已有本机语音会话正在运行。"}};
          session_ = session; text_.clear(); interim_.clear(); error_.clear(); state_ = "starting"; requested_ = true; }
        SetEvent(command_);
        return {{"state", "starting"}, {"message", "正在启动 Windows 本机语音识别…"}};
    }
    json status(const std::string& session) const {
        std::lock_guard<std::mutex> lock(mutex_);
        if (session.empty() || session != session_) return {{"error", "Speech session revoked"}};
        if (state_ == "error") return {{"error", error_}};
        return {{"state", state_}, {"message", state_ == "recording" ? "本机录音中" : "正在启动 Windows 本机语音识别…"}, {"text", text_ + interim_}};
    }
    json stop(const std::string& session) {
        { std::lock_guard<std::mutex> lock(mutex_);
          if (session.empty() || session != session_) return {{"error", "Speech session revoked"}};
          requested_ = false; state_ = "stopped"; session_.clear(); error_.clear(); }
        SetEvent(command_);
        return {{"state", "stopped"}, {"text", text_ + interim_}};
    }
    void stop() {
        { std::lock_guard<std::mutex> lock(mutex_); requested_ = false; state_ = "stopped"; session_.clear(); error_.clear(); }
        SetEvent(command_);
    }
};

}
