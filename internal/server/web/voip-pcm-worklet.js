"use strict";
// 8 kHz mono for the SIP gateway. Browser media and the assistant keep their native rates.
class AideVoIPAudio extends AudioWorkletProcessor {
    constructor() {
        super();
        this.frame = new ArrayBuffer(320);
        this.view = new DataView(this.frame);
        this.samples = new Float32Array(1600);
        this.readAt = 0;
        this.writeAt = 0;
        this.length = 0;
        this.index = this.sum = this.count = this.capturePhase = this.playPhase = 0;
        this.playing = false;
        this.port.onmessage = ({ data }) => {
            const frame = new DataView(data);
            if (frame.byteLength / 2 > this.samples.length)
                return;
            for (let i = 0; i < frame.byteLength; i += 2) {
                this.samples[this.writeAt] = frame.getInt16(i, true) / 32768;
                this.writeAt = (this.writeAt + 1) % this.samples.length;
                if (this.length === this.samples.length)
                    this.readAt = this.writeAt;
                else
                    this.length++;
            }
        };
    }
    process(inputs, outputs) {
        const input = inputs[0]?.[0], output = outputs[0][0];
        if (!this.playing && this.length >= 320)
            this.playing = true;
        for (let i = 0; i < output.length; i++) {
            if (this.playing && this.length) {
                output[i] = this.samples[this.readAt];
                this.playPhase += 8000;
                if (this.playPhase >= sampleRate) {
                    this.playPhase -= sampleRate;
                    this.readAt = (this.readAt + 1) % this.samples.length;
                    this.length--;
                }
            }
            else {
                this.playing = false;
                output[i] = 0;
            }
            this.sum += input?.[i] || 0;
            this.count++;
            this.capturePhase += 8000;
            if (this.capturePhase >= sampleRate) {
                this.capturePhase -= sampleRate;
                const value = Math.max(-1, Math.min(1, this.sum / this.count));
                this.view.setInt16(this.index, Math.round(value * (value < 0 ? 32768 : 32767)), true);
                this.sum = this.count = 0;
                this.index += 2;
                if (this.index === this.frame.byteLength) {
                    this.port.postMessage(this.frame, [this.frame]);
                    this.frame = new ArrayBuffer(320);
                    this.view = new DataView(this.frame);
                    this.index = 0;
                }
            }
        }
        return true;
    }
}
registerProcessor('aide-voip-audio', AideVoIPAudio);
