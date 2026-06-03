import { useState, useCallback, useEffect, useRef, useMemo } from "react"
import { type KeyEvent } from "@opentui/core"
import { useKeyboard, useTerminalDimensions, useRenderer, useFocus } from "@opentui/react"
import type { CliRenderer } from "@opentui/core"

// Module-level quit function, set during App init
let quitFn: (() => void) | null = null
export function quit() { quitFn?.() }
import { JJRunner, type LogEntry, type StatusEntry, loadConfig } from "./jj.js"
import { colors, spinnerFrames } from "./styles.js"
import { useSpinner } from "./hooks.js"
import { LogView } from "./views/LogView.js"
import { DiffPanel } from "./views/DiffPanel.js"
import { HelpView } from "./views/HelpView.js"

// ── Types ─────────────────────────────────────────────────────────────────

export type View = "log" | "help"
export type BookmarkSubcmd = "c" | "d" | "f" | "m" | "r" | "s" | "t" | "T" | "l" | ""
export type RemoteSubcmd = "a" | "l" | "r" | "m" | "s" | ""

// Helper to build styled keybind label spans: each [text, match] becomes
// a word with the first occurrence of `match` underlined + purple.
function hlNodes(items: Array<[string, string]>, color: string, hlColor = colors.purple, sep = " ") {
	const nodes: any[] = []
	for (let i = 0; i < items.length; i++) {
		const [text, match] = items[i]
		const idx = text.indexOf(match)
		if (idx < 0) {
			nodes.push(<span fg={color}>{text}</span>)
		} else {
			if (idx > 0) nodes.push(<span fg={color}>{text.slice(0, idx)}</span>)
			nodes.push(<u fg={hlColor}>{match}</u>)
			if (idx + match.length < text.length) nodes.push(<span fg={color}>{text.slice(idx + match.length)}</span>)
		}
		if (i < items.length - 1) nodes.push(<span fg={color}>{sep}</span>)
	}
	return nodes
}

// ── App ───────────────────────────────────────────────────────────────────

export function App() {
	const { width, height } = useTerminalDimensions()
	const renderer = useRenderer() as CliRenderer

	// Register clean quit handler
	useEffect(() => {
		quitFn = () => { renderer.destroy(); process.exit(0) }
		return () => { quitFn = null }
	}, [renderer])

	// Boot jj runner
	const runnerRef = useRef<JJRunner | null>(null)
	const [ready, setReady] = useState(false)
	const [bootError, setBootError] = useState<string | null>(null)

	useEffect(() => {
		loadConfig()
			.then((cfg) => {
				runnerRef.current = new JJRunner(cfg.jjPath, cfg.repoRoot, cfg)
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
	const [diffScrollY, setDiffScrollY] = useState(0)

	// Bookmark mode
	const [bookmarkMode, setBookmarkMode] = useState(false)
	const [bookmarkAction, setBookmarkAction] = useState<BookmarkSubcmd>("")
	const [bookmarkInput, setBookmarkInput] = useState("")

	// Autocomplete state
	const [acOriginal, setAcOriginal] = useState<string | null>(null)
	const [acIdx, setAcIdx] = useState(0)

	// Git mode
	const [gitMode, setGitMode] = useState(false)

	// Remote mode
	const [remoteMode, setRemoteMode] = useState(false)
	const [remoteAction, setRemoteAction] = useState<RemoteSubcmd>("")
	const [remoteInput, setRemoteInput] = useState("")

	// Autocomplete candidates (bookmark names + change/commit IDs)
	const allCandidates = useMemo(() => {
		const seen = new Set<string>()
		for (const entry of logEntries) {
			for (const bm of entry.bookmarks) {
				if (bm) seen.add(bm)
			}
			seen.add(entry.changeId)
			seen.add(entry.commitId)
		}
		return [...seen]
	}, [logEntries])

	// Display suggestions (filtered, limited)
	const displaySuggestions = useMemo(() => {
		if (!bookmarkAction) return []
		if (bookmarkAction === "r" && bookmarkInput.includes(" ")) return []
		const prefix = acOriginal ?? bookmarkInput
		const filtered = prefix
			? allCandidates.filter(c => c.startsWith(prefix))
			: allCandidates
		return filtered.slice(0, 10)
	}, [bookmarkAction, bookmarkInput, acOriginal, allCandidates])

	const suggestionsVisible = bookmarkAction && displaySuggestions.length > 0

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

	// Auto-refresh when terminal window regains focus
	useFocus(() => {
		if (ready) refresh()
	})

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
			setDiffScrollY(0)
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
		(entry: LogEntry) => {
			if (!runnerRef.current) return

			// Suspend the renderer so the terminal is restored for $EDITOR
			renderer.suspend()

			const child = import("node:child_process").then(({ spawn }) => {
				const p = spawn("jj", ["describe", "-r", entry.changeId], {
					stdio: "inherit",
				})
				return new Promise<number | null>((resolve) => {
					p.on("exit", resolve)
				})
			})

			child.then((code) => {
				// Give the terminal a moment to settle after editor exits
				setTimeout(() => {
					renderer.resume()
					setMessage("described " + entry.changeId)
					loadLog()
				}, 50)
			})
		},
		[loadLog, renderer],
	)

	const doAiDescribe = useCallback(
		async (entry: LogEntry) => {
			if (!runnerRef.current) return
			setAiLoading(prev => new Set(prev).add(entry.changeId))
			setError(null)
			setMessage("AI generating message for " + entry.changeId + "…")
			try {
				const msg = await runnerRef.current.aiDescribe(entry.changeId)
				await runnerRef.current.describe(entry.changeId, msg)
				setMessage("AI described " + entry.changeId + ": " + msg)
				await loadLog()
			} catch (err: unknown) {
				setError(err instanceof Error ? err.message : String(err))
			} finally {
				setAiLoading(prev => {
					const next = new Set(prev)
					next.delete(entry.changeId)
					return next
				})
			}
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
			const entry = selectedEntry()
			await runnerRef.current.new(entry?.changeId)
			setMessage("created new change")
			await loadLog()
		} catch (err: unknown) {
			setError(err instanceof Error ? err.message : String(err))
		}
	}, [loadLog, selectedEntry])

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

	// ── Remote action execution ────────────────────────────────────────
	const executeRemote = useCallback(
		async (action: RemoteSubcmd, input: string) => {
			if (!runnerRef.current) return

			try {
				switch (action) {
					case "a": {
						const parts = input.split(/\s+/)
						if (parts.length < 2) throw new Error("add requires: <name> <url>")
						await runnerRef.current.remoteAdd(parts[0], parts.slice(1).join(" "))
						break
					}
					case "l": {
						const out = await runnerRef.current.remoteList()
						setDiffContent(out)
						setDiffRev("remote list")
						setDiffOpen(true)
						setDiffLoading(false)
						setRevStatusEntries([])
						break
					}
					case "r": {
						await runnerRef.current.remoteRemove(input)
						break
					}
					case "m": {
						const parts = input.split(/\s+/)
						if (parts.length < 2) throw new Error("rename requires: <old> <new>")
						await runnerRef.current.remoteRename(parts[0], parts[1])
						break
					}
					case "s": {
						const parts = input.split(/\s+/)
						if (parts.length < 2) throw new Error("set-url requires: <name> <url>")
						await runnerRef.current.remoteSetUrl(parts[0], parts.slice(1).join(" "))
						break
					}
				}
				if (action !== "l") {
					setMessage(`remote ${action}: ${input}`)
					await refresh()
				}
			} catch (err: unknown) {
				setError(err instanceof Error ? err.message : String(err))
			}

			setRemoteMode(false)
			setRemoteAction("")
			setRemoteInput("")
		},
		[refresh],
	)

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
			quit()
			return
		}

		// Bookmark mode input
		if (bookmarkMode) {
			if (bookmarkAction) {
				if (k === "escape") {
					if (acOriginal !== null) {
						setBookmarkInput(acOriginal)
						setAcOriginal(null)
						setAcIdx(0)
						return
					}
					setBookmarkAction("")
					setBookmarkInput("")
					setAcOriginal(null)
					setAcIdx(0)
					return
				}
				if (k === "return") {
					setAcOriginal(null)
					setAcIdx(0)
					executeBookmark(bookmarkAction, bookmarkInput)
					return
				}
				if (k === "tab") {
					const prefix = acOriginal ?? bookmarkInput
					const filtered = prefix
						? allCandidates.filter(c => c.startsWith(prefix))
						: allCandidates
					if (filtered.length > 0) {
						if (acOriginal === null) {
							setAcOriginal(bookmarkInput)
							setAcIdx(0)
							setBookmarkInput(filtered[0])
						} else {
							const next = (acIdx + 1) % filtered.length
							setAcIdx(next)
							setBookmarkInput(filtered[next])
						}
					}
					return
				}
				if (k === "backspace" || k === "delete") {
					setBookmarkInput((prev: string) => prev.slice(0, -1))
					setAcOriginal(null)
					setAcIdx(0)
					return
				}
				// Printable characters via sequence
				const seq = key.sequence
				if (seq && seq.length === 1 && seq.charCodeAt(0) >= 32 && seq.charCodeAt(0) < 127) {
					setBookmarkInput((prev: string) => prev + seq)
					setAcOriginal(null)
					setAcIdx(0)
				}
				return
			}
			// Bookmark mode menu
			if (k === "escape" || k === "q") { setBookmarkMode(false); setAcOriginal(null); setAcIdx(0); return }
			if (["c", "d", "f", "m", "r", "s", "t"].includes(k)) {
				setBookmarkAction(k as BookmarkSubcmd)
				setBookmarkInput("")
				setAcOriginal(null)
				setAcIdx(0)
				return
			}
			if (key.shift && k === "t") {
				setBookmarkAction("T")
				setBookmarkInput("")
				setAcOriginal(null)
				setAcIdx(0)
				return
			}
			if (k === "l") { executeBookmark("l", ""); return }
			return
		}

		// Git mode
		if (gitMode) {
			// Remote sub-mode input
			if (remoteMode) {
				if (remoteAction) {
					if (k === "escape") {
						setRemoteAction("")
						setRemoteInput("")
						return
					}
					if (k === "return") {
						executeRemote(remoteAction, remoteInput)
						return
					}
					if (k === "backspace" || k === "delete") {
						setRemoteInput((prev: string) => prev.slice(0, -1))
						return
					}
					const seq = key.sequence
					if (seq && seq.length === 1 && seq.charCodeAt(0) >= 32 && seq.charCodeAt(0) < 127) {
						setRemoteInput((prev: string) => prev + seq)
					}
					return
				}
				// Remote mode menu
				if (k === "escape" || k === "q") { setRemoteMode(false); return }
				if (["a", "l", "r", "m", "s"].includes(k)) {
					if (k === "l") { executeRemote("l", ""); return }
					setRemoteAction(k as RemoteSubcmd)
					setRemoteInput("")
					return
				}
				return
			}

			if (k === "escape" || k === "q") { setGitMode(false); return }
			if (k === "f") { setGitMode(false); doGitFetch(); return }
			if (k === "p") { setGitMode(false); doGitPush(); return }
			if (k === "r") {
				setRemoteMode(true)
				setRemoteAction("")
				setRemoteInput("")
				return
			}
			return
		}

		// Global keys
		if (k === "q") {
			if (view === "help") { setView("log"); return }
			if (diffOpen) { setDiffOpen(false); return }
			quit()
			return
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
				if (k === "return" || k === "q" || k === "escape") { setDiffOpen(false); return }
				if (k === "up" || k === "k") { setDiffScrollY(y => Math.max(0, y - 1)); return }
				if (k === "down" || k === "j") { setDiffScrollY(y => y + 1); return }
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
				const entry = selectedEntry()
				if (entry) doAiDescribe(entry)
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
				setAcOriginal(null)
				setAcIdx(0)
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
			<box width={width} height={height}>
				<text fg={colors.red} content={` error: ${bootError} `} />
			</box>
		)
	}

	if (!ready || (logEntries.length === 0 && !error)) {
		return (
			<box width={width} height={height}>
				<text fg={colors.gray} content=" loading…" />
			</box>
		)
	}

	// Status bar
	let statusText: string | null = null
	let statusNodes: any[] | null = null
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
			statusText = ` [bookmark] ${(prompts[bookmarkAction] || "") + bookmarkInput}\u2588`
		} else {
			statusNodes = [
				<span fg={statusFg}>{" [bookmark mode] "}</span>,
				...hlNodes([
					["create", "c"], ["delete", "d"], ["forget", "f"],
					["list", "l"], ["move", "m"], ["rename", "r"],
					["set", "s"], ["track", "t"], ["untrack", "T"],
				], statusFg),
				<span fg={statusFg}>{"  "}</span>,
				...hlNodes([["cancel", "esc"]], statusFg),
				<span fg={statusFg}>{" "}</span>,
			]
		}
	} else if (gitMode) {
		if (remoteMode) {
			statusFg = colors.pink
			if (remoteAction) {
				const prompts: Record<string, string> = {
					a: "add (name url): ",
					r: "remove (name): ",
					m: "rename (old new): ",
					s: "set-url (name url): ",
				}
				statusText = ` [git > remote] ${(prompts[remoteAction] || "") + remoteInput}\u2588`
			} else {
				statusNodes = [
					<span fg={statusFg}>{" [git > remote] "}</span>,
					...hlNodes([
						["add", "a"], ["list", "l"], ["remove", "r"],
						["rename", "m"], ["set-url", "s"],
					], statusFg),
					<span fg={statusFg}>{"  "}</span>,
					...hlNodes([["cancel", "esc"]], statusFg),
					<span fg={statusFg}>{" "}</span>,
				]
			}
		} else {
			statusFg = colors.darkOrange
			statusNodes = [
				<span fg={statusFg}>{" [git mode] "}</span>,
				...hlNodes([
					["fetch", "f"], ["push", "p"], ["remote", "r"],
				], statusFg),
				<span fg={statusFg}>{"  "}</span>,
				...hlNodes([["cancel", "esc"]], statusFg),
				<span fg={statusFg}>{" "}</span>,
			]
		}
	} else if (error) {
		statusFg = colors.red
		statusText = ` \u2716 ${error.slice(0, width - 4)}`
	} else if (message) {
		statusText = ` ${message}`
	} else if (statusEntries.length > 0) {
		statusText = ` ${statusEntries.length} changed file(s)`
	} else {
		statusText = " clean working copy \u2713"
	}

	const helpBarNodes = [
		...hlNodes([
			["⏎diff", "⏎"], ["describe", "d"],
			["AI Desc", "D"], ["bookmark", "b"], ["git", "g"],
			["undo", "u"], ["edit", "e"], ["new", "n"],
			["abandon", "a"], ["?help", "?"], ["refresh", "r"], ["quit", "q"],
		], colors.gray, colors.purple, "  "),
	]

	return (
		<box width={width} height={height} flexDirection="column">
			{/* Content area */}
			<box flexGrow={1} flexDirection="column">
				{view === "help" ? (
					<HelpView width={width} />
				) : diffOpen ? (
					<DiffPanel
						width={width}
						rev={diffRev}
						loading={diffLoading}
						diffContent={diffContent}
						statusEntries={revStatusEntries}
						scrollY={diffScrollY}
					/>
				) : (
					<LogView
						width={width}
						height={height - 2 - (suggestionsVisible ? 1 : 0)}
						entries={logEntries}
						cursor={cursor}
						offset={offset}
						onOffsetChange={setOffset}
						aiLoading={aiLoading}
						aiSpinnerFrame={aiSpinnerFrame}
					/>
				)}
			</box>

			{/* Autocomplete suggestions */}
			{suggestionsVisible && (
				<box width={width} height={1} style={{ backgroundColor: colors.darkerGray }}>
					<text>
						<span fg={colors.darkGray}>{" tab:"}</span>
						{displaySuggestions.map((s, i) => (
							<span key={i} fg={i === (acOriginal !== null ? acIdx : -1) ? colors.yellow : colors.cyan}>
								{i > 0 ? " · " : " "}{s}
							</span>
						))}
					</text>
				</box>
			)}

			{/* Status bar */}
			<box width={width} height={1} style={{ backgroundColor: colors.darkerGray }}>
				{statusText !== null
					? <text content={statusText} fg={statusFg} />
					: <text>{...statusNodes!}</text>
				}
			</box>

			{/* Help bar */}
			<box width={width} height={1} style={{ backgroundColor: colors.darkerGray }}>
				<text>
					{" "}
					{...helpBarNodes}
				</text>
			</box>
		</box>
	)
}
