import { type StatusEntry } from "../jj.js"
import { colors } from "../styles.js"

interface DiffPanelProps {
	width: number
	rev: string
	loading: boolean
	diffContent: string
	statusEntries: StatusEntry[]
}

export function DiffPanel({ width, rev, loading, diffContent, statusEntries }: DiffPanelProps) {
	const lines: string[] = []

	// Title
	lines.push(` ${rev}${loading ? "  loading…" : ""}  (enter/q to close) `)

	// Status summary
	lines.push(" status")
	if (statusEntries.length === 0) {
		lines.push("  (no changes)")
	} else {
		for (const e of statusEntries) {
			const sym = e.status === "Added" ? "A" : e.status === "Modified" ? "M" : e.status === "Removed" ? "D" : "C"
			lines.push(`  ${sym} ${e.path}`)
		}
	}

	// Separator + diff
	lines.push("─".repeat(width))
	if (diffContent) {
		lines.push(diffContent)
	}

	const content = lines.join("\n")

	return (
		<scrollbox width={width} flexGrow={1} scrollY={true} focused={true} style={{ backgroundColor: colors.darkerGray }}>
			<text content={content} fg={colors.white} />
		</scrollbox>
	)
}
