<script lang="ts">
	import { consent } from '$lib/cookieConsent.svelte';
	import { Button } from '$lib/components/ui/button';

	let showPrefs = $state(false);

	// Hide until a choice has been made. SSR / prerender renders nothing,
	// then the banner appears on hydration if undecided.
	let visible = $state(false);
	$effect(() => {
		visible = !consent.decided;
	});
</script>

{#if visible}
	<div class="cookie-banner">
		<p class="text-sm leading-relaxed text-muted-foreground">
			This site stores your consent choice in your browser. No optional analytics or marketing
			scripts are currently active. See our
			<a href="/privacy" class="text-brand-300 underline-offset-4 hover:underline">privacy policy</a
			>.
		</p>
		<div class="mt-3 flex flex-col gap-2">
			<Button
				size="sm"
				onclick={() => consent.acceptAll()}
				class="cursor-pointer bg-brand-400 text-white hover:bg-brand-500">Allow all</Button
			>
			<div class="flex gap-2">
				<Button
					variant="outline"
					size="sm"
					onclick={() => consent.rejectAll()}
					class="flex-1 cursor-pointer border-border">Essential only</Button
				>
				<Button
					variant="ghost"
					size="sm"
					onclick={() => (showPrefs = true)}
					class="flex-1 cursor-pointer">Preferences</Button
				>
			</div>
		</div>
	</div>
{/if}

{#if showPrefs}
	{@const prefs = consent.value}
	<div
		class="fixed inset-0 z-[60] flex items-center justify-center bg-black/70 p-4 backdrop-blur-sm"
	>
		<button
			type="button"
			class="absolute inset-0 cursor-default"
			aria-label="Close cookie preferences"
			onclick={() => (showPrefs = false)}
		></button>
		<div
			class="relative w-full max-w-md rounded-2xl border border-white/10 bg-card p-6 shadow-2xl"
			role="dialog"
			aria-modal="true"
			aria-labelledby="cookie-prefs-title"
			tabindex="-1"
		>
			<h2 id="cookie-prefs-title" class="text-lg font-semibold">Cookie preferences</h2>
			<p class="mt-1 text-sm text-muted-foreground">
				Choose which categories to allow. You can change this any time.
			</p>

			<div class="mt-5 space-y-3">
				<label class="flex items-start gap-3 rounded-lg border border-white/5 bg-white/[0.02] p-3">
					<input type="checkbox" checked disabled class="mt-0.5 size-4 accent-brand-400" />
					<span>
						<span class="text-sm font-medium">Essential</span>
						<span class="block text-xs text-muted-foreground">
							Required for the site to function. Always on.
						</span>
					</span>
				</label>

				<label class="flex items-start gap-3 rounded-lg border border-white/5 bg-white/[0.02] p-3">
					<input
						type="checkbox"
						class="mt-0.5 size-4 accent-brand-400"
						checked={!!prefs.analytics}
						onchange={(e) => consent.save({ analytics: e.currentTarget.checked })}
					/>
					<span>
						<span class="text-sm font-medium">Analytics</span>
						<span class="block text-xs text-muted-foreground">
							Permission for optional usage measurement if analytics is introduced later.
						</span>
					</span>
				</label>

				<label class="flex items-start gap-3 rounded-lg border border-white/5 bg-white/[0.02] p-3">
					<input
						type="checkbox"
						class="mt-0.5 size-4 accent-brand-400"
						checked={!!prefs.preferences}
						onchange={(e) => consent.save({ preferences: e.currentTarget.checked })}
					/>
					<span>
						<span class="text-sm font-medium">Preferences</span>
						<span class="block text-xs text-muted-foreground">
							Permission to remember non-essential interface settings if introduced later.
						</span>
					</span>
				</label>

				<label class="flex items-start gap-3 rounded-lg border border-white/5 bg-white/[0.02] p-3">
					<input
						type="checkbox"
						class="mt-0.5 size-4 accent-brand-400"
						checked={!!prefs.marketing}
						onchange={(e) => consent.save({ marketing: e.currentTarget.checked })}
					/>
					<span>
						<span class="text-sm font-medium">Marketing</span>
						<span class="block text-xs text-muted-foreground">
							Permission for campaign measurement if marketing tools are introduced later.
						</span>
					</span>
				</label>
			</div>

			<div class="mt-6 flex items-center justify-end gap-2">
				<Button variant="ghost" size="sm" onclick={() => (showPrefs = false)}>Cancel</Button>
				<Button
					size="sm"
					onclick={() => (showPrefs = false)}
					class="bg-brand-400 text-white hover:bg-brand-500">Save selection</Button
				>
			</div>
		</div>
	</div>
{/if}

<style>
	.cookie-banner {
		position: fixed;
		right: 1rem;
		bottom: 1rem;
		z-index: 60;
		width: 17rem;
		border: 1px solid var(--border);
		border-radius: 1rem;
		background:
			color-mix(in srgb, var(--background) 92%, transparent),
			rgba(0, 0, 0, 0.4);
		backdrop-filter: blur(16px);
		box-shadow: 0 8px 32px rgba(0, 0, 0, 0.45);
		padding: 0.875rem;
	}

	@media (max-width: 640px) {
		.cookie-banner {
			left: 1rem;
			right: 1rem;
			width: auto;
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.cookie-banner {
			animation: none;
		}
	}
</style>
