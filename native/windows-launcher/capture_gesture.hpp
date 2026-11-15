#pragma once

#include <cstdint>

namespace aide_desktop {

// Windows global capture uses the same conservative gesture envelope as the
// macOS host: two short modifier taps, with no unrelated input between them.
class CaptureGesture {
    uint64_t pressedAt_ = 0;
    uint64_t lastTap_ = 0;
    bool interrupted_ = false;

public:
    bool keyDown(bool targetModifier, bool repeat, bool otherKeyDown, uint64_t now) {
        if (!targetModifier || repeat || otherKeyDown) {
            cancel();
            return false;
        }
        if (!pressedAt_) pressedAt_ = now ? now : 1;
        return false;
    }

    bool keyUp(bool targetModifier, bool otherKeyDown, uint64_t now) {
        if (!targetModifier || !pressedAt_ || otherKeyDown || interrupted_) {
            cancel();
            return false;
        }
        auto down = pressedAt_;
        pressedAt_ = 0;
        if (now < down || now - down > 400) {
            lastTap_ = 0;
            interrupted_ = false;
            return false;
        }
        bool trigger = lastTap_ && now >= lastTap_ && now - lastTap_ <= 800;
        lastTap_ = trigger ? 0 : now;
        interrupted_ = false;
        return trigger;
    }

    void interrupt(uint64_t now) {
        if (pressedAt_ || (lastTap_ && now >= lastTap_ && now - lastTap_ <= 800)) interrupted_ = true;
        else lastTap_ = 0;
    }

    void cancel() { pressedAt_ = 0; lastTap_ = 0; interrupted_ = false; }
};

}
