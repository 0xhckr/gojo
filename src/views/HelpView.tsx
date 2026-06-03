import { colors } from "../styles.js"

interface HelpViewProps {
	width: number
}

export function HelpView({ width }: HelpViewProps) {
	const sections = [
		{ title: "Global", bindings: [
			["?", "this help"],
			["r", "refresh"],
			["q", "quit / close diff"],
		]},
		{ title: "Log View", bindings: [
			["↑/k, ↓/j", "navigate commits"],
			["G", "last commit"],
			["Home", "first commit"],
			["enter", "open diff panel"],
			["d", "jj describe ($EDITOR)"],
			["D", "AI generate commit msg"],
			["e", "jj edit (checkout commit)"],
			["n", "jj new (create change)"],
			["a", "jj abandon (remove commit)"],
			["b", "bookmark mode"],
			["g", "git mode"],
			["u", "jj undo"],
		]},
		{ title: "Diff Panel", bindings: [
			["↑/k, ↓/j", "scroll diff"],
			["pgup/b, pgdn/f", "half-page scroll"],
			["g / G", "top / bottom"],
			["enter/q", "close diff"],
		]},
		{ title: "Bookmark Mode", bindings: [
			["c", "create bookmark"],
			["d", "delete bookmark"],
			["f", "forget bookmark"],
			["l", "list bookmarks"],
			["m", "move bookmark"],
			["r", "rename bookmark"],
			["s", "set bookmark"],
			["t", "track bookmark"],
			["T", "untrack bookmark"],
			["tab", "autocomplete (cycle suggestions)"],
			["esc", "dismiss suggestions / cancel / exit"],
		]},
		{ title: "Git Mode", bindings: [
			["f", "git fetch"],
			["p", "git push"],
			["r", "remote mode"],
			["esc", "cancel / exit"],
		]},
		{ title: "Remote Mode", bindings: [
			["a", "add remote (name url)"],
			["l", "list remotes"],
			["r", "remove remote (name)"],
			["m", "rename remote (old new)"],
			["s", "set-url (name url)"],
			["esc", "cancel / exit"],
		]},
	]

	const lines: string[] = []
	for (const section of sections) {
		lines.push("")
		lines.push(` ${section.title}`)
		for (const [key, desc] of section.bindings) {
			const pad = Math.max(0, 16 - key.length)
			lines.push(`  ${key}${" ".repeat(pad)} ${desc}`)
		}
	}

	return (
		<scrollbox width={width} flexGrow={1} scrollY={true}>
			<text content={lines.join("\n")} fg={colors.white} />
		</scrollbox>
	)
}
