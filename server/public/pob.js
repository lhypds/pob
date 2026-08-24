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

  function resetMove() {
    pendingDX = 0;
    pendingDY = 0;
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

  // Browser-level shortcuts (Cmd+W, Cmd+R, Cmd+T) are claimed whatever the page
  // says, so preventDefault can't stop them and the browser acts on them as
  // well as the machine. The one thing that could stop it is Keyboard Lock, and
  // it only takes effect in fullscreen — which meant the page swallowed the
  // screen the moment the keyboard button was pressed, a far bigger surprise
  // than those few keys are worth.
  //
  // So the page says nothing about it, and that is a decision rather than an
  // omission: a toast naming the chord was written and never got read. The
  // shortcuts worth warning about are the ones that close the tab or reload it,
  // and both take the warning with them; the two that carry nothing at all —
  // Cmd+Tab, Alt+Tab — are the window manager's and never reach the page to be
  // remarked on in the first place. Nothing said at the moment of the keystroke
  // can survive the keystroke, so it is said in the docs instead.
  //
  // No beforeunload guard on purpose either: it would prompt on every reload,
  // and keepalive already gets the keystroke out through an accidental close.

  // --- the trackpad ---------------------------------------------------------
  // Pointing without a picture: the surface is a pad, not the machine's screen,
  // so what goes out is how far the finger travelled rather than where it
  // landed. The control page is nothing but this pad; the view page offers it
  // as one of its two ways of pointing, for when the picture is too small to
  // hit a target on — which on a phone it usually is. Both get it from here,
  // because a trackpad that behaves differently depending on which page it is
  // on is two trackpads.
  //
  // attachTrackpad returns a handle: detach() takes the listeners off and lets
  // go of anything still held, which is what a page switching pointing modes
  // mid-gesture needs.
  const CLICK_MOVE_THRESHOLD = 6; // px per finger below which a gesture is a tap
  const DOUBLE_CLICK_WINDOW_MS = 300;
  const SCROLL_PX_PER_NOTCH = 20; // finger travel that equals one wheel notch

  function attachTrackpad(surface, { onActive = () => {} } = {}) {
    const pointers = new Map(); // fingers on the pad: pointerId -> last position
    let primaryId = null; // the finger whose motion drives moves and scrolls
    let gestureFingers = 0; // most fingers seen during the current gesture
    let totalDX = 0,
      totalDY = 0;
    let pendingClickTimer = null;
    // Double-tap-and-hold drags: the second touch of a double-tap presses
    // the button the moment it lands, so the grab happens at the
    // double-click point. Motion then drags; lifting lets go. A quick lift
    // in place completes a double-click instead (see endGesture).
    let dragging = false; // Pob is holding the button; motion now drags
    let pressedAt = 0; // when the drag press went down, to tell tap from hold

    function onPointerDown(e) {
      // Don't steal focus: mid-mirroring, a tap here would otherwise blur
      // the text field and close the soft keyboard being mirrored.
      e.preventDefault();
      surface.setPointerCapture(e.pointerId);
      onActive(true);
      pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
      gestureFingers = Math.max(gestureFingers, pointers.size);
      if (pointers.size === 1) {
        primaryId = e.pointerId;
        totalDX = 0;
        totalDY = 0;
        resetMove();
        // A touch inside the double-click window is the second half of a
        // double-tap: claim the pending single click and press right away,
        // so the grab happens at the double-click point — waiting for
        // movement instead loses the start of a fast drag to the slop test.
        if (pendingClickTimer && e.button === 0) {
          clearTimeout(pendingClickTimer);
          pendingClickTimer = null;
          dragging = true;
          pressedAt = performance.now();
          enqueue("mouse=PRESS(0,0)");
        }
      } else {
        // A second finger turns the gesture into a scroll (or two-finger
        // tap): the first finger's accumulated movement is no longer a
        // pointer move.
        resetMove();
        // Fingers landing one-by-one right after a tap are that scroll,
        // not a drag: let the still-unmoved button go. The press+release
        // pair amounts to the single click the claimed tap was owed.
        if (dragging && Math.hypot(totalDX, totalDY) <= CLICK_MOVE_THRESHOLD) {
          dragging = false;
          enqueue("mouse=RELEASE(0,0)");
        }
      }
    }

    function onPointerMove(e) {
      const p = pointers.get(e.pointerId);
      if (!p) return;
      const dx = e.clientX - p.x;
      const dy = e.clientY - p.y;
      p.x = e.clientX;
      p.y = e.clientY;
      totalDX += dx;
      totalDY += dy;
      if (e.pointerId !== primaryId) return; // fingers travel together; count one
      if (pointers.size >= 2 && !dragging) {
        // Two-finger drag scrolls, touch-style: content follows the fingers.
        pendingScroll += dy / SCROLL_PX_PER_NOTCH;
      } else if (dragging && Math.hypot(totalDX, totalDY) <= CLICK_MOVE_THRESHOLD) {
        // Wobble under a still-unmoved press is held back: this may yet
        // end as a double-click, which the host voids if the pointer
        // strays between the clicks. Only sub-slop wobble is ever
        // swallowed — the event that passes the slop flows through below
        // whole, so a fast drag loses none of its motion.
        return;
      } else {
        pendingDX += dx;
        pendingDY += dy;
      }
      pump();
    }

    function dropPointer(e) {
      if (!pointers.delete(e.pointerId)) return false;
      if (pointers.size > 0) {
        // Hand the lead to a remaining finger so motion keeps flowing.
        if (e.pointerId === primaryId) primaryId = pointers.keys().next().value;
        return false;
      }
      primaryId = null;
      onActive(false);
      pump();
      return true; // gesture over
    }

    function endGesture(e) {
      if (!dropPointer(e)) return;
      const fingers = gestureFingers;
      gestureFingers = 0;

      if (dragging) {
        dragging = false;
        const quickTap = performance.now() - pressedAt < DOUBLE_CLICK_WINDOW_MS;
        if (quickTap && Math.hypot(totalDX, totalDY) <= CLICK_MOVE_THRESHOLD) {
          // A quick lift in place: it was a double-tap after all.
          enqueue("mouse=DOUBLE_CLICK(0,0)");
        } else {
          // A drag (or a deliberate press-and-hold, released as just the
          // one click). Flush motion still coalesced, so the drop lands
          // where the finger stopped instead of the last few pixels
          // applying after the button is already up.
          const dx = Math.round(pendingDX);
          const dy = Math.round(pendingDY);
          resetMove();
          if (dx !== 0 || dy !== 0) enqueue(`mouse=MOVE(${dx},${dy})`);
          enqueue("mouse=RELEASE(0,0)");
        }
        return;
      }

      // Looser tap test for multi-finger taps: each finger wobbles a little.
      if (Math.hypot(totalDX, totalDY) > CLICK_MOVE_THRESHOLD * fingers) return;

      // Touch has no right button, so a two-finger tap fills in for it.
      if (e.button === 2 || fingers === 2) {
        enqueue("mouse=RIGHT_CLICK(0,0)");
        return;
      }
      if (fingers > 2) return; // 3+ finger tap has no meaning here

      pendingClickTimer = setTimeout(() => {
        pendingClickTimer = null;
        enqueue("mouse=CLICK(0,0)");
      }, DOUBLE_CLICK_WINDOW_MS);
    }

    // A cancelled gesture (the system took over the touch, e.g. an edge
    // swipe) must not turn into a click — but a held button must still be
    // let go, or it stays stuck down on the target machine.
    function cancelGesture(e) {
      if (!dropPointer(e)) return;
      gestureFingers = 0;
      if (dragging) {
        dragging = false;
        enqueue("mouse=RELEASE(0,0)");
      }
    }

    // Physical mouse wheel / desktop trackpad scrolling over the pad. It
    // scrolls wherever the machine's own pointer already is: this surface says
    // nothing about where on the screen the wheel was turned.
    function onWheel(e) {
      e.preventDefault(); // the page itself has nowhere to scroll
      // deltaMode 1 is lines (~3 per wheel notch); pixels (~100) otherwise.
      pendingScroll += -(e.deltaMode === 1 ? e.deltaY / 3 : e.deltaY / 100);
      pump();
    }

    const listeners = [
      ["pointerdown", onPointerDown, undefined],
      ["pointermove", onPointerMove, undefined],
      ["pointerup", endGesture, undefined],
      ["pointercancel", cancelGesture, undefined],
      ["wheel", onWheel, { passive: false }],
    ];
    for (const [type, fn, opts] of listeners) surface.addEventListener(type, fn, opts);

    return {
      detach() {
        for (const [type, fn, opts] of listeners) surface.removeEventListener(type, fn, opts);
        clearTimeout(pendingClickTimer);
        pendingClickTimer = null;
        pointers.clear();
        primaryId = null;
        gestureFingers = 0;
        onActive(false);
        // A mode switch mid-drag must not leave the button down on the
        // machine: nothing else is going to release it.
        if (dragging) {
          dragging = false;
          enqueue("mouse=RELEASE(0,0)");
        }
        resetMove();
      },
    };
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
      } else {
        window.removeEventListener("keydown", onMirrorKey);
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
    attachTrackpad,

    // Coalesced pointer state. The pages accumulate into it and call pump();
    // whatever has piled up by the time a request slot frees goes out as one
    // command, since a stale position is worth less than a fresh one.
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
