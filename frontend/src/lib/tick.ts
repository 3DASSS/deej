// synthesized ui tick for hardware slider movement — no audio assets needed

// minimum travel (0..1) before a slider produces another tick, so raw analog
// jitter stays silent
const STEP = 0.015;
// global cooldown so several sliders moving at once don't stack into a buzz
const COOLDOWN_MS = 30;

const TICK_FREQ_HZ = 500;
const TICK_GAIN = 0.02;
const TICK_LENGTH_S = 0.03;

let ctx: AudioContext | null = null;
let lastValues: number[] | null = null;
let lastTickAt = 0;

// feed every incoming slider snapshot here; ticks when any slider crosses a
// step boundary. the first snapshot only sets the baseline, so opening the
// window doesn't click
export function tickOnChange(values: number[]): void {
  if (lastValues === null || lastValues.length !== values.length) {
    lastValues = [...values];
    return;
  }

  let moved = false;
  for (let i = 0; i < values.length; i++) {
    if (Math.abs(values[i] - lastValues[i]) >= STEP) {
      lastValues[i] = values[i];
      moved = true;
    }
  }

  const now = performance.now();
  if (!moved || now - lastTickAt < COOLDOWN_MS) return;
  if (document.visibilityState !== "visible") return;
  lastTickAt = now;
  play();
}

function play(): void {
  try {
    ctx ??= new AudioContext();
    if (ctx.state !== "running") {
      // autoplay policy keeps the context suspended until the user interacts
      // with the window; retry on the next tick instead of queueing sounds
      void ctx.resume();
      return;
    }

    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.frequency.value = TICK_FREQ_HZ;
    gain.gain.setValueAtTime(TICK_GAIN, ctx.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + TICK_LENGTH_S);
    osc.connect(gain).connect(ctx.destination);
    osc.start();
    osc.stop(ctx.currentTime + TICK_LENGTH_S);
  } catch (err) {
    console.error("tick playback failed", err);
  }
}
