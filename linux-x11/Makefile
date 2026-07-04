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

# Lazily expanded so `make clean` never invokes pkg-config.
CC      ?= cc
CFLAGS  ?= -O2
CFLAGS  += -Wall -Wextra -std=c11 $(shell pkg-config --cflags $(PKGS))
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
