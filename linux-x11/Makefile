# Pob Linux/X11 shell.
# Dependencies: GTK 3, json-glib, X11, XTest (see setup.sh for packages).

PKGS    := gtk+-3.0 json-glib-1.0 x11 xtst

# Fail fast with an install hint instead of compiling with empty CFLAGS.
ifeq ($(filter clean,$(MAKECMDGOALS)),)
ifneq ($(shell pkg-config --exists $(PKGS) && echo yes),yes)
$(error Missing development libraries ($(PKGS)). Install them first — \
Debian/Ubuntu/Raspberry Pi OS: sudo apt install libgtk-3-dev libjson-glib-dev libx11-dev libxtst-dev)
endif
endif

# The version the About box and the window title show, compiled in: an
# installed or unzipped copy has no VERSION file at a path it can guess, so
# reading one at runtime only ever worked inside a checkout. build_docker.sh
# passes this explicitly — the container mounts linux-x11/ alone, without the
# repository root the VERSION file sits in.
POB_VERSION ?= $(shell cat ../VERSION 2>/dev/null)

# Lazily expanded so `make clean` never invokes pkg-config.
CC      ?= cc
CFLAGS  ?= -O2
CFLAGS  += -Wall -Wextra -std=c11 -DPOB_VERSION='"$(POB_VERSION)"' $(shell pkg-config --cflags $(PKGS))
LDLIBS   = $(shell pkg-config --libs $(PKGS)) -lm

SRC := $(wildcard src/*.c)
OBJ := $(patsubst src/%.c,build/%.o,$(SRC))
BIN := bin/pob

.PHONY: all clean

all: $(BIN)

$(BIN): $(OBJ)
	@mkdir -p bin
	$(CC) -o $@ $(OBJ) $(LDLIBS)

build/%.o: src/%.c $(wildcard src/*.h)
	@mkdir -p build
	$(CC) $(CFLAGS) -c -o $@ $<

clean:
	rm -rf build bin
