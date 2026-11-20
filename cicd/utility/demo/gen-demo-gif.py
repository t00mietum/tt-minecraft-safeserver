#!/usr/bin/env python3

##	Purpose: Render an animated demo GIF of a CLI program, without recording a
##		real terminal (no ttyd, no asciinema, no ffmpeg - just Pillow). A TOML
##		scenario file scripts the session; each command is "typed" into a fake
##		terminal window with human timing (slower digits, a beat before flags,
##		the occasional corrected typo), then actually executed so the captured
##		output can never go stale. A command that stops to ask something gets a
##		terminal on its stdin, so it really does ask, and its answer is typed on
##		camera like every other keystroke. Scrolling is pixel-smooth (content settles
##		back onto the line grid at rest) and the cursor glides between cells
##		rather than teleporting. At the end it holds the last frame still, then
##		hard-cuts to a black frame before repeating - a held black frame is one
##		cheap frame, not a bloaty fade. Frames share one exact master palette,
##		so nothing is ever re-dithered. Project-agnostic - point it at any scenario.
##	Syntax:
##		gen-demo-gif.py --scenario FILE --out FILE [--bin PATH] [--seed N] [-q]
##		  --scenario FILE  TOML scenario (see fLoadScenario for the format)
##		  --out FILE       GIF to write (required)
##		  --bin PATH       program under demo; substituted for {bin} in run=
##		  --seed N         RNG seed; fixed default so reruns are byte-stable
##		  --font NAME      override the scenario's font preference list
##		  -q, --quiet      only errors
##	Exit: 0 wrote the GIF, 2 non-fatal skip (no Pillow, bad scenario, cmd failed).
##	History: At bottom of script.

##	Copyright (c) 2026 Bubbles
##	Licensed under The MIT License (MIT). Full text at:
##		https://mit-license.org/
##	SPDX-License-Identifier: MIT


import argparse
import os
import random
import re
import shlex
import subprocess
import sys
import time
import unicodedata

try:
	import tomllib
except ImportError:
	sys.stderr.write("gen-demo-gif: needs python 3.11+ (tomllib)\n")
	sys.exit(2)
try:
	from PIL import Image, ImageDraw, ImageFont
except ImportError:
	sys.stderr.write("gen-demo-gif: Pillow not installed\n")
	sys.exit(2)


##	Canvas and window chrome. 960x540 total; the window fills the whole view
##	except a thin black border. Square corners, no shadow.
CANVAS_W, CANVAS_H = 960, 540
MARGIN     = 4        # thin black border around the window
TITLE_H    = 34       # title bar height
PAD        = 12       # text inset inside the terminal area

THEME = {
	"outer":    (0, 0, 0),         # thin border around the window
	"border":   (58, 58, 62),      # 1px window outline
	"titlebar": (44, 44, 48),
	"titletxt": (150, 150, 152),
	"dots":     [(158, 96, 92), (158, 142, 92), (105, 148, 100)],   # muted traffic lights
	"bg":       (30, 32, 30),      # terminal background: dark gray, hint of warmth
	"fg":       (166, 227, 161),   # bright pale green
	"gray":     (148, 148, 148),   # prompt punctuation ("standard gray")
	"dim":      (106, 112, 106),   # comments, truncation ellipsis
}
##	user@host tints: dimmer than fg, complementary hues (green sits across from
##	these on the wheel). Two distinct picks per run, seeded.
IDENT_TINTS = [
	(196, 148, 108),   # tan
	(160, 136, 200),   # violet
	(112, 178, 196),   # cyan
	(198, 140, 156),   # rose
	(170, 166, 108),   # olive
]
IDENT_USERS = ["mika", "joss", "arlo", "remy", "kai", "nova", "wren", "finn"]
IDENT_HOSTS = ["basalt", "kestrel", "onyx", "lyra", "quartz", "mesa", "flint", "juno"]

##	Typing model. WPM -> ms/char at the usual 5 chars/word.
WPM_LETTERS   = (135, 183)   # per-command draw, then per-char jitter
WPM_DIGITS    = 63           # default; scenario wpm_digits overrides
WPM_NOTES     = (233, 263)   # "# comment" lines fly by
FLAG_PAUSE_MS = (200, 380)   # a beat of thought before a -flag token
TYPO_RATE     = 0.018        # per letter; capped at 2 fixes per command
BLINK_MS      = 520          # a multiple of FRAME_MS, so blinks stay on the grid
FRAME_MS      = 20           # 50 fps: every frame duration is a multiple of this
SCROLL_RATE   = 150          # px/s smooth scroll; per-step scrollrate overrides
NOTE_HOLD_MS  = 700          # a caption that appears whole still has to be read

QWERTY_ROWS = ["1234567890", "qwertyuiop", "asdfghjkl", "zxcvbnm"]

ANSI_RE = re.compile(r"\x1b(\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|.)")


def fEmojiInit():
	##	Color emoji are beyond FreeType-via-Pillow now (Noto ships COLRv1), so
	##	glyph tiles come from Pango+cairo instead. Everything here is optional:
	##	if any piece is missing the chars just draw through the monochrome
	##	fallback like before.
	try:
		import gi
		import cairo
		gi.require_version("Pango", "1.0")
		gi.require_version("PangoCairo", "1.0")
		from gi.repository import Pango, PangoCairo
		from fontTools.ttLib import TTFont
	except (ImportError, ValueError):
		return None
	try:
		out = subprocess.run(["fc-match", "-f", "%{file}\t%{family}", "emoji"],
		                     capture_output=True, text=True, timeout=10).stdout
		path, family = (out.split("\t") + [""])[:2]
		if "emoji" not in family.lower():
			return None
		cmap = set(TTFont(path, lazy=True).getBestCmap())
	except Exception:
		return None
	return {"Pango": Pango, "PangoCairo": PangoCairo, "cairo": cairo,
	        "family": family, "cmap": cmap}


EMOJI = fEmojiInit()


def fSkip(msg):
	##	2 = non-fatal skip, same convention as the other cicd utilities: the
	##	stage warns and the pipeline continues.
	sys.stderr.write(f"gen-demo-gif: {msg}\n")
	sys.exit(2)


def fFindFont(prefs, size):
	##	Resolve the first available preference via fc-match. fc-match always
	##	answers with its best match, so verify the family before trusting it;
	##	the final fallback takes whatever monospace fontconfig offers.
	for name in prefs + ["monospace"]:
		try:
			out = subprocess.run(
				["fc-match", "-f", "%{file}\t%{family}\t%{style}", name],
				capture_output=True, text=True, timeout=10).stdout
		except OSError:
			break
		path, family, style = (out.split("\t") + ["", ""])[:3]
		want = re.sub(r"[^a-z]", "", name.lower())
		got  = re.sub(r"[^a-z]", "", (family + style).lower())
		if name == "monospace" or (path and want in got):
			try:
				return ImageFont.truetype(path, size), f"{family} {style}".strip()
			except OSError:
				continue
	fSkip("no usable monospace font found")


def fLoadScenario(path):
	##	Scenario format (TOML):
	##	  title = "window title"        prog = "name shown in typed commands"
	##	  font = ["pref1", "pref2"]     seed = 11
	##	  wpm_digits = 42               digit typing speed (numbers-heavy demos: raise it)
	##	  type_notes = false            captions appear whole instead of being typed out
	##	  end_hold = 3.0                seconds the final frame holds before the loop
	##	  end_black = 2.0               seconds of black after the hold, then repeat
	##	  [[step]]
	##	  note = "shown as a typed # comment first"     (optional)
	##	  show = "{prog} 255 16"        the command line as typed
	##	  run  = "echo hi | {bin} ..."  what actually executes (default: show)
	##	  pause = 2.6                   read time after the output, seconds
	##	  overflow = "truncate"         or "wrap" for real feature output
	##	  scrollrate = 260              px/s smooth scroll during this step's output
	##	  linems = 20                   ms per output line before scrolling kicks in
	##	  clear = true                  wipe the scrollback first, as `clear` does
	##	  cwd = "~/dev/thing"           shown in the prompt (default: "~")
	##	  ask = "Continue? (y|n): "     text the command stops on and waits for input
	##	  answer = "y"                  typed on camera in reply, then Enter
	##	  askpause = 3.0                seconds to read the plan before the answer
	try:
		with open(path, "rb") as f:
			sc = tomllib.load(f)
	except (OSError, tomllib.TOMLDecodeError) as e:
		fSkip(f"scenario: {e}")
	if not sc.get("step"):
		fSkip("scenario has no [[step]] entries")
	return sc


def fLines(text):
	##	Captured bytes -> terminal lines: escapes gone, tabs expanded, no trailing
	##	blanks on the ends of lines.
	return [ln.expandtabs(8).rstrip() for ln in ANSI_RE.sub("", text).split("\n")]


def fRunStep(step, prog, binpath):
	##	Execute the step's command for real, and hand back the pieces the step will
	##	draw: ("out", lines) for ordinary output, ("ask", question, answer) where the
	##	command stopped and waited. stderr shares the stdout pipe rather than being
	##	appended after it, so a program that mixes the two (git writes most of its
	##	progress to stderr) reads in the order it actually printed.
	cmd = step.get("run", step["show"])
	cmd = cmd.replace("{bin}", shlex.quote(binpath)).replace("{prog}", shlex.quote(binpath))
	if step.get("ask"):
		return fRunAsked(cmd, str(step["ask"]), str(step.get("answer", "")))
	try:
		res = subprocess.run(["bash", "-c", cmd], stdout=subprocess.PIPE,
		                     stderr=subprocess.STDOUT, text=True,
		                     timeout=30, errors="replace")
	except subprocess.TimeoutExpired:
		fSkip(f"command timed out: {cmd}")
	return [("out", fLines(res.stdout.rstrip("\n")))]


def fRunAsked(cmd, ask, answer):
	##	A command that asks before it acts will not ask a pipe - it either skips the
	##	question or refuses - so stdin gets a real terminal. Only stdin: stdout stays
	##	a pipe, so colour and cursor tricks meant for a terminal stay out of the
	##	captured text and the frames look the same as every other step's.
	##	The answer goes in here to unblock the command; drawing it is the render
	##	loop's job, from this same string, so what runs and what is on screen cannot
	##	disagree.
	import pty      # local: no ptys off Unix, and only this path needs one
	import select
	master, slave = pty.openpty()
	proc = subprocess.Popen(["bash", "-c", cmd], stdin=slave,
	                        stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
	os.close(slave)
	buf, sent, deadline = b"", False, time.monotonic() + 30
	while True:
		if time.monotonic() > deadline:
			proc.kill()
			fSkip(f"command never asked {ask!r}, and timed out: {cmd}")
		if select.select([proc.stdout], [], [], 0.2)[0]:
			chunk = os.read(proc.stdout.fileno(), 65536)
			if not chunk:
				break
			buf += chunk
		if not sent and ANSI_RE.sub("", buf.decode("utf-8", "replace")).endswith(ask):
			os.write(master, (answer + "\n").encode())
			sent = True
	proc.wait()
	os.close(master)
	text = ANSI_RE.sub("", buf.decode("utf-8", "replace"))
	if not sent:
		fSkip(f"command exited without asking {ask!r}: {cmd}")
	##	The question is written without a newline and the answer is read back, so
	##	neither is a line yet. Split there: what precedes it is ordinary output, the
	##	last piece of it shares the question's line, and what follows starts with the
	##	newline the typed Enter would have supplied.
	at = text.index(ask)
	before = fLines(text[:at])
	lead = before.pop()
	return [("out", before), ("ask", lead + ask, answer),
	        ("out", fLines(text[at + len(ask):].rstrip("\n")))]


def fStepText(segs):
	##	Every line a step will put on screen, for the palette scan below.
	for seg in segs:
		if seg[0] == "ask":
			yield seg[1] + seg[2]
		else:
			yield from seg[1]


def fTypeEvents(text, rng, wpm_range, typos=True, wpmDigits=WPM_DIGITS):
	##	Turn a command string into ((action, char), delay_ms) keystroke events.
	##	Letters ride the per-command WPM draw with per-char jitter, digits get
	##	their own WPM (slow by default, fast for numbers-heavy demos), a -flag
	##	token gets a small hesitation, and a couple of seeded typos get noticed
	##	and backspaced away.
	wpm = rng.uniform(*wpm_range)
	events, fixes = [], 0
	firstSpace = text.find(" ")
	for i, ch in enumerate(text):
		if ch.isdigit():
			delay = 60000.0 / (wpmDigits * 5) * rng.uniform(0.82, 1.22)
		else:
			delay = 60000.0 / (wpm * 5) * rng.uniform(0.70, 1.34)
			if i < firstSpace:
				delay *= 0.78        # the leading token is muscle memory
		if ch == "-" and (i == 0 or text[i - 1] == " "):
			delay += rng.uniform(*FLAG_PAUSE_MS)
		if typos and fixes < 2 and ch.isalpha() and rng.random() < TYPO_RATE:
			wrong = fNeighborKey(ch, rng)
			events.append((("type", wrong), delay))
			overshoot = None
			if i + 1 < len(text) and text[i + 1].isalpha() and rng.random() < 0.4:
				overshoot = text[i + 1]      # one more char before noticing
				events.append((("type", overshoot), 60000.0 / (wpm * 5)))
			events.append((("pause", None), rng.uniform(320, 640)))
			for _ in range(2 if overshoot else 1):
				events.append((("bs", None), rng.uniform(95, 150)))
			events.append((("type", ch), rng.uniform(140, 260)))
			fixes += 1
			continue
		events.append((("type", ch), delay))
	return events


def fNeighborKey(ch, rng):
	for row in QWERTY_ROWS:
		pos = row.find(ch.lower())
		if pos < 0:
			continue
		near = [row[j] for j in (pos - 1, pos + 1) if 0 <= j < len(row)]
		pick = rng.choice(near)
		return pick.upper() if ch.isupper() else pick
	return ch


##	Palette: one shared 256-entry table for every frame. The text colors carry
##	short blend ramps toward their background so antialiased edges quantize
##	cleanly instead of dithering, and the leftover entries are median-cut from
##	the emoji tiles the scenario actually produces. One global table means no
##	per-frame palette cost.
def fBuildPalette(userTint, hostTint, emojiTiles):
	def blend(a, b, k):
		return tuple(round(a[i] + (b[i] - a[i]) * k) for i in range(3))
	t = THEME
	colors = [(0, 0, 0), t["outer"], t["border"], t["titlebar"],
	          t["titletxt"], t["bg"], t["fg"], t["gray"], t["dim"],
	          userTint, hostTint] + t["dots"]
	for c in (t["fg"], t["gray"], t["dim"], userTint, hostTint):
		for k in (0.22, 0.45, 0.68, 0.86):
			colors.append(blend(t["bg"], c, k))
	for k in (0.35, 0.7):
		colors.append(blend(t["titlebar"], t["titletxt"], k))
	colors = list(dict.fromkeys(colors))
	if emojiTiles:
		w = sum(tile.width for tile in emojiTiles)
		h = max(tile.height for tile in emojiTiles)
		strip = Image.new("RGB", (max(w, 1), max(h, 1)), t["bg"])
		x = 0
		for tile in emojiTiles:
			strip.paste(tile, (x, 0), tile)
			x += tile.width
		room = min(256 - len(colors), 220)
		q = strip.quantize(colors=room, method=Image.Quantize.MEDIANCUT)
		qp = q.getpalette()
		got = [tuple(qp[i:i + 3]) for i in range(0, room * 3, 3)]
		colors = list(dict.fromkeys(colors + got))
	##	Pad to the next power of two rather than a flat 256: GIF's LZW code width
	##	tracks the palette size, so a text-only demo (~35 colors) writes about 12%
	##	smaller at 64 entries, pixel for pixel identical.
	slots = 2
	while slots < len(colors):
		slots *= 2
	colors = (colors + [(0, 0, 0)] * slots)[:slots]
	pal = Image.new("P", (1, 1))
	pal.putpalette([v for c in colors for v in c])
	return pal


class Screen:
	##	The fake terminal: a scrollback of styled lines plus an in-progress
	##	prompt line, viewed through a pixel-scrolled window. render() draws the
	##	full chrome each time and quantizes straight to the master palette.
	def __init__(self, font, fontName, title, prompt, palImg, fontSize):
		self.font, self.title, self.prompt, self.pal = font, title, prompt, palImg
		self.fontSize = fontSize
		self.showPrompt = True   # hidden while a command's output is scrolling in
		self._glyphFont = {}     # ch -> font that can draw it
		self._fbFonts = {}       # font file -> ImageFont
		self._emojiTiles = {}    # ch -> RGBA tile (or None if it would not render)
		self._notdef = self._mask(font, "\U000FFFFD")
		ascent, descent = font.getmetrics()
		self.cw = font.getlength("0")
		self.lh = ascent + descent + 3
		self.winW = CANVAS_W - 2 * MARGIN
		self.winH = CANVAS_H - 2 * MARGIN
		self.termX = MARGIN + PAD
		self.termY = MARGIN + TITLE_H + PAD
		self.cols = int((self.winW - 2 * PAD) // self.cw)
		self.rows = int((self.winH - TITLE_H - 2 * PAD) // self.lh)
		self.textW = self.winW - 2 * PAD
		self.viewH = self.rows * self.lh
		self.lines = []          # committed scrollback: list of [(text, colorkey)]
		self.typed = ""          # text after the prompt on the live line
		self.scroll = 0.0        # view offset into the content, px
		self.fontName = fontName

	def fPut(self, spans):
		self.lines.append(spans)

	@staticmethod
	def _mask(font, ch):
		##	getmask returns a raw ImagingCore; box it to get comparable bytes.
		return Image.Image()._new(font.getmask(ch)).tobytes()

	def fFontFor(self, ch):
		##	The primary font, unless it draws ch as .notdef; then whatever
		##	fontconfig says covers that codepoint (e.g. a CJK face for the
		##	Kanji/Hanzi base aliases). Cached hard - fc-match is not cheap.
		if ord(ch) < 0x80:
			return self.font
		hit = self._glyphFont.get(ch)
		if hit:
			return hit
		use = self.font
		if self._mask(self.font, ch) == self._notdef:
			try:
				path = subprocess.run(
					["fc-match", "-f", "%{file}", f":charset={ord(ch):x}"],
					capture_output=True, text=True, timeout=10).stdout.strip()
			except OSError:
				path = ""
			if path:
				if path not in self._fbFonts:
					try:
						self._fbFonts[path] = ImageFont.truetype(path, self.fontSize)
					except OSError:
						self._fbFonts[path] = self.font
				use = self._fbFonts[path]
		self._glyphFont[ch] = use
		return use

	def fIsEmoji(self, ch):
		##	Only chars the emoji face covers, and only from the emoji blocks -
		##	CJK and friends stay on the monochrome fallback fonts.
		return EMOJI is not None and ord(ch) >= 0x2600 and ord(ch) in EMOJI["cmap"]

	def fEmojiTile(self, ch):
		##	Rasterize one emoji via Pango+cairo at 4x, then shrink into the cell
		##	box (two columns wide, like a terminal). Cached; None = unrenderable.
		if ch in self._emojiTiles:
			return self._emojiTiles[ch]
		e = EMOJI
		px = self.lh * 4
		surf = e["cairo"].ImageSurface(e["cairo"].FORMAT_ARGB32, px * 2, px * 2)
		ctx = e["cairo"].Context(surf)
		layout = e["PangoCairo"].create_layout(ctx)
		desc = e["Pango"].FontDescription(e["family"])
		desc.set_absolute_size(px * e["Pango"].SCALE)
		layout.set_font_description(desc)
		layout.set_text(ch, -1)
		e["PangoCairo"].show_layout(ctx, layout)
		surf.flush()
		img = Image.frombuffer("RGBA", (px * 2, px * 2), bytes(surf.get_data()),
		                       "raw", "BGRa", surf.get_stride())
		box = img.getbbox()
		tile = None
		if box:
			glyph = img.crop(box)
			s = min((2 * self.cw - 1) / glyph.width, (self.lh - 2) / glyph.height)
			tile = glyph.resize((max(1, round(glyph.width * s)),
			                     max(1, round(glyph.height * s))), Image.LANCZOS)
		self._emojiTiles[ch] = tile
		return tile

	def fDrawText(self, d, img, x, y, text, fill):
		##	Draw in runs of a single font, so fallback glyphs slot inline. Emoji
		##	go down as color tiles pasted on two terminal cells.
		i = 0
		while i < len(text):
			ch = text[i]
			if self.fIsEmoji(ch):
				tile = self.fEmojiTile(ch)
				if tile:
					img.paste(tile, (round(x + (2 * self.cw - tile.width) / 2),
					                 round(y + (self.lh - 2 - tile.height) / 2) + 1), tile)
					x += 2 * self.cw
					i += 1
					continue
			f = self.fFontFor(ch)
			j = i + 1
			##	Combining marks ride the run of their base char, else shaping
			##	splits and a Devanagari matra draws as dotted-circle + box.
			while j < len(text) and not self.fIsEmoji(text[j]) \
					and (unicodedata.category(text[j]).startswith("M")
					     or self.fFontFor(text[j]) is f):
				j += 1
			d.text((x, y), text[i:j], font=f, fill=fill)
			x += d.textlength(text[i:j], font=f)
			i = j
		return x

	def fCells(self, ch):
		##	Terminal cell count: emoji and East-Asian wide chars take two.
		if self.fIsEmoji(ch) or unicodedata.east_asian_width(ch) in ("W", "F"):
			return 2
		return 1

	def fPutText(self, text, colorkey="fg", overflow="truncate"):
		##	Split on terminal cell widths, not char counts, so wide glyphs
		##	(emoji, CJK) don't push a line past the window edge.
		while True:
			used, cut = 0, len(text)
			for i, ch in enumerate(text):
				used += self.fCells(ch)
				if used > self.cols:
					cut = i
					break
			if overflow == "truncate" and cut < len(text):
				self.fPut([(text[: max(0, cut - 1)], colorkey), ("…", "dim")])
				return
			self.fPut([(text[:cut], colorkey)])
			text = text[cut:]
			if not text:
				break

	def fTextW(self, text):
		##	Pixel width with the same runs fDrawText uses.
		w, i = 0.0, 0
		while i < len(text):
			if self.fIsEmoji(text[i]) and self.fEmojiTile(text[i]):
				w += 2 * self.cw
				i += 1
				continue
			f = self.fFontFor(text[i])
			j = i + 1
			while j < len(text) and not self.fIsEmoji(text[j]) \
					and (unicodedata.category(text[j]).startswith("M")
					     or self.fFontFor(text[j]) is f):
				j += 1
			w += f.getlength(text[i:j])
			i = j
		return w

	def fRestScroll(self):
		##	Where the view settles: content bottom (incl. the live line when the
		##	prompt is showing) on the grid.
		rows = len(self.lines) + (1 if self.showPrompt else 0)
		return max(0.0, rows * self.lh - self.viewH)

	def fCursorTarget(self):
		##	Cursor cell on the live line, in view (layer) px at current scroll.
		x = self.fTextW("".join(t for t, _ in self.prompt) + self.typed)
		return x, len(self.lines) * self.lh - self.scroll

	def render(self, cursor, identColors):
		##	cursor: None = hidden, else (x, y) view px of the block's top-left.
		img = Image.new("RGB", (CANVAS_W, CANVAS_H), THEME["outer"])
		d = ImageDraw.Draw(img)
		x0, y0 = MARGIN, MARGIN
		d.rectangle([x0, y0, x0 + self.winW, y0 + self.winH],
		            fill=THEME["bg"], outline=THEME["border"])
		d.rectangle([x0, y0, x0 + self.winW, y0 + TITLE_H],
		            fill=THEME["titlebar"])
		for i, c in enumerate(THEME["dots"]):
			cx = x0 + 18 + i * 20
			d.ellipse([cx, y0 + 12, cx + 11, y0 + 23], fill=c)
		tw = d.textlength(self.title, font=self.font)
		self.fDrawText(d, img, x0 + (self.winW - tw) / 2,
		               y0 + (TITLE_H - self.lh) / 2 + 1, self.title, THEME["titletxt"])

		##	Text: content lines at their scrolled offsets, drawn on a layer the
		##	size of the view so partial lines clip cleanly at the edges.
		colors = dict(THEME, **identColors)
		layer = Image.new("RGB", (self.textW, self.viewH), THEME["bg"])
		ld = ImageDraw.Draw(layer)
		content = self.lines + ([self.prompt + [(self.typed, "fg")]]
		                        if self.showPrompt else [])
		first = max(0, int(self.scroll // self.lh))
		last = min(len(content), int((self.scroll + self.viewH) // self.lh) + 2)
		for i in range(first, last):
			x, y = 0, round(i * self.lh - self.scroll)
			for text, key in content[i]:
				x = self.fDrawText(ld, layer, x, y, text, colors[key])
		if cursor is not None:
			cx, cy = cursor
			ld.rectangle([cx + 1, cy + 1, cx + self.cw, cy + self.lh - 3],
			             fill=THEME["fg"])
		img.paste(layer, (self.termX, self.termY))
		return img.quantize(palette=self.pal, dither=Image.Dither.NONE)


class Movie:
	##	Ordered (frame, duration) list. Identical consecutive frames merge into
	##	one longer frame. Durations snap to the FRAME_MS grid rather than GIF's
	##	own centisecond one, so the whole loop plays at an exact 50 fps instead
	##	of drifting onto odd 30/50/90ms frames; bank the rounding remainder
	##	instead of losing it every keystroke.
	def __init__(self):
		self.frames, self.durs, self._rem = [], [], 0.0

	def add(self, img, ms):
		ms += self._rem
		dur = max(FRAME_MS, int(round(ms / FRAME_MS)) * FRAME_MS)
		self._rem = ms - dur if ms > FRAME_MS else 0.0
		if self.frames and img.tobytes() == self.frames[-1].tobytes():
			self.durs[-1] += dur
		else:
			self.frames.append(img)
			self.durs.append(dur)


def fMain():
	ap = argparse.ArgumentParser(add_help=True)
	ap.add_argument("--scenario", required=True)
	ap.add_argument("--out", required=True)
	ap.add_argument("--bin", default="")
	ap.add_argument("--seed", type=int, default=None)
	ap.add_argument("--font", default="")
	ap.add_argument("-q", "--quiet", action="store_true")
	args = ap.parse_args()

	sc = fLoadScenario(args.scenario)
	rng = random.Random(args.seed if args.seed is not None else sc.get("seed", 11))
	prog = sc.get("prog", "prog")
	binpath = args.bin or sc.get("bin", prog)

	prefs = [args.font] if args.font else sc.get("font", [])
	prefs = prefs + ["Monaspace Argon SemiBold", "JetBrains Mono",
	                 "Cascadia Mono", "DejaVu Sans Mono"]
	font, fontName = fFindFont([p for p in prefs if p], sc.get("fontsize", 15))

	userTint, hostTint = rng.sample(IDENT_TINTS, 2)
	user = rng.choice(IDENT_USERS)
	host = rng.choice(IDENT_HOSTS)

	def fPromptFor(cwd):
		##	Per step, because a demo about folders has to show the folder. Steps
		##	that say nothing get "~", so a scenario written before this reads the same.
		return [(user, "user"), ("@", "gray"), (host, "host"), (":", "gray"),
		        (cwd, "gray"), ("$ ", "gray")]

	identColors = {"user": userTint, "host": hostTint}

	scr = Screen(font, fontName, sc.get("title", prog),
	             fPromptFor(sc["step"][0].get("cwd", "~")), None, sc.get("fontsize", 15))

	##	A typed line past the right edge is cut off with nothing said, which reads back as a
	##	typo in the caption rather than as a caption that needs shortening - and the budget
	##	moves whenever a step's cwd does. Output lines are left alone: overflow= already says
	##	what should happen to those, and they are the program's to decide, not the author's.
	for stepNo, step in enumerate(sc["step"], 1):
		promptW = len("".join(t for t, _ in fPromptFor(step.get("cwd", "~"))))
		typed = [("note", "# " + step["note"]) if step.get("note") else None,
		         ("show", step["show"].replace("{prog}", prog).replace("{bin}", prog))]
		for what, text in (t for t in typed if t):
			if promptW + len(text) > scr.cols:
				sys.stderr.write(
					f"gen-demo-gif: step {stepNo} {what} is {promptW + len(text) - scr.cols}"
					f" characters wider than the {scr.cols}-column screen, and will be cut off\n")

	##	Run every command up front: the outputs feed the demo AND tell the
	##	palette which emoji it must carry before the first frame renders.
	stepOut = [fRunStep(step, prog, binpath) for step in sc["step"]]
	emojiSet = sorted({ch for segs in stepOut for ln in fStepText(segs) for ch in ln
	                   if scr.fIsEmoji(ch)})
	tiles = [t for t in (scr.fEmojiTile(ch) for ch in emojiSet) if t]
	scr.pal = fBuildPalette(userTint, hostTint, tiles)

	mov = Movie()
	wpmDigits = sc.get("wpm_digits", WPM_DIGITS)
	typeNotes = bool(sc.get("type_notes", True))
	shown = list(scr.fCursorTarget())    # displayed cursor; glides toward its cell

	def snap(ms, cursor=True):
		mov.add(scr.render(tuple(shown) if cursor else None, identColors), ms)

	def glideCursor(ms):
		##	Slide the shown cursor toward its cell across the keystroke's time,
		##	one step per frame - a cap here would judder the long keystrokes
		##	(flag beats, slowed digits) that most need the glide.
		tx, ty = scr.fCursorTarget()
		steps = max(1, int(ms // FRAME_MS))
		x0, y0 = shown
		for s in range(1, steps + 1):
			shown[0] = x0 + (tx - x0) * s / steps
			shown[1] = y0 + (ty - y0) * s / steps
			snap(ms / steps)

	def settle(rate, cursor=True):
		##	Smooth-scroll the view to rest; the cursor rides along on its line.
		while abs(scr.fRestScroll() - scr.scroll) >= 0.5:
			d = scr.fRestScroll() - scr.scroll
			scr.scroll += (1 if d > 0 else -1) * min(abs(d), rate * FRAME_MS / 1000.0)
			shown[:] = scr.fCursorTarget()
			snap(FRAME_MS, cursor)
		scr.scroll = scr.fRestScroll()
		shown[:] = scr.fCursorTarget()

	def blinkPause(totalMs):
		##	Idle at the prompt: block cursor blinking at the usual cadence.
		on = True
		left = totalMs
		while left > 0:
			step = min(BLINK_MS, left)
			snap(step, cursor=on)
			on = not on
			left -= step

	def drawOut(lines, rate, lineMs, overflow):
		for ln in lines:
			scr.fPutText(ln, "fg", overflow)
			if scr.fRestScroll() > scr.scroll + 0.5:
				settle(rate, cursor=False)
			else:
				snap(lineMs, cursor=False)

	def drawAsk(ask, answer, rate, askPause):
		##	The question is a live line, the way the shell's own prompt is: it holds
		##	the cursor, the answer types onto it, and Enter commits the pair. A demo
		##	of a tool whose whole claim is that it asks first has to show somebody
		##	being asked, so this is played out rather than skipped past.
		shellPrompt = scr.prompt
		scr.prompt, scr.showPrompt = [(ask, "fg")], True
		shown[:] = scr.fCursorTarget()
		if scr.fRestScroll() > scr.scroll + 0.5:
			settle(rate)
		readMs = rng.uniform(900, 1400)              # drawn either way, so the rest of the run doesn't shift
		blinkPause(readMs if askPause is None else 1000 * askPause)   # read the plan before answering
		for (action, ch), delay in fTypeEvents(answer, rng, WPM_LETTERS, False, wpmDigits):
			if action == "type":
				scr.typed += ch
			elif action == "bs":
				scr.typed = scr.typed[:-1]
			glideCursor(delay)
		snap(rng.uniform(260, 480))                  # beat before Enter
		scr.fPut([(ask, "fg"), (scr.typed, "fg")])
		scr.typed = ""
		scr.prompt, scr.showPrompt = shellPrompt, False
		if scr.fRestScroll() > scr.scroll + 0.5:
			settle(rate, cursor=False)

	snap(700)                                        # opening frame = loop-in target
	for stepIdx, step in enumerate(sc["step"]):
		rate = float(step.get("scrollrate", SCROLL_RATE))
		lineMs = float(step.get("linems", FRAME_MS))
		##	Before the clear below, which re-places the cursor against this prompt's width.
		scr.prompt = fPromptFor(step.get("cwd", "~"))
		if step.get("clear"):
			##	Start the step at the top of a fresh screen. Smooth scrolling
			##	costs a near-full-frame delta per frame, so a demo that never
			##	builds a long scrollback stays far smaller than one that does.
			scr.lines, scr.scroll = [], 0.0
			shown[:] = scr.fCursorTarget()
		for noteOrCmd, key, wpm, typos in (
				(("# " + step["note"]) if step.get("note") else None, "dim", WPM_NOTES, False),
				(step["show"].replace("{prog}", prog).replace("{bin}", prog), "fg", WPM_LETTERS, True)):
			if noteOrCmd is None:
				continue
			if key == "dim" and not typeNotes:
				##	A caption is narration, not demonstration. Watching one arrive a
				##	letter at a time costs more screen time than the command it
				##	introduces, so it appears whole and is simply read.
				scr.typed = noteOrCmd
				shown[:] = scr.fCursorTarget()
				blinkPause(NOTE_HOLD_MS)
			else:
				for (action, ch), delay in fTypeEvents(noteOrCmd, rng, wpm, typos, wpmDigits):
					if action == "type":
						scr.typed += ch
					elif action == "bs":
						scr.typed = scr.typed[:-1]
					glideCursor(delay)
				snap(rng.uniform(260, 480) if key == "fg" else 130)   # beat before Enter
			scr.fPut(list(scr.prompt) + [(scr.typed, key)])
			scr.typed = ""
			if key == "dim":                         # notes: no output to run
				if scr.fRestScroll() > scr.scroll + 0.5:
					settle(rate)
				else:
					glideCursor(130)                 # cursor hop onto the new line
				snap(140)
				continue
			##	Prompt stays hidden until the command's output is fully in, the
			##	way a real shell does it - except where the command stops to ask
			##	something, which puts a live line back on screen mid-output.
			scr.showPrompt = False
			if scr.fRestScroll() > scr.scroll + 0.5:
				settle(rate, cursor=False)
			tail = []
			for seg in stepOut[stepIdx]:
				if seg[0] == "ask":
					drawAsk(seg[1], seg[2], rate, step.get("askpause"))
					tail = []
				else:
					drawOut(seg[1], rate, lineMs, step.get("overflow", "truncate"))
					tail = seg[1]
			if tail and tail[-1].strip():
				scr.fPut([("", "fg")])               # breathe before the next prompt
				settle(rate, cursor=False)
			scr.showPrompt = True
			shown[:] = scr.fCursorTarget()
			if scr.fRestScroll() > scr.scroll + 0.5:
				settle(rate)
			snap(60)
			blinkPause(1000 * float(step.get("pause", 2.6)))

	##	Tail: hold the last frame dead still, then a hard cut to black before the
	##	loop repeats. The black is one held frame (a cut, not a fade), so it costs
	##	almost nothing.
	holdMs  = 1000 * float(sc.get("end_hold", 3.0))
	blackMs = 1000 * float(sc.get("end_black", 2.0))
	if holdMs > 0:
		snap(holdMs)
	if blackMs > 0:
		black = Image.new("RGB", (CANVAS_W, CANVAS_H), (0, 0, 0)).quantize(
			palette=scr.pal, dither=Image.Dither.NONE)
		mov.add(black, blackMs)

	os.makedirs(os.path.dirname(os.path.abspath(args.out)) or ".", exist_ok=True)
	mov.frames[0].save(args.out, format="GIF", save_all=True, append_images=mov.frames[1:],
	                   duration=mov.durs, loop=0, optimize=False)
	if not args.quiet:
		secs = sum(mov.durs) / 1000.0
		kb = os.path.getsize(args.out) // 1024
		print(f"gen-demo-gif: {args.out}: {len(mov.frames)} frames, "
		      f"{secs:.1f}s loop, {kb} KiB, font: {fontName}, "
		      f"{scr.cols}x{scr.rows} cells, ident: {user}@{host}")


if __name__ == "__main__":
	fMain()


##	History:
##		- 20261120: Copied from gitsby to tt-minecraft-safeserver, unchanged.
##		- 20261110: askpause= sets how long a step that asks holds on the plan
##			before its answer is typed.
##		- 20261107: One import per line, no `;` statements.
##		- 20260916: type_notes = false shows each caption whole instead of typing
##			it out. Typing them was 12.9s of a 67.4s loop - more screen time than
##			the commands they introduce, spent on narration.
##		- 20260916: A step can carry ask= and answer=. The command runs with a
##			terminal on its stdin, so it really does stop and ask, and the answer
##			is typed on camera before the rest of its output arrives. stdout stays
##			a pipe, so nothing a program saves for a terminal reaches the frames.
##		- 20260914: -q is accepted as well as --quiet. The pipeline hands the
##			same flag to everything it runs.
##		- 20260727: Every frame duration now snaps to the FRAME_MS grid, so the
##			loop really is 50 fps end to end (it had been drifting onto 30/50/
##			90ms frames off GIF's own centisecond rounding). Cursor glide runs
##			one step per frame, uncapped. Scroll slowed to 150 px/s - 7 frames
##			per line instead of 5, which is where it stops reading as stepped -
##			while non-scrolling output drops to one frame per line, so a screen
##			fills instantly and only the scroll is deliberate. Scroll frames are
##			~97% of the file, so that rate is the one real size dial.
##		- 20260725: 50 fps (20ms) frame interval for scrolling and cursor glide -
##			at the old 80ms any scrollrate over ~275 px/s finished a line in one
##			frame, so the scroll was a jump and the rate knob did nothing. New
##			per-step clear= wipes the scrollback, which keeps a smooth scroll
##			affordable (every scroll frame is a near-full-frame delta).
##		- 20260725: stderr shares the stdout pipe, so mixed-stream output keeps
##			its real order. Palette padded to the next power of two instead of
##			256, so the GIF's LZW code width matches the colors actually in use.
##		- 20260713: End of loop holds the final frame (end_hold, 3s) then hard-
##			cuts to black (end_black, 2s) before repeating.
##		- 20260711: v1.2. Antialiased text again (ramped 256 palette), color
##			emoji tiles, prompt hidden until output lands, faster typing and
##			scrolling, cell-width wrap.
##		- 20260711: v1.1. Pixel-smooth scrolling and cursor glide; scenario
##			knobs wpm_digits, scrollrate, linems.
##		- 20260711: v1.0. Scenario-driven typing/render/fade engine.
