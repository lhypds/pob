// What both pages need to drive the machine: the send queue, the text field
// and the keyboard mirror. The control page adds a trackpad on top of it and
// the view page adds a picture you can click on, but the way a keystroke gets
// out of the browser is the same on both — and being the same code is the only
// way it stays that way.
//
// Loaded relatively ("pob.js"), so it is fetched from whichever address the
// page was reached by.
(function () {
  // One queue, one request at a time. Pob answers a command only once it has
  // run, so a single request in flight is what keeps the commands in the order
  // they were made — and the reply is the signal that the next one may go.
  //
  // No gap between requests, unlike the pico-hid board this came from: there
  // the wait was the board's per-key hold time, whereas Pob's reply already
  // means "done". A pause here would only make the trackpad choppy, since
  // every pointer move is a request.
  const SEND_GAP_MS = 0;
  const MAX_TRIES = 3;
  // Bound each attempt: Safari keeps half-open speculative connections a
  // request can land on, and a fetch that never settles would wedge the queue
  // for good. A timed-out attempt retries under the same seq token, so Pob
  // won't run it twice.
  const SEND_TIMEOUT_MS = 6000;

  // No physical keyboard means no Cmd+W-style teardown to survive, so
  // keepalive buys nothing — and iOS WebKit has a history of dropping
  // keepalive fetches.
  const TOUCH_ONLY = matchMedia("(hover: none) and (pointer: coarse)").matches;

  // Commands go to the instance root, which is the page's own path with its
  // name taken off: the root is where the machine takes commands, and the path
  // leading to it is what names the instance. Reading it off the location
  // rather than baking it in means the same page works however it was reached
  // — by name, by address, or through the bare root.
  const ENDPOINT = location.pathname.replace(/\/(control|view)\/?$/, "/");

  let inFlight = false;
  const queue = []; // discrete commands, delivered in order and never dropped
  let pendingDX = 0; // pointer movement, coalesced rather than queued
  let pendingDY = 0;
  let pendingTo = null; // an absolute point to go to, coalesced the same way
  let pendingScroll = 0; // wheel notches, coalesced too; positive scrolls up

  // Delivery is at-least-once: a retry can re-send a command Pob already ran
  // (response lost, not the request). Every command carries a seq token,
  // stamped once and reused on retry, and Pob skips a token it just handled.
  // The random prefix keeps tokens unique across reloads and tabs.
  const SEQ_PREFIX = Math.random().toString(36).slice(2, 7);
  let seqCounter = 0;

  function stampSeq(body) {
    seqCounter += 1;
    return "seq=" + SEQ_PREFIX + "-" + seqCounter + "&" + body;
  }

  // Resolves true/false instead of rejecting, so a failure can be retried and
  // can't surface as an unhandled rejection that stalls the pump.
  function sendCommand(body) {
    const opts = {
      method: "POST",
      body,
      // Let the request outlive the page: Cmd+W tears the tab down
      // mid-request, and without this the keystroke dies en route.
      keepalive: !TOUCH_ONLY,
    };
    if (typeof AbortSignal !== "undefined" && AbortSignal.timeout) {
      opts.signal = AbortSignal.timeout(SEND_TIMEOUT_MS);
    }
    return fetch(ENDPOINT, opts).then(
      () => true,
      () => false,
    );
  }

  function enqueue(body) {
    queue.push({ body: stampSeq(body), tries: 0, retry: true });
    pump();
  }

  // Typed characters merge into the last still-queued typing command, so fast
  // typing doesn't build one request per keystroke. Only items made here are
  // extendable — merging into a keycode= would reorder.
  function enqueueTyping(text) {
    const last = queue[queue.length - 1];
    if (last && last.canExtend) {
      last.body += text;
      return;
    }
    queue.push({ body: stampSeq("typing=" + text), tries: 0, retry: true, canExtend: true });
    pump();
  }

  function pump() {
    if (inFlight) return;

    let item = queue.shift();
    if (!item) {
      // Nothing queued: send accumulated movement or scrolling. All of it is
      // coalesced because a stale position is worth less than a fresh one, so
      // unlike a keystroke it's fine to merge or drop.
      const to = takeMoveTo();
      const dx = Math.round(pendingDX);
      const dy = Math.round(pendingDY);
      const notches = Math.round(pendingScroll);
      if (to) {
        item = { body: stampSeq(`mouse=MOVE_TO(${to[0]},${to[1]})`), tries: 0, retry: false };
      } else if (dx !== 0 || dy !== 0) {
        pendingDX -= dx;
        pendingDY -= dy;
        item = { body: stampSeq(`mouse=MOVE(${dx},${dy})`), tries: 0, retry: false };
      } else if (notches !== 0) {
        pendingScroll -= notches;
        item = { body: stampSeq(`mouse=SCROLL(0,${notches})`), tries: 0, retry: false };
      } else {
        return;
      }
    }

    // Once an attempt starts the body is frozen: a retry must resend exactly
    // what was sent the first time, or the seq dedup would eat the difference.
    item.canExtend = false;

    inFlight = true;
    sendCommand(item.body).then((ok) => {
      item.tries++;
      if (!ok && item.retry && item.tries < MAX_TRIES) queue.unshift(item);
      if (SEND_GAP_MS === 0) {
        inFlight = false;
        pump();
        return;
      }
      setTimeout(() => {
        inFlight = false;
        pump();
      }, SEND_GAP_MS);
    });
  }

  function takeMoveTo() {
    const to = pendingTo;
    pendingTo = null;
    return to;
  }

  // --- text field and keyboard mirror ------------------------------------
  // iOS "smart punctuation" rewrites quotes and dashes into typographic
  // characters; fold them back to ASCII so what lands on the target is what
  // was typed.
  const SMART_CHARS = {
    "‘": "'",
    "’": "'",
    "“": '"',
    "”": '"',
    "–": "-",
    "—": "-",
    "…": "...",
    " ": " ", // no-break space
  };
  const SMART_CHARS_RE = /[‘’“”–—… ]/g;

  // Keyed by KeyboardEvent.code rather than .key: we forward which physical
  // key was pressed, not the character it produced, so the target machine
  // applies its own layout — that's what makes shifted keys and shortcuts land
  // correctly.
  // code -> [name sent to Pob, display label]
  const KEYS = {
    Space: ["SPACE", "␣"],
    Enter: ["ENTER", "⏎"],
    NumpadEnter: ["ENTER", "⏎"],
    Tab: ["TAB", "⇥"],
    Backspace: ["BACKSPACE", "⌫"],
    Delete: ["DELETE", "⌦"],
    Escape: ["ESCAPE", "⎋"],
    Insert: ["INSERT", "Ins"],
    Home: ["HOME", "↖"],
    End: ["END", "↘"],
    PageUp: ["PAGE_UP", "⇞"],
    PageDown: ["PAGE_DOWN", "⇟"],
    CapsLock: ["CAPS_LOCK", "⇪"],
    PrintScreen: ["PRINT_SCREEN", "PrtSc"],
    ScrollLock: ["SCROLL_LOCK", "ScrLk"],
    Pause: ["PAUSE", "PAUSE"],
    ContextMenu: ["APPLICATION", "▤"],
    ArrowUp: ["UP", "↑"],
    ArrowDown: ["DOWN", "↓"],
    ArrowLeft: ["LEFT", "←"],
    ArrowRight: ["RIGHT", "→"],
    Minus: ["MINUS", "-"],
    Equal: ["EQUALS", "="],
    BracketLeft: ["LEFT_BRACKET", "["],
    BracketRight: ["RIGHT_BRACKET", "]"],
    Backslash: ["BACKSLASH", "\\"],
    Semicolon: ["SEMICOLON", ";"],
    Quote: ["QUOTE", "'"],
    Backquote: ["GRAVE_ACCENT", "`"],
    Comma: ["COMMA", ","],
    Period: ["PERIOD", "."],
    Slash: ["FORWARD_SLASH", "/"],
  };

  function keyInfo(code) {
    if (/^Key[A-Z]$/.test(code)) return [code.slice(3).toLowerCase(), code.slice(3)];
    if (/^Digit[0-9]$/.test(code)) return [code.slice(5), code.slice(5)];
    if (/^Numpad[0-9]$/.test(code)) return [code.slice(6), code.slice(6)];
    if (/^F([1-9]|1[0-9]|2[0-4])$/.test(code)) return [code, code];
    return KEYS[code] || null;
  }

  const MODIFIER_CODES = new Set([
    "ControlLeft",
    "ControlRight",
    "ShiftLeft",
    "ShiftRight",
    "AltLeft",
    "AltRight",
    "MetaLeft",
    "MetaRight",
  ]);

  // Display only — macOS-style symbols for the key readout. The chord sent to
  // Pob still uses the plain names.
  const MOD_SYMBOL = { CTRL: "^", ALT: "⌥", SHIFT: "⇧", GUI: "⌘" };

  // heldModifiers() already emits Control, Option, Shift, Command — the order
  // Apple writes them in — so a plain join matches the convention.
  function modSymbols(mods) {
    return mods.map((m) => MOD_SYMBOL[m]).join("");
  }

  function heldModifiers(e) {
    const mods = [];
    if (e.ctrlKey) mods.push("CTRL");
    if (e.altKey) mods.push("ALT");
    if (e.shiftKey) mods.push("SHIFT");
    if (e.metaKey) mods.push("GUI");
    return mods;
  }

  function foldToAscii(text) {
    return text.replace(SMART_CHARS_RE, (c) => SMART_CHARS[c]).replace(/[^\x20-\x7e]/g, "");
  }

  // Browser-level shortcuts (Cmd+W, Cmd+R, Cmd+Tab) are claimed before the
  // page sees them, so preventDefault can't stop them. Keyboard Lock is the
  // only thing that can, and it needs a secure context plus fullscreen — so it
  // engages on localhost but not on the plain-HTTP page served over the
  // network. No beforeunload guard on purpose: it would prompt on every
  // reload, and keepalive already gets the keystroke out through an accidental
  // close.
  const canLockKeys = !!(navigator.keyboard && navigator.keyboard.lock);
  let tookFullscreen = false;

  async function captureKeys() {
    if (!canLockKeys) return;
    try {
      if (!document.fullscreenElement) {
        await document.documentElement.requestFullscreen();
        tookFullscreen = true;
      }
      await navigator.keyboard.lock();
    } catch {
      // Fullscreen or lock refused — preventDefault still covers the rest.
    }
  }

  function releaseKeys() {
    if (canLockKeys) {
      try {
        navigator.keyboard.unlock();
      } catch {}
    }
    if (tookFullscreen && document.fullscreenElement) {
      tookFullscreen = false;
      document.exitFullscreen().catch(() => {});
    }
  }

  // attachInput wires up a text field and its two buttons — send and keyboard
  // mirror — which together are the whole typing half of the API. Both pages
  // have the same three, so both get them from here.
  function attachInput({ input, sendBtn, kbBtn }) {
    let mirroring = false;
    let mirrorPrev = ""; // field content already forwarded to the target
    let composing = false; // mid-IME-composition edits aren't final yet

    function sendText() {
      const text = input.value.replace(SMART_CHARS_RE, (c) => SMART_CHARS[c]);
      if (!text) return;
      enqueue("typing=" + text);
      input.value = "";
      input.focus();
    }

    function onMirrorKey(e) {
      // Swallow every key while mirroring, so shortcuts act on the target
      // machine instead of this browser. The OS keeps a few for itself
      // (Cmd+Tab, Alt+Tab) — those can't be forwarded.
      e.preventDefault();
      if (e.repeat) return; // don't flood the target with auto-repeat

      const mods = heldModifiers(e);
      if (MODIFIER_CODES.has(e.code)) {
        input.value = modSymbols(mods);
        return;
      }

      const info = keyInfo(e.code);
      if (!info) {
        input.value = e.code + " (unsupported)";
        return;
      }

      input.value = modSymbols(mods) + info[1];
      enqueue("keycode=" + mods.concat(info[0]).join("+"));
    }

    // --- Soft mirroring (touch devices) ---
    // A soft keyboard can't be mirrored key-by-key: IMEs, swipe typing and
    // autocorrect rewrite text without usable keydown codes (Android reports
    // 229/Unidentified for nearly everything). So mirror the *field* instead
    // of the keys: whatever changes in it is diffed and forwarded.
    function mirrorFieldChange() {
      const next = input.value;
      let prefix = 0;
      const max = Math.min(mirrorPrev.length, next.length);
      while (prefix < max && mirrorPrev[prefix] === next[prefix]) prefix++;
      let suffix = 0;
      while (
        suffix < max - prefix &&
        mirrorPrev[mirrorPrev.length - 1 - suffix] === next[next.length - 1 - suffix]
      )
        suffix++;
      // Replay the edit: delete what left the field, type what entered it.
      // Both land at the *target's* cursor, so this assumes typing at the end
      // of the field — which is what a phone keyboard does.
      for (let i = mirrorPrev.length - prefix - suffix; i > 0; i--) enqueue("keycode=BACKSPACE");
      const inserted = foldToAscii(next.slice(prefix, next.length - suffix));
      if (inserted) enqueueTyping(inserted);
      // The field is a display of the last keystroke, not an editor: keep just
      // the final character — still enough context to diff the next edit, and
      // it gives backspace something to delete.
      const tail = next.slice(-1);
      input.value = tail;
      mirrorPrev = tail;
    }

    function setMirroring(on) {
      mirroring = on;
      kbBtn.classList.toggle("filled", on);
      kbBtn.setAttribute("aria-pressed", String(on));
      sendBtn.classList.toggle("filled", !on);
      input.value = "";
      mirrorPrev = "";
      if (TOUCH_ONLY) {
        // Soft mirroring: the field must stay editable and focused, or the
        // soft keyboard — the thing being mirrored — would close.
        input.focus();
        return;
      }
      input.readOnly = on;
      if (on) {
        // Listen on window so keys are caught wherever focus sits, and drop
        // focus from the field so the caret doesn't imply it's editable.
        window.addEventListener("keydown", onMirrorKey);
        input.blur();
        captureKeys();
      } else {
        window.removeEventListener("keydown", onMirrorKey);
        releaseKeys();
        input.focus(); // ready to type straight away
      }
    }

    // Buttons must not take focus on tap: on iOS the blur would close the soft
    // keyboard on every send. Click still fires.
    sendBtn.addEventListener("pointerdown", (e) => e.preventDefault());
    kbBtn.addEventListener("pointerdown", (e) => e.preventDefault());

    // The two buttons form a mode switch, so in mirror mode this returns to
    // input mode instead of sending — the field holds a key display, not text.
    sendBtn.addEventListener("click", () => {
      if (mirroring) setMirroring(false);
      else sendText();
    });
    kbBtn.addEventListener("click", () => setMirroring(!mirroring));

    input.addEventListener("keydown", (e) => {
      if (!mirroring) {
        if (e.key === "Enter") sendText();
        return;
      }
      // Physical-keyboard mirroring is handled by the window listener.
      if (!TOUCH_ONLY || e.isComposing) return;
      if (e.key === "Enter") {
        e.preventDefault();
        enqueue("keycode=ENTER");
        // A sent line is finished context; start the next one clean.
        input.value = "";
        mirrorPrev = "";
      } else if (e.key === "Backspace" && input.value === "") {
        // Nothing local to delete means no input event will follow, so the
        // field diff below can't see this one; forward it directly.
        enqueue("keycode=BACKSPACE");
      }
    });

    input.addEventListener("compositionstart", () => {
      composing = true;
    });
    input.addEventListener("compositionend", () => {
      composing = false;
      if (mirroring && TOUCH_ONLY) mirrorFieldChange();
    });
    input.addEventListener("input", (e) => {
      if (!mirroring || !TOUCH_ONLY) return;
      if (composing || e.isComposing) return;
      mirrorFieldChange();
    });

    return { isMirroring: () => mirroring };
  }

  window.Pob = {
    ENDPOINT,
    TOUCH_ONLY,
    enqueue,
    enqueueTyping,
    pump,
    attachInput,

    // Coalesced pointer state. The pages accumulate into it and call pump();
    // whatever has piled up by the time a request slot frees goes out as one
    // command, since a stale position is worth less than a fresh one.
    moveBy(dx, dy) {
      pendingDX += dx;
      pendingDY += dy;
    },
    resetMove() {
      pendingDX = 0;
      pendingDY = 0;
    },
    // takeMove hands back the whole accumulated move so a caller can put it in
    // the queue itself — which is what a drag does on release, so the drop
    // lands where the finger stopped rather than a few pixels after the button
    // is already up.
    takeMove() {
      const dx = Math.round(pendingDX);
      const dy = Math.round(pendingDY);
      pendingDX -= dx;
      pendingDY -= dy;
      return [dx, dy];
    },
    moveTo(x, y) {
      pendingTo = [Math.round(x), Math.round(y)];
    },
    // flushMoveTo puts a coalesced absolute move in the queue, so what comes
    // next is ordered after it — a release must not overtake the move that
    // says where it happened.
    flushMoveTo() {
      const to = takeMoveTo();
      if (to) enqueue(`mouse=MOVE_TO(${to[0]},${to[1]})`);
    },
    scrollBy(notches) {
      pendingScroll += notches;
    },
  };
})();
