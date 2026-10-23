// Mono microphone -> 16 kHz PCM16, 20 ms packets. No audio is stored.
class AidePCMInput extends AudioWorkletProcessor {
 constructor(){super();this.phase=0;this.sum=0;this.count=0;this.chunk=new Int16Array(320);this.index=0;}
 process(inputs){
  const channel=inputs[0]?.[0];if(!channel)return true;
  for(const value of channel){
   this.sum+=value;this.count++;this.phase+=16000;
   if(this.phase>=sampleRate){this.phase-=sampleRate;const v=Math.max(-1,Math.min(1,this.sum/this.count));this.sum=0;this.count=0;this.chunk[this.index++]=Math.round(v*(v<0?32768:32767));
    if(this.index===320){const bytes=new ArrayBuffer(640),view=new DataView(bytes);for(let i=0;i<320;i++)view.setInt16(i*2,this.chunk[i],true);this.port.postMessage(bytes,[bytes]);this.index=0;}
   }
  }
  return true;
 }
}
registerProcessor('aide-pcm-input',AidePCMInput);
