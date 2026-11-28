#include "../native/windows-launcher/third_party/json.hpp"
using json = nlohmann::json;
#include "../native/windows-launcher/desktop_speech.hpp"
#include <iostream>

// Exercise an ordinary SAPI token without creating a recognizer or audio input.
// A conflicting root value ensures language lookup uses the Attributes subkey.
static bool tokenLanguageCheck() {
    HRESULT init = CoInitializeEx(nullptr, COINIT_MULTITHREADED);
    if (FAILED(init)) return false;
    CLSID clsid{};
    ISpObjectToken* token = nullptr;
    ISpDataKey* attributes = nullptr;
    LPWSTR language = nullptr;
    bool created = false;
    GUID unique{};
    wchar_t suffix[40]{};
    HRESULT hr = CoCreateGuid(&unique);
    if (SUCCEEDED(hr) && !StringFromGUID2(unique, suffix, 40)) hr = E_FAIL;
    const auto id = std::wstring(L"HKEY_CURRENT_USER\\Software\\AideSpeechBridgeSmoke-") + suffix;
    if (SUCCEEDED(hr)) hr = CLSIDFromProgID(L"SAPI.SpObjectToken", &clsid);
    if (SUCCEEDED(hr)) hr = CoCreateInstance(clsid, nullptr, CLSCTX_INPROC_SERVER,
        __uuidof(ISpObjectToken), reinterpret_cast<void**>(&token));
    if (SUCCEEDED(hr)) { hr = token->SetId(nullptr, id.c_str(), TRUE); created = SUCCEEDED(hr); }
    if (SUCCEEDED(hr)) hr = token->SetStringValue(L"Language", L"409");
    if (SUCCEEDED(hr)) hr = token->CreateKey(L"Attributes", &attributes);
    if (SUCCEEDED(hr)) hr = attributes->SetStringValue(L"Language", L"804;409");
    if (attributes) attributes->Release();
    if (SUCCEEDED(hr)) hr = aide_desktop::localSpeechTokenLanguage(token, &language);
    const bool ok = SUCCEEDED(hr) && language && std::wstring(language) == L"804;409";
    if (language) CoTaskMemFree(language);
    bool removed = !created || SUCCEEDED(token->Remove(nullptr));
    if (token) token->Release();
    CoUninitialize();
    return ok && removed;
}

int wmain() {
    if (!tokenLanguageCheck()) {
        std::cerr << "SAPI Language lookup did not read Attributes or clean up its owned test token" << std::endl;
        return 1;
    }
    aide_desktop::LocalSpeech speech;
    auto invalidStart = speech.start("invalid-session");
    auto correctShape = aide_desktop::LocalSpeech::validSession("01234567-89ab-4def-8123-456789abcdef");
    auto wrongShape = aide_desktop::LocalSpeech::validSession("x1234567-89ab-4def-8123-456789abcdef");
    auto neverStartedStatus = speech.status("01234567-89ab-4def-8123-456789abcdef");
    auto invalidStatus = speech.status("invalid-session");
    auto invalidStop = speech.stop("invalid-session");
    if (!correctShape || wrongShape || !neverStartedStatus.contains("error") || !invalidStart.contains("error") || !invalidStatus.contains("error") || !invalidStop.contains("error")) {
        std::cerr << "Speech session boundary accepted an invalid session" << std::endl;
        return 1;
    }
    std::cout << "PASS SAPI token Attributes/Language, worker lifecycle and invalid session rejection; microphone remains inactive" << std::endl;
    return 0;
}
