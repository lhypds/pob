// The --macropsl flag: which PSL file a command works on, when it is not the
// instance's own src/main.macro.psl.
//
// An instance has one entry point and that is what Execute runs, which is the
// right default and a poor rule for a terminal: a macro is written one piece at
// a time, and the piece being written is not usually the one main calls first.
// The flag is the way to say which file — `pob start --macropsl` runs it,
// `pob check --macropsl` reads it — and everything downstream of that name
// follows the file rather than the instance. A call() inside it names its own
// neighbours, its own extension says whether psl is started for it at all, and
// the session log keeps it under its own name.
package main

import (
	"os"
	"path/filepath"
	"strings"
)

// macroPSLFlag names a PSL file in place of the instance's own macro. Written
// out in full rather than abbreviated: it is typed in front of a run that moves
// the cursor, and the file it names is the whole of what that run will do.
const macroPSLFlag = "--macropsl"

// macroPSLUsage is what to type, said the same way wherever the flag was
// mistyped.
const macroPSLUsage = macroPSLFlag + " wants the path of a PSL file — " + macroPSLFlag + " login.macro.psl"

// takeMacroPSL pulls --macropsl and its argument out of a command's arguments
// and hands back what is left, so the command goes on reading the rest the way
// it always did.
//
// Both spellings are taken — `--macropsl f` and `--macropsl=f` — since a flag
// with a filename after it is written both ways and neither is a guess about
// what was meant. A flag written last with nothing after it is said rather than
// ignored, which would otherwise start a run over the wrong file.
func takeMacroPSL(args []string) (rest []string, file string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var value string
		switch {
		case arg == macroPSLFlag:
			// The next argument is the filename unless it is another flag, which
			// is what `pob launch --macropsl --start` is: taking it would leave the
			// command asking to run a file called --start, and the error would be
			// about that file rather than about the argument nobody typed. A name
			// that really does begin with a dash is written --macropsl=-name.
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				fail("%s", macroPSLUsage)
			}
			i++
			value = args[i]
		case strings.HasPrefix(arg, macroPSLFlag+"="):
			value = strings.TrimPrefix(arg, macroPSLFlag+"=")
		default:
			rest = append(rest, arg)
			continue
		}
		if strings.TrimSpace(value) == "" {
			fail("%s", macroPSLUsage)
		}
		file = strings.TrimSpace(value)
	}
	return rest, file
}

// macroPSLOnly reads the flag out of a command that takes nothing else —
// `start` and `check` — and hands back the file it named.
//
// What is left over ends the command rather than being passed over. A flag that
// was not quite typed right is the case this is for: `--macropsl` misspelled is
// an argument nothing recognises, and passed over silently it would leave
// `pob start` replaying the instance's own macro — a run of the wrong file,
// moving the cursor, in answer to a command that named another one.
func macroPSLOnly(args []string, command string) string {
	rest, file := takeMacroPSL(args)
	if len(rest) > 0 {
		fail("%s takes no arguments besides %s — %q is not one, run `pob help`", command, macroPSLFlag, rest[0])
	}
	return file
}

// resolveMacroPSL works out which file --macropsl named, as an absolute path.
//
// It is resolved here rather than by the core because the two are separate
// processes in separate directories: core is a child of the app and runs
// wherever the app was started from, and a relative path means the directory it
// was typed in. The Control API refuses one that is not absolute for that
// reason — see handleRunMacro.
//
// A bare name that is not in the current directory is looked for in the
// instance's src/, which is where an instance's macros live, so
// `pob start --macropsl login.macro.psl` runs the file beside main.macro.psl
// from wherever it is typed. The current directory wins where both have one, and
// a name with a directory in it is a path and is looked for nowhere else — the
// fallback is a convenience for the common name, not a search.
//
// A file that is not there ends the command. It is the whole of what was asked
// for, and a run started without it would be a run of something else.
func resolveMacroPSL(inst *Instance, file string) string {
	path := file
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			fail("cannot determine home directory: %v", err)
		}
		path = filepath.Join(home, rest)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		fail("cannot resolve %s: %v", file, err)
	}
	if exists(abs) {
		return abs
	}
	if filepath.Base(file) == file {
		if inSrc := filepath.Join(inst.Dir, "src", file); exists(inSrc) {
			return inSrc
		}
	}
	fail("no such file: %s — %s names a PSL file, by path or by its name in %s",
		abs, macroPSLFlag, filepath.Join(inst.Dir, "src"))
	return ""
}
