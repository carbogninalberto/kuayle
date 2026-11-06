export const prerender = true;

/**
 * Real GitHub star count, fetched once at build time. Any failure (offline build, rate limit,
 * unexpected payload) returns null and the page simply hides the count.
 */
export async function load({ fetch }) {
	try {
		const response = await fetch('https://api.github.com/repos/carbogninalberto/kuayle', {
			headers: { Accept: 'application/vnd.github+json', 'User-Agent': 'kuayle-web-build' },
			signal: AbortSignal.timeout(5000)
		});
		if (!response.ok) return { stars: null };
		const body: unknown = await response.json();
		const stars = (body as { stargazers_count?: unknown })?.stargazers_count;
		return { stars: typeof stars === 'number' && Number.isFinite(stars) && stars >= 0 ? stars : null };
	} catch {
		return { stars: null };
	}
}
