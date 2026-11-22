"""Exercise the real Bubble Tea CLI in two disposable test terminals."""
import errno
import fcntl
import os
from pathlib import Path
import pty
import re
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time

CLI = str(Path(".bin/workspace").resolve())


class Terminal:
    def __init__(self, args, env):
        pid, fd = pty.fork()
        if pid == 0:
            fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 96, 0, 0))
            os.execve(CLI, [CLI, *args], env)
        self.pid, self.fd = pid, fd
        self.output = b""


    def send(self, data):
        os.write(self.fd, data)

    def wait(self, pattern, timeout=15):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            found = re.search(pattern, self.output)
            if found:
                return found
            if select.select([self.fd], [], [], 0.2)[0]:
                try:
                    chunk = os.read(self.fd, 8192)
                except OSError as error:
                    if error.errno != errno.EIO:
                        raise
                    break
                if not chunk:
                    break
                self.output += chunk
        raise AssertionError(f"Missing {pattern!r} in terminal output {self.output[-1600:]!r}")

    def close(self):
        os.close(self.fd)
        pid, _ = os.waitpid(self.pid, os.WNOHANG)
        if pid == 0:
            os.kill(self.pid, signal.SIGTERM)
            os.waitpid(self.pid, 0)


with tempfile.TemporaryDirectory(prefix="pi-m1-tui-") as directory, tempfile.TemporaryDirectory(prefix="pi-m1-tui-project-") as project:
    env = {**os.environ, "TERM": "xterm-256color", "PI_WORKSPACE_HOME": directory, "PI_PROVIDER": "fixture"}
    Path(project, "notes.txt").write_text("Go TUI scratch input\n")
    terminals = []
    try:
        first = Terminal(["tui", "--cwd", project], env)
        terminals.append(first)
        first.wait(rb"New project")
        first.send(b"\r")  # Confirm directory.
        first.wait(rb"add project and start")
        first.send(b"\r")  # Register project and start its conversation.
        first.wait(rb"ctx ")
        first.send(b"fixture:tools-slow\r")
        first.wait(rb"streamed model output")
        # Type an unsent draft, then detach the first client while the agent runs.
        first.send(b"unsent draft")
        first.send(b"\x11")  # Ctrl+Q detaches while the run continues.
        conversation = first.wait(rb"Conversation ([0-9]+)").group(1)
        subprocess.run([CLI, "server", "status"], env=env, check=True, timeout=5)

        second = Terminal(["tui", "--conversation", conversation.decode()], env)
        terminals.append(second)
        second.wait(rb"streamed model output")
        assert Path(project, "result.txt").read_text() == "written by Durable\n"
        second.send(b"reconnect draft")
        second.wait(rb"reconnect draft")
        second.output = b""
        subprocess.run([CLI, "server", "stop"], env=env, check=True, timeout=5)
        second.wait(rb"reconnecting")
        second.output = b""
        subprocess.run([CLI, "server", "start"], env=env, check=True, timeout=12)
        second.wait(rb"streamed model output")  # Transcript returns after reconnect.
        second.send(b"\r")
        second.wait(rb"streamed model output")
        second.send(b"\x11")  # Detach even if a queued draft has started running.
        assert second.wait(rb"Conversation ([0-9]+)").group(1) == conversation
        print("Real Bubble Tea clients detached, reattached, and sent a draft preserved across daemon reconnect")
    finally:
        for terminal in terminals:
            terminal.close()
        subprocess.run([CLI, "server", "stop"], env=env, timeout=5, check=False)
        # Allow graceful shutdown to close SQLite before removing the temp data.
        for _ in range(50):
            try:
                subprocess.run([CLI, "server", "status"], env=env, timeout=2, check=True, capture_output=True)
            except subprocess.CalledProcessError:
                break
            time.sleep(0.02)
