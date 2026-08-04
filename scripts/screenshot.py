#!/usr/bin/env python3
"""Drives tc-tui inside a pty and renders chosen frames to PNG via pyte.

Not part of the build; a one-off tool used to generate docs/screenshots/*.png.
Usage: python3 scripts/screenshot.py
"""
import os
import pty
import pyte
import select
import sys
import time
from PIL import Image, ImageDraw, ImageFont

COLS, ROWS = 160, 40
CELL_W, CELL_H = 9, 18
FONT_PATH_CANDIDATES = [
    "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
    "/usr/share/fonts/dejavu/DejaVuSansMono.ttf",
]

PALETTE = {
    "default": (216, 216, 216),
    "black": (20, 20, 20),
    "red": (224, 90, 90),
    "green": (100, 200, 120),
    "yellow": (220, 200, 90),
    "blue": (100, 150, 230),
    "magenta": (200, 110, 220),
    "cyan": (90, 200, 210),
    "white": (230, 230, 230),
    "brightblack": (110, 110, 110),
    "brightred": (255, 120, 120),
    "brightgreen": (130, 230, 150),
    "brightyellow": (240, 220, 120),
    "brightblue": (130, 170, 250),
    "brightmagenta": (230, 140, 240),
    "brightcyan": (130, 220, 230),
    "brightwhite": (255, 255, 255),
}
BG = (18, 18, 22)


def find_font(size):
    for p in FONT_PATH_CANDIDATES:
        if os.path.exists(p):
            return ImageFont.truetype(p, size)
    return ImageFont.load_default()


FONT = find_font(15)
FONT_BOLD = find_font(15)


def color_for(name, default):
    if not name or name == "default":
        return default
    if name in PALETTE:
        return PALETTE[name]
    return default


def render(screen, out_path):
    img = Image.new("RGB", (COLS * CELL_W, ROWS * CELL_H), BG)
    draw = ImageDraw.Draw(img)
    for y in range(ROWS):
        line = screen.buffer[y]
        for x in range(COLS):
            ch = line[x]
            fg = color_for(ch.fg, PALETTE["default"])
            bg = color_for(ch.bg, None)
            if ch.reverse:
                fg, bg = (bg or BG), fg
            if bg:
                draw.rectangle(
                    [x * CELL_W, y * CELL_H, (x + 1) * CELL_W, (y + 1) * CELL_H],
                    fill=bg,
                )
            if ch.data and ch.data != " ":
                draw.text((x * CELL_W, y * CELL_H), ch.data, font=FONT, fill=fg)
    img.save(out_path)
    print(f"wrote {out_path}")


def main():
    root_url = os.environ.get("TASKCLUSTER_ROOT_URL")
    if not root_url:
        print("TASKCLUSTER_ROOT_URL must be set", file=sys.stderr)
        sys.exit(1)

    binary = os.environ.get("TC_TUI_BINARY") or os.path.abspath("./tc-tui")
    binary = os.path.abspath(binary)
    out_dir = os.path.abspath("docs/screenshots")
    os.makedirs(out_dir, exist_ok=True)

    import fcntl
    import signal
    import struct
    import tempfile
    import termios

    winsize = struct.pack("HHHH", ROWS, COLS, 0, 0)

    # tc-tui persists its navigation stack per root URL under the OS user cache
    # dir (see state.Path -> os.UserCacheDir) and restores it on the next
    # launch. For screenshots we want a deterministic start (worker pools, the
    # root resource) and must not clobber the human's real saved session — so
    # point the child at a throwaway HOME/cache. os.UserCacheDir derives from
    # HOME on both Linux ($HOME/.cache, or $XDG_CACHE_HOME) and macOS
    # ($HOME/Library/Caches), so overriding both covers every platform.
    fake_home = tempfile.mkdtemp(prefix="tc-tui-shots-")

    pid, master_fd = pty.fork()
    if pid == 0:
        os.environ["TERM"] = "xterm-256color"
        os.environ["COLUMNS"] = str(COLS)
        os.environ["LINES"] = str(ROWS)
        os.environ["HOME"] = fake_home
        os.environ["XDG_CACHE_HOME"] = os.path.join(fake_home, ".cache")
        # Size the pty *before* exec so the app's very first paint already
        # happens at COLSxROWS. Resizing only from the parent (below) races the
        # app's startup render — it can paint once at the default 80x24 and
        # leave stale cells that never get cleared, which pyte then captures as
        # garbled overlapping text in the (now denser) header.
        fcntl.ioctl(0, termios.TIOCSWINSZ, winsize)
        os.execv(binary, [binary])
        os._exit(1)

    fcntl.ioctl(master_fd, termios.TIOCSWINSZ, winsize)

    stream = pyte.Stream()
    screen = pyte.Screen(COLS, ROWS)
    stream.attach(screen)

    def pump(duration):
        end = time.time() + duration
        while time.time() < end:
            r, _, _ = select.select([master_fd], [], [], 0.1)
            if master_fd in r:
                try:
                    data = os.read(master_fd, 65536)
                except OSError:
                    break
                if not data:
                    break
                stream.feed(data.decode("utf-8", "ignore"))

    def send(s):
        os.write(master_fd, s.encode())

    def snap(name, settle=1.0):
        pump(settle)
        # Force tcell to do a full clear+resync so no stale cells from an
        # earlier (differently sized or wider) frame survive into the capture.
        os.kill(pid, signal.SIGWINCH)
        pump(0.4)
        render(screen, os.path.join(out_dir, name))

    # 1. worker pools list (the root resource). Navigate to it explicitly
    # rather than trusting the default landing view — even with an isolated
    # cache the command bar is the deterministic way to pin the first frame.
    pump(3.0)
    send(":workerpools\r")
    snap("worker-pools.png", settle=2.5)

    # 2. select first row -> worker pool detail
    send("\r")
    snap("worker-pool-detail.png", settle=1.5)

    send("\x1b")
    pump(0.5)

    # 3. jump straight to a busy pool's workers (real running workers, not
    # the empty pool that happened to sort first)
    send(":workers proj-fuzzing/grizzly-reduce-worker\r")
    snap("workers.png", settle=1.5)

    send("\r")  # open first worker
    snap("worker-detail.png", settle=1.0)

    send("\x1b")
    pump(0.3)
    send("\x1b")
    pump(0.3)

    # 4. jump to a plain global list via the command bar
    send(":roles\r")
    snap("roles.png", settle=1.5)
    send("\x1b")
    pump(0.3)

    # 5. a single task's detail — colored state, runs, and the action hints
    # (rerun/retrigger/cancel/priority). Resolved live from the community-tc
    # task index (project.fuzzing.orion.ci-node-22.amd64.master).
    send(":task dMmVDAO4TamzRLH1OJey3w\r")
    snap("task-detail.png", settle=2.5)
    send("\x1b")
    pump(0.3)

    # 6. that task's task group — a big list with state-colored rows
    send(":taskgroup J_PKu6BjS0mD6xCu7uR7qQ\r")
    snap("task-group.png", settle=3.0)
    send("\x1b")
    pump(0.3)

    # 7. that task's artifacts (across all runs)
    send(":artifacts dMmVDAO4TamzRLH1OJey3w\r")
    snap("artifacts.png", settle=2.5)
    send("\x1b")
    pump(0.3)

    # 8. a Github pull request's Taskcluster builds
    send(":githubbuilds taskcluster/taskcluster/pull/8693\r")
    snap("github-builds.png", settle=3.0)
    send("\x1b")
    pump(0.3)

    # 9. help screen
    send("?")
    snap("help.png", settle=0.5)
    send("\x1b")
    pump(0.3)

    send("q")
    time.sleep(0.3)
    try:
        os.kill(pid, 15)
    except ProcessLookupError:
        pass


if __name__ == "__main__":
    main()
