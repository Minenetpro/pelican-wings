# Console Output Color Codes — Frontend Reference

The SSE endpoint (`GET /api/events`) and the console history endpoint (`GET /api/servers/:server/console`) both return console output with color/formatting codes preserved. The frontend needs to handle two distinct types.

---

## 1. ANSI Escape Sequences

These are standard terminal escape codes present in all console output — both from the game server process and from Wings daemon messages.

Format: `ESC[<params>m` where ESC is `\u001b` (hex `\x1b`).

Examples as they appear in JSON payloads:

| Sequence         | Meaning              |
|------------------|----------------------|
| `\u001b[31m`     | Red text             |
| `\u001b[1m`      | Bold                 |
| `\u001b[0m`      | Reset all formatting |
| `\u001b[33;1m`   | Yellow + bold        |

Wings daemon messages (e.g. `[Pelican Daemon]: Server marked as running...`) use ANSI codes for styling. They arrive as yellow bold text via sequences like `\u001b[33m\u001b[1m`.

A library like **xterm.js** (terminal emulator) or **ansi-to-html** will handle these natively.

---

## 2. Minecraft § Color Codes (and similar game-specific codes)

Some game servers — Minecraft most notably — use the section sign (`§`) followed by a character to indicate color/formatting. These are NOT converted or stripped by the backend.

Common codes:

| Code          | Meaning       |
|---------------|---------------|
| `§0` – `§9`  | Colors        |
| `§a` – `§f`  | Colors        |
| `§l`          | Bold          |
| `§o`          | Italic        |
| `§n`          | Underline     |
| `§m`          | Strikethrough |
| `§r`          | Reset         |

Example raw line:

```
§e§lPlayer §r§ajoined the game
```

The frontend will need a custom parser or library (e.g. **minecraft-text**) to render these if Minecraft server support matters. Other game engines may have their own conventions, but Minecraft's `§` codes are by far the most common.

---

## What to expect in the JSON payloads

Both endpoints return console lines as JSON strings. The ANSI escape character (`0x1b`) is encoded as `\u001b` by Go's JSON marshaler. After JSON parsing, you'll have the raw string with the actual ESC byte — feed that directly into your terminal renderer.

### SSE event example

```
event: console output
data: {"server_id":"abc123","line":"\u001b[33m\u001b[1m[Pelican Daemon]:\u001b[0m Server marked as running..."}
```

### Console history response example

```json
{
    "state": "running",
    "line_count": 2,
    "lines": [
        "\u001b[33m\u001b[1m[Pelican Daemon]:\u001b[0m Starting server...",
        "§aServer started on port 25565"
    ]
}
```

---

## Recommendation

Use xterm.js or an equivalent ANSI-aware terminal renderer as the primary display. If Minecraft support is needed, add a `§`-code parser that runs before or alongside the ANSI renderer, converting `§` codes to their ANSI equivalents or to styled HTML spans.
