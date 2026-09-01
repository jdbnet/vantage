export type PopoutHandle = {
  win: Window;
  mount: HTMLElement;
  close: () => void;
};

function copyStyles(from: Document, to: Document) {
  to.documentElement.className = from.documentElement.className;
  to.documentElement.style.cssText = from.documentElement.style.cssText;
  for (const node of from.head.querySelectorAll('link[rel="stylesheet"], style')) {
    to.head.appendChild(node.cloneNode(true));
  }
}

export function openPopout(opts: {
  name: string;
  title: string;
  onClose: () => void;
}): PopoutHandle | null {
  const win = window.open(
    "about:blank",
    opts.name,
    "popup=yes,width=1280,height=800,menubar=no,toolbar=no,location=no,status=no,scrollbars=yes,resizable=yes",
  );
  if (!win) return null;

  const doc = win.document;
  doc.open();
  doc.write(
    '<!DOCTYPE html><html class="dark"><head><meta charset="utf-8"></head><body></body></html>',
  );
  doc.close();
  copyStyles(document, doc);
  const css = doc.createElement("style");
  css.textContent =
    "html,body{margin:0;height:100%;width:100%;background:#0d1117;overflow:hidden;color-scheme:dark}" +
    "#popout-root{height:100%;width:100%;display:flex;min-height:0;min-width:0}";
  doc.head.appendChild(css);
  doc.title = opts.title;
  doc.body.style.margin = "0";
  doc.body.style.height = "100%";
  doc.body.style.background = "#0d1117";

  const mount = doc.createElement("div");
  mount.id = "popout-root";
  doc.body.appendChild(mount);

  let closed = false;
  const notifyClosed = () => {
    if (closed) return;
    closed = true;
    window.clearInterval(closedPoll);
    opts.onClose();
  };
  const closedPoll = window.setInterval(() => {
    if (win.closed) notifyClosed();
  }, 400);
  win.addEventListener("beforeunload", notifyClosed);
  win.addEventListener("pagehide", notifyClosed);

  return {
    win,
    mount,
    close() {
      closed = true;
      window.clearInterval(closedPoll);
      win.removeEventListener("beforeunload", notifyClosed);
      win.removeEventListener("pagehide", notifyClosed);
      try {
        win.close();
      } catch {
        /* ignore */
      }
    },
  };
}
