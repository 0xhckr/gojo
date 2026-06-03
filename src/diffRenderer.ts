/**
 * Diff rendering bridge using @pierre/diffs.
 *
 * Parses unified diff output (from `jj diff`) via Pierre's patch parser,
 * syntax-highlights with Shiki, then converts the HAST output into
 * terminal-friendly styled spans for OpenTUI rendering.
 */
import {
	parsePatchFiles,
	getSharedHighlighter,
	getHighlighterOptions,
	renderDiffWithHighlighter,
	cleanLastNewline,
	type FileDiffMetadata,
	type ParsedPatch,
} from "@pierre/diffs"

// ── Types ─────────────────────────────────────────────────────────────────

/** A styled text span for terminal rendering. */
export interface RenderSpan {
	text: string
	fg?: string
	bg?: string
}

/** One line in a rendered diff file. */
export interface DiffFileHeader {
	type: "file-header"
	key: string
	path: string
	prevPath?: string
	changeType: string
}

export interface DiffHunkHeader {
	type: "hunk-header"
	key: string
	text: string
}

export interface DiffLine {
	type: "line"
	key: string
	/** "addition" | "deletion" | "context" */
	kind: "addition" | "deletion" | "context"
	sign: string
	oldLineNum?: number
	newLineNum?: number
	spans: RenderSpan[]
}

export type DiffRow = DiffFileHeader | DiffHunkHeader | DiffLine

/** Parsed diff result for one commit. */
export interface ParsedDiffResult {
	files: DiffRow[][]
}

// ── HAST helpers ──────────────────────────────────────────────────────────

interface HastTextNode {
	type: "text"
	value: string
}

interface HastElementNode {
	type: "element"
	tagName: string
	properties?: Record<string, unknown>
	children?: HastNode[]
}

type HastNode = HastTextNode | HastElementNode

// ── Highlighting ──────────────────────────────────────────────────────────

const RENDER_OPTIONS = {
	theme: "pierre-dark" as const,
	useTokenTransformer: false,
	tokenizeMaxLineLength: 1_000,
	lineDiffType: "word-alt" as const,
	maxLineDiffLength: 10_000,
}

let highlighterPromise: Promise<any> | null = null

async function getHighlighter() {
	if (!highlighterPromise) {
		highlighterPromise = (async () => {
			const opts = getHighlighterOptions("typescript", { theme: "pierre-dark" })
			return getSharedHighlighter({
				...opts,
				preferredHighlighter: "shiki-wasm",
			})
		})()
	}
	return highlighterPromise
}

// ── HAST → RenderSpan ────────────────────────────────────────────────────

const EMPTY_STYLE_VALUES = new Map<string, string>()
const parsedStyleCache = new Map<string, Map<string, string>>()

/** Parse an inline CSS style string from Pierre's highlighted HAST output. */
function parseStyleValue(styleValue: unknown): Map<string, string> {
	if (typeof styleValue !== "string") return EMPTY_STYLE_VALUES

	const cached = parsedStyleCache.get(styleValue)
	if (cached) return cached

	const styles = new Map<string, string>()
	for (const segment of styleValue.split(";")) {
		const sep = segment.indexOf(":")
		if (sep <= 0) continue
		const key = segment.slice(0, sep).trim()
		const value = segment.slice(sep + 1).trim()
		if (key && value) styles.set(key, value)
	}

	parsedStyleCache.set(styleValue, styles)
	return styles
}

/** Append a span, coalescing adjacent runs with identical colors. */
function pushSpan(target: RenderSpan[], next: RenderSpan) {
	if (next.text.length === 0) return
	const prev = target[target.length - 1]
	if (prev && prev.fg === next.fg && prev.bg === next.bg) {
		prev.text += next.text
		return
	}
	target.push(next)
}

/** Flatten one highlighted HAST line into terminal-friendly styled spans. */
function flattenHastLine(node: HastNode | undefined): RenderSpan[] {
	if (!node) return []

	const spans: RenderSpan[] = []
	const colorVariable = "--diffs-token-dark"

	const visit = (current: HastNode | undefined, inherited: Pick<RenderSpan, "fg" | "bg">) => {
		if (!current) return

		if (current.type === "text") {
			const cleaned = cleanLastNewline(current.value)
			if (cleaned) {
				pushSpan(spans, { text: cleaned, fg: inherited.fg, bg: inherited.bg })
			}
			return
		}

		const properties = current.properties ?? {}
		const styles = parseStyleValue(properties.style)
		const nextStyle: Pick<RenderSpan, "fg" | "bg"> = {
			fg: styles.get(colorVariable) ?? styles.get("color") ?? inherited.fg,
			bg: Object.hasOwn(properties, "data-diff-span") ? inherited.bg : inherited.bg,
		}

		for (const child of current.children ?? []) {
			visit(child, nextStyle)
		}
	}

	visit(node, {})
	return spans
}

// ── Change type labels ────────────────────────────────────────────────────

function changeTypeLabel(type: string): string {
	switch (type) {
		case "new": return "added"
		case "deleted": return "deleted"
		case "rename-pure": return "renamed"
		case "change": return "modified"
		default: return type
	}
}

// ── Main parsing + rendering ──────────────────────────────────────────────

/** Parse raw unified diff text into structured rows per file. */
function parsePatch(raw: string): ParsedPatch[] {
	const trimmed = raw.trim()
	if (!trimmed) return []
	return parsePatchFiles(trimmed, undefined, false)
}

/** Build diff rows for a single file without syntax highlighting (fast path). */
function buildPlainRows(metadata: FileDiffMetadata): DiffRow[] {
	const rows: DiffRow[] = []
	const changeLabel = changeTypeLabel(metadata.type)

	// File header
	rows.push({
		type: "file-header",
		key: `header:${metadata.name}`,
		path: metadata.name,
		prevPath: metadata.prevName,
		changeType: changeLabel,
	})

	for (const [hunkIdx, hunk] of metadata.hunks.entries()) {
		// Hunk header
		rows.push({
			type: "hunk-header",
			key: `hunk:${metadata.name}:${hunkIdx}`,
			text: `@@ -${hunk.deletionStart},${hunk.deletionCount} +${hunk.additionStart},${hunk.additionCount} @@`,
		})

		let delLineNum = hunk.deletionStart
		let addLineNum = hunk.additionStart
		let delLineIdx = hunk.deletionLineIndex
		let addLineIdx = hunk.additionLineIndex

		for (const content of hunk.hunkContent) {
			if (content.type === "context") {
				for (let i = 0; i < content.lines; i++) {
					const raw = metadata.additionLines[addLineIdx + i] as string | undefined
					const text = cleanLastNewline(raw ?? "")
					rows.push({
						type: "line",
						key: `line:${metadata.name}:${hunkIdx}:ctx:${delLineNum + i}`,
						kind: "context",
						sign: " ",
						oldLineNum: delLineNum + i,
						newLineNum: addLineNum + i,
						spans: text ? [{ text }] : [],
					})
				}
				delLineNum += content.lines
				addLineNum += content.lines
				delLineIdx += content.lines
				addLineIdx += content.lines
			} else {
				for (let i = 0; i < content.deletions; i++) {
					const raw = metadata.deletionLines[delLineIdx + i] as string | undefined
					const text = cleanLastNewline(raw ?? "")
					rows.push({
						type: "line",
						key: `line:${metadata.name}:${hunkIdx}:del:${delLineNum + i}`,
						kind: "deletion",
						sign: "-",
						oldLineNum: delLineNum + i,
						spans: text ? [{ text }] : [],
					})
				}
				delLineIdx += content.deletions
				delLineNum += content.deletions

				for (let i = 0; i < content.additions; i++) {
					const raw = metadata.additionLines[addLineIdx + i] as string | undefined
					const text = cleanLastNewline(raw ?? "")
					rows.push({
						type: "line",
						key: `line:${metadata.name}:${hunkIdx}:add:${addLineNum + i}`,
						kind: "addition",
						sign: "+",
						newLineNum: addLineNum + i,
						spans: text ? [{ text }] : [],
					})
				}
				addLineIdx += content.additions
				addLineNum += content.additions
			}
		}
	}

	return rows
}

/** Build diff rows for a single file with syntax highlighting. */
function buildHighlightedRows(
	metadata: FileDiffMetadata,
	code: { deletionLines: any[]; additionLines: any[] },
): DiffRow[] {
	const rows: DiffRow[] = []
	const changeLabel = changeTypeLabel(metadata.type)

	rows.push({
		type: "file-header",
		key: `header:${metadata.name}`,
		path: metadata.name,
		prevPath: metadata.prevName,
		changeType: changeLabel,
	})

	for (const [hunkIdx, hunk] of metadata.hunks.entries()) {
		rows.push({
			type: "hunk-header",
			key: `hunk:${metadata.name}:${hunkIdx}`,
			text: `@@ -${hunk.deletionStart},${hunk.deletionCount} +${hunk.additionStart},${hunk.additionCount} @@`,
		})

		let delLineNum = hunk.deletionStart
		let addLineNum = hunk.additionStart
		let delLineIdx = hunk.deletionLineIndex
		let addLineIdx = hunk.additionLineIndex

		for (const content of hunk.hunkContent) {
			if (content.type === "context") {
				for (let i = 0; i < content.lines; i++) {
					const raw = metadata.additionLines[addLineIdx + i] as string | undefined
					const hastNode = code.additionLines[addLineIdx + i] as HastNode | undefined
					const spans = hastNode
						? flattenHastLine(hastNode)
						: cleanLastNewline(raw ?? "").length > 0
							? [{ text: cleanLastNewline(raw ?? "") }]
							: []
					rows.push({
						type: "line",
						key: `line:${metadata.name}:${hunkIdx}:ctx:${delLineNum + i}`,
						kind: "context",
						sign: " ",
						oldLineNum: delLineNum + i,
						newLineNum: addLineNum + i,
						spans,
					})
				}
				delLineNum += content.lines
				addLineNum += content.lines
				delLineIdx += content.lines
				addLineIdx += content.lines
			} else {
				for (let i = 0; i < content.deletions; i++) {
					const raw = metadata.deletionLines[delLineIdx + i] as string | undefined
					const hastNode = code.deletionLines[delLineIdx + i] as HastNode | undefined
					const spans = hastNode
						? flattenHastLine(hastNode)
						: cleanLastNewline(raw ?? "").length > 0
							? [{ text: cleanLastNewline(raw ?? "") }]
							: []
					rows.push({
						type: "line",
						key: `line:${metadata.name}:${hunkIdx}:del:${delLineNum + i}`,
						kind: "deletion",
						sign: "-",
						oldLineNum: delLineNum + i,
						spans,
					})
				}
				delLineIdx += content.deletions
				delLineNum += content.deletions

				for (let i = 0; i < content.additions; i++) {
					const raw = metadata.additionLines[addLineIdx + i] as string | undefined
					const hastNode = code.additionLines[addLineIdx + i] as HastNode | undefined
					const spans = hastNode
						? flattenHastLine(hastNode)
						: cleanLastNewline(raw ?? "").length > 0
							? [{ text: cleanLastNewline(raw ?? "") }]
							: []
					rows.push({
						type: "line",
						key: `line:${metadata.name}:${hunkIdx}:add:${addLineNum + i}`,
						kind: "addition",
						sign: "+",
						newLineNum: addLineNum + i,
						spans,
					})
				}
				addLineIdx += content.additions
				addLineNum += content.additions
			}
		}
	}

	return rows
}

/**
 * Render a raw unified diff string into structured, optionally-highlighted rows.
 *
 * Returns immediately with plain-text rows, then resolves the promise
 * with syntax-highlighted rows once Shiki loads.
 */
export async function renderDiff(rawDiff: string): Promise<ParsedDiffResult> {
	const patches = parsePatch(rawDiff)
	if (patches.length === 0) return { files: [] }

	const allFiles: DiffRow[][] = []

	for (const patch of patches) {
		for (const metadata of patch.files) {
			// Try syntax highlighting
			let highlightedCode: { deletionLines: any[]; additionLines: any[] } | null = null
			try {
				const highlighter = await getHighlighter()
				const result = renderDiffWithHighlighter(metadata, highlighter, RENDER_OPTIONS)
				highlightedCode = {
					deletionLines: result.code.deletionLines,
					additionLines: result.code.additionLines,
				}
			} catch {
				// Fall back to plain rendering
			}

			const rows = highlightedCode
				? buildHighlightedRows(metadata, highlightedCode)
				: buildPlainRows(metadata)
			allFiles.push(rows)
		}
	}

	return { files: allFiles }
}
