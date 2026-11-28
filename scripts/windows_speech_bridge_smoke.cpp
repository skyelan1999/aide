#include "../native/windows-launcher/third_party/json.hpp"
using json = nlohmann::json;
#include "../native/windows-launcher/desktop_speech.hpp"
#include <iostream>

int wmain() {
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
    std::cout << "PASS SAPI bridge worker lifecycle and invalid session rejection; microphone remains inactive" << std::endl;
    return 0;
}
