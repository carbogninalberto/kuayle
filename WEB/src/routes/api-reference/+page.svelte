<script lang="ts">
	import Nav from '$lib/components/Nav.svelte';
	import Footer from '$lib/components/Footer.svelte';
	import Seo from '$lib/components/Seo.svelte';
	import Breadcrumbs from '$lib/components/Breadcrumbs.svelte';
	import CtaSection from '$lib/components/CtaSection.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import Download from '@lucide/svelte/icons/download';
	import { contentModifiedAt, url } from '$lib/config/site';
	import { breadcrumbsFrom, webPageLd } from '$lib/data/routes';
	import { schemaAnchor, type TypePart } from '$lib/data/api-reference';
	import { FALLBACK_VERSION } from '$lib/release.svelte';

	let { data } = $props();
	const reference = $derived(data.reference);

	const meta = {
		title: 'Kuayle API Reference — OpenAPI 3.1 for Issues, Wikis and Webhooks',
		description:
			'Reference for the Kuayle HTTP API: personal access token scopes, pagination, errors, rate limits, webhook signatures, every stable endpoint and its schema.',
		canonical: url('/api-reference'),
		modifiedAt: contentModifiedAt('/api-reference')
	};
	const crumbs = breadcrumbsFrom('api-reference', 'API reference');
	const jsonLd = webPageLd(meta.title, meta.description, meta.canonical, crumbs, meta.modifiedAt);

	const methodClass: Record<string, string> = {
		GET: 'border-sky-400/40 text-sky-200',
		POST: 'border-emerald-400/40 text-emerald-200',
		PUT: 'border-amber-400/40 text-amber-200',
		PATCH: 'border-amber-400/40 text-amber-200',
		DELETE: 'border-rose-400/40 text-rose-200'
	};
</script>

{#snippet typeParts(parts: TypePart[])}
	{#each parts as part}{#if part.ref}<a class="text-brand-300 underline-offset-4 hover:underline" href={`#${schemaAnchor(part.ref)}`}>{part.text}</a>{:else}{part.text}{/if}{/each}
{/snippet}

<Seo meta={{ ...meta, ogType: 'article', jsonLd }} />

<Nav />

<main id="main" tabindex="-1" class="outline-none mx-auto max-w-4xl px-6 pt-28 pb-20">
	<div class="mb-8">
		<Breadcrumbs breadcrumbs={crumbs} />
	</div>

	<article class="min-w-0 [overflow-wrap:anywhere]">
		<header>
			<h1 class="text-4xl font-bold tracking-tight sm:text-5xl">
				<span class="gradient-text">Kuayle API reference</span>
			</h1>
			<p class="mt-4 text-lg leading-relaxed text-muted-foreground">
				{reference.summary} Every Kuayle instance serves this API, and a personal access token is all a script,
				importer or integration needs to use it.
			</p>
			<p class="mt-4 text-sm text-muted-foreground">
				OpenAPI 3.1, Kuayle {FALLBACK_VERSION}, {reference.operationCount} stable operations. This page is generated from the
				document that the backend's tests check against its routes and data types.
			</p>
			<div class="mt-6 flex flex-wrap items-center gap-3">
				<Button href="/openapi.json" download="kuayle-openapi.json" variant="outline" size="lg" class="h-11 px-5">
					<Download />
					Download openapi.json
				</Button>
				<p class="text-sm text-muted-foreground">
					Your own instance serves the document for its release at <code class="font-mono text-brand-200">/api/openapi.json</code>.
				</p>
			</div>
		</header>

		<nav aria-label="On this page" class="mt-10 rounded-lg border border-white/5 bg-white/[0.02] p-5 text-sm">
			<h2 class="font-semibold">On this page</h2>
			<ul class="mt-3 grid gap-x-6 gap-y-1.5 sm:grid-cols-2">
				{#each reference.guide as section}
					<li><a class="text-brand-300 underline-offset-4 hover:underline" href={`#${section.id}`}>{section.title}</a></li>
				{/each}
				{#each reference.groups as group}
					<li>
						<a class="text-brand-300 underline-offset-4 hover:underline" href={`#${group.anchor}`}>{group.name} endpoints</a>
					</li>
				{/each}
				<li><a class="text-brand-300 underline-offset-4 hover:underline" href="#schemas">Schemas</a></li>
			</ul>
		</nav>

		<div class="mt-12 space-y-14">
			{#each reference.guide as section}
				<section id={section.id} class="scroll-mt-24">
					<h2 class="flex items-center gap-3 text-2xl font-semibold tracking-tight">
						<span class="h-px w-6 bg-brand-400/60"></span>
						{section.title}
					</h2>
					{#each section.paragraphs as paragraph}
						<p class="mt-3 leading-relaxed text-muted-foreground">{paragraph}</p>
					{/each}
					{#if section.list}
						<ul class="mt-4 list-disc space-y-1.5 pl-5 text-sm text-muted-foreground">
							{#each section.list as item}
								<li>{item}</li>
							{/each}
						</ul>
					{/if}
					{#if section.id === 'webhooks' && reference.webhook}
						<p class="mt-3 text-sm text-muted-foreground">
							Delivery body: {@render typeParts(reference.webhook.body)}. Headers:
							<code class="font-mono text-brand-200">{reference.webhook.signatureHeader}</code> and
							<code class="font-mono text-brand-200">{reference.webhook.eventHeader}</code>.
						</p>
					{/if}
					{#if section.code}
						<pre
							class="mt-4 overflow-x-auto rounded-lg border border-white/10 bg-black/40 p-4 font-mono text-xs leading-relaxed [overflow-wrap:normal]"><code
								>{section.code.text}</code
							></pre>
					{/if}
				</section>
			{/each}
		</div>

		<h2 class="mt-20 flex items-center gap-3 text-3xl font-bold tracking-tight">Endpoints</h2>
		<p class="mt-3 leading-relaxed text-muted-foreground">
			Paths are relative to your instance and already include <code class="font-mono text-brand-200">/api</code>. Each endpoint lists
			the token scope it needs, its parameters, and the schemas of its request and response.
		</p>

		<div class="mt-10 space-y-16">
			{#each reference.groups as group}
				<section id={group.anchor} class="scroll-mt-24">
					<h3 class="text-2xl font-semibold tracking-tight">{group.name}</h3>
					<p class="mt-2 text-muted-foreground">{group.description}</p>
					<div class="mt-6 space-y-5">
						{#each group.operations as operation}
							<section id={`op-${operation.id}`} class="scroll-mt-24 rounded-lg border border-white/5 bg-white/[0.02] p-4 sm:p-5">
								<h4 class="font-semibold">{operation.summary}</h4>
								<p class="mt-2 flex flex-wrap items-center gap-2 font-mono text-sm">
									<Badge variant="outline" class={methodClass[operation.method]}>{operation.method}</Badge>
									<code class="min-w-0 text-brand-100">{operation.path}</code>
								</p>
								{#if operation.description}
									<p class="mt-3 text-sm leading-relaxed text-muted-foreground">{operation.description}</p>
								{/if}
								<dl class="mt-4 space-y-2 text-sm">
									<div>
										<dt class="inline font-medium">Scope:</dt>
										<dd class="inline text-muted-foreground">
											{#each operation.scopes as scope, index}{index > 0 ? ' and ' : ''}<code class="font-mono text-brand-200">{scope}</code>{/each}
										</dd>
									</div>
									{#if operation.request}
										<div>
											<dt class="inline font-medium">Request body:</dt>
											<dd class="inline text-muted-foreground">{@render typeParts(operation.request)}</dd>
										</div>
									{/if}
									<div>
										<dt class="inline font-medium">Response {operation.status}:</dt>
										<dd class="inline text-muted-foreground">{@render typeParts(operation.response)}</dd>
									</div>
									<div>
										<dt class="inline font-medium">Errors:</dt>
										<dd class="inline text-muted-foreground">
											{operation.errors.join(', ')} (<a class="text-brand-300 underline-offset-4 hover:underline" href="#errors">error format</a>)
										</dd>
									</div>
								</dl>
								{#if operation.parameters.length > 0}
									<h5 class="mt-4 text-sm font-medium">Parameters</h5>
									<ul class="mt-2 space-y-1.5 text-sm text-muted-foreground">
										{#each operation.parameters as parameter}
											<li>
												<code class="font-mono text-brand-200">{parameter.name}</code>
												<span class="text-xs">({parameter.in}, {@render typeParts(parameter.type)}{parameter.required ? ', required' : ''})</span>
												{parameter.description}
											</li>
										{/each}
									</ul>
								{/if}
							</section>
						{/each}
					</div>
				</section>
			{/each}
		</div>

		<section id="schemas" class="scroll-mt-24">
			<h2 class="mt-20 text-3xl font-bold tracking-tight">Schemas</h2>
			<p class="mt-3 leading-relaxed text-muted-foreground">
				The JSON objects the endpoints accept and return. Required fields of a request must be sent; required fields of a response are
				always present. A field marked "or null" may hold null.
			</p>
			<div class="mt-8 space-y-8">
				{#each reference.schemas as schema}
					<section id={schemaAnchor(schema.name)} class="scroll-mt-24">
						<h3 class="font-mono text-lg font-semibold">{schema.name}</h3>
						<ul class="mt-3 space-y-1.5 border-l border-white/10 pl-4 text-sm text-muted-foreground">
							{#each schema.fields as field}
								<li>
									<code class="font-mono text-brand-200">{field.name}</code>
									<span>{@render typeParts(field.type)}</span>{#if field.required}<span class="text-xs">, required</span>{/if}{#if field.notes}<span class="text-xs">; {field.notes}</span>{/if}
								</li>
							{/each}
						</ul>
					</section>
				{/each}
			</div>
		</section>

		<CtaSection
			heading="Run the API on your own server"
			description="Deploy Kuayle with Docker Compose, create a personal access token and point your client at your instance."
		/>
	</article>
</main>

<Footer />
