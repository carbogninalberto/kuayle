<script lang="ts">
	import { Monitor, Sun, Moon } from 'lucide-svelte';
	import * as Select from '$lib/components/ui/select';
	import { Switch } from '$lib/components/ui/switch';
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { preferencesState } from '$lib/features/preferences/preferences.state.svelte';

	const fontSizeLabels: Record<string, string> = {
		small: 'Small (13px)',
		default: 'Default (14px)',
		large: 'Large (16px)',
	};

	const lightThemeLabels: Record<string, string> = {
		light: 'Light',
		'rose-light': 'Rose Light',
		'blue-light': 'Blue Light',
	};

	const darkThemeLabels: Record<string, string> = {
		dark: 'Dark',
		'amethyst-dark': 'Amethyst Dark',
		'emerald-dark': 'Emerald Dark',
	};
</script>

<div class="h-full">
	<div class="flex h-[49px] items-center border-b border-[var(--app-border)] px-6">
		<h1 class="text-sm font-medium text-[var(--color-text-primary)]">Preferences</h1>
	</div>
	<div class="max-w-xl p-6 space-y-6">
		<div>
			<h2 class="text-sm font-medium text-[var(--color-text-primary)]">Interface and theme</h2>
			<p class="mt-1 text-xs text-[var(--color-text-tertiary)]">
				Customize the appearance and behavior of the application.
			</p>
		</div>

		<!-- Font size -->
		<div class="flex items-center justify-between">
			<div>
				<p class="text-sm text-[var(--color-text-primary)]">Font size</p>
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
					<Select.Item value="small">Small (13px)</Select.Item>
					<Select.Item value="default">Default (14px)</Select.Item>
					<Select.Item value="large">Large (16px)</Select.Item>
				</Select.Content>
			</Select.Root>
		</div>

		<!-- Pointer cursors -->
		<div class="flex items-center justify-between">
			<div>
				<p class="text-sm text-[var(--color-text-primary)]">Use pointer cursors</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">
					Display a pointer cursor on interactive elements.
				</p>
			</div>
			<Switch
				size="sm"
				checked={preferencesState.pointerCursors}
				onCheckedChange={(v) => preferencesState.setPointerCursors(v)}
			/>
		</div>

		<!-- Interface theme -->
		<div class="flex items-center justify-between">
			<div>
				<p class="text-sm text-[var(--color-text-primary)]">Interface theme</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">
					Select your preferred color mode.
				</p>
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

		<!-- Light theme variant -->
		<div class="flex items-center justify-between">
			<div>
				<p class="text-sm text-[var(--color-text-primary)]">Light theme</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">
					Theme variant used in light mode.
				</p>
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

		<!-- Dark theme variant -->
		<div class="flex items-center justify-between">
			<div>
				<p class="text-sm text-[var(--color-text-primary)]">Dark theme</p>
				<p class="text-xs text-[var(--color-text-tertiary)]">
					Theme variant used in dark mode.
				</p>
			</div>
			<Select.Root
				type="single"
				value={preferencesState.darkTheme}
				onValueChange={(v) => {
					if (v) preferencesState.setDarkTheme(v as 'dark' | 'amethyst-dark' | 'emerald-dark');
				}}
			>
				<Select.Trigger size="sm" class="w-[130px]">
					{darkThemeLabels[preferencesState.darkTheme]}
				</Select.Trigger>
				<Select.Content>
					<Select.Item value="dark">Dark</Select.Item>
					<Select.Item value="amethyst-dark">Amethyst Dark</Select.Item>
					<Select.Item value="emerald-dark">Emerald Dark</Select.Item>
				</Select.Content>
			</Select.Root>
		</div>
	</div>
</div>
