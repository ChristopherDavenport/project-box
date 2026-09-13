#!/bin/sh
# Installs the dogfood box as systemd --user units so it comes back when the
# machine does (issue #113's launcher half; the daemon-started record this
# lands beside, #113's daemon half, is what shows a boot happened at all).
#
#   ./hack/dogfood/install.sh
#
# A systemd --user unit inherits neither this checkout's location nor the
# installing shell's PATH, so this script derives both at install time
# rather than guessing them at start time, when a unit file has no
# $(dirname "$0") of its own to resolve:
#
#   - WorkingDirectory: the checkout root, resolved absolute here.
#   - Environment=PATH=: captured from the shell running this script.
#     run.sh needs go, gh and node/npm on it; room.sh needs nothing beyond
#     that (read both preambles before changing this if that stops being
#     true).
#
# ExecStart invokes run.sh and room.sh themselves rather than re-deriving
# what they do: run.sh is already the single description of how the box
# starts (state-dir migration, the gh credential-helper workaround, build,
# socket-theft refusal, and a final `exec ./bin/boxd` that makes systemd's
# MainPID the daemon itself, not a wrapper shell) — Type=exec below is
# correct because of that final exec, not in spite of it. Duplicating any of
# that here would be a second copy to keep in sync.
#
# Idempotent: re-run after editing this script, moving the checkout, or
# changing your PATH, and it rewrites both units and reloads systemd's view
# of them. It does not start or stop anything already running — see the
# "enabled (not started)" note at the end for why the first install is
# manual on purpose.
set -eu

ROOT=$(cd "$(dirname "$0")/../.." && pwd)

if ! command -v systemctl >/dev/null 2>&1; then
  echo "install.sh: no systemctl — this launcher targets systemd --user (WSL2: add 'systemd=true' under [boot] in /etc/wsl.conf, then run 'wsl --shutdown' from Windows and reopen)" >&2
  exit 1
fi
if ! systemctl --user show-environment >/dev/null 2>&1; then
  echo "install.sh: systemd --user is not reachable from this shell — same fix as above if this is WSL2" >&2
  exit 1
fi

BOXD_UNIT=project-box-dogfood-boxd.service
ROOM_UNIT=project-box-dogfood-room.service
UNIT_DIR=${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user
mkdir -p "$UNIT_DIR"

# Comfortably above box.yaml's `drain: 60s`: Daemon.Run's ctx.Done branch
# (internal/daemon/daemon.go) gives in-flight work that whole window to
# settle before it cancels anything, and returns only after. A
# TimeoutStopSec tighter than the drain would have systemd SIGKILL a daemon
# still inside its own quiesce — raise this if box.yaml's drain ever does.
BOXD_STOP_TIMEOUT=150

cat > "$UNIT_DIR/$BOXD_UNIT" <<UNIT
[Unit]
Description=project-box dogfood daemon (boxd)

[Service]
Type=exec
WorkingDirectory=$ROOT
Environment=PATH=$PATH
ExecStart=$ROOT/hack/dogfood/run.sh
Restart=on-failure
RestartSec=5
TimeoutStopSec=$BOXD_STOP_TIMEOUT

[Install]
WantedBy=default.target
UNIT

cat > "$UNIT_DIR/$ROOM_UNIT" <<UNIT
[Unit]
Description=project-box dogfood control room
After=$BOXD_UNIT
PartOf=$BOXD_UNIT

[Service]
Type=exec
WorkingDirectory=$ROOT
Environment=PATH=$PATH
ExecStart=$ROOT/hack/dogfood/room.sh
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
UNIT

systemctl --user daemon-reload
systemctl --user enable "$BOXD_UNIT" "$ROOM_UNIT"

# Not run automatically: it edits /var/lib/systemd/linger outside this
# repository, and doing that silently on a machine that also runs a human's
# own session is the kind of host-wide change this script asks for rather
# than takes. Without it WSL tears the user session down (and these units
# with it) when the last shell to it closes; with it the units start at WSL
# boot with no login at all.
if command -v loginctl >/dev/null 2>&1; then
  echo "install.sh: run once, so the box survives logout and reboot:"
  echo "  loginctl enable-linger $(id -un)"
fi

echo "install.sh: $BOXD_UNIT and $ROOM_UNIT are generated and enabled (not started)."
echo "  If run.sh/room.sh are already running by hand (nohup, a terminal), stop"
echo "  those first — a unit started alongside them would lose run.sh's own"
echo "  socket-theft refusal and fail to come up. Then:"
echo "    systemctl --user start $BOXD_UNIT $ROOM_UNIT"
echo "  Status:  systemctl --user status $BOXD_UNIT $ROOM_UNIT"
echo "  Logs:    journalctl --user -u $BOXD_UNIT -f"
echo "  Restart: hack/dogfood/restart.sh \"reason\""
