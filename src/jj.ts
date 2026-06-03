import { execFile } from "node:child_process"
import { promisify } from "node:util"
import { homedir } from "node:os"
import { join } from "node:path"
import { readFile, stat } from "node:fs/promises"

const execFileAsync = promisify(execFile)

// ── Config ────────────────────────────────────────────────────────────────

export interface Config {
	jjPath: string
	repoRoot: string
	openRouterApiKey?: string
	openRouterModel?: string
	commitPrompt?: string
}

export async function loadConfig(): Promise<Config> {
	const jjPath = await findBinary("jj")
	const repoRoot = await findRepoRoot()

	const cfg: Config = { jjPath, repoRoot }

	// Overlay TOML config (optional)
	const configPath = join(homedir(), ".config", "gojo", "gojo.toml")
	try {
		const raw = await readFile(configPath, "utf-8")
		// Minimal TOML parser for flat key=value pairs
		for (const line of raw.split("\n")) {
			const trimmed = line.trim()
			if (!trimmed || trimmed.startsWith("#")) continue
			const eqIdx = trimmed.indexOf("=")
			if (eqIdx < 0) continue
			const key = trimmed.slice(0, eqIdx).trim()
			let val = trimmed.slice(eqIdx + 1).trim()
			// Strip quotes
			if ((val.startsWith('"') && val.endsWith('"')) || (val.startsWith("'") && val.endsWith("'"))) {
				val = val.slice(1, -1)
			}
			if (key === "openrouter_api_key") cfg.openRouterApiKey = val
			else if (key === "openrouter_model") cfg.openRouterModel = val
			else if (key === "commit_prompt") cfg.commitPrompt = val
		}
	} catch {
		// No config file — that's fine
	}

	return cfg
}

async function findBinary(name: string): Promise<string> {
	try {
		const { stdout } = await execFileAsync("which", [name])
		return stdout.trim()
	} catch {
		throw new Error(`${name} not found in PATH`)
	}
}

async function findRepoRoot(): Promise<string> {
	let dir = process.cwd()
	for (;;) {
		try {
			await stat(join(dir, ".jj"))
			return dir
		} catch {
			const parent = join(dir, "..")
			if (parent === dir) throw new Error("no .jj directory found")
			dir = parent
		}
	}
}

// ── JJ Runner ─────────────────────────────────────────────────────────────

// Two-line template with \x01 marker to separate graph prefix from data.
const LOG_TEMPLATE =
	'"\\x01" ++ change_id.short(8) ++ "|" ++ change_id.shortest() ++ "|" ++ commit_id.short(8) ++ "|" ++ commit_id.shortest() ++ "|" ++ author.email() ++ "|" ++ author.timestamp().local().format("%Y-%m-%d %H:%M") ++ "|" ++ if(current_working_copy, "Y", "N") ++ "|" ++ if(immutable, "Y", "N") ++ "|" ++ bookmarks.join(",") ++ "\\n" ++ "\\x01" ++ description.first_line() ++ "\\n"'

export interface LogEntry {
	changeId: string
	changeIdPrefixLen: number
	commitId: string
	commitIdPrefixLen: number
	authors: string
	date: string
	subject: string
	bookmarks: string[]
	isWorkingCopy: boolean
	isImmutable: boolean
	headerPrefix: string
	bodyPrefix: string
	edgeLines: string[]
}

export interface StatusEntry {
	path: string
	status: "Added" | "Modified" | "Removed" | "Conflicted"
}

export class JJRunner {
	private config: Config

	constructor(
		private jjPath: string,
		private repoDir: string,
		config?: Config,
	) {
		this.config = config ?? { jjPath, repoRoot: repoDir }
	}

	private async run(...args: string[]): Promise<string> {
		try {
			const { stdout } = await execFileAsync(this.jjPath, args, {
				cwd: this.repoDir,
				maxBuffer: 10 * 1024 * 1024,
			})
			return stdout
		} catch (err: any) {
			throw new Error(`jj ${args.join(" ")}: ${err.message || err}`)
		}
	}

	async log(limit = 50): Promise<LogEntry[]> {
		const out = await this.run("log", "--color", "never", "-T", LOG_TEMPLATE, "-n", String(limit))
		return parseLog(out)
	}

	async status(): Promise<StatusEntry[]> {
		const out = await this.run("status")
		return parseStatus(out)
	}

	async diff(rev?: string): Promise<string> {
		const args = ["diff", "--color", "never"]
		if (rev) args.push("-r", rev)
		return this.run(...args)
	}

	async diffSummary(rev: string): Promise<StatusEntry[]> {
		const out = await this.run("diff", "--summary", "-r", rev)
		return parseStatus(out)
	}

	async fileShow(rev: string, path: string): Promise<string> {
		return this.run("file", "show", "-r", rev, path)
	}

	async describe(rev: string, message: string): Promise<void> {
		await this.run("describe", "-r", rev, "-m", message)
	}

	async edit(rev: string): Promise<void> {
		await this.run("edit", "-r", rev)
	}

	async new(rev?: string): Promise<void> {
		const args = ["new"]
		if (rev) args.push("-r", rev)
		await this.run(...args)
	}

	async abandon(rev: string): Promise<void> {
		await this.run("abandon", "-r", rev)
	}

	async undo(): Promise<void> {
		await this.run("undo")
	}

	async bookmarkCreate(name: string, rev?: string): Promise<void> {
		const args = ["bookmark", "create", name]
		if (rev) args.push("-r", rev)
		await this.run(...args)
	}

	async bookmarkDelete(name: string): Promise<void> {
		await this.run("bookmark", "delete", name)
	}

	async bookmarkForget(name: string): Promise<void> {
		await this.run("bookmark", "forget", name)
	}

	async bookmarkList(): Promise<string> {
		return this.run("bookmark", "list")
	}

	async bookmarkMove(name: string, rev: string): Promise<void> {
		await this.run("bookmark", "move", name, "--to", rev)
	}

	async bookmarkRename(oldName: string, newName: string): Promise<void> {
		await this.run("bookmark", "rename", oldName, newName)
	}

	async bookmarkSet(name: string, rev: string): Promise<void> {
		const args = ["bookmark", "set", name]
		if (rev) args.push("-r", rev)
		await this.run(...args)
	}

	async bookmarkTrack(name: string): Promise<void> {
		await this.run("bookmark", "track", name)
	}

	async bookmarkUntrack(name: string): Promise<void> {
		await this.run("bookmark", "untrack", name)
	}

	async gitFetch(): Promise<void> {
		await this.run("git", "fetch")
	}

	async gitPush(): Promise<void> {
		await this.run("git", "push")
	}

	async remoteAdd(name: string, url: string): Promise<void> {
		await this.run("git", "remote", "add", name, url)
	}

	async remoteList(): Promise<string> {
		return this.run("git", "remote", "list")
	}

	async remoteRemove(name: string): Promise<void> {
		await this.run("git", "remote", "remove", name)
	}

	async remoteRename(oldName: string, newName: string): Promise<void> {
		await this.run("git", "remote", "rename", oldName, newName)
	}

	async remoteSetUrl(name: string, url: string): Promise<void> {
		await this.run("git", "remote", "set-url", name, url)
	}

	async aiDescribe(rev: string): Promise<string> {
		if (!this.config.openRouterApiKey) {
			throw new Error("No OpenRouter API key configured. Add openrouter_api_key to ~/.config/gojo/gojo.toml")
		}

		const diffText = await this.diff(rev)
		if (!diffText.trim()) {
			throw new Error("No diff available for this commit")
		}

		const model = this.config.openRouterModel || "anthropic/claude-sonnet-4"
		const prompt = this.config.commitPrompt
			?? "Write a clear, concise commit message (subject line only, no body) for this diff. Reply with ONLY the commit message text, nothing else:\n\n"

		const body = {
			model,
			messages: [{ role: "user", content: prompt + diffText }],
			max_tokens: 200,
		}

		const resp = await fetch("https://openrouter.ai/api/v1/chat/completions", {
			method: "POST",
			headers: {
				"Content-Type": "application/json",
				Authorization: `Bearer ${this.config.openRouterApiKey}`,
			},
			body: JSON.stringify(body),
		})

		if (!resp.ok) {
			const text = await resp.text()
			throw new Error(`OpenRouter API error (${resp.status}): ${text.slice(0, 200)}`)
		}

		const data = await resp.json() as any
		const message = data?.choices?.[0]?.message?.content?.trim()
		if (!message) {
			throw new Error("Empty response from AI")
		}
		return message
	}
}

// ── Parsers ───────────────────────────────────────────────────────────────

interface ParsedLine {
	prefix: string
	data: string
	isData: boolean
}

function parseLog(raw: string): LogEntry[] {
	raw = raw.trim()
	if (!raw) return []

	const parsed: ParsedLine[] = []
	for (const line of raw.split("\n")) {
		const trimmed = line.replace(/\r$/, "")
		if (!trimmed) continue
		const idx = trimmed.indexOf("\x01")
		if (idx >= 0) {
			parsed.push({ prefix: trimmed.slice(0, idx), data: trimmed.slice(idx + 1), isData: true })
		} else {
			parsed.push({ prefix: trimmed, data: "", isData: false })
		}
	}

	const entries: LogEntry[] = []
	let pendingEdges: string[] = []
	let i = 0

	while (i < parsed.length) {
		const p = parsed[i]
		if (!p.isData) {
			pendingEdges.push(p.prefix)
			i++
			continue
		}

		const fields = p.data.split("|")
		if (fields.length < 9) {
			i++
			continue
		}

		// Attach pending edge lines to the previous commit
		if (entries.length > 0) {
			entries[entries.length - 1].edgeLines = pendingEdges
		}
		pendingEdges = []

		const bookmarks = fields[8] ? fields[8].split(",").map(b => b.replace(/\*$/, "")) : []
		const changeIdPrefixLen = fields[1].length
		const commitIdPrefixLen = fields[3].length

		const entry: LogEntry = {
			headerPrefix: p.prefix,
			changeId: fields[0],
			changeIdPrefixLen,
			commitId: fields[2],
			commitIdPrefixLen,
			authors: fields[4],
			date: fields[5],
			isWorkingCopy: fields[6] === "Y",
			isImmutable: fields[7] === "Y",
			subject: "",
			bookmarks,
			bodyPrefix: "",
			edgeLines: [],
		}

		// Next line should be the body (subject)
		i++
		if (i < parsed.length && parsed[i].isData) {
			entry.bodyPrefix = parsed[i].prefix
			entry.subject = parsed[i].data
			i++
		}

		entries.push(entry)
	}

	// Attach trailing edge lines to the last commit
	if (pendingEdges.length > 0 && entries.length > 0) {
		entries[entries.length - 1].edgeLines.push(...pendingEdges)
	}

	return entries
}

function parseStatus(raw: string): StatusEntry[] {
	const entries: StatusEntry[] = []
	for (const line of raw.split("\n")) {
		const trimmed = line.trim()
		if (!trimmed) continue
		if (trimmed.startsWith("Working copy") || trimmed.startsWith("Parent commit")) continue
		if (trimmed.length < 3) continue
		const statusChar = trimmed[0]
		const path = trimmed.slice(1).trim()
		if (!path) continue
		switch (statusChar) {
			case "M":
				entries.push({ path, status: "Modified" })
				break
			case "A":
				entries.push({ path, status: "Added" })
				break
			case "D":
				entries.push({ path, status: "Removed" })
				break
			case "C":
				entries.push({ path, status: "Conflicted" })
				break
		}
	}
	return entries
}
