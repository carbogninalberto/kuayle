<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Switch } from '$lib/components/ui/switch';
	import {
		resetInstanceConfigValue,
		setInstanceConfigValue,
		type InstanceConfigVariable
	} from '$lib/api/instance-config';
	import { m } from '$lib/features/i18n/messages';
	import { appToast } from '$lib/features/toast/toast';

	// The editor of a variable that can change without a restart: a switch for
	// a flag, a field for a duration, and a reset while a change made here is
	// in effect. The server validates every value.
	let { variable, onchange }: { variable: InstanceConfigVariable; onchange: (next: InstanceConfigVariable) => void } =
		$props();

	let busy = $state(false);
	let edited = $state<string | null>(null);
	const draft = $derived(edited ?? variable.value ?? '');
	const dirty = $derived(edited !== null && edited.trim() !== (variable.value ?? ''));
	const label = $derived(m['settings.instance_config.value_label']({ name: variable.name }));
	const rangeId = $derived(`instance-config-range-${variable.name}`);

	async function run(action: () => Promise<InstanceConfigVariable>, done: string) {
		if (busy) return;
		busy = true;
		try {
			const next = await action();
			edited = null;
			onchange(next);
			appToast.success(done);
		} catch (err) {
			appToast.apiError(err, m['settings.instance_config.save_failed']({ name: variable.name }));
		} finally {
			busy = false;
		}
	}

	function save(value: string) {
		return run(
			() => setInstanceConfigValue(variable.name, value),
			m['settings.instance_config.saved']({ name: variable.name })
		);
	}

	function reset() {
		return run(
			() => resetInstanceConfigValue(variable.name),
			m['settings.instance_config.reset_done']({ name: variable.name })
		);
	}
</script>

<div class="flex flex-wrap items-center gap-x-3 gap-y-2">
	{#if variable.kind === 'bool'}
		<Switch
			bind:checked={() => variable.value === 'true', (checked) => void save(String(checked))}
			disabled={busy}
			aria-label={label}
		/>
		<span class="font-mono text-sm">{variable.value}</span>
	{:else}
		<form
			class="flex items-center gap-2"
			onsubmit={(event) => {
				event.preventDefault();
				if (dirty) void save(draft);
			}}
		>
			<Input
				class="h-8 w-32 font-mono"
				value={draft}
				oninput={(event) => (edited = event.currentTarget.value)}
				disabled={busy}
				aria-label={label}
				aria-describedby={variable.min && variable.max ? rangeId : undefined}
				autocomplete="off"
				spellcheck={false}
			/>
			<Button type="submit" size="sm" disabled={busy || !dirty}>{m['common.save']()}</Button>
		</form>
		{#if variable.min && variable.max}
			<span id={rangeId} class="text-xs text-muted-foreground">
				{m['settings.instance_config.range']({ min: variable.min, max: variable.max })}
			</span>
		{/if}
	{/if}
	{#if variable.source === 'override'}
		<Button variant="outline" size="sm" disabled={busy} onclick={reset}>
			{m['settings.instance_config.reset_to']({ value: variable.base_value ?? '' })}
		</Button>
	{/if}
</div>
