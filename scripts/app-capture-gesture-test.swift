import Foundation
@main struct GestureTests {
 static func main() {
  var g=DoubleModifierGesture()
  func down(_ t:Double)->Bool { g.flagsChanged(targetKey:true,onlyTargetDown:true,allReleased:false,time:t) }
  func up(_ t:Double)->Bool { g.flagsChanged(targetKey:true,onlyTargetDown:false,allReleased:true,time:t) }
  assert(!down(0));assert(!up(0.08));assert(!down(0.25));assert(up(0.32))
  assert(!down(0.4));assert(!up(0.45),"triple tap cannot produce a second capture")
  g.cancel();assert(!down(1));assert(!up(1.5));assert(!down(1.6));assert(!up(1.7),"held Shift is not a clean first tap")
  g.cancel();assert(!down(2));g.cancel();assert(!up(2.1));assert(!down(2.2));assert(!up(2.3),"typing/click cancels pair")
  g.cancel();assert(!down(3));assert(!up(3.1));assert(!g.flagsChanged(targetKey:false,onlyTargetDown:false,allReleased:false,time:3.15));assert(!down(3.2));assert(!up(3.3))
  g.cancel();assert(!down(4));assert(!up(4.1));assert(!down(5));assert(!up(5.1),"slow taps are not paired")
  print("PASS double-modifier: clean pair, long hold, typing/click/chord, timing and triple-tap rejection")
 }
}
