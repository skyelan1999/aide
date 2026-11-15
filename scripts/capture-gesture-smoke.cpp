#include "../native/windows-launcher/capture_gesture.hpp"
#include <cstdlib>
#include <iostream>

using aide_desktop::CaptureGesture;

static void require(bool value, const char* name) {
    if (!value) { std::cerr << "FAIL " << name << '\n'; std::exit(1); }
}

int main() {
    CaptureGesture gesture;
    require(!gesture.keyDown(true, false, false, 100), "first press");
    require(!gesture.keyUp(true, false, 180), "first release");
    require(!gesture.keyDown(true, false, false, 300), "second press");
    require(gesture.keyUp(true, false, 390), "double tap triggers");

    gesture.keyDown(true, false, false, 1000);
    require(!gesture.keyUp(true, false, 1500), "long press ignored");
    gesture.keyDown(true, false, false, 1700);
    require(!gesture.keyUp(true, false, 1750), "long press does not seed a tap");

    gesture.keyDown(true, false, false, 2000);
    gesture.keyUp(true, false, 2050);
    gesture.interrupt(2100);
    gesture.keyDown(true, false, false, 2200);
    require(!gesture.keyUp(true, false, 2250), "intervening input cancels");

    gesture.keyDown(true, false, false, 3000);
    gesture.keyUp(true, false, 3050);
    gesture.keyDown(true, false, true, 3200);
    require(!gesture.keyUp(true, false, 3250), "another key cancels");

    gesture.keyDown(true, false, false, 4000);
    gesture.keyUp(true, false, 4050);
    gesture.keyDown(true, false, false, 4900);
    require(!gesture.keyUp(true, false, 4950), "slow double tap ignored");
    std::cout << "PASS Windows capture gesture boundaries\n";
}
