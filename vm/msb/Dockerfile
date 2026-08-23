# The machine Pob runs on inside a microsandbox VM: a Debian desktop with no
# monitor. Xvfb is the screen, openbox the window manager `launch()` asks to
# place windows, xcompmgr the compositor Pob's overlay needs to be see-through
# at all, x11vnc the way to look at that screen from the host, and Firefox
# something for a macro to drive.
#
# Nothing of Pob's own is baked in. The app, the run script and ~/.pob all
# arrive as read-only mounts at boot (see launch.sh), so a rebuilt Linux app or
# an edited run.sh is picked up by the next launch and this image is rebuilt
# only when the desktop under it changes.
#
# launch.sh builds it for the host's own architecture — a microVM runs the
# host's, and nothing else:
#
#   docker build --platform linux/arm64 -t pob-msb:latest vm/msb
FROM debian:stable-slim

# One layer, and the package lists thrown away after it: a rebuilt image is
# copied into the microVM store whole, so its size is a cost paid every time.
#
# The four lib packages are what the GTK shell is linked against — the same
# list linux-x11/build_docker.sh compiles it with, minus the -dev.
#
# The fonts are there because a page with no font for its characters draws them
# as boxes, and a macro cannot read boxes any better than a person can: Liberation
# for the metric-compatible faces the web asks for by name, Noto CJK for Chinese,
# Japanese and Korean, and the emoji face for the rest of what a page puts in a
# heading. A base image ships none of this — nothing in it was ever meant to
# have a screen.
#
# The row after them is a desktop somebody can use: xterm and mousepad are the
# terminal and the text editor Pob's own toolbar reaches for (the chains in
# linux-x11/src/settings_service.c — xterm also registers the
# x-terminal-emulator alternative, and mousepad is in the editor list),
# pcmanfm is the file manager beside them, and vim and nano are for once you
# are in that terminal. Without them the buttons find nothing and a VNC
# session is a screen with no way to open anything on it.
#
# desktop-file-utils is what makes the desktop able to answer "what opens a
# text file": it builds /usr/share/applications/mimeinfo.cache, without which
# every application here is installed but registered for nothing. That is not
# a small thing to leave out — asked for main.macro.psl with no cache to read,
# xdg-open falls through to its list of last resorts, which begins with
# firefox, and the macro opens in a browser tab. It comes to a few hundred
# kilobytes; shared-mime-info, the types themselves, is already here as a
# dependency of GTK.
RUN apt-get update -qq && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends \
        xvfb x11vnc openbox xcompmgr x11-utils x11-xserver-utils \
        libgtk-3-0t64 libjson-glib-1.0-0 libx11-6 libxtst6 \
        dbus-x11 ca-certificates procps \
        fonts-dejavu-core fonts-liberation fonts-noto-cjk fonts-noto-color-emoji \
        xterm mousepad pcmanfm xdg-utils desktop-file-utils vim nano \
        firefox-esr && \
    rm -rf /var/lib/apt/lists/*

# Debian ships the browser as firefox-esr. On Linux `launch("firefox")` is
# handed to a shell, so the name a macro writes is the name a person would
# type — and this is what makes that name resolve.
RUN ln -s /usr/bin/firefox-esr /usr/local/bin/firefox

# The CLI, so `msb exec pob-msb -- pob status` works inside the guest the way
# `pob status` works outside it. It points into the mount the app arrives on,
# which is why it is made with -s and not checked here.
RUN ln -s /mnt/pob-app/Helpers/pob /usr/local/bin/pob

ENV DISPLAY=:99 \
    HOME=/root \
    XDG_RUNTIME_DIR=/run/user/0

# The workload: the desktop, and then Pob on it. It lives on the mount rather
# than in the image so that editing it is a launch away, not a rebuild.
CMD ["/bin/bash", "/mnt/pob-vm/run.sh"]
