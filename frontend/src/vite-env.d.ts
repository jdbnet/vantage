/// <reference types="vite/client" />

declare module "guacamole-common-js" {
  const Guacamole: {
    WebSocketTunnel: new (url: string) => {
      onerror: ((e: { message?: string; code?: number }) => void) | null;
    };
    Client: new (tunnel: unknown) => {
      getDisplay(): {
        getElement(): HTMLElement;
        getWidth(): number;
        getHeight(): number;
        getScale(): number;
        scale(scale: number): void;
        onresize: ((width: number, height: number) => void) | null;
      };
      connect(data?: string): void;
      disconnect(): void;
      sendSize(width: number, height: number): void;
      onerror: ((e: { message?: string; code?: number }) => void) | null;
      onstatechange: ((state: number) => void) | null;
      onclipboard: ((stream: unknown, mimetype: string) => void) | null;
      sendMouseState(state: unknown, applyDisplayScale?: boolean): void;
      sendKeyEvent(pressed: number, keysym: number): void;
      createClipboardStream(mimetype: string): {
        index: number;
      };
    };
    Mouse: new (el: HTMLElement) => {
      onmousedown: ((s: unknown) => void) | null;
      onmouseup: ((s: unknown) => void) | null;
      onmousemove: ((s: unknown) => void) | null;
    };
    Keyboard: new (el: Document | HTMLElement) => {
      onkeydown: ((keysym: number) => boolean) | null;
      onkeyup: ((keysym: number) => boolean) | null;
      reset(): void;
    };
    StringReader: new (stream: unknown) => {
      ontext: ((text: string) => void) | null;
      onend: (() => void) | null;
    };
    StringWriter: new (stream: unknown) => {
      sendText(text: string): void;
      sendEnd(): void;
    };
  };
  export default Guacamole;
}
