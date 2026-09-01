import Guacamole from "guacamole-common-js";

export type GuacKeySink = {
  isActive: () => boolean;
  /** Return true to allow the browser default (needed for Ctrl/Cmd+V paste). */
  keydown: (keysym: number) => boolean | void;
  keyup: (keysym: number) => void;
};

const sinks = new Set<GuacKeySink>();
let keyboard: InstanceType<typeof Guacamole.Keyboard> | null = null;

function ensureKeyboard() {
  if (keyboard) return;
  keyboard = new Guacamole.Keyboard(document);
  keyboard.onkeydown = (keysym: number) => {
    for (const s of sinks) {
      if (s.isActive()) {
        return s.keydown(keysym) === true;
      }
    }
    return true;
  };
  keyboard.onkeyup = (keysym: number) => {
    for (const s of sinks) {
      if (s.isActive()) {
        s.keyup(keysym);
        return true;
      }
    }
    return true;
  };
}

export function addGuacKeySink(sink: GuacKeySink) {
  ensureKeyboard();
  sinks.add(sink);
}

export function removeGuacKeySink(sink: GuacKeySink) {
  sinks.delete(sink);
}
