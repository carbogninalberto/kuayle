<script lang="ts">
	import { Monitor, Sun, Moon } from 'lucide-svelte';
	import * as Select from '$lib/components/ui/select';
	import { Switch } from '$lib/components/ui/switch';
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { preferencesState } from '$lib/features/preferences/preferences.state.svelte';

	const fontSizeLabels: Record<string, string> = {
		small: 'Small',
		default: 'Default',
		large: 'Large',
	};

	const lightThemeLabels: Record<string, string> = {
		light: 'Light',
		'rose-light': 'Rose Light',
		'blue-light': 'Blue Light',
	};

	const darkThemeLabels: Record<string, string> = {
		dark: 'Dark',
		'dark-gray': 'Dark Gray',
		'amethyst-dark': 'Amethyst Dark',
		'emerald-dark': 'Emerald Dark',
		'cyber-77': 'Cyber 77',
		'blade-49': 'Blade 49',
		'pipboy': 'Pip-Boy',
	};
</script>

<div class="mx-auto max-w-2xl px-8 py-10">
	<h1 class="text-2xl font-semibold text-[var(--color-text-primary)]">Preferences</h1>

	<!-- Interface and theme -->
	<h2 class="mt-8 text-sm font-medium text-[var(--color-text-secondary)]">Interface and theme</h2>

	<div class="mt-3 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)]">
		<!-- Font size -->
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">Font size</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">Set the font size for the interface.</p>
			</div>
			<Select.Root
				type="single"
				value={preferencesState.fontSize}
				onValueChange={(v) => {
					if (v) preferencesState.setFontSize(v as 'small' | 'default' | 'large');
				}}
			>
				<Select.Trigger size="sm" class="w-[130px]">
					{fontSizeLabels[preferencesState.fontSize]}
				</Select.Trigger>
				<Select.Content>
					<Select.Item value="small">Small</Select.Item>
					<Select.Item value="default">Default</Select.Item>
					<Select.Item value="large">Large</Select.Item>
				</Select.Content>
			</Select.Root>
		</div>

		<div class="border-t border-[var(--app-border)]"></div>

		<!-- Pointer cursors -->
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">Use pointer cursors</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">Display a pointer cursor on interactive elements.</p>
			</div>
			<Switch
				size="sm"
				checked={preferencesState.pointerCursors}
				onCheckedChange={(v) => preferencesState.setPointerCursors(v)}
			/>
		</div>

		<div class="border-t border-[var(--app-border)]"></div>

		<!-- Interface theme -->
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">Interface theme</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">Select your preferred color mode.</p>
			</div>
			<ToggleGroup.Root
				type="single"
				variant="outline"
				size="sm"
				value={preferencesState.themeMode}
				onValueChange={(v) => {
					if (v) preferencesState.setThemeMode(v as 'system' | 'light' | 'dark');
				}}
			>
				<ToggleGroup.Item value="system" aria-label="System preference">
					<Monitor size={14} />
				</ToggleGroup.Item>
				<ToggleGroup.Item value="light" aria-label="Light mode">
					<Sun size={14} />
				</ToggleGroup.Item>
				<ToggleGroup.Item value="dark" aria-label="Dark mode">
					<Moon size={14} />
				</ToggleGroup.Item>
			</ToggleGroup.Root>
		</div>

		<div class="border-t border-[var(--app-border)]"></div>

		<!-- Light theme variant -->
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">Light theme</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">Theme variant used in light mode.</p>
			</div>
			<Select.Root
				type="single"
				value={preferencesState.lightTheme}
				onValueChange={(v) => {
					if (v) preferencesState.setLightTheme(v as 'light' | 'rose-light' | 'blue-light');
				}}
			>
				<Select.Trigger size="sm" class="w-[130px]">
					{lightThemeLabels[preferencesState.lightTheme]}
				</Select.Trigger>
				<Select.Content>
					<Select.Item value="light">Light</Select.Item>
					<Select.Item value="rose-light">Rose Light</Select.Item>
					<Select.Item value="blue-light">Blue Light</Select.Item>
				</Select.Content>
			</Select.Root>
		</div>

		<div class="border-t border-[var(--app-border)]"></div>

		<!-- Dark theme variant -->
		<div class="flex items-center justify-between px-5 py-4">
			<div>
				<p class="text-sm font-medium text-[var(--color-text-primary)]">Dark theme</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">Theme variant used in dark mode.</p>
			</div>
			<Select.Root
				type="single"
				value={preferencesState.darkTheme}
				onValueChange={(v) => {
					if (v) preferencesState.setDarkTheme(v as 'dark' | 'dark-gray' | 'amethyst-dark' | 'emerald-dark' | 'cyber-77' | 'blade-49' | 'pipboy');
				}}
			>
				<Select.Trigger size="sm" class="w-[130px]">
					{darkThemeLabels[preferencesState.darkTheme]}
				</Select.Trigger>
				<Select.Content>
					<Select.Item value="dark">Dark</Select.Item>
					<Select.Item value="dark-gray">Dark Gray</Select.Item>
					<Select.Item value="amethyst-dark">Amethyst Dark</Select.Item>
					<Select.Item value="emerald-dark">Emerald Dark</Select.Item>
					<Select.Item value="cyber-77">Cyber 77</Select.Item>
					<Select.Item value="blade-49">Blade 49</Select.Item>
					<Select.Item value="pipboy">Pip-Boy</Select.Item>
				</Select.Content>
			</Select.Root>
		</div>
	</div>
</div>
