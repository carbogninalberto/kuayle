<script lang="ts">
	import { Search } from '@lucide/svelte';
	import LoadingState from '$lib/components/shared/LoadingState.svelte';
	import PageTitle from '$lib/components/shared/PageTitle.svelte';
	import { Badge, type BadgeVariant } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import * as InputGroup from '$lib/components/ui/input-group';
	import {
		getInstanceConfig,
		type InstanceConfig,
		type InstanceConfigSource,
		type InstanceConfigVariable,
		type InstanceRestartIssue
	} from '$lib/api/instance-config';
	import { authState } from '$lib/features/auth/auth.state.svelte';
	import { m } from '$lib/features/i18n/messages';
	import InstanceConfigControl from '$lib/features/instance-config/InstanceConfigControl.svelte';
	import InstanceConfigRestartControl from '$lib/features/instance-config/InstanceConfigRestartControl.svelte';
	import InstanceConfigRestartPanel from '$lib/features/instance-config/InstanceConfigRestartPanel.svelte';
	import {
		draftValue,
		isReverting,
		issuesByName,
		runInProgress,
		withDraft,
		withRevert,
		withoutDraft,
		type Pending
	} from '$lib/features/instance-config/restart';
	import { getErrorMessage } from '$lib/features/toast/toast';

	// Settings -> Instance configuration (sysadmins only): every variable the
	// API reads, grouped and searchable. Secrets show only whether they are
	// set. Runtime variables are editable here and apply at once; restart-only
	// variables on the allowlist are drafted here and applied together with
	// "Apply and restart", after a dry run. Everything else is read-only.
	const admin = $derived(authState.user?.is_sysadmin === true);
	let config = $state<InstanceConfig>();
	let loading = $state(false);
	let loaded = false;
	let error = $state('');
	let query = $state('');
	let pending = $state<Pending>({});
	let issues = $state<InstanceRestartIssue[]>([]);
	const problems = $derived(issuesByName(issues));
	const restarting = $derived(runInProgress(config?.restart.run));

	const groupLabels: Record<string, () => string> = {
		server: () => m['settings.instance_config.group_server'](),
		data: () => m['settings.instance_config.group_data'](),
		security: () => m['settings.instance_config.group_security'](),
		sign_in: () => m['settings.instance_config.group_sign_in'](),
		workflows: () => m['settings.instance_config.group_workflows'](),
		storage: () => m['settings.instance_config.group_storage'](),
		email: () => m['settings.instance_config.group_email'](),
		push: () => m['settings.push.title'](),
		backups: () => m['settings.nav.backups'](),
		network: () => m['settings.instance_config.group_network'](),
		integrations: () => m['settings.instance_config.group_integrations'](),
		assistant: () => m['settings.nav.assistant'](),
		updates: () => m['settings.instance_config.group_updates'](),
		dev_machines: () => m['settings.nav.dev_machines']()
	};
	const groupLabel = (id: string) => groupLabels[id]?.() ?? id;

	const sources: Record<InstanceConfigSource, { label: () => string; variant: BadgeVariant }> = {
		override: { label: () => m['settings.instance_config.source_override'](), variant: 'default' },
		environment: { label: () => m['settings.instance_config.source_environment'](), variant: 'secondary' },
		default: { label: () => m['settings.instance_config.source_default'](), variant: 'outline' }
	};

	const needle = $derived(query.trim().toLowerCase());
	const visible = $derived(
		(config?.variables ?? []).filter(
			(variable) =>
				!needle ||
				variable.name.toLowerCase().includes(needle) ||
				variable.description.toLowerCase().includes(needle) ||
				groupLabel(variable.group).toLowerCase().includes(needle)
		)
	);
	const sections = $derived(
		(config?.groups ?? [])
			.map((id) => ({ id, variables: visible.filter((variable) => variable.group === id) }))
			.filter((section) => section.variables.length > 0)
	);

	$effect(() => {
		if (admin && !loaded) {
			loaded = true;
			void load();
		}
	});

	async function load() {
		loading = true;
		error = '';
		try {
			config = await getInstanceConfig();
		} catch (err) {
			error = getErrorMessage(err, m['settings.instance_config.failed']());
		} finally {
			loading = false;
		}
	}

	function setIssues(next: InstanceRestartIssue[]) {
		issues = next;
	}

	function discardAll() {
		pending = {};
		issues = [];
	}

	function edit(variable: InstanceConfigVariable, raw: string) {
		pending = withDraft(pending, variable, raw);
		issues = issues.filter((issue) => issue.name !== variable.name);
	}

	function replace(next: InstanceConfigVariable) {
		if (!config) return;
		config = { ...config, variables: config.variables.map((variable) => (variable.name === next.name ? next : variable)) };
	}

	/** The value as shown: a secret only as set or not set, never its value. */
	function shown(variable: InstanceConfigVariable): { text: string; plain: boolean } {
		if (variable.secret)
			return { text: variable.set ? m['settings.instance_config.set']() : m['settings.instance_config.not_set'](), plain: false };
		if (variable.withheld) return { text: m['settings.instance_config.withheld'](), plain: false };
		if (!variable.value) return { text: m['settings.instance_config.not_set'](), plain: false };
		return { text: variable.value, plain: true };
	}
</script>

<PageTitle parts={[m['settings.instance_config.title'](), m['settings.title']()]} />

<div class="mx-auto max-w-4xl space-y-6 px-5 py-10 sm:px-8">
	<header class="space-y-1">
		<h1 class="text-2xl font-semibold">{m['settings.instance_config.title']()}</h1>
		<p class="text-sm text-muted-foreground">{m['settings.instance_config.intro']()}</p>
		<p class="text-sm text-muted-foreground">{m['settings.instance_config.restart_intro']()}</p>
	</header>

	{#if !admin}
		<p>{m['settings.instance_config.denied']()}</p>
	{:else if loading && !config}
		<LoadingState />
	{:else if error && !config}
		<div class="space-y-3">
			<p role="alert" class="text-sm text-destructive">{error}</p>
			<Button variant="outline" onclick={load}>{m['settings.tokens.retry']()}</Button>
		</div>
	{:else if config}
		<div class="space-y-2">
			<InputGroup.Root class="max-w-md">
				<InputGroup.Addon><Search aria-hidden="true" /></InputGroup.Addon>
				<InputGroup.Input
					type="search"
					bind:value={query}
					placeholder={m['settings.instance_config.search']()}
					aria-label={m['settings.instance_config.search']()}
				/>
			</InputGroup.Root>
			<p class="text-xs text-muted-foreground" aria-live="polite">
				{m['settings.instance_config.count']({ shown: visible.length, total: config.variables.length })}
			</p>
		</div>

		<InstanceConfigRestartPanel
			{config}
			{pending}
			general={problems.general}
			ondiscard={discardAll}
			onissues={setIssues}
			onconfig={(next) => (config = next)}
		/>

		{#each sections as section (section.id)}
			<section class="rounded-lg border bg-card" aria-labelledby="instance-config-group-{section.id}">
				<h2 id="instance-config-group-{section.id}" class="border-b px-4 py-2.5 text-sm font-medium">
					{groupLabel(section.id)}
				</h2>
				<ul class="divide-y">
					{#each section.variables as variable (variable.name)}
						{@const value = shown(variable)}
						{@const source = sources[variable.source]}
						<li class="grid gap-1.5 px-4 py-3" data-testid="instance-config-{variable.name}">
							<div class="flex flex-wrap items-center gap-2">
								<code class="font-mono text-sm font-medium break-all">{variable.name}</code>
								<Badge variant={source.variant}>{source.label()}</Badge>
								{#if variable.secret}
									<Badge variant="outline">{m['settings.instance_config.secret']()}</Badge>
								{/if}
								{#if variable.restart_editable}
									<Badge variant="outline">{m['settings.instance_config.restart_badge']()}</Badge>
								{:else if !variable.tunable}
									<Badge variant="outline" class="text-muted-foreground">{m['settings.instance_config.readonly_badge']()}</Badge>
								{/if}
							</div>
							<p class="text-xs text-muted-foreground">{variable.description}</p>
							{#if variable.tunable}
								<InstanceConfigControl {variable} onchange={replace} />
								<p class="text-xs text-muted-foreground">{m['settings.instance_config.live']()}</p>
							{:else if variable.restart_editable}
								<InstanceConfigRestartControl
									{variable}
									value={draftValue(variable, pending)}
									changed={variable.name in pending}
									reverting={isReverting(pending, variable.name)}
									error={problems.byName[variable.name]}
									disabled={restarting}
									onedit={(raw) => edit(variable, raw)}
									onrevert={() => (pending = withRevert(pending, variable))}
									ondiscard={() => (pending = withoutDraft(pending, variable.name))}
								/>
								<p class="text-xs text-muted-foreground">
									{#if isReverting(pending, variable.name)}
										{m['settings.instance_config.restart_reverting']({ value: variable.base_value ?? '' })}
									{:else}
										{m['settings.instance_config.restart_after']()}
									{/if}
								</p>
							{:else}
								<div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
									<span class={value.plain ? 'font-mono text-sm break-all' : 'text-sm'}>{value.text}</span>
									<span class="text-xs text-muted-foreground">{m['settings.instance_config.needs_restart']()}</span>
								</div>
							{/if}
						</li>
					{/each}
				</ul>
			</section>
		{:else}
			<p class="text-sm text-muted-foreground">{m['settings.instance_config.no_match']()}</p>
		{/each}

		<p class="text-xs text-muted-foreground">{m['settings.instance_config.other_services']()}</p>
	{/if}
</div>
