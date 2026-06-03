import { useEffect, useState, useCallback, useRef } from "react"

/**
 * Hook that wraps an async function, returning { data, loading, error, execute }.
 */
export function useAsync<T>(fn: () => Promise<T>) {
	const [data, setData] = useState<T | null>(null)
	const [loading, setLoading] = useState(false)
	const [error, setError] = useState<string | null>(null)

	const fnRef = useRef(fn)
	fnRef.current = fn

	const execute = useCallback(async (): Promise<T | null> => {
		setLoading(true)
		setError(null)
		try {
			const result = await fnRef.current()
			setData(result)
			setLoading(false)
			return result
		} catch (err: unknown) {
			const msg = err instanceof Error ? err.message : String(err)
			setError(msg)
			setLoading(false)
			return null
		}
	}, [])

	return { data, loading, error, execute }
}

/**
 * Interval-based spinner tick. Returns the current frame index.
 */
export function useSpinner(active: boolean, ms = 100): number {
	const [frame, setFrame] = useState(0)
	useEffect(() => {
		if (!active) return
		const id = setInterval(() => setFrame((f: number) => f + 1), ms)
		return () => clearInterval(id)
	}, [active, ms])
	return frame
}
