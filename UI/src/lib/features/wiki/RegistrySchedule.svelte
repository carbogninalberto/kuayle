<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import * as Dialog from '$lib/components/ui/dialog';
	import SelectField from '$lib/components/shared/SelectField.svelte';
	import { getWikiSettings, updateWikiSettings } from '$lib/api/wiki';
	import type { OverviewSchedule, WikiScope, WikiSettings } from '$lib/types/wiki';
	import { appToast } from '$lib/features/toast/toast';
	import { m, getLocale } from '$lib/features/i18n/messages';
	import { CalendarClock } from '@lucide/svelte';

	// When the registry writes its AI overview on its own. Only writers see
	// this; the API enforces wiki:write on the change.
	let { slug, scope }: { slug: string; scope: WikiScope } = $props();

	const defaults: OverviewSchedule = { enabled: false, frequency: 'daily', weekday: 1, time: '18:00', period_days: 1 };
	let open = $state(false);
	let settings = $state<WikiSettings | null>(null);
	let draft = $state<OverviewSchedule>({ ...defaults });
	let saving = $state(false);
	// SelectField binds strings; the schedule stores numbers.
	let weekday = $state('1');
	let periodDays = $state('1');
	let failed = $state(false);

	const weekdays = $derived(
		Array.from({ length: 7 }, (_, day) => ({
			value: String(day),
			// 4 January 2026 was a Sunday.
			label: new Date(2026, 0, 4 + day).toLocaleDateString(getLocale(), { weekday: 'long' })
		}))
	);

	async function load() {
		failed = false;
		try {
			settings = await getWikiSettings(slug, scope);
			draft = { ...defaults, ...(settings.overview_schedule ?? {}) };
			weekday = String(draft.weekday);
			periodDays = String(draft.period_days);
		} catch {
			failed = true;
		}
	}

	async function save() {
		if (saving) return;
		saving = true;
		try {
			settings = await updateWikiSettings(slug, scope, {
				overview_schedule: { ...draft, weekday: Number(weekday), period_days: Number(periodDays) }
			});
			appToast.success(m['registry.schedule.saved']());
			open = false;
		} catch (error) {
			appToast.apiError(error, m['registry.schedule.failed']());
		} finally {
			saving = false;
		}
	}

	function when(value: string | null | undefined): string {
		return value ? new Date(value).toLocaleString(getLocale(), { dateStyle: 'medium', timeStyle: 'short' }) : '—';
	}
</script>

<Dialog.Root
	bind:open
	onOpenChange={(value) => {
		if (value) void load();
	}}
>
	<Dialog.Trigger>
		{#snippet child({ props })}
			<Button {...props} variant="outline" size="sm" title={m['registry.schedule.title']()}>
				<CalendarClock size={14} />
				{m['registry.schedule.button']()}
			</Button>
		{/snippet}
	</Dialog.Trigger>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>{m['registry.schedule.title']()}</Dialog.Title>
			<Dialog.Description>{m['registry.schedule.desc']()}</Dialog.Description>
		</Dialog.Header>
		{#if failed}
			<p role="alert" class="text-sm text-[var(--color-error)]">{m['registry.load_failed']()}</p>
		{:else}
			<form
				class="grid gap-4"
				onsubmit={(event) => {
					event.preventDefault();
					void save();
				}}
			>
				<div class="flex items-center justify-between gap-3">
					<Label for="overview-schedule-enabled">{m['registry.schedule.enabled']()}</Label>
					<Switch id="overview-schedule-enabled" bind:checked={draft.enabled} />
				</div>
				<div class="grid grid-cols-2 gap-3">
					<div class="grid gap-1.5">
						<Label>{m['registry.schedule.frequency']()}</Label>
						<SelectField
							bind:value={draft.frequency}
							disabled={!draft.enabled}
							aria-label={m['registry.schedule.frequency']()}
							options={[
								{ value: 'daily', label: m['registry.schedule.daily']() },
								{ value: 'weekly', label: m['registry.schedule.weekly']() }
							]}
						/>
					</div>
					{#if draft.frequency === 'weekly'}
						<div class="grid gap-1.5">
							<Label>{m['registry.schedule.weekday']()}</Label>
							<SelectField
								bind:value={weekday}
								disabled={!draft.enabled}
								aria-label={m['registry.schedule.weekday']()}
								options={weekdays}
							/>
						</div>
					{/if}
					<div class="grid gap-1.5">
						<Label for="overview-schedule-time"
							>{m['registry.schedule.time']({ zone: settings?.overview_timezone ?? 'UTC' })}</Label
						>
						<Input id="overview-schedule-time" type="time" bind:value={draft.time} disabled={!draft.enabled} required />
					</div>
					<div class="grid gap-1.5">
						<Label>{m['registry.schedule.period']()}</Label>
						<SelectField
							bind:value={periodDays}
							disabled={!draft.enabled}
							aria-label={m['registry.schedule.period']()}
							options={[1, 7, 14, 30].map((days) => ({
								value: String(days),
								label:
									days === 1 ? m['registry.schedule.period_day']() : m['registry.schedule.period_days']({ count: days })
							}))}
						/>
					</div>
				</div>
				{#if settings}
					<dl class="grid gap-1 text-xs text-[var(--color-text-tertiary)]">
						<div class="flex gap-1">
							<dt>{m['registry.schedule.next']()}</dt>
							<dd>{when(settings.overview_next_run_at)}</dd>
						</div>
						<div class="flex gap-1">
							<dt>{m['registry.schedule.last']()}</dt>
							<dd>{when(settings.overview_last_run_at)}</dd>
						</div>
						{#if settings.overview_last_error}
							<div class="flex gap-1 text-[var(--color-error)]">
								<dt>{m['registry.schedule.last_error']()}</dt>
								<dd class="break-words">{settings.overview_last_error}</dd>
							</div>
						{/if}
					</dl>
				{/if}
				<Dialog.Footer>
					<Button type="button" variant="outline" onclick={() => (open = false)}>{m['common.cancel']()}</Button>
					<Button type="submit" disabled={saving || !settings}>{m['registry.schedule.save']()}</Button>
				</Dialog.Footer>
			</form>
		{/if}
	</Dialog.Content>
</Dialog.Root>
