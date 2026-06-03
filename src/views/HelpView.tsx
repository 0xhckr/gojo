import { colors } from "../styles.js"

interface HelpViewProps {
	width: number
	height: number
	scrollY: number
}

interface Binding {
	key: string
	desc: string
}

interface Section {
	title: string
	color: string
	bindings: Binding[]
}

const SECTIONS: Section[] = [
	{
		title: "Global",
		color: colors.white,
		bindings: [
			{ key: "?", desc: "this help" },
			{ key: "r", desc: "refresh" },
			{ key: "q", desc: "quit / close panel" },
			{ key: "ctrl+c", desc: "force quit" },
		],
	},
	{
		title: "Log View",
		color: colors.blue,
		bindings: [
			{ key: "↑/k, ↓/j", desc: "navigate commits" },
			{ key: "Home", desc: "first commit" },
			{ key: "G", desc: "last commit" },
			{ key: "enter", desc: "open diff panel" },
			{ key: "d", desc: "jj describe  ($EDITOR)" },
			{ key: "D", desc: "AI generate commit message" },
			{ key: "e", desc: "jj edit  (set working copy)" },
			{ key: "n", desc: "jj new  (create change)" },
			{ key: "a", desc: "jj abandon  (remove commit)" },
			{ key: "b", desc: "bookmark mode" },
			{ key: "g", desc: "git mode" },
			{ key: "u", desc: "jj undo" },
		],
	},
	{
		title: "Diff Panel",
		color: colors.green,
		bindings: [
			{ key: "↑/k, ↓/j", desc: "scroll diff" },
			{ key: "pgup/b", desc: "scroll up half page" },
			{ key: "pgdn/f", desc: "scroll down half page" },
			{ key: "g / G", desc: "jump top / bottom" },
			{ key: "enter / q", desc: "close diff" },
		],
	},
	{
		title: "Help View",
		color: colors.purple,
		bindings: [
			{ key: "↑/k, ↓/j", desc: "scroll help" },
			{ key: "pgup/b", desc: "scroll up half page" },
			{ key: "pgdn/f", desc: "scroll down half page" },
			{ key: "g / Home", desc: "jump to top" },
			{ key: "G / End", desc: "jump to bottom" },
			{ key: "? / q", desc: "close help" },
		],
	},
	{
		title: "Bookmark Mode",
		color: colors.cyan,
		bindings: [
			{ key: "c", desc: "create bookmark" },
			{ key: "d", desc: "delete bookmark" },
			{ key: "f", desc: "forget bookmark" },
			{ key: "l", desc: "list bookmarks" },
			{ key: "m", desc: "move bookmark" },
			{ key: "r", desc: "rename bookmark" },
			{ key: "s", desc: "set bookmark" },
			{ key: "t", desc: "track bookmark" },
			{ key: "T", desc: "untrack bookmark" },
			{ key: "tab", desc: "autocomplete  (cycle suggestions)" },
			{ key: "esc", desc: "dismiss / cancel / exit" },
		],
	},
	{
		title: "Git Mode",
		color: colors.orange,
		bindings: [
			{ key: "f", desc: "git fetch" },
			{ key: "p", desc: "git push" },
			{ key: "r", desc: "remote mode" },
			{ key: "esc / q", desc: "cancel / exit" },
		],
	},
	{
		title: "Remote Mode",
		color: colors.pink,
		bindings: [
			{ key: "a", desc: "add remote  (name url)" },
			{ key: "l", desc: "list remotes" },
			{ key: "r", desc: "remove remote  (name)" },
			{ key: "m", desc: "rename remote  (old new)" },
			{ key: "s", desc: "set-url  (name url)" },
			{ key: "esc / q", desc: "cancel / exit" },
		],
	},
]

// Key column width (longest key: "autocomplete (cycle suggestions)" — actually longest key string)
const KEY_COL = 16

/** Total content rows (same calculation HelpView uses internally). */
export function helpTotalRows(): number {
	let n = 0
	for (const s of SECTIONS) n += 3 + s.bindings.length // blank + title + sep + bindings
	return n
}

export function helpMaxScroll(contentHeight: number): number {
	return Math.max(0, helpTotalRows() - contentHeight)
}

export function HelpView({ width, height, scrollY }: HelpViewProps) {
	// Build all content rows
	const rows: Array<{ type: "blank" | "title" | "sep" | "binding"; section?: Section; binding?: Binding }> = []

	for (const section of SECTIONS) {
		rows.push({ type: "blank" })
		rows.push({ type: "title", section })
		rows.push({ type: "sep" })
		for (const b of section.bindings) {
			rows.push({ type: "binding", section, binding: b })
		}
	}

	const totalRows = rows.length
	const titleH = 1 // title bar
	const contentH = height - titleH
	const maxScroll = Math.max(0, totalRows - contentH)
	const clampedY = Math.min(Math.max(0, scrollY), maxScroll)
	const sliced = rows.slice(clampedY, clampedY + contentH)

	// Pad to fill content area so OpenTUI always gets a full-height column
	while (sliced.length < contentH) {
		sliced.push({ type: "blank" })
	}

	// Title bar
	const titleLeft = " gojo help"
	const titleRight = `(${clampedY + 1}-${Math.min(clampedY + contentH, totalRows)}/${totalRows}) ?/q close `
	const titlePad = Math.max(1, width - titleLeft.length - titleRight.length)

	return (
		<box width={width} height={height} flexDirection="column">
			{/* Title bar */}
			<box width={width} height={1} style={{ backgroundColor: colors.darkPurple }}>
				<text content={titleLeft + " ".repeat(titlePad) + titleRight} fg={colors.purple} />
			</box>

			{/* Scrollable content */}
			{sliced.map((row, i) => {
				if (row.type === "blank") {
					return <box key={`r${i}`} width={width} height={1} />
				}

				if (row.type === "title") {
					return (
						<box key={`r${i}`} width={width} height={1}>
							<text content={"  " + row.section!.title} fg={row.section!.color} />
						</box>
					)
				}

				if (row.type === "sep") {
					return (
						<box key={`r${i}`} width={width} height={1}>
							<text fg={colors.darkGray} content={"  " + "─".repeat(Math.min(width - 4, 30))} />
						</box>
					)
				}

				// Binding row
				const b = row.binding!
				const s = row.section!
				const keyPad = Math.max(0, KEY_COL - b.key.length)
				const line = "    " + b.key + " ".repeat(keyPad) + b.desc
				return (
					<box key={`r${i}`} width={width} height={1}>
						<text content={line} fg={colors.gray} />
					</box>
				)
			})}
		</box>
	)
}
