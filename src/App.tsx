import { useState, useCallback, useEffect, useRef } from "react"
import { type KeyEvent } from "@opentui/core"
import { useKeyboard, useTerminalDimensions, useRenderer } from "@opentui/react"
import { JJRunner, type LogEntry, type StatusEntry, loadConfig } from "./jj.js"
import { colors, spinnerFrames } from "./styles.js"
import { useSpinner } from "./hooks.js"
import { LogView } from "./views/LogView.js"
import { DiffPanel } from "./views/DiffPanel.js"
import { HelpView } from "./views/HelpView.js"

// ── Types ─────────────────────────────────────────────────────────────────

export type View = "log" | "help"
export type BookmarkSubcmd = "c" | "d" | "f" | "m" | "r" | "s" | "t" | "T" | "l" | ""

// ── App ───────────────────────────────────────────────────────────────────

export function App() {
	const { width, height } = useTerminalDimensions()
	const renderer = useRenderer()

	// Boot jj runner
	const runnerRef = useRef<JJRunner | null>(null)
	const [ready, setReady] = useState(false)
	const [bootError, setBootError] = useState<string | null>(null)

	useEffect(() => {
		loadConfig()
			.then((cfg) => {
				runnerRef.current = new JJRunner(cfg.jjPath, cfg.repoRoot)
				setReady(true)
			})
			.catch((err: unknown) => setBootError(err instanceof Error ? err.message : String(err)))
	}, [])

	// View state
	const [view, setView] = useState<View>("log")
	const [logEntries, setLogEntries] = useState<LogEntry[]>([])
	const [cursor, setCursor] = useState(0)
	const [offset, setOffset] = useState(0)
	const [statusEntries, setStatusEntries] = useState<StatusEntry[]>([])
	const [message, setMessage] = useState("")
	const [error, setError] = useState<string | null>(null)

	// Diff panel
	const [diffOpen, setDiffOpen] = useState(false)
	const [diffRev, setDiffRev] = useState("")
	const [diffContent, setDiffContent] = useState("")
	const [diffLoading, setDiffLoading] = useState(false)
	const [revStatusEntries, setRevStatusEntries] = useState<StatusEntry[]>([])

	// Bookmark mode
	const [bookmarkMode, setBookmarkMode] = useState(false)
	const [bookmarkAction, setBookmarkAction] = useState<BookmarkSubcmd>("")
	const [bookmarkInput, setBookmarkInput] = useState("")

	// Git mode
	const [gitMode, setGitMode] = useState(false)

	// AI describe
	const [aiLoading, setAiLoading] = useState<Set<string>>(new Set())
	const aiSpinnerFrame = useSpinner(aiLoading.size > 0)

	// ── Data loading ───────────────────────────────────────────────────
	const loadLog = useCallback(async () => {
		if (!runnerRef.current) return
		try {
			const entries = await runnerRef.current.log()
			setLogEntries(entries)
			setError(null)
			setMessage("")
		} catch (err: unknown) {
			setError(err instanceof Error ? err.message : String(err))
		}
	}, [])

	const loadStatus = useCallback(async () => {
		if (!runnerRef.current) return
		try {
			const entries = await runnerRef.current.status()
			setStatusEntries(entries)
		} catch (err: unknown) {
			setError(err instanceof Error ? err.message : String(err))
		}
	}, [])

	const refresh = useCallback(async () => {
		setMessage("refreshing…")
		await Promise.all([loadLog(), loadStatus()])
		setMessage("")
	}, [loadLog, loadStatus])

	// Initial load
	useEffect(() => {
		if (ready) refresh()
	}, [ready, refresh])

	// ── Helpers ────────────────────────────────────────────────────────
	const selectedEntry = useCallback((): LogEntry | null => {
		if (logEntries.length === 0 || cursor >= logEntries.length) return null
		return logEntries[cursor]
	}, [logEntries, cursor])

	// ── Actions ────────────────────────────────────────────────────────
	const openDiff = useCallback(
		async (entry: LogEntry) => {
			if (!runnerRef.current) return
			setDiffRev(entry.changeId)
			setDiffLoading(true)
			setDiffOpen(true)
			try {
				const [entries, diffOut] = await Promise.all([
					runnerRef.current.diffSummary(entry.commitId),
					runnerRef.current.diff(entry.commitId),
				])
				setRevStatusEntries(entries)
				setDiffContent(diffOut)
				setDiffLoading(false)
			} catch (err: unknown) {
				setError(err instanceof Error ? err.message : String(err))
				setDiffLoading(false)
			}
		},
		[],
	)

	const doDescribe = useCallback(
		async (entry: LogEntry) => {
			if (!runnerRef.current) return
			setMessage("opening editor…")
			const { spawn } = await import("node:child_process")
			const child = spawn("jj", ["describe", "-r", entry.changeId], { stdio: "inherit" })
			child.on("exit", () => {
				setMessage("described " + entry.changeId)
				loadLog()
			})
		},
		[loadLog],
	)

	const doEdit = useCallback(
		async (entry: LogEntry) => {
			if (!runnerRef.current) return
			setMessage("editing " + entry.changeId + "…")
			try {
				await runnerRef.current.edit(entry.changeId)
				setMessage("editing " + entry.changeId)
				await loadLog()
			} catch (err: unknown) {
				setError(err instanceof Error ? err.message : String(err))
			}
		},
		[loadLog],
	)

	const doNew = useCallback(async () => {
		if (!runnerRef.current) return
		setMessage("creating new change…")
		try {
			await runnerRef.current.new()
			setMessage("created new change")
			await loadLog()
		} catch (err: unknown) {
			setError(err instanceof Error ? err.message : String(err))
		}
	}, [loadLog])

	const doAbandon = useCallback(
		async (entry: LogEntry) => {
			if (!runnerRef.current) return
			if (entry.isWorkingCopy) {
				setError("cannot abandon the working copy")
				return
			}
			setMessage("abandoning " + entry.changeId + "…")
			try {
				await runnerRef.current.abandon(entry.changeId)
				setMessage("abandoned " + entry.changeId)
				await loadLog()
			} catch (err: unknown) {
				setError(err instanceof Error ? err.message : String(err))
			}
		},
		[loadLog],
	)

	const doUndo = useCallback(async () => {
		if (!runnerRef.current) return
		setMessage("undoing…")
		try {
			await runnerRef.current.undo()
			setMessage("undone")
			await refresh()
		} catch (err: unknown) {
			setError(err instanceof Error ? err.message : String(err))
		}
	}, [refresh])

	const doGitFetch = useCallback(async () => {
		if (!runnerRef.current) return
		setMessage("fetching…")
		try {
			await runnerRef.current.gitFetch()
			setMessage("fetched")
			await refresh()
		} catch (err: unknown) {
			setError(err instanceof Error ? err.message : String(err))
		}
	}, [refresh])

	const doGitPush = useCallback(async () => {
		if (!runnerRef.current) return
		setMessage("pushing…")
		try {
			await runnerRef.current.gitPush()
			setMessage("pushed")
			await refresh()
		} catch (err: unknown) {
			setError(err instanceof Error ? err.message : String(err))
		}
	}, [refresh])

	// ── Bookmark action execution ──────────────────────────────────────
	const executeBookmark = useCallback(
		async (action: BookmarkSubcmd, input: string) => {
			if (!runnerRef.current) return
			const entry = selectedEntry()
			const rev = entry?.changeId || ""

			try {
				switch (action) {
					case "c": await runnerRef.current.bookmarkCreate(input, rev); break
					case "d": await runnerRef.current.bookmarkDelete(input); break
					case "f": await runnerRef.current.bookmarkForget(input); break
					case "m": await runnerRef.current.bookmarkMove(input, rev); break
					case "r": {
						const parts = input.split(/\s+/)
						if (parts.length < 2) throw new Error("rename requires: <old> <new>")
						await runnerRef.current.bookmarkRename(parts[0], parts[1])
						break
					}
					case "s": await runnerRef.current.bookmarkSet(input, rev); break
					case "t": await runnerRef.current.bookmarkTrack(input); break
					case "T": await runnerRef.current.bookmarkUntrack(input); break
					case "l": {
						const out = await runnerRef.current.bookmarkList()
						setDiffContent(out)
						setDiffRev("bookmark list")
						setDiffOpen(true)
						setDiffLoading(false)
						setRevStatusEntries([])
						break
					}
				}
				if (action !== "l") {
					setMessage(`bookmark ${action}: ${input}`)
					await refresh()
				}
			} catch (err: unknown) {
				setError(err instanceof Error ? err.message : String(err))
			}

			setBookmarkMode(false)
			setBookmarkAction("")
			setBookmarkInput("")
		},
		[selectedEntry, refresh],
	)

	// ── Keyboard handling ──────────────────────────────────────────────
	useKeyboard((key: KeyEvent) => {
		if (!ready) return

		const k = key.name

		// Global: ctrl+c always quits
		if (key.ctrl && k === "c") {
			process.exit(0)
		}

		// Bookmark mode input
		if (bookmarkMode) {
			if (bookmarkAction) {
				if (k === "escape") {
					setBookmarkAction("")
					setBookmarkInput("")
					return
				}
				if (k === "return") {
					executeBookmark(bookmarkAction, bookmarkInput)
					return
				}
				if (k === "backspace" || k === "delete") {
					setBookmarkInput((prev: string) => prev.slice(0, -1))
					return
				}
				// Printable characters via sequence
				const seq = key.sequence
				if (seq && seq.length === 1 && seq.charCodeAt(0) >= 32 && seq.charCodeAt(0) < 127) {
					setBookmarkInput((prev: string) => prev + seq)
				}
				return
			}
			// Bookmark mode menu
			if (k === "escape" || k === "q") { setBookmarkMode(false); return }
			if (["c", "d", "f", "m", "r", "s", "t"].includes(k)) {
				setBookmarkAction(k as BookmarkSubcmd)
				setBookmarkInput("")
				return
			}
			if (key.shift && k === "t") {
				setBookmarkAction("T")
				setBookmarkInput("")
				return
			}
			if (k === "l") { executeBookmark("l", ""); return }
			return
		}

		// Git mode
		if (gitMode) {
			if (k === "escape" || k === "q") { setGitMode(false); return }
			if (k === "f") { setGitMode(false); doGitFetch(); return }
			if (k === "p") { setGitMode(false); doGitPush(); return }
			return
		}

		// Global keys
		if (k === "q") {
			if (view === "help") { setView("log"); return }
			if (diffOpen) { setDiffOpen(false); return }
			process.exit(0)
		}
		if (k === "question" || (key.shift && k === "/")) {
			if (diffOpen) { setDiffOpen(false); return }
			setView((v: View) => v === "help" ? "log" : "help")
			return
		}
		if (k === "r" && !diffOpen) { refresh(); return }

		// View-specific keys
		if (view === "log") {
			if (diffOpen) {
				if (k === "return" || k === "q") setDiffOpen(false)
				return
			}

			if (k === "up" || k === "k") { setCursor((c: number) => Math.max(0, c - 1)); return }
			if (k === "down" || k === "j") { setCursor((c: number) => Math.min(logEntries.length - 1, c + 1)); return }
			if (k === "home") { setCursor(0); return }
			if (k === "end" || (key.shift && k === "g")) { setCursor(logEntries.length - 1); return }
			if (k === "return") {
				const entry = selectedEntry()
				if (entry) openDiff(entry)
				return
			}
			if (k === "d" && !key.shift) {
				const entry = selectedEntry()
				if (entry) doDescribe(entry)
				return
			}
			if (key.shift && k === "d") {
				setError("AI describe not yet implemented in TypeScript version")
				return
			}
			if (k === "e") {
				const entry = selectedEntry()
				if (entry) doEdit(entry)
				return
			}
			if (k === "n") { doNew(); return }
			if (k === "a") {
				const entry = selectedEntry()
				if (entry) doAbandon(entry)
				return
			}
			if (k === "b") {
				setBookmarkMode(true)
				setBookmarkAction("")
				setBookmarkInput("")
				setError(null)
				setMessage("")
				return
			}
			if (k === "g" && !key.shift) {
				setGitMode(true)
				setError(null)
				setMessage("")
				return
			}
			if (k === "u") { doUndo(); return }
		}
	})

	// ── Layout ─────────────────────────────────────────────────────────
	if (bootError) {
		return (
			<box width={width} height={height} style={{ backgroundColor: colors.darkerGray }}>
				<text fg={colors.red} content={` error: ${bootError} `} />
			</box>
		)
	}

	if (!ready || (logEntries.length === 0 && !error)) {
		return (
			<box width={width} height={height} style={{ backgroundColor: colors.darkerGray }}>
				<text fg={colors.gray} content=" loading…" />
			</box>
		)
	}

	// Status bar text
	let statusBar = ""
	let statusFg = colors.gray
	if (bookmarkMode) {
		statusFg = colors.cyan
		if (bookmarkAction) {
			const prompts: Record<string, string> = {
				c: "create: ", d: "delete: ", f: "forget: ",
				m: `move to ${selectedEntry()?.changeId || ""}: `,
				r: "rename (old new): ",
				s: `set to ${selectedEntry()?.changeId || ""}: `,
				t: "track: ", T: "untrack: ",
			}
			statusBar = ` [bookmark] ${(prompts[bookmarkAction] || "") + bookmarkInput}█`
		} else {
			statusBar = " [bookmark mode] c:create d:delete f:forget l:list m:move r:rename s:set t:track T:untrack  esc:cancel "
		}
	} else if (gitMode) {
		statusFg = colors.darkOrange
		statusBar = " [git mode] f:fetch p:push  esc:cancel "
	} else if (error) {
		statusFg = colors.red
		statusBar = ` ✖ ${error.slice(0, width - 4)}`
	} else if (message) {
		statusBar = ` ${message}`
	} else if (statusEntries.length > 0) {
		statusBar = ` ${statusEntries.length} changed file(s)`
	} else {
		statusBar = " clean working copy ✓"
	}

	const helpBarText = " enter:diff  d:describe  shift+d:AI desc  b:bookmark  g:git  u:undo  e:edit  n:new  a:abandon  ?:help  r:refresh  q:quit "

	return (
		<box width={width} height={height} flexDirection="column" style={{ backgroundColor: colors.darkerGray }}>
			{/* Content area */}
			<box flexGrow={1} flexDirection="column" style={{ backgroundColor: colors.darkerGray }}>
				{view === "help" ? (
					<HelpView width={width} />
				) : diffOpen ? (
					<DiffPanel
						width={width}
						rev={diffRev}
						loading={diffLoading}
						diffContent={diffContent}
						statusEntries={revStatusEntries}
					/>
				) : (
					<LogView
						width={width}
						height={height - 2}
						entries={logEntries}
						cursor={cursor}
						offset={offset}
						onOffsetChange={setOffset}
						aiLoading={aiLoading}
						aiSpinnerFrame={aiSpinnerFrame}
					/>
				)}
			</box>

			{/* Status bar */}
			<box width={width} height={1} style={{ backgroundColor: colors.darkerGray }}>
				<text content={statusBar} fg={statusFg} />
			</box>

			{/* Help bar */}
			<box width={width} height={1} style={{ backgroundColor: colors.darkerGray }}>
				<text content={helpBarText} fg={colors.gray} />
			</box>
		</box>
	)
}
