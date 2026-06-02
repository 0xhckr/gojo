import { fg, bg, bold, dim, StyledText, type TextChunk, type StylableInput } from "@opentui/core"
import { type LogEntry } from "../jj.js"
import { colors, spinnerFrames } from "../styles.js"

interface LogViewProps {
	width: number
	height: number
	entries: LogEntry[]
	cursor: number
	offset: number
	onOffsetChange: (o: number) => void
	aiLoading: Set<string>
	aiSpinnerFrame: number
}

// Helper to build StyledText from chunks
function makeStyledText(chunks: StylableInput[]): StyledText {
	const textChunks: TextChunk[] = chunks.map((c) => {
		if (typeof c === "string" || typeof c === "number" || typeof c === "boolean") {
			return { __isChunk: true as const, text: String(c) }
		}
		return c as TextChunk
	})
	return new StyledText(textChunks)
}

export function LogView({ width, height, entries, cursor, offset, onOffsetChange, aiLoading, aiSpinnerFrame }: LogViewProps) {
	if (entries.length === 0) {
		return <text fg={colors.gray} content="  no revisions found" />
	}

	// ── Calculate visible range ────────────────────────────────────────
	const availableLines = height - 1 // top padding

	const commitLines = (idx: number) => 2 + entries[idx].edgeLines.length

	let off = offset
	if (cursor < off) off = cursor

	let end = off
	let usedLines = 0
	while (end < entries.length) {
		const h = commitLines(end)
		if (usedLines + h > availableLines && end > off) break
		usedLines += h
		end++
	}

	if (cursor >= end) {
		off = cursor
		end = cursor + 1
		usedLines = commitLines(cursor)
		while (off > 0) {
			const h = commitLines(off - 1)
			if (usedLines + h > availableLines) break
			usedLines += h
			off--
		}
	}

	if (off !== offset) onOffsetChange(off)

	// ── Build styled text lines ────────────────────────────────────────
	interface Line { chunks: StylableInput[]; highlighted: boolean }
	const lines: Line[] = []

	for (let i = off; i < end; i++) {
		const e = entries[i]
		const isHighlighted = i === cursor

		// Edge lines (graph branching) — skip for last visible
		if (i < end - 1) {
			for (const edge of e.edgeLines) {
				lines.push({ chunks: [fg(colors.darkGray)(edge)], highlighted: false })
			}
		}

		// Header line: graph_prefix + node + changeId + author + date + commitId + bookmarks
		const headerChunks: StylableInput[] = []

		// Graph prefix before node
		headerChunks.push(fg(colors.darkGray)(e.headerPrefix))

		// Node character
		if (e.isWorkingCopy) {
			headerChunks.push(bold(fg(colors.yellow)("@")))
		} else if (e.isImmutable) {
			headerChunks.push(fg(colors.darkGray)("◆"))
		} else {
			headerChunks.push(fg(colors.darkGray)("○"))
		}

		headerChunks.push(" ")

		// Change ID with highlighted prefix
		if (e.changeIdPrefixLen > 0 && e.changeIdPrefixLen < e.changeId.length) {
			headerChunks.push(bold(fg(colors.yellow)(e.changeId.slice(0, e.changeIdPrefixLen))))
			headerChunks.push(bold(fg(colors.purple)(e.changeId.slice(e.changeIdPrefixLen))))
		} else {
			headerChunks.push(bold(fg(colors.purple)(e.changeId)))
		}

		headerChunks.push(" ")
		headerChunks.push(fg(colors.blue)(e.authors))
		headerChunks.push(" ")
		headerChunks.push(fg(colors.gray)(e.date))
		headerChunks.push(" ")
		headerChunks.push(fg(colors.gray)(e.commitId))

		// Bookmarks
		for (const bm of e.bookmarks) {
			headerChunks.push(" ")
			headerChunks.push(bold(fg(colors.green)(bm)))
		}

		lines.push({ chunks: headerChunks, highlighted: isHighlighted })

		// Body line: graph_prefix + subject
		const bodyChunks: StylableInput[] = []
		bodyChunks.push(fg(colors.darkGray)(e.bodyPrefix))
		bodyChunks.push(" ")

		if (aiLoading.has(e.changeId)) {
			const frame = spinnerFrames[aiSpinnerFrame % spinnerFrames.length]
			bodyChunks.push(bold(fg(colors.purple)(`${frame} generating…`)))
		} else {
			const subject = e.subject || "(no description set)"
			if (e.isWorkingCopy) {
				bodyChunks.push(bold(fg(colors.yellow)(subject)))
			} else if (e.isImmutable) {
				bodyChunks.push(dim(subject))
			} else {
				bodyChunks.push(fg(colors.white)(subject))
			}
		}

		lines.push({ chunks: bodyChunks, highlighted: isHighlighted })

		// Trailing edge lines for last visible commit
		if (i === end - 1) {
			for (const edge of e.edgeLines) {
				lines.push({ chunks: [fg(colors.darkGray)(edge)], highlighted: false })
			}
		}
	}

	// ── Render ─────────────────────────────────────────────────────────
	return (
		<box width={width} height={height} flexDirection="column">
			{/* Top padding */}
			<box height={1} />
			{/* Lines */}
			{lines.map((line, idx) => (
				<box
					key={idx}
					height={1}
					width={width}
					style={line.highlighted ? { backgroundColor: colors.darkPurple } : undefined}
				>
					<text content={makeStyledText(line.chunks)} />
				</box>
			))}
		</box>
	)
}
