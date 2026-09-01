import Guacamole from "guacamole-common-js";

export type GuacKeySink = {
  isActive: () => boolean;
  /** Return true to allow the browser default (needed for Ctrl/Cmd+V paste). */
  keydown: (keysym: number) => boolean | void;
  keyup: (keysym: number) => void;
};

type Binding = {
  keyboard: InstanceType<typeof Guacamole.Keyboard>;
  sink: GuacKeySink;
};

const bindings = new Map<HTMLElement, Binding>();

export function addGuacKeySink(el: HTMLElement, sink: GuacKeySink) {
  removeGuacKeySink(el);
  const keyboard = new Guacamole.Keyboard(el);
  keyboard.onkeydown = (keysym: number) => {
    if (!sink.isActive()) return true;
    return sink.keydown(keysym) === true;
  };
  keyboard.onkeyup = (keysym: number) => {
    if (sink.isActive()) {
      sink.keyup(keysym);
    }
    return true;
  };
  bindings.set(el, { keyboard, sink });
}

export function removeGuacKeySink(el: HTMLElement) {
  const b = bindings.get(el);
  if (!b) return;
  b.keyboard.onkeydown = null;
  b.keyboard.onkeyup = null;
  try {
    b.keyboard.reset();
  } catch {
    /* ignore */
  }
  bindings.delete(el);
}
