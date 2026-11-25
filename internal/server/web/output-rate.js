"use strict";
(function () {
    const windowMs = 3000;
    const refreshMs = 250;
    function create(options) {
        const now = options.now || (() => Date.now());
        const schedule = options.schedule || ((callback) => window.setTimeout(callback, refreshMs));
        const cancel = options.cancel || ((timer) => window.clearTimeout(timer));
        const streams = new Map();
        let timer = null;
        function snapshot() {
            const timestamp = now();
            let tokens = 0;
            let active = 0;
            let earliest = timestamp;
            for (const [id, stream] of streams) {
                stream.samples = stream.samples.filter(sample => timestamp - sample.at <= windowMs);
                if (!stream.samples.length) {
                    streams.delete(id);
                    continue;
                }
                const subtotal = stream.samples.reduce((sum, sample) => sum + sample.tokens, 0);
                if (subtotal <= 0)
                    continue;
                tokens += subtotal;
                active++;
                earliest = Math.min(earliest, stream.samples[0].at);
            }
            // Avoid a spike on the first packet; after three seconds this becomes a
            // true rolling-window rate. All active request subtotals are summed first.
            const elapsed = Math.min(windowMs, Math.max(1000, timestamp - earliest)) / 1000;
            return { rate: tokens > 0 ? tokens / elapsed : 0, tokens, active };
        }
        function render() {
            timer = null;
            const current = snapshot();
            if (!current.tokens) {
                options.element.hidden = true;
                options.element.textContent = '';
                return;
            }
            const formatted = current.rate >= 100 ? String(Math.round(current.rate)) : current.rate.toFixed(1);
            let text = options.translate('会话输出 ≈ {0} tok/s', formatted);
            if (current.active > 1)
                text += ' · ' + options.translate('{0} 路合计', current.active);
            options.element.textContent = text;
            options.element.title = options.translate('当前会话全部模型请求的输出 Token；多路并行时求和，提供商用量返回后自动校准');
            options.element.hidden = false;
            if (timer == null)
                timer = schedule(render);
        }
        function scheduleRender() {
            if (timer == null)
                timer = schedule(render);
        }
        function record(streamId, tokens) {
            const id = String(streamId || '').slice(0, 200);
            const increment = Math.trunc(Number(tokens));
            if (!id || !Number.isFinite(increment) || increment === 0 || Math.abs(increment) > 1000000)
                return;
            let stream = streams.get(id);
            if (!stream) {
                if (streams.size >= 64) {
                    const oldest = streams.keys().next().value;
                    if (oldest)
                        streams.delete(oldest);
                }
                stream = { samples: [] };
                streams.set(id, stream);
            }
            stream.samples.push({ at: now(), tokens: increment });
            scheduleRender();
        }
        function reset() {
            streams.clear();
            if (timer != null)
                cancel(timer);
            timer = null;
            options.element.hidden = true;
            options.element.textContent = '';
        }
        return { record, reset, render, snapshot };
    }
    window.AideOutputRate = { create };
})();
