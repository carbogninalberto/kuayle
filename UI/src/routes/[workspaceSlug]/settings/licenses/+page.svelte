<script lang="ts">
	import packageLock from '../../../../../package-lock.json';
	import { Search } from 'lucide-svelte';

	type LockPackage = {
		version?: string;
		license?: string;
		licenses?: string | string[];
		resolved?: string;
		dev?: boolean;
		optional?: boolean;
	};

	type LicenseEntry = {
		name: string;
		version: string;
		license: string;
		resolved?: string;
		dev: boolean;
		optional: boolean;
	};

	let query = $state('');

	function packageNameFromPath(path: string): string | null {
		if (!path.includes('node_modules/')) return null;
		const namePath = path.split('node_modules/').pop();
		if (!namePath) return null;
		const parts = namePath.split('/');
		return parts[0]?.startsWith('@') ? `${parts[0]}/${parts[1]}` : parts[0];
	}

	function licenseText(pkg: LockPackage): string {
		if (Array.isArray(pkg.licenses)) return pkg.licenses.join(', ');
		return pkg.license ?? pkg.licenses ?? 'Unknown';
	}

	const packages = (packageLock as { packages: Record<string, LockPackage> }).packages;
	const licenseEntries = Object.entries(packages)
		.map(([path, pkg]) => {
			const name = packageNameFromPath(path);
			if (!name || !pkg.version) return null;
			return {
				name,
				version: pkg.version,
				license: licenseText(pkg),
				resolved: pkg.resolved,
				dev: Boolean(pkg.dev),
				optional: Boolean(pkg.optional)
			} satisfies LicenseEntry;
		})
		.filter((entry): entry is LicenseEntry => Boolean(entry))
		.filter(
			(entry, index, entries) =>
				entries.findIndex((item) => item.name === entry.name && item.version === entry.version) === index
		)
		.sort((a, b) => a.name.localeCompare(b.name));

	const filteredEntries = $derived(
		licenseEntries.filter((entry) => {
			const value = query.trim().toLowerCase();
			return !value || entry.name.toLowerCase().includes(value) || entry.license.toLowerCase().includes(value);
		})
	);
</script>

<div class="mx-auto max-w-4xl px-8 py-10">
	<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">Open source licenses</h1>
	<p class="mt-2 text-sm text-[var(--color-text-tertiary)]">
		Automatically generated from the UI package lockfile. Includes direct and transitive open source packages bundled
		with the app.
	</p>

	<div class="mt-6 flex items-center justify-between gap-4">
		<div class="relative w-full max-w-sm">
			<Search size={14} class="absolute left-2 top-1/2 -translate-y-1/2 text-[var(--color-text-tertiary)]" />
			<input
				bind:value={query}
				placeholder="Search package or license"
				class="h-8 w-full rounded-md border border-[var(--app-border)] bg-[var(--color-bg-secondary)] pl-7 pr-2 text-sm text-[var(--color-text-primary)] outline-none placeholder:text-[var(--color-text-tertiary)] focus:border-[var(--app-accent)]"
			/>
		</div>
		<p class="shrink-0 text-xs text-[var(--color-text-tertiary)]">{filteredEntries.length} packages</p>
	</div>

	<div class="mt-4 overflow-hidden rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<div
			class="grid grid-cols-[minmax(0,1fr)_120px_160px] gap-3 border-b border-[var(--app-border)] px-4 py-2 text-xs font-medium text-[var(--color-text-tertiary)]"
		>
			<span>Package</span>
			<span>Version</span>
			<span>License</span>
		</div>
		<div class="divide-y divide-[var(--app-border)]">
			{#each filteredEntries as entry}
				<div class="grid grid-cols-[minmax(0,1fr)_120px_160px] gap-3 px-4 py-2 text-sm">
					<div class="min-w-0">
						{#if entry.resolved}
							<a
								href={entry.resolved}
								target="_blank"
								rel="noopener"
								class="truncate text-[var(--color-text-primary)] hover:underline"
							>
								{entry.name}
							</a>
						{:else}
							<p class="truncate text-[var(--color-text-primary)]">{entry.name}</p>
						{/if}
						{#if entry.dev || entry.optional}
							<p class="mt-0.5 text-[10px] text-[var(--color-text-tertiary)]">
								{entry.dev ? 'Development dependency' : ''}{entry.dev && entry.optional ? ' · ' : ''}{entry.optional
									? 'Optional'
									: ''}
							</p>
						{/if}
					</div>
					<span class="font-mono text-xs text-[var(--color-text-secondary)]">{entry.version}</span>
					<span class="text-xs text-[var(--color-text-secondary)]">{entry.license}</span>
				</div>
			{/each}
		</div>
	</div>
</div>
