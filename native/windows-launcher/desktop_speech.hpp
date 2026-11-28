#pragma once

#include <chrono>
#include <mutex>
#include <stdexcept>
#include <string>
#include <utility>
#include <thread>
#include <windows.h>
#include <sapi.h>

namespace aide_desktop {

inline void clearSpeechEvent(SPEVENT& event) {
    if (event.elParamType == SPET_LPARAM_IS_OBJECT && event.lParam) {
        reinterpret_cast<IUnknown*>(event.lParam)->Release();
    }
    event = {};
}

// Owns the recognizer on a dedicated COM thread. The thread waits without
// opening the microphone; only an explicit start request activates dictation.
class LocalSpeech {
    HANDLE command_ = CreateEventW(nullptr, FALSE, FALSE, nullptr);
    std::thread worker_;
    mutable std::mutex mutex_;
    bool closing_ = false;
    bool requested_ = false;
    bool busy_ = false;
    static constexpr size_t transcriptLimit_ = 60000;
    std::string session_;
    std::string state_ = "stopped";
    std::string text_;
    std::string interim_;
    std::string error_;

    static std::string utf8Text(const wchar_t* value) {
        if (!value) return {};
        int size = WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, value, -1, nullptr, 0, nullptr, nullptr);
        if (size <= 1 || static_cast<size_t>(size - 1) > transcriptLimit_) return {};
        std::string out(size, '\0');
        WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, value, -1, out.data(), size, nullptr, nullptr);
        out.pop_back();
        return out;
    }

    bool requested() const {
        std::lock_guard<std::mutex> lock(mutex_);
        return requested_ && !closing_;
    }

    void record() {
        const auto deadline = std::chrono::steady_clock::now() + std::chrono::minutes(15);
        const auto expired = [&]{return std::chrono::steady_clock::now() >= deadline;};
        const HRESULT init = CoInitializeEx(nullptr, COINIT_MULTITHREADED);
        ISpRecognizer* recognizer = nullptr;
        ISpAudio* input = nullptr;
        ISpRecoContext* context = nullptr;
        ISpRecoGrammar* grammar = nullptr;
        CLSID engine{}, microphone{};
        HRESULT hr = FAILED(init) ? init : CLSIDFromProgID(L"SAPI.SpInprocRecognizer", &engine);
        if (SUCCEEDED(hr)) hr = CoCreateInstance(engine, nullptr, CLSCTX_INPROC_SERVER,
            __uuidof(ISpRecognizer), reinterpret_cast<void**>(&recognizer));
        if (SUCCEEDED(hr)) {
            ISpObjectToken* token = nullptr;
            LPWSTR language = nullptr;
            hr = recognizer->GetRecognizer(&token);
            if (SUCCEEDED(hr)) hr = token->GetStringValue(L"Language", &language);
            bool chinese = false;
            if (SUCCEEDED(hr) && language) {
                auto cursor = language;
                while (*cursor) {
                    wchar_t* end = nullptr;
                    const auto lcid = wcstoul(cursor, &end, 16);
                    if (end == cursor) break;
                    if (lcid == 0x0804) chinese = true;
                    cursor = *end ? end + 1 : end;
                }
            }
            if (language) CoTaskMemFree(language);
            if (token) token->Release();
            if (SUCCEEDED(hr) && !chinese) hr = HRESULT_FROM_WIN32(ERROR_NOT_SUPPORTED);
        }
        if (SUCCEEDED(hr)) hr = recognizer->CreateRecoContext(&context);
        if (SUCCEEDED(hr)) hr = context->SetNotifyWin32Event();
        const ULONGLONG speechEvents = SPFEI(SPEI_RECOGNITION) | SPFEI(SPEI_HYPOTHESIS);
        if (SUCCEEDED(hr)) hr = context->SetInterest(speechEvents, speechEvents);
        if (SUCCEEDED(hr)) hr = context->CreateGrammar(1, &grammar);
        if (SUCCEEDED(hr)) hr = grammar->LoadDictation(nullptr, SPLO_STATIC);
        HANDLE events[2] = {command_, SUCCEEDED(hr) ? context->GetNotifyEventHandle() : nullptr};
        if (SUCCEEDED(hr) && (!events[1] || events[1] == INVALID_HANDLE_VALUE)) hr = E_HANDLE;
        if (SUCCEEDED(hr) && (!requested() || expired())) hr = E_ABORT;
        if (SUCCEEDED(hr)) hr = CLSIDFromProgID(L"SAPI.SpMMAudioIn", &microphone);
        if (SUCCEEDED(hr)) hr = CoCreateInstance(microphone, nullptr, CLSCTX_INPROC_SERVER,
            __uuidof(ISpAudio), reinterpret_cast<void**>(&input));
        if (SUCCEEDED(hr) && (!requested() || expired())) hr = E_ABORT;
        if (SUCCEEDED(hr)) hr = recognizer->SetInput(input, TRUE);
        if (SUCCEEDED(hr) && (!requested() || expired())) hr = E_ABORT;
        if (SUCCEEDED(hr)) hr = grammar->SetDictationState(SPRS_ACTIVE);
        const bool active = SUCCEEDED(hr);
        {
            std::lock_guard<std::mutex> lock(mutex_);
            if (requested_ && active) state_ = "recording";
        }

        while (active && requested() && !expired()) {
            const auto wait = WaitForMultipleObjects(2, events, FALSE, 250);
            if (wait == WAIT_FAILED) {hr = HRESULT_FROM_WIN32(GetLastError()); break;}
            if (wait != WAIT_OBJECT_0 + 1) continue;
            for (int i = 0; i < 64 && requested() && !expired(); ++i) {
                SPEVENT event{};
                ULONG count = 0;
                if (FAILED(context->GetEvents(1, &event, &count)) || !count) break;
                if (event.eEventId == SPEI_RECOGNITION || event.eEventId == SPEI_HYPOTHESIS) {
                    auto result = reinterpret_cast<ISpRecoResult*>(event.lParam);
                    wchar_t* phrase = nullptr;
                    BYTE attrs = 0;
                    if (result && SUCCEEDED(result->GetText(SP_GETWHOLEPHRASE, SP_GETWHOLEPHRASE, TRUE, &phrase, &attrs))) {
                        auto converted = utf8Text(phrase);
                        std::lock_guard<std::mutex> lock(mutex_);
                        if (requested_ && converted.size() <= transcriptLimit_ - text_.size()) {
                            if (event.eEventId == SPEI_RECOGNITION) {text_ += converted; interim_.clear();}
                            else interim_ = std::move(converted);
                        }
                    }
                    if (phrase) CoTaskMemFree(phrase);
                }
                clearSpeechEvent(event);
            }
        }
        if (active && grammar) grammar->SetDictationState(SPRS_INACTIVE);
        if (grammar) grammar->Release();
        if (context) context->Release();
        if (recognizer) recognizer->Release();
        if (input) input->Release();
        if (SUCCEEDED(init)) CoUninitialize();
        std::lock_guard<std::mutex> lock(mutex_);
        if (requested_) {
            if (FAILED(hr) && hr != E_ABORT) {
                state_ = "error";
                error_ = "Windows 本机中文语音识别未能启动或已中断，请检查麦克风权限及简体中文语音组件。";
            } else {
                state_ = "stopped";
            }
        }
        requested_ = false;
    }

    void run() {
        while (WaitForSingleObject(command_, INFINITE) == WAIT_OBJECT_0) {
            {
                std::lock_guard<std::mutex> lock(mutex_);
                if (closing_) return;
                if (!requested_) continue;
                busy_ = true;
            }
            record();
            std::lock_guard<std::mutex> lock(mutex_);
            busy_ = false;
            if (closing_) return;
        }
    }

public:
    static bool validSession(const std::string& session) {
        if (session.size() != 36) return false;
        for (size_t i=0; i<session.size(); ++i) {
            const char ch = session[i];
            if (i==8 || i==13 || i==18 || i==23) {
                if (ch!='-') return false;
            } else if (!(ch>='0'&&ch<='9') && !(ch>='a'&&ch<='f')) return false;
        }
        return true;
    }
    LocalSpeech() {
        if (!command_) throw std::runtime_error("Speech worker event unavailable");
        try { worker_ = std::thread([this] { run(); }); }
        catch (...) { CloseHandle(command_); throw; }
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
        if (!validSession(session)) return {{"error", "语音会话无效，请重新开始。"}};
        { std::lock_guard<std::mutex> lock(mutex_);
          if (closing_ || busy_ || requested_) return {{"error", "已有本机语音会话正在运行。"}};
          session_ = session; text_.clear(); interim_.clear(); error_.clear(); state_ = "starting"; requested_ = true; }
        SetEvent(command_);
        return {{"state", "starting"}, {"message", "正在启动 Windows 本机语音识别…"}};
    }
    json status(const std::string& session) const {
        std::lock_guard<std::mutex> lock(mutex_);
        if (session.empty() || session != session_) return {{"error", "Speech session revoked"}};
        if (state_ == "error") return {{"error", error_}};
        return {{"state", state_}, {"message", state_ == "recording" ? "本机录音中" : state_ == "stopped" ? "本机录音已停止" : "正在启动 Windows 本机语音识别…"}, {"text", text_ + interim_}};
    }
    json stop(const std::string& session) {
        std::lock_guard<std::mutex> lock(mutex_);
        if (session.empty() || session != session_) return {{"error", "Speech session revoked"}};
        auto transcript = text_ + interim_;
        requested_ = false; state_ = "stopped"; session_.clear(); error_.clear(); text_.clear(); interim_.clear();
        SetEvent(command_);
        return {{"state", "stopped"}, {"text", std::move(transcript)}};
    }
    void stop() {
        { std::lock_guard<std::mutex> lock(mutex_); requested_ = false; state_ = "stopped"; session_.clear(); error_.clear(); text_.clear(); interim_.clear(); }
        SetEvent(command_);
    }
};

}
