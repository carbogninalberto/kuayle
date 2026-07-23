const GITHUB_ORIGIN = 'https://github.com';
const REPOSITORY_PART = /^[A-Za-z0-9_.-]+$/;

interface ParsedRepositoryUrl {
	url: URL;
	path: string[];
}

function parseGitHubUrl(value: unknown): { url: URL; path: string[] } | null {
	if (typeof value !== 'string' || value.trim() !== value || value === '') return null;

	try {
		const url = new URL(value);
		if (
			url.protocol !== 'https:' ||
			url.hostname.toLowerCase() !== 'github.com' ||
			url.username !== '' ||
			url.password !== '' ||
			url.port !== ''
		) {
			return null;
		}

		const path = url.pathname
			.split('/')
			.slice(1)
			.map((part) => decodeURIComponent(part));
		return { url, path };
	} catch {
		return null;
	}
}

function parseRepositoryUrl(value: unknown, repositoryFullName: string): ParsedRepositoryUrl | null {
	const repository = repositoryFullName.split('/');
	if (
		repository.length !== 2 ||
		repository.some((part) => !REPOSITORY_PART.test(part) || part === '.' || part === '..')
	) {
		return null;
	}

	const parsed = parseGitHubUrl(value);
	if (
		!parsed ||
		parsed.path.length < 2 ||
		parsed.path[0].toLowerCase() !== repository[0].toLowerCase() ||
		parsed.path[1].toLowerCase() !== repository[1].toLowerCase()
	) {
		return null;
	}

	return parsed;
}

export function safeGitHubRepositoryUrl(value: unknown, repositoryFullName: string): string | null {
	return parseRepositoryUrl(value, repositoryFullName)?.url.href ?? null;
}

export function safeGitHubPullRequestUrl(value: unknown, repositoryFullName: string): string | null {
	const parsed = parseRepositoryUrl(value, repositoryFullName);
	if (!parsed || parsed.path.length !== 4 || parsed.path[2] !== 'pull' || !/^[1-9]\d*$/.test(parsed.path[3])) {
		return null;
	}
	return parsed.url.href;
}

export function safeGitHubBranchUrl(value: unknown, repositoryFullName: string): string | null {
	const parsed = parseRepositoryUrl(value, repositoryFullName);
	if (
		!parsed ||
		parsed.path.length < 4 ||
		parsed.path[2] !== 'tree' ||
		parsed.path.slice(3).some((part) => part === '')
	) {
		return null;
	}
	return parsed.url.href;
}

export function safeGitHubCommitUrl(value: unknown, repositoryFullName: string): string | null {
	const parsed = parseRepositoryUrl(value, repositoryFullName);
	if (!parsed || parsed.path.length !== 4 || parsed.path[2] !== 'commit' || !/^[0-9a-f]{7,64}$/i.test(parsed.path[3])) {
		return null;
	}
	return parsed.url.href;
}

export function safeGitHubReleaseUrl(value: unknown, repositoryFullName: string, expectedTag?: string): string | null {
	const parsed = parseRepositoryUrl(value, repositoryFullName);
	if (
		!parsed ||
		parsed.path.length !== 5 ||
		parsed.path[2] !== 'releases' ||
		parsed.path[3] !== 'tag' ||
		parsed.path[4] === '' ||
		(expectedTag !== undefined && parsed.path[4] !== expectedTag)
	) {
		return null;
	}
	return parsed.url.href;
}

export function safeGitHubAppInstallUrl(value: unknown): string | null {
	const parsed = parseGitHubUrl(value);
	if (
		!parsed ||
		parsed.path.length !== 4 ||
		parsed.path[0] !== 'apps' ||
		!REPOSITORY_PART.test(parsed.path[1]) ||
		parsed.path[2] !== 'installations' ||
		parsed.path[3] !== 'new'
	) {
		return null;
	}
	return parsed.url.href;
}

export function githubRepositoryHomeUrl(repositoryFullName: string): string | null {
	return safeGitHubRepositoryUrl(`${GITHUB_ORIGIN}/${repositoryFullName}`, repositoryFullName);
}
