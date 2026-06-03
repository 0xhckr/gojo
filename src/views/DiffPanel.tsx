import { useState, useEffect, useMemo } from "react"
import { type StatusEntry } from "../jj.js"
import { colors } from "../styles.js"
import { renderDiff, type DiffRow, type DiffLine, type DiffFileHeader, type DiffHunkHeader } from "../diffRenderer.js"

interface DiffPanelProps {
	width: number
	rev: string
	loading: boolean
	diffContent: string
	statusEntries: StatusEntry[]
	scrollY: number
}

// ── Color palette for diff rendering ──────────────────────────────────────

const DIFF_COLORS = {
	addedSign: "#5ecc71",
	addedBg: "#1a2e1a",
	removedSign: "#ff6762",
	removedBg: "#2e1a1a",
	contextFg: "#d0d0d0",
	hunkHeaderFg: "#5fafaf",
	hunkHeaderBg: "#1a2a2a",
	fileHeaderFg: "#d7af5f",
	fileHeaderBg: "#2a2a1a",
	lineNumber: "#605F6B",
	border: "#444444",
}

/** Build line-number gutter text for a diff line. */
function lineNumText(line: DiffLine, digits: number): string {
	const pad = (n: number | undefined) =>
		n !== undefined ? String(n).padStart(digits, " ") : " ".repeat(digits)
	return `${pad(line.oldLineNum)} ${pad(line.newLineNum)} ${line.sign}`
}

/** Compute the max line-number width across all rows. */
function maxLineDigits(files: DiffRow[][]): number {
	let max = 1
	for (const rows of files) {
		for (const row of rows) {
			if (row.type === "line") {
				const line = row as DiffLine
				if (line.oldLineNum !== undefined) max = Math.max(max, String(line.oldLineNum).length)
				if (line.newLineNum !== undefined) max = Math.max(max, String(line.newLineNum).length)
			}
		}
	}
	return max
}

// ── DiffPanel ─────────────────────────────────────────────────────────────

export function DiffPanel({ width, rev, loading, diffContent, statusEntries, scrollY }: DiffPanelProps) {
	const [parsedFiles, setParsedFiles] = useState<DiffRow[][]>([])
	const [parseError, setParseError] = useState<string | null>(null)

	// Parse diff content when it arrives
	useEffect(() => {
		if (!diffContent) {
			setParsedFiles([])
			return
		}

		let cancelled = false
		setParseError(null)

		renderDiff(diffContent)
			.then((result) => {
				if (!cancelled) setParsedFiles(result.files)
			})
			.catch((err) => {
				if (!cancelled) setParseError(err instanceof Error ? err.message : String(err))
			})

		return () => { cancelled = true }
	}, [diffContent])

	// Flatten all file rows into one list for scrolling
	const allRows = useMemo(() => parsedFiles.flat(), [parsedFiles])

	// Measure line-number width
	const digits = maxLineDigits(parsedFiles)
	const gutterWidth = digits * 2 + 4 // "NNN NNN + "

	// Title bar
	const titleLine = ` ${rev}${loading ? "  loading…" : ""}  (enter/q to close) `

	// Status color map
	const STATUS_COLORS: Record<string, string> = {
		Added: colors.green,
		Modified: colors.yellow,
		Removed: colors.red,
		Conflicted: colors.magenta,
	}

	// Build typed status entries for rendering
	type StatusItem = { sym: string; path: string; color: string }
	const statusItems: StatusItem[] = []
	for (const e of statusEntries) {
		const sym = e.status === "Added" ? "A" : e.status === "Modified" ? "M" : e.status === "Removed" ? "D" : "C"
		statusItems.push({ sym, path: e.path, color: STATUS_COLORS[e.status] ?? colors.gray })
	}

	// Whether we have parsed results ready
	const hasParsed = allRows.length > 0 || !diffContent

	// Fixed header: title + status + separator = N lines
	const statusLineCount = 1 + Math.max(statusItems.length, 1) // header + items or "(no changes)"
	const headerLines = 1 + statusLineCount + 1 // title + status + separator

	// Available height for diff content (we don't know terminal height here,
	// so render all rows and let the parent box clip)
	const visibleRows = allRows.slice(scrollY)

	return (
		<box width={width} flexDirection="column">
			{/* Title */}
			<box width={width} height={1} style={{ backgroundColor: colors.darkPurple }}>
				<text fg={colors.white} content={titleLine} />
			</box>

			{/* Status summary */}
			{/* Status header */}
			<text fg={colors.gray} content={" status"} />

			{statusItems.length === 0 ? (
				<text fg={colors.gray} content="  (no changes)" />
			) : (
				statusItems.map((item, i) => (
					<text key={`status:${i}`}>
						<span fg={item.color}>{`  ${item.sym} `}</span>
						<span fg={item.color}>{item.path}</span>
					</text>
				))
			)}

			{/* Separator */}
			<text fg={DIFF_COLORS.border} content={"─".repeat(width)} />

			{/* Parse error */}
			{parseError && (
				<text fg={colors.red} content={` parse error: ${parseError}`} />
			)}

			{/* Rendered diff files */}
			{hasParsed ? (
				visibleRows.map((row) => {
					if (row.type === "file-header") {
						const header = row as DiffFileHeader
						const label = header.prevPath
							? `${header.prevPath} → ${header.path}  (${header.changeType})`
							: `${header.path}  (${header.changeType})`
						return (
							<box key={row.key} width={width} height={1} style={{ backgroundColor: DIFF_COLORS.fileHeaderBg }}>
								<text>
									<b fg={DIFF_COLORS.fileHeaderFg}>{` ${label}`}</b>
								</text>
							</box>
						)
					}

					if (row.type === "hunk-header") {
						const hunk = row as DiffHunkHeader
						return (
							<box key={row.key} width={width} height={1} style={{ backgroundColor: DIFF_COLORS.hunkHeaderBg }}>
								<text fg={DIFF_COLORS.hunkHeaderFg} content={` ${hunk.text}`} />
							</box>
						)
					}

					// Diff line
					const line = row as DiffLine
					const gutter = lineNumText(line, digits)
					const contentWidth = width - gutterWidth

					let lineFg: string | undefined
					let lineBg: string | undefined
					switch (line.kind) {
						case "addition":
							lineFg = DIFF_COLORS.addedSign
							lineBg = DIFF_COLORS.addedBg
							break
						case "deletion":
							lineFg = DIFF_COLORS.removedSign
							lineBg = DIFF_COLORS.removedBg
							break
						default:
							lineFg = DIFF_COLORS.contextFg
							break
					}

					// Measure visible width from spans
					let usedWidth = 0
					for (const s of line.spans) usedWidth += s.text.length
					const padNeeded = Math.max(0, contentWidth - usedWidth)

					return (
						<box key={row.key} width={width} height={1} style={lineBg ? { backgroundColor: lineBg } : undefined}>
							<text>
								{/* Line number gutter */}
								<span fg={DIFF_COLORS.lineNumber}>{gutter}</span>
								{/* Code content with syntax colors */}
								{line.spans.map((span, i) => (
									<span
										key={i}
										fg={span.fg ?? lineFg}
										bg={span.bg ?? lineBg}
									>
										{span.text}
									</span>
								))}
								{/* Trailing padding to fill bg color */}
								{padNeeded > 0 && lineBg ? (
									<span bg={lineBg}>{" ".repeat(padNeeded)}</span>
								) : null}
							</text>
						</box>
					)
				})
			) : (
				/* Fallback: raw diff while parsing */
				<text fg={colors.white} content={diffContent} />
			)}
		</box>
	)
}
