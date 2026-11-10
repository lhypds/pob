#!/usr/bin/env bash
# pobctl — drive this computer through Pob, for the `pob` skill.
#
# Starts Pob with the system `pob` command when it is not up, and then talks to
# the Pob server's Operation API (docs/Pob/10_Operation API.md) with curl:
# GET is a picture of what Pob frames, POST is a pointer or keyboard command.
#
# Every coordinate this script takes is a pixel of the picture the last `shot`
# saved. Pob itself works in the frame's own pixels, which on a HiDPI screen is
# a bigger grid than the picture's, so `shot` remembers the ratio and every
# command after it converts.
#
# Needs: pob, curl, and a bash (Git Bash on Windows).

set -euo pipefail

SHOT_MAX="${POB_SHOT_MAX:-1500}"   # long edge of a shot, small enough that no host resizes it again
STATE_DIR="${TMPDIR:-/tmp}/pob-skill"

die() { echo "pobctl: $*" >&2; exit 1; }

usage() {
    cat <<'EOF'
Usage: pobctl.sh <command> [args]

Pob itself:
  up [--window] [instance]   Start Pob if it is not running (fullscreen unless
                             --window), and say what it frames
  status                     What the running Pob is doing (JSON)
  down                       Quit Pob

Looking:
  shot [file.png]            Save a picture of what Pob frames; prints its path
                             and size. Coordinates below are pixels of it
  zoom x y w h [file.png]    Save that region of the last shot at full
                             resolution, for text the shot left too small

Pointer (x y = pixels of the last shot, origin top-left):
  move x y                   Move the cursor there
  click [x y]                Click there — or where the cursor is
  rclick [x y]               Right-click
  dclick [x y]               Double-click
  drag x1 y1 x2 y2           Press at the first point, release at the second
  scroll [x y] up|down|left|right [notches]
                             Scroll at that point, 3 notches unless said

Keyboard:
  type <text>                Type text at the keyboard focus
  key <chord> [chord...]     Press keys in turn: return, escape, tab, cmd+v,
                             ctrl+shift+t, alt+f4, f5, up, pagedown …

Environment:
  POB_SERVER_PORT   the Pob server's port (default: settings.json, then 8033)
  POB_SHOT_MAX      long edge of a shot in pixels (default 1500)
  POB_DRY_RUN=1     print the commands instead of sending them
EOF
}

# ── where the server is ──────────────────────────────────────────────────────

server_port() {
    if [ -n "${POB_SERVER_PORT:-}" ]; then
        echo "$POB_SERVER_PORT"
        return
    fi
    local port=""
    if [ -f "$HOME/.pob/settings.json" ]; then
        port=$(sed -n 's/.*"server_port"[^0-9]*\([0-9][0-9]*\).*/\1/p' "$HOME/.pob/settings.json" | head -n 1)
    fi
    echo "${port:-8033}"
}

BASE="http://127.0.0.1:$(server_port)"

server_up() { curl -fsS -m 3 -o /dev/null "$BASE/status" 2>/dev/null; }

instance_id() {
    local id
    id=$(curl -fsS -m 5 "$BASE/status" 2>/dev/null | sed -n 's/.*"instance":"\([^"]*\)".*/\1/p')
    [ -n "$id" ] || die "Pob is not answering on $BASE — run: pobctl.sh up"
    echo "$id"
}

post() {
    if [ -n "${POB_DRY_RUN:-}" ]; then
        printf '%s\n' "$1"
        return
    fi
    curl -fsS -m 30 -o /dev/null -H 'Content-Type: text/plain' --data-binary "$1" "$BASE/$ID/" \
        || die "Pob did not take the command: $1"
}

# ── the picture, and its scale ───────────────────────────────────────────────

header() { tr -d '\r' < "$1" | sed -n "s/^$2: *//Ip" | tail -n 1; }

# capture_failed says why no picture came back, which on a Mac is nearly always
# a permission nobody has granted: the app logs it at every start.
capture_failed() {
    local reason=""
    if [ -f "$HOME/.pob/app.log" ]; then
        reason=$(tail -n 20 "$HOME/.pob/app.log" | grep 'Permissions not granted' | tail -n 1 | sed 's/^\[[^]]*\] *//')
    fi
    if [ -n "$reason" ]; then
        die "Pob could not capture the view. Its log says: $reason
Grant them to Pob.app in System Settings > Privacy & Security (Accessibility, Screen Recording), then: pob relaunch --fullscreen"
    fi
    die "Pob could not capture the view — see ~/.pob/app.log"
}

# source_size sets SW and SH to the size of what Pob frames, in its own pixels.
source_size() {
    local headers="$STATE_DIR/headers.$$"
    if ! curl -fsS -m 25 -o /dev/null -D "$headers" "$BASE/$ID/?format=jpeg&w=16&q=1" 2>/dev/null; then
        rm -f "$headers"
        capture_failed
    fi
    SW=$(header "$headers" X-Pob-Source-Width)
    SH=$(header "$headers" X-Pob-Source-Height)
    rm -f "$headers"
    [ -n "$SW" ] && [ -n "$SH" ] || die "the frame came back without its size"
}

# png_size prints "<width> <height>" read from a PNG's own header.
png_size() {
    local b
    # shellcheck disable=SC2207
    b=($(od -An -tu1 -j16 -N8 "$1"))
    [ "${#b[@]}" -eq 8 ] || die "$1 is not a PNG"
    echo "$(( (b[0] << 24) | (b[1] << 16) | (b[2] << 8) | b[3] )) $(( (b[4] << 24) | (b[5] << 16) | (b[6] << 8) | b[7] ))"
}

cmd_shot() {
    local out="${1:-$STATE_DIR/shot-$(date +%Y%m%d-%H%M%S).png}"
    local sw sh long want size pw ph
    source_size
    sw=$SW sh=$SH
    long=$(( sw > sh ? sw : sh ))
    want=$sw
    if [ "$long" -gt "$SHOT_MAX" ]; then
        want=$(( sw * SHOT_MAX / long ))
    fi
    curl -fsS -m 30 -o "$out" "$BASE/$ID/?format=png&w=$want" 2>/dev/null || capture_failed
    size=$(png_size "$out")
    pw=${size% *} ph=${size#* }
    echo "$sw $pw" > "$STATE_DIR/$ID.scale"
    echo "$out"
    echo "picture: ${pw}x${ph}  (Pob frames ${sw}x${sh}; give coordinates in picture pixels)"
}

# crop_png cuts <x> <y> <w> <h> out of a PNG with whatever this machine has
# for it, and brings the long edge down to SHOT_MAX when the piece is bigger.
crop_png() {
    local in="$1" out="$2" x="$3" y="$4" w="$5" h="$6"
    if command -v sips >/dev/null 2>&1; then
        sips -c "$h" "$w" --cropOffset "$y" "$x" "$in" --out "$out" >/dev/null 2>&1 || die "sips could not crop the picture"
        if [ "$w" -gt "$SHOT_MAX" ] || [ "$h" -gt "$SHOT_MAX" ]; then
            sips -Z "$SHOT_MAX" "$out" >/dev/null 2>&1 || die "sips could not resize the picture"
        fi
    elif command -v magick >/dev/null 2>&1; then
        magick "$in" -crop "${w}x${h}+${x}+${y}" +repage -resize "${SHOT_MAX}x${SHOT_MAX}>" "$out"
    elif command -v convert >/dev/null 2>&1 && convert -version 2>/dev/null | grep -q ImageMagick; then
        convert "$in" -crop "${w}x${h}+${x}+${y}" +repage -resize "${SHOT_MAX}x${SHOT_MAX}>" "$out"
    elif command -v powershell.exe >/dev/null 2>&1; then
        local win_in win_out
        win_in=$(cygpath -w "$in" 2>/dev/null || echo "$in")
        win_out=$(cygpath -w "$out" 2>/dev/null || echo "$out")
        POB_IN="$win_in" POB_OUT="$win_out" powershell.exe -NoProfile -Command "
            Add-Type -AssemblyName System.Drawing
            \$src = [System.Drawing.Image]::FromFile(\$env:POB_IN)
            \$f = [Math]::Min(1.0, $SHOT_MAX / [Math]::Max($w, $h))
            \$dst = New-Object System.Drawing.Bitmap ([int]($w * \$f)), ([int]($h * \$f))
            \$g = [System.Drawing.Graphics]::FromImage(\$dst)
            \$g.InterpolationMode = 'HighQualityBicubic'
            \$g.DrawImage(\$src, (New-Object System.Drawing.Rectangle 0, 0, \$dst.Width, \$dst.Height), (New-Object System.Drawing.Rectangle $x, $y, $w, $h), 'Pixel')
            \$dst.Save(\$env:POB_OUT, [System.Drawing.Imaging.ImageFormat]::Png)
            \$g.Dispose(); \$dst.Dispose(); \$src.Dispose()
        " || die "PowerShell could not crop the picture"
    else
        die "nothing here can crop a picture (sips, ImageMagick or PowerShell) — work from the whole shot instead"
    fi
}

# zoom saves one region of the last shot at the frame's full resolution, for
# reading what a shrunk shot left too small. The region is in shot pixels, and
# so is every coordinate afterwards: the zoom is for looking, and its output
# says how a point on it maps back.
cmd_zoom() {
    [ "$#" -ge 4 ] || die "zoom takes x y width height [file.png], in pixels of the last shot"
    local out="${5:-$STATE_DIR/zoom-$(date +%Y%m%d-%H%M%S).png}"
    local full="$STATE_DIR/full.$$.png" sw pw size fw fh x y w h zw
    [ -f "$STATE_DIR/$ID.scale" ] || die "no shot taken yet — the region is in pixels of a shot, so run: pobctl.sh shot"
    read -r sw pw < "$STATE_DIR/$ID.scale"
    case "$1$2$3$4" in
        *[!0-9]*) die "zoom takes whole pixels of the last shot, got: $1 $2 $3 $4" ;;
    esac
    curl -fsS -m 60 -o "$full" "$BASE/$ID/?format=png" 2>/dev/null || capture_failed
    size=$(png_size "$full")
    fw=${size% *} fh=${size#* }
    x=$(( $1 * sw / pw )) y=$(( $2 * sw / pw ))
    w=$(( $3 * sw / pw )) h=$(( $4 * sw / pw ))
    [ "$x" -lt "$fw" ] && [ "$y" -lt "$fh" ] && [ "$w" -gt 0 ] && [ "$h" -gt 0 ] || { rm -f "$full"; die "that region is outside the shot"; }
    [ $(( x + w )) -le "$fw" ] || w=$(( fw - x ))
    [ $(( y + h )) -le "$fh" ] || h=$(( fh - y ))
    crop_png "$full" "$out" "$x" "$y" "$w" "$h"
    rm -f "$full"
    size=$(png_size "$out")
    zw=${size% *}
    echo "$out"
    echo "picture: ${zw}x${size#* }, the region $1,$2 ${3}x${4} of the shot"
    # zoom pixels per shot pixel, to two places
    local f100=$(( zw * 100 * sw / (w * pw) ))
    echo "to act on a point (zx, zy) of this picture, use shot coordinates: x = $1 + zx / $(( f100 / 100 )).$(printf '%02d' $(( f100 % 100 ))),  y = $2 + zy / $(( f100 / 100 )).$(printf '%02d' $(( f100 % 100 )))"
}

# to_source converts picture pixels to the frame's own. Sets SX and SY.
to_source() {
    local sw pw
    [ -f "$STATE_DIR/$ID.scale" ] || die "no shot taken yet — coordinates are pixels of a shot, so run: pobctl.sh shot"
    read -r sw pw < "$STATE_DIR/$ID.scale"
    case "$1$2" in
        *[!0-9]*|"") die "coordinates are whole pixels of the last shot, got: $1 $2" ;;
    esac
    SX=$(( ($1 * sw + pw / 2) / pw ))
    SY=$(( ($2 * sw + pw / 2) / pw ))
}

move_to() {
    to_source "$1" "$2"
    post "mouse=MOVE_TO($SX,$SY)"
}

# pointer_action moves first when a point was given, then acts where the cursor is.
pointer_action() {
    local action="$1"
    shift
    case "$#" in
        0) ;;
        2) move_to "$1" "$2" ;;
        *) die "give both x and y, or neither" ;;
    esac
    post "mouse=$action(0,0)"
}

cmd_drag() {
    [ "$#" -eq 4 ] || die "drag takes x1 y1 x2 y2"
    move_to "$1" "$2"
    post "mouse=PRESS(0,0)"
    move_to "$3" "$4"
    post "mouse=RELEASE(0,0)"
}

cmd_scroll() {
    if [ "$#" -ge 3 ] && [[ "$1" =~ ^[0-9]+$ ]]; then
        move_to "$1" "$2"
        shift 2
    fi
    local direction="${1:-}" notches="${2:-3}"
    [[ "$notches" =~ ^[0-9]+$ ]] || die "notches is a whole number, got: $notches"
    # The API counts wheel notches, positive y scrolling up.
    case "$direction" in
        up)    post "mouse=SCROLL(0,$notches)" ;;
        down)  post "mouse=SCROLL(0,-$notches)" ;;
        left)  post "mouse=SCROLL(-$notches,0)" ;;
        right) post "mouse=SCROLL($notches,0)" ;;
        *) die "scroll [x y] up|down|left|right [notches]" ;;
    esac
}

# ── keys ─────────────────────────────────────────────────────────────────────

upper() { printf '%s' "$1" | tr '[:lower:]' '[:upper:]'; }
lower() { printf '%s' "$1" | tr '[:upper:]' '[:lower:]'; }

# hid_chord turns a chord written the way Pob's docs write one (cmd+shift+t,
# docs/Pob/04_Keys.md) into the HID spelling the Operation API takes.
hid_chord() {
    local chord="$1" key mods="" out="" part
    case "$chord" in
        "+")  key="+" ;;
        *"++") key="+"; mods="${chord%++}" ;;
        *+*)  key="${chord##*+}"; mods="${chord%+*}" ;;
        *)    key="$chord" ;;
    esac

    local IFS='+'
    for part in $mods; do
        case "$(lower "$part")" in
            cmd|command)
                # The ordinary-shortcut modifier: Command on a Mac, Ctrl elsewhere.
                if [ "$(uname -s)" = "Darwin" ]; then out="${out}GUI+"; else out="${out}CTRL+"; fi ;;
            ctrl|control)        out="${out}CTRL+" ;;
            alt|option|opt)      out="${out}ALT+" ;;
            shift)               out="${out}SHIFT+" ;;
            gui|win|super|meta)  out="${out}GUI+" ;;
            *) die "unknown modifier \"$part\" in \"$chord\"" ;;
        esac
    done

    if [ "${#key}" -eq 1 ]; then
        # A single character is that character's key. "," is the API's own
        # separator, so it goes by name.
        [ "$key" = "," ] && key="COMMA"
        # "+" joins a chord there too, so under a modifier it goes by the key
        # a US board has it on.
        if [ "$key" = "+" ] && [ -n "$out" ]; then key="SHIFT+EQUALS"; fi
    else
        case "$(lower "$key")" in
            return|enter)       key="ENTER" ;;
            escape|esc)         key="ESCAPE" ;;
            backspace|delete)   key="BACKSPACE" ;;
            forwarddelete|del)  key="DELETE" ;;
            pageup)             key="PAGE_UP" ;;
            pagedown)           key="PAGE_DOWN" ;;
            capslock)           key="CAPS_LOCK" ;;
            printscreen)        key="PRINT_SCREEN" ;;
            scrolllock)         key="SCROLL_LOCK" ;;
            menu)               key="APPLICATION" ;;
            leftbracket)        key="LEFT_BRACKET" ;;
            rightbracket)       key="RIGHT_BRACKET" ;;
            grave)              key="GRAVE_ACCENT" ;;
            slash)              key="FORWARD_SLASH" ;;
            tab|space|insert|home|end|pause|up|down|left|right|minus|equals|backslash|semicolon|quote|comma|period)
                                key="$(upper "$key")" ;;
            f[0-9]|f1[0-9]|f2[0-4])
                                key="$(upper "$key")" ;;
            *) die "unknown key \"$key\" — see docs/Pob/04_Keys.md; for text use: pobctl.sh type" ;;
        esac
    fi
    printf '%s%s' "$out" "$key"
}

cmd_key() {
    [ "$#" -ge 1 ] || die "key takes at least one chord, e.g. key cmd+v"
    local chord hid
    for chord in "$@"; do
        hid=$(hid_chord "$chord")
        post "keycode=$hid"
    done
}

# ── Pob itself ───────────────────────────────────────────────────────────────

cmd_up() {
    local mode="--fullscreen" args=()
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --window)     mode="" ;;
            --fullscreen) mode="--fullscreen" ;;
            *)            args+=("$1") ;;
        esac
        shift
    done

    if server_up; then
        echo "Pob is already running."
    else
        command -v pob >/dev/null 2>&1 || die "the pob command is not on the PATH — install Pob first (see the README)"
        if pob status >/dev/null 2>&1; then
            die "Pob is running but its server is not answering on $BASE — is \"server\": false in ~/.pob/settings.json?"
        fi
        # stdin is a pipe: with nobody to ask, launch starts the current
        # instance instead of putting up its list.
        : | pob launch $mode ${args[@]+"${args[@]}"}
        local tries=0
        until server_up; do
            tries=$(( tries + 1 ))
            [ "$tries" -le 50 ] || die "Pob started but its server is not answering on $BASE — is \"server\": false in ~/.pob/settings.json?"
            sleep 0.3
        done
    fi

    ID=$(instance_id)
    echo "instance: $ID"
    echo "server:   $BASE/$ID/"
    source_size
    echo "frames:   ${SW}x${SH} pixels"
}

# ── main ─────────────────────────────────────────────────────────────────────

command="${1:-help}"
[ "$#" -gt 0 ] && shift
mkdir -p "$STATE_DIR"

case "$command" in
    help|-h|--help) usage; exit 0 ;;
    up)             cmd_up "$@"; exit 0 ;;
    down)           exec pob kill ;;
esac

if [ -n "${POB_DRY_RUN:-}" ] && [ "$command" = "key" -o "$command" = "type" ]; then
    ID="dry-run"
else
    ID=$(instance_id)
fi

case "$command" in
    status) curl -fsS -m 5 "$BASE/status"; echo ;;
    shot)   cmd_shot "$@" ;;
    zoom)   cmd_zoom "$@" ;;
    move)   [ "$#" -eq 2 ] || die "move takes x y"; move_to "$1" "$2" ;;
    click)  pointer_action CLICK "$@" ;;
    rclick) pointer_action RIGHT_CLICK "$@" ;;
    dclick) pointer_action DOUBLE_CLICK "$@" ;;
    drag)   cmd_drag "$@" ;;
    scroll) cmd_scroll "$@" ;;
    type)   [ "$#" -ge 1 ] || die "type takes the text to type"; post "typing=$*" ;;
    key)    cmd_key "$@" ;;
    *)      usage >&2; die "unknown command: $command" ;;
esac
